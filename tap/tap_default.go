//go:build !linux && !windows
// +build !linux,!windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package tap

import (
	"errors"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
)

func CreateTAP(iconfig mtypes.InterfaceConf, NodeID mtypes.Vertex) (Device, error) {
	return nil, errors.New("TAP interfaces are not supported on this platform")
}
