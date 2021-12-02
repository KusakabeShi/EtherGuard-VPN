//go:build !windows
// +build !windows

package tap

import (
	"errors"
	"os"
	"strconv"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
)

type FdTap struct {
	name   string
	mtu    int
	fileRX *os.File
	fileTX *os.File
	events chan Event
}

// New creates and returns a new TUN interface for the application.
func CreateFdTAP(iconfig mtypes.InterfaceConf, NodeID mtypes.Vertex) (tapdev Device, err error) {
	// Setup TUN Config
	fdRXstr, has := os.LookupEnv("EG_FD_RX")
	if !has {
		return nil, errors.New("Need Environment Variable EG_FD_RX")
	}
	fdRX, err := strconv.Atoi(fdRXstr)
	if err != nil {
		return nil, err
	}

	fdTxstr, has := os.LookupEnv("EG_FD_TX")
	if !has {
		return nil, errors.New("Need Environment Variable EG_FD_TX")
	}
	fdTX, err := strconv.Atoi(fdTxstr)
	if err != nil {
		return nil, err
	}

	fileRX := os.NewFile(uintptr(fdRX), "pipeRX")
	fileTX := os.NewFile(uintptr(fdTX), "pipeTX")

	tapdev = &FdTap{
		name:   iconfig.Name,
		fileRX: fileRX,
		fileTX: fileTX,
		events: make(chan Event, 1<<5),
	}
	tapdev.Events() <- EventUp
	return
}

// SetMTU sets the Maximum Tansmission Unit Size for a
// Packet on the interface.

func (tap *FdTap) Read(buf []byte, offset int) (int, error) {
	size, err := tap.fileRX.Read(buf[offset:])
	return size, err
} // read a packet from the device (without any additional headers)
func (tap *FdTap) Write(buf []byte, offset int) (size int, err error) {
	packet := buf[offset:]
	size, err = tap.fileTX.Write(packet)
	if err != nil {
		return 0, err
	}
	//err = syscall.Fsync(int(tap.fileTX.Fd()))
	//err = tap.fileTX.Sync()
	return
} // writes a packet to the device (without any additional headers)
func (tap *FdTap) Flush() error {
	return nil
} // flush all previous writes to the device
func (tap *FdTap) MTU() (int, error) {
	return tap.mtu, nil
} // returns the MTU of the device
func (tap *FdTap) Name() (string, error) {
	return tap.name, nil
} // fetches and returns the current name
func (tap *FdTap) Events() chan Event {
	return tap.events
} // returns a constant channel of events related to the device
func (tap *FdTap) Close() error {
	tap.events <- EventDown
	tap.fileRX.Close()
	tap.fileTX.Close()
	close(tap.events)
	return nil
} // stops the device and closes the event channel
