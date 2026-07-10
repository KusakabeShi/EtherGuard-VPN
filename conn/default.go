//go:build !linux && !windows
// +build !linux,!windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2019-2021 WireGuard LLC. All Rights Reserved.
 */

package conn

func NewDefaultBind(af EnabledAf, bindmode string, fwmark uint32) Bind {
	ipv4, ipv6 := listenAddresses(af)
	return NewStdNetBindAf(af.IPv4, af.IPv6, ipv4, ipv6, fwmark)
}
