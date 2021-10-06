package tap

import (
	"errors"
	"fmt"
	"net"

	"github.com/KusakabeSi/EtherGuardVPN/config"
)

type UdpSockTap struct {
	name    string
	mtu     int
	recv    *net.UDPConn
	send    *net.UDPAddr
	static  bool
	L2mode  L2MODE
	events  chan Event
}

// New creates and returns a new TUN interface for the application.
func CreateUDPSockTAP(iconfig config.InterfaceConf, NodeID config.Vertex) (tapdev Device, err error) {
	// Setup TUN Config

	tap := &UdpSockTap{
		name:    iconfig.Name,
		mtu:     1500,
		recv:    nil,
		send:    nil,
		static:  false,
		L2mode:  GetL2Mode(iconfig.L2HeaderMode),
		events:  make(chan Event, 1<<5),
	}

	if iconfig.RecvAddr == "" && iconfig.SendAddr == "" {
		return nil, errors.New("At least one of RecvAddr or SendAddr required.")
	}

	if iconfig.RecvAddr == "" {
		iconfig.RecvAddr = ":0"
	}
	listenAddr, err := net.ResolveUDPAddr("udp", iconfig.RecvAddr)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}
	listener, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		fmt.Println(err.Error())
		return nil, err
	}
	tap.recv = listener

	if iconfig.SendAddr != "" {
		sendAddr, err := net.ResolveUDPAddr("udp", iconfig.SendAddr)
		if err != nil {
			fmt.Println(err.Error())
			return nil, err
		}
		tap.send = sendAddr
		tap.static = true
	}

	tapdev = tap
	tapdev.Events() <- EventUp
	return
}

// SetMTU sets the Maximum Tansmission Unit Size for a
// Packet on the interface.

func (tap *UdpSockTap) Read(buf []byte, offset int) (int, error) {
	size, source, err := tap.recv.ReadFromUDP(buf[offset:])
	if tap.static == false {
		tap.send = source
	}
	return size, err
} // read a packet from the device (without any additional headers)
func (tap *UdpSockTap) Write(buf []byte, offset int) (size int, err error) {
	size, err = tap.recv.WriteToUDP(buf[offset:], tap.send)
	return
} // writes a packet to the device (without any additional headers)
func (tap *UdpSockTap) Flush() error {
	return nil
} // flush all previous writes to the device
func (tap *UdpSockTap) MTU() (int, error) {
	return tap.mtu, nil
} // returns the MTU of the device
func (tap *UdpSockTap) Name() (string, error) {
	return tap.name, nil
} // fetches and returns the current name
func (tap *UdpSockTap) Events() chan Event {
	return tap.events
} // returns a constant channel of events related to the device
func (tap *UdpSockTap) Close() error {
	tap.events <- EventDown
	tap.recv.Close()
	close(tap.events)
	return nil
} // stops the device and closes the event channel
