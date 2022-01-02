//go:build !windows
// +build !windows

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
	"net"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/rwcancel"
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

func ioctlRequest(cmd uintptr, arg uintptr) (err error) {
	fd, err := unix.Socket(
		unix.AF_INET,
		unix.SOCK_DGRAM,
		0,
	)
	if err != nil {
		return err
	}
	defer unix.Close(fd)

	_, _, err = unix.Syscall(
		unix.SYS_IOCTL,
		uintptr(fd),
		cmd,
		arg,
	)
	if err.(syscall.Errno) == syscall.Errno(0) {
		err = nil
	}
	return
}

func (tap *NativeTap) setMacAddr(mac MacAddress) (err error) {
	var ifr [ifReqSize]byte
	name, err := tap.Name()
	if err != nil {
		return err
	}
	/*
		ifreq = struct.pack('16sH6B8x', self.name, AF_UNIX, *macbytes)
		fcntl.ioctl(sockfd, SIOCSIFHWADDR, ifreq)
	*/
	copy(ifr[:unix.IFNAMSIZ], name)
	binary.LittleEndian.PutUint16(ifr[unix.IFNAMSIZ:unix.IFNAMSIZ+2], unix.AF_UNIX)
	copy(ifr[unix.IFNAMSIZ+2:unix.IFNAMSIZ+8], mac[:])

	err = ioctlRequest(unix.SIOCSIFHWADDR, uintptr(unsafe.Pointer(&ifr[0])))
	return
}

func (tap *NativeTap) addIPAddr(version string, ip net.IP, mask net.IPMask) (err error) {
	var ifr [ifReqSize]byte
	name, err := tap.Name()
	if err != nil {
		return err
	}
	/*
				bin_ip = socket.inet_aton(ip)
		    	ifreq = struct.pack('16sH2s4s8s', iface, socket.AF_INET, '\x00' * 2, bin_ip, '\x00' * 8)
		    	fcntl.ioctl(sock, SIOCSIFADDR, ifreq)
	*/
	if version == "4" {
		{
			iparr := [4]byte{}
			copy(iparr[:], ip[len(ip)-4:])
			sockaddr := unix.RawSockaddrInet4{
				Family: unix.AF_INET,
				Port:   0,
				Addr:   iparr,
			}
			sockaddr_arr := (*[unsafe.Sizeof(sockaddr)]byte)(unsafe.Pointer(&sockaddr))[:]
			copy(ifr[:unix.IFNAMSIZ], name)         // 0-16
			copy(ifr[unix.IFNAMSIZ:], sockaddr_arr) // 20-
			err = ioctlRequest(unix.SIOCSIFADDR, uintptr(unsafe.Pointer(&ifr[0])))
			if err != nil {
				return
			}
		}
		{
			iparr := [4]byte{}
			copy(iparr[:], mask[len(mask)-4:])
			sockaddr := unix.RawSockaddrInet4{
				Family: unix.AF_INET,
				Port:   0,
				Addr:   iparr,
			}
			sockaddr_arr := (*[unsafe.Sizeof(sockaddr)]byte)(unsafe.Pointer(&sockaddr))[:]
			copy(ifr[:unix.IFNAMSIZ], name)         // 0-16
			copy(ifr[unix.IFNAMSIZ:], sockaddr_arr) // 20-
			err = ioctlRequest(unix.SIOCSIFNETMASK, uintptr(unsafe.Pointer(&ifr[0])))
			if err != nil {
				return
			}
		}

	} else if version == "6" {
		o, _ := mask.Size()
		masklen := strconv.Itoa(o)
		e := exec.Command("ip", "addr", "add", ip.String()+"/"+masklen, "dev", name)
		ret, err := e.CombinedOutput()
		if err != nil {
			fmt.Printf("Failed to set ip %v to interface %v, please make sure `ip` tool installed\n", ip.String()+"/"+masklen, name)
			return fmt.Errorf(string(ret))
		}
	} else if version == "6ll" {
		_, llnet, _ := net.ParseCIDR("fe80::/64")

		if !llnet.Contains(ip) {
			return fmt.Errorf("%v is not a link-local address", ip)
		}
		e := exec.Command("ip", "addr", "add", ip.String()+"/64", "dev", name)
		ret, err := e.CombinedOutput()
		if err != nil {
			fmt.Printf("Failed to set ip %v to interface %v, please make sure `ip` tool installed\n", ip.String()+"/64", name)
			return fmt.Errorf(string(ret))
		}
	}
	return
}

func (tap *NativeTap) setUp() (err error) {
	var ifr [ifReqSize]byte
	name, err := tap.Name()
	if err != nil {
		return err
	}
	/*
		ifreq = struct.pack('16sH6B8x', self.name, AF_UNIX, *macbytes)
		fcntl.ioctl(sockfd, SIOCSIFHWADDR, ifreq)
	*/
	flags := uint16(unix.IFF_UP)
	copy(ifr[:unix.IFNAMSIZ], name)
	binary.LittleEndian.PutUint16(ifr[unix.IFNAMSIZ:unix.IFNAMSIZ+2], unix.AF_UNIX)
	binary.LittleEndian.PutUint16(ifr[unix.IFNAMSIZ+2:unix.IFNAMSIZ+4], flags)
	err = ioctlRequest(unix.SIOCSIFFLAGS, uintptr(unsafe.Pointer(&ifr[0])))
	return
}

func getIFIndex(name string) (ret int32, err error) {
	var ifr [ifReqSize]byte
	copy(ifr[:unix.IFNAMSIZ], name) // 0-16
	err = ioctlRequest(unix.SIOCGIFINDEX, uintptr(unsafe.Pointer(&ifr[0])))
	return *(*int32)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])), err
}

func (tap *NativeTap) setMTU(n uint16) (err error) {
	name, err := tap.Name()
	if err != nil {
		return err
	}
	// do ioctl call
	var ifr [ifReqSize]byte
	copy(ifr[:], name)
	*(*uint32)(unsafe.Pointer(&ifr[unix.IFNAMSIZ])) = uint32(n)

	err = ioctlRequest(unix.SIOCSIFMTU, uintptr(unsafe.Pointer(&ifr[0])))

	return
}

func (tap *NativeTap) MTU() (int, error) {
	name, err := tap.Name()
	if err != nil {
		return 0, err
	}
	// do ioctl call
	var ifr [ifReqSize]byte
	copy(ifr[:], name)
	err = ioctlRequest(unix.SIOCGIFMTU, uintptr(unsafe.Pointer(&ifr[0])))

	return int(*(*int32)(unsafe.Pointer(&ifr[unix.IFNAMSIZ]))), err
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

func CreateTAP(iconfig mtypes.InterfaceConf, NodeID mtypes.Vertex) (Device, error) {
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

	return CreateTAPFromFile(fd, iconfig, NodeID)
}

func CreateTAPFromFile(file *os.File, iconfig mtypes.InterfaceConf, NodeID mtypes.Vertex) (Device, error) {
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
	defer func() {
		if err != nil {
			unix.Close(tap.netlinkSock)
		}
	}()
	tap.netlinkCancel, err = rwcancel.NewRWCancel(tap.netlinkSock)
	if err != nil {
		return nil, err
	}

	tap.hackListenerClosed.Lock()
	go tap.routineNetlinkListener()
	go tap.routineHackListener() // cross namespace

	err = tap.setMTU(iconfig.MTU)
	if err != nil {
		return nil, err
	}
	IfMacAddr, err := GetMacAddr(iconfig.MacAddrPrefix, uint32(NodeID))
	if err != nil {
		fmt.Println("ERROR: Failed parse mac address:", iconfig.MacAddrPrefix)
		return nil, err
	}
	err = tap.setMacAddr(IfMacAddr)
	if err != nil {
		return nil, err
	}
	tapname, err := tap.Name()
	if err != nil {
		return nil, err
	}

	err = tap.setUp()
	if err != nil {
		return nil, err
	}

	if iconfig.IPv6LLPrefix != "" {
		e := exec.Command("ip", "addr", "flush", "dev", tapname)
		_, err := e.CombinedOutput()
		if err != nil {
			fmt.Printf("Failed to flush ip from interface %v , please make sure `ip` tool installed\n", tapname)
			return nil, err
		}
		cidrstr := iconfig.IPv6LLPrefix
		ip, mask, err := GetIP(6, cidrstr, uint32(NodeID))
		if err != nil {
			return nil, err
		}
		err = tap.addIPAddr("6ll", ip, mask)
		if err != nil {
			return nil, err
		}
	}
	if iconfig.IPv6CIDR != "" {
		cidrstr := iconfig.IPv6CIDR
		ip, mask, err := GetIP(6, cidrstr, uint32(NodeID))
		if err != nil {
			return nil, err
		}
		err = tap.addIPAddr("6", ip, mask)
		if err != nil {
			return nil, err
		}
	}
	if iconfig.IPv4CIDR != "" {
		cidrstr := iconfig.IPv4CIDR
		ip, mask, err := GetIP(4, cidrstr, uint32(NodeID))
		if err != nil {
			return nil, err
		}
		err = tap.addIPAddr("4", ip, mask)
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
