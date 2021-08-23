package tap

import (
	"fmt"
	"os"
)

type StdIOTap struct {
	name          string
	mtu           int
	HumanFriendly bool
	events        chan Event
}

func Charform2mac(b byte) MacAddress {
	if b == 'b' {
		return MacAddress{0xff, 0xff, 0xff, 0xff, 0xff, 0xff}
	}
	return MacAddress{0xff, 0xff, 0xff, 0xff, 0xff, b - 48}
}
func Mac2charForm(m []byte) byte {
	var M MacAddress
	copy(M[:], m)
	if IsBoardCast(M) {
		return 'b'
	}
	return m[5] + 48
}

// New creates and returns a new TUN interface for the application.
func CreateStdIOTAP(interfaceName string, HumanFriendly bool) (tapdev Device, err error) {
	// Setup TUN Config

	if err != nil {
		fmt.Println(err.Error())
	}
	tapdev = &StdIOTap{
		name:          interfaceName,
		mtu:           1500,
		HumanFriendly: HumanFriendly,
		events:        make(chan Event, 1<<5),
	}
	tapdev.Events() <- EventUp
	return
}

// SetMTU sets the Maximum Tansmission Unit Size for a
// Packet on the interface.

func (tap *StdIOTap) File() *os.File {
	var tapFile *os.File
	return tapFile
} // returns the file descriptor of the device
func (tap *StdIOTap) Read(buf []byte, offset int) (int, error) {
	if tap.HumanFriendly {
		size, err := os.Stdin.Read(buf[offset+10:])
		packet := buf[offset:]
		src := Charform2mac(packet[11])
		dst := Charform2mac(packet[10])
		copy(packet[0:6], dst[:])
		copy(packet[6:12], src[:])
		return size - 2 + 12, err
	} else {
		size, err := os.Stdin.Read(buf[offset:])
		return size, err
	}
} // read a packet from the device (without any additional headers)
func (tap *StdIOTap) Write(buf []byte, offset int) (size int, err error) {
	packet := buf[offset:]
	if tap.HumanFriendly {
		src := Mac2charForm(packet[6:12])
		dst := Mac2charForm(packet[0:6])
		packet[10] = dst
		packet[11] = src
		packet = packet[10:]
	}
	size, err = os.Stdout.Write(packet)
	return
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
	return nil
} // stops the device and closes the event channel
