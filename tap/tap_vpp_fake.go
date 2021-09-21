//+build novpp

package tap

import (
	"errors"

	"github.com/KusakabeSi/EtherGuardVPN/config"
)

const (
	VPP_SUPPORT = "VPP support disabled"
)

type VppTap struct {
	stopRead chan struct{}
	events   chan Event
}

// New creates and returns a new TUN interface for the application.
func CreateVppTAP(iconfig config.InterfaceConf, NodeID config.Vertex, loglevel string) (tapdev Device, err error) {
	return nil, errors.New("VPP support disabled.")
}

func (tap *VppTap) Read([]byte, int) (int, error) {
	return 0, errors.New("Device stopped")
}
func (tap *VppTap) Write(packet []byte, size int) (int, error) {
	return size, nil
}
func (tap *VppTap) Flush() error {
	return nil
}
func (tap *VppTap) MTU() (int, error) {
	return 1500, nil
}
func (tap *VppTap) Name() (string, error) {
	return "Invalid device", nil
}
func (tap *VppTap) Events() chan Event {
	return tap.events
}
func (tap *VppTap) Close() error {
	return nil
}
