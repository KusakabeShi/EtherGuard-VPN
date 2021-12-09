package tap

import (
	"fmt"
	"os"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
)

type L2MODE uint8

const (
	NoChange      L2MODE = iota
	KeyboardDebug        //Register to server
	BoardcastAndNodeID
)

func GetL2Mode(mode string) L2MODE {
	switch mode {
	case "nochg":
		return NoChange
	case "kbdbg":
		return KeyboardDebug
	case "noL2":
		return BoardcastAndNodeID
	}
	return NoChange
}

type StdIOTap struct {
	name    string
	mtu     int
	macaddr MacAddress
	L2mode  L2MODE
	events  chan Event
}

func Charform2mac(b byte) MacAddress {
	if b == 'b' {
		return MacAddress{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	}
	return MacAddress{0xaa, 0xbb, 0xcc, 0xdd, 0xee, b - 48}
}
func Mac2charForm(m []byte) byte {
	var M MacAddress
	copy(M[:], m)
	if IsNotUnicast(M) {
		return 'b'
	}
	return m[5] + 48
}

// New creates and returns a new TUN interface for the application.
func CreateStdIOTAP(iconfig mtypes.InterfaceConf, NodeID mtypes.Vertex) (tapdev Device, err error) {
	// Setup TUN Config

	if err != nil {
		fmt.Println(err.Error())
	}
	macaddr, err := GetMacAddr(iconfig.MacAddrPrefix, uint32(NodeID))
	if err != nil {
		fmt.Println("ERROR: Failed parse mac address:", iconfig.MacAddrPrefix)
		return nil, err
	}
	tapdev = &StdIOTap{
		name:    iconfig.Name,
		mtu:     1500,
		macaddr: macaddr,
		L2mode:  GetL2Mode(iconfig.L2HeaderMode),
		events:  make(chan Event, 1<<5),
	}
	tapdev.Events() <- EventUp
	return
}

// SetMTU sets the Maximum Tansmission Unit Size for a
// Packet on the interface.

func (tap *StdIOTap) Read(buf []byte, offset int) (int, error) {
	switch tap.L2mode {
	case KeyboardDebug:
		size, err := os.Stdin.Read(buf[offset+10:])
		packet := buf[offset:]
		src := Charform2mac(packet[11])
		dst := Charform2mac(packet[10])
		copy(packet[0:6], dst[:])
		copy(packet[6:12], src[:])
		return size - 2 + 12, err
	case BoardcastAndNodeID:
		size, err := os.Stdin.Read(buf[offset+12:])
		packet := buf[offset:]
		src := tap.macaddr
		dst := Charform2mac('b')
		copy(packet[0:6], dst[:])
		copy(packet[6:12], src[:])
		return size + 12, err
	default:
		size, err := os.Stdin.Read(buf[offset:])
		return size, err
	}
} // read a packet from the device (without any additional headers)
func (tap *StdIOTap) Write(buf []byte, offset int) (size int, err error) {
	packet := make([]byte, len(buf[offset:]))
	copy(packet, buf[offset:])
	switch tap.L2mode {
	case KeyboardDebug:
		src := Mac2charForm(packet[6:12])
		dst := Mac2charForm(packet[0:6])
		packet[10] = dst
		packet[11] = src
		size, err = os.Stdout.Write(packet[10:])
		return
	case BoardcastAndNodeID:
		size, err = os.Stdout.Write(packet[12:])
		return
	default:
		size, err = os.Stdout.Write(packet)
		return
	}
} // writes a packet to the device (without any additional headers)
func (tap *StdIOTap) Flush() error {
	return nil
} // flush all previous writes to the device
func (tap *StdIOTap) MTU() (int, error) {
	return tap.mtu, nil
} // returns the MTU of the device
func (tap *StdIOTap) Name() (string, error) {
	return tap.name, nil
} // fetches and returns the current name
func (tap *StdIOTap) Events() chan Event {
	return tap.events
} // returns a constant channel of events related to the device
func (tap *StdIOTap) Close() error {
	tap.events <- EventDown
	os.Stdin.Close()
	os.Stdin.WriteString("end\n")
	close(tap.events)
	panic("No solution for this issue: https://stackoverflow.com/questions/44270803/is-there-a-good-way-to-cancel-a-blocking-read , I'm panic!")
} // stops the device and closes the event channel
