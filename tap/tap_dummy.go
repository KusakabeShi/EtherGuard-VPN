package tap

import "errors"

type DummyTap struct {
	stopRead chan struct{}
	events   chan Event
}

// New creates and returns a new TUN interface for the application.
func CreateDummyTAP() (tapdev Device, err error) {
	// Setup TUN Config
	tapdev = &DummyTap{
		stopRead: make(chan struct{}, 1<<5),
		events:   make(chan Event, 1<<5),
	}
	tapdev.Events() <- EventUp
	return
}

// SetMTU sets the Maximum Tansmission Unit Size for a
// Packet on the interface.

func (tap *DummyTap) Read([]byte, int) (int, error) {
	_ = <-tap.stopRead
	return 0, errors.New("Device stopped")
} // read a packet from the device (without any additional headers)
func (tap *DummyTap) Write(packet []byte, size int) (int, error) {
	return size, nil
} // writes a packet to the device (without any additional headers)
func (tap *DummyTap) Flush() error {
	return nil
} // flush all previous writes to the device
func (tap *DummyTap) MTU() (int, error) {
	return 1500, nil
} // returns the MTU of the device
func (tap *DummyTap) Name() (string, error) {
	return "DummyDevice", nil
} // fetches and returns the current name
func (tap *DummyTap) Events() chan Event {
	return tap.events
} // returns a constant channel of events related to the device
func (tap *DummyTap) Close() error {
	tap.events <- EventDown
	tap.stopRead <- struct{}{}
	//close(tap.stopRead)
	//close(tap.events)
	return nil
} // stops the device and closes the event channel
