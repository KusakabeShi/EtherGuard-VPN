package tap

import (
	"fmt"
	"net"

	"github.com/KusakabeSi/EtherGuardVPN/config"
)

type UdpSockTap struct {
	name    string
	mtu     int
	recv    *net.UDPConn
	send    *net.UDPAddr
	L2mode  L2MODE
	macaddr MacAddress
	events  chan Event
}

// New creates and returns a new TUN interface for the application.
func CreateUDPSockTAP(iconfig config.InterfaceConf, listenAddr *net.UDPAddr, sendAddr *net.UDPAddr) (tapdev Device, err error) {
	// Setup TUN Config

	listener, err := net.ListenUDP("udp", listenAddr)
	if err != nil {
		fmt.Println(err.Error())
	}
	macaddr, err := GetMacAddr(iconfig.MacAddrPrefix, iconfig.VPPIfaceID)
	if err != nil {
		fmt.Println("ERROR: Failed parse mac address:", iconfig.MacAddrPrefix)
		return nil, err
	}
	tapdev = &UdpSockTap{
		name:    iconfig.Name,
		mtu:     1500,
		recv:    listener,
		send:    sendAddr,
		macaddr: macaddr,
		L2mode:  GetL2Mode(iconfig.L2HeaderMode),
		events:  make(chan Event, 1<<5),
	}
	tapdev.Events() <- EventUp
	return
}

// SetMTU sets the Maximum Tansmission Unit Size for a
// Packet on the interface.

func (tap *UdpSockTap) Read(buf []byte, offset int) (int, error) {
	switch tap.L2mode {
	case KeyboardDebug:
		size, _, err := tap.recv.ReadFromUDP(buf[offset+10:])
		packet := buf[offset:]
		src := Charform2mac(packet[11])
		dst := Charform2mac(packet[10])
		copy(packet[0:6], dst[:])
		copy(packet[6:12], src[:])
		return size + 10, err
	case BoardcastAndNodeID:
		size, _, err := tap.recv.ReadFromUDP(buf[offset+12:])
		packet := buf[offset:]
		src := tap.macaddr
		dst := Charform2mac('b')
		copy(packet[0:6], dst[:])
		copy(packet[6:12], src[:])
		return size + 12, err
	default:
		size, _, err := tap.recv.ReadFromUDP(buf[offset:])
		return size, err
	}

} // read a packet from the device (without any additional headers)
func (tap *UdpSockTap) Write(buf []byte, offset int) (size int, err error) {
	packet := buf[offset:]
	switch tap.L2mode {
	case KeyboardDebug:
		src := Mac2charForm(packet[6:12])
		dst := Mac2charForm(packet[0:6])
		packet[10] = dst
		packet[11] = src
		size, err = tap.recv.WriteToUDP(packet[10:], tap.send)
		return
	case BoardcastAndNodeID:
		size, err = tap.recv.WriteToUDP(packet[12:], tap.send)
		return
	default:
		size, err = tap.recv.WriteToUDP(packet, tap.send)
		return
	}
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
