/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package tap

/* Implementation of the TUN device interface for linux
 */

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	"github.com/KusakabeSi/EtherGuardVPN/rwcancel"
)

const (
	cloneDevicePath = "/dev/net/tun"
	ifReqSize       = unix.IFNAMSIZ + 64
)

type NativeTap struct {
	tapFile                 *os.File
	index                   int32      // if index
	errors                  chan error // async error handling
	events                  chan Event // device related events
	nopi                    bool       // the device was passed IFF_NO_PI
	netlinkSock             int
	netlinkCancel           *rwcancel.RWCancel
	hackListenerClosed      sync.Mutex
	statusListenersShutdown chan struct{}

	closeOnce sync.Once

	nameOnce  sync.Once // guards calling initNameCache, which sets following fields
	nameCache string    // name of interface
	nameErr   error
}

func (tap *NativeTap) File() *os.File {
	return tap.tapFile
}

func (tap *NativeTap) routineHackListener() {
	defer tap.hackListenerClosed.Unlock()
	/* This is needed for the detection to work across network namespaces
	 * If you are reading this and know a better method, please get in touch.
	 */
	last := 0
	const (
		up   = 1
		down = 2
	)
	for {
		sysconn, err := tap.tapFile.SyscallConn()
		if err != nil {
			return
		}
		err2 := sysconn.Control(func(fd uintptr) {
			_, err = unix.Write(int(fd), nil)
		})
		if err2 != nil {
			return
		}
		switch err {
		case unix.EINVAL:
			if last != up {
				// If the tunnel is up, it reports that write() is
				// allowed but we provided invalid data.
				tap.events <- EventUp
				last = up
			}
		case unix.EIO:
			if last != down {
				// If the tunnel is down, it reports that no I/O
				// is possible, without checking our provided data.
				tap.events <- EventDown
				last = down
			}
		default:
			return
		}
		select {
		case <-time.After(time.Second):
			// nothing
		case <-tap.statusListenersShutdown:
			return
		}
	}
}

func createNetlinkSocket() (int, error) {
	sock, err := unix.Socket(unix.AF_NETLINK, unix.SOCK_RAW, unix.NETLINK_ROUTE)
	if err != nil {
		return -1, err
	}
	saddr := &unix.SockaddrNetlink{
		Family: unix.AF_NETLINK,
		Groups: unix.RTMGRP_LINK | unix.RTMGRP_IPV4_IFADDR | unix.RTMGRP_IPV6_IFADDR,
	}
	err = unix.Bind(sock, saddr)
	if err != nil {
		return -1, err
	}
	return sock, nil
}

func (tap *NativeTap) routineNetlinkListener() {
	defer func() {
		unix.Close(tap.netlinkSock)
		tap.hackListenerClosed.Lock()
		close(tap.events)
		tap.netlinkCancel.Close()
	}()

	for msg := make([]byte, 1<<16); ; {
		var err error
		var msgn int
		for {
			msgn, _, _, _, err = unix.Recvmsg(tap.netlinkSock, msg[:], nil, 0)
			if err == nil || !rwcancel.RetryAfterError(err) {
				break
			}
			if !tap.netlinkCancel.ReadyRead() {
				tap.errors <- fmt.Errorf("netlink socket closed: %w", err)
				return
			}
		}
		if err != nil {
			tap.errors <- fmt.Errorf("failed to receive netlink message: %w", err)
			return
		}

		select {
		case <-tap.statusListenersShutdown:
			return
		default:
		}

		wasEverUp := false
		for remain := msg[:msgn]; len(remain) >= unix.SizeofNlMsghdr; {

			hdr := *(*unix.NlMsghdr)(unsafe.Pointer(&remain[0]))

			if int(hdr.Len) > len(remain) {
				break
			}

			switch hdr.Type {
			case unix.NLMSG_DONE:
				remain = []byte{}

			case unix.RTM_NEWLINK:
				info := *(*unix.IfInfomsg)(unsafe.Pointer(&remain[unix.SizeofNlMsghdr]))
				remain = remain[hdr.Len:]

				if info.Index != tap.index {
					// not our interface
					continue
				}

				if info.Flags&unix.IFF_RUNNING != 0 {
					tap.events <- EventUp
					wasEverUp = true
				}

				if info.Flags&unix.IFF_RUNNING == 0 {
					// Don't emit EventDown before we've ever emitted EventUp.
					// This avoids a startup race with HackListener, which
					// might detect Up before we have finished reporting Down.
					if wasEverUp {
						tap.events <- EventDown
					}
				}

				tap.events <- EventMTUUpdate

			default:
				remain = remain[hdr.Len:]
			}
		}
	}
}

func (tap *NativeTap) setMacAddr(mac MacAddress) (err error) {
	fd, err := unix.Socket(
		unix.AF_INET,
		unix.SOCK_DGRAM,
		0,
	)
	if err != nil {
		return err
	}
	defer unix.Close(fd)
	var ifr [ifReqSize]byte
	name, err := tap.Name()
	if err != nil {
		return err
	}
	/*
		ifreq = struct.pack('16sH6B8x', self.name, AF_UNIX, *macbytes)
		fcntl.ioctl(sockfd, SIOCSIFHWADDR, ifreq)
	*/
	copy(ifr[:16], name)
	binary.BigEndian.PutUint16(ifr[16:18], unix.AF_UNIX)
	copy(ifr[18:24], mac[:])
	copy(ifr[24:32], make([]byte, 8))
	_, _, err = unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.SIOCSIFHWADDR),
		uintptr(unsafe.Pointer(&ifr[0])),
	)
	return
}

func getIFIndex(name string) (int32, error) {
	fd, err := unix.Socket(
		unix.AF_INET,
		unix.SOCK_DGRAM,
		0,
	)
	if err != nil {
		return 0, err
	}

	defer unix.Close(fd)

	var ifr [ifReqSize]byte
	copy(ifr[:], name)
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.SIOCGIFINDEX),
		uintptr(unsafe.Pointer(&ifr[0])),
	)

	if errno != 0 {
		return 0, errno
	}

	return *(*int32)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])), nil
}

func (tap *NativeTap) setMTU(n int) error {
	name, err := tap.Name()
	if err != nil {
		return err
	}

	// open datagram socket
	fd, err := unix.Socket(
		unix.AF_INET,
		unix.SOCK_DGRAM,
		0,
	)

	if err != nil {
		return err
	}

	defer unix.Close(fd)

	// do ioctl call
	var ifr [ifReqSize]byte
	copy(ifr[:], name)
	*(*uint32)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = uint32(n)
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.SIOCSIFMTU),
		uintptr(unsafe.Pointer(&ifr[0])),
	)

	if errno != 0 {
		return fmt.Errorf("failed to set MTU of TUN device: %w", errno)
	}

	return nil
}

func (tap *NativeTap) MTU() (int, error) {
	name, err := tap.Name()
	if err != nil {
		return 0, err
	}

	// open datagram socket
	fd, err := unix.Socket(
		unix.AF_INET,
		unix.SOCK_DGRAM,
		0,
	)

	if err != nil {
		return 0, err
	}

	defer unix.Close(fd)

	// do ioctl call

	var ifr [ifReqSize]byte
	copy(ifr[:], name)
	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		uintptr(unix.SIOCGIFMTU),
		uintptr(unsafe.Pointer(&ifr[0])),
	)
	if errno != 0 {
		return 0, fmt.Errorf("failed to get MTU of TUN device: %w", errno)
	}

	return int(*(*int32)(unsafe.Pointer(&ifr[unix.IFNAMSIZ]))), nil
}

func (tap *NativeTap) Name() (string, error) {
	tap.nameOnce.Do(tap.initNameCache)
	return tap.nameCache, tap.nameErr
}

func (tap *NativeTap) initNameCache() {
	tap.nameCache, tap.nameErr = tap.nameSlow()
}

func (tap *NativeTap) nameSlow() (string, error) {
	sysconn, err := tap.tapFile.SyscallConn()
	if err != nil {
		return "", err
	}
	var ifr [ifReqSize]byte
	var errno syscall.Errno
	err = sysconn.Control(func(fd uintptr) {
		_, _, errno = unix.Syscall(
			unix.SYS_IOCTL,
			fd,
			uintptr(unix.TUNGETIFF),
			uintptr(unsafe.Pointer(&ifr[0])),
		)
	})
	if err != nil {
		return "", fmt.Errorf("failed to get name of TUN device: %w", err)
	}
	if errno != 0 {
		return "", fmt.Errorf("failed to get name of TUN device: %w", errno)
	}
	name := ifr[:]
	if i := bytes.IndexByte(name, 0); i != -1 {
		name = name[:i]
	}
	return string(name), nil
}

func (tap *NativeTap) Write(buf []byte, offset int) (int, error) {
	buf = buf[offset:]
	n, err := tap.tapFile.Write(buf)
	if errors.Is(err, syscall.EBADFD) {
		err = os.ErrClosed
	}
	return n, err
}

func (tap *NativeTap) Flush() error {
	// TODO: can flushing be implemented by buffering and using sendmmsg?
	return nil
}

func (tap *NativeTap) Read(buf []byte, offset int) (n int, err error) {
	select {
	case err = <-tap.errors:
	default:
		n, err = tap.tapFile.Read(buf[offset:])
		if errors.Is(err, syscall.EBADFD) {
			err = os.ErrClosed
		}

	}
	return
}

func (tap *NativeTap) Events() chan Event {
	return tap.events
}

func (tap *NativeTap) Close() error {
	var err1, err2 error
	tap.closeOnce.Do(func() {
		if tap.statusListenersShutdown != nil {
			close(tap.statusListenersShutdown)
			if tap.netlinkCancel != nil {
				err1 = tap.netlinkCancel.Cancel()
			}
		} else if tap.events != nil {
			close(tap.events)
		}
		err2 = tap.tapFile.Close()
	})
	if err1 != nil {
		return err1
	}
	return err2
}

func CreateTAP(iconfig config.InterfaceConf) (Device, error) {
	nfd, err := unix.Open(cloneDevicePath, os.O_RDWR, 0)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("CreateTAP(%q) failed; %s does not exist", iconfig.Name, cloneDevicePath)
		}
		return nil, err
	}

	var ifr [ifReqSize]byte
	var flags uint16 = unix.IFF_TAP | unix.IFF_NO_PI // (disabled for TUN status hack)
	if err != nil {
		fmt.Println("ERROR: Failed parse mac address:", iconfig.MacAddrPrefix)
		return nil, err
	}
	nameBytes := []byte(iconfig.Name)
	if len(nameBytes) >= unix.IFNAMSIZ {
		return nil, fmt.Errorf("interface name too long: %w", unix.ENAMETOOLONG)
	}
	copy(ifr[:], nameBytes)
	*(*uint16)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = flags

	_, _, errno := unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(nfd),
		uintptr(unix.TUNSETIFF),
		uintptr(unsafe.Pointer(&ifr[0])),
	)
	if errno != 0 {
		return nil, errno
	}
	err = unix.SetNonblock(nfd, true)

	// Note that the above -- open,ioctl,nonblock -- must happen prior to handing it to netpoll as below this line.

	fd := os.NewFile(uintptr(nfd), cloneDevicePath)
	if err != nil {
		return nil, err
	}

	return CreateTAPFromFile(fd, iconfig)
}

func CreateTAPFromFile(file *os.File, iconfig config.InterfaceConf) (Device, error) {
	tap := &NativeTap{
		tapFile:                 file,
		events:                  make(chan Event, 5),
		errors:                  make(chan error, 5),
		statusListenersShutdown: make(chan struct{}),
		nopi:                    false,
	}

	name, err := tap.Name()
	if err != nil {
		return nil, err
	}

	// start event listener

	tap.index, err = getIFIndex(name)
	if err != nil {
		return nil, err
	}

	tap.netlinkSock, err = createNetlinkSocket()
	if err != nil {
		return nil, err
	}
	tap.netlinkCancel, err = rwcancel.NewRWCancel(tap.netlinkSock)
	if err != nil {
		unix.Close(tap.netlinkSock)
		return nil, err
	}

	tap.hackListenerClosed.Lock()
	go tap.routineNetlinkListener()
	go tap.routineHackListener() // cross namespace

	err = tap.setMTU(iconfig.MTU)
	if err != nil {
		unix.Close(tap.netlinkSock)
		return nil, err
	}
	IfMacAddr, err := GetMacAddr(iconfig.MacAddrPrefix, iconfig.VPPIfaceID)
	if err != nil {
		fmt.Println("ERROR: Failed parse mac address:", iconfig.MacAddrPrefix)
		return nil, err
	}
	err = tap.setMacAddr(IfMacAddr)
	if err != nil {
		unix.Close(tap.netlinkSock)
		return nil, err
	}
	return tap, nil
}

func CreateUnmonitoredTUNFromFD(fd int) (Device, string, error) {
	err := unix.SetNonblock(fd, true)
	if err != nil {
		return nil, "", err
	}
	file := os.NewFile(uintptr(fd), "/dev/tap")
	tap := &NativeTap{
		tapFile: file,
		events:  make(chan Event, 5),
		errors:  make(chan error, 5),
		nopi:    true,
	}
	name, err := tap.Name()
	if err != nil {
		return nil, "", err
	}
	return tap, name, nil
}
