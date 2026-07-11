package tap

import (
	"errors"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
)

func CreateTAP(iconfig mtypes.InterfaceConf, NodeID mtypes.Vertex) (Device, error) {
	return nil, errors.New("TAP interfaces are not supported on Windows")
}

func CreateFdTAP(iconfig mtypes.InterfaceConf, NodeID mtypes.Vertex) (Device, error) {
	return nil, errors.New("file descriptor TAP interfaces are not supported on Windows")
}
