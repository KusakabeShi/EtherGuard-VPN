//go:build !linux && !windows
// +build !linux,!windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2019-2021 WireGuard LLC. All Rights Reserved.
 */

package conn

import "net/netip"

func NewDefaultBind(Af EnabledAf, bindmode string, fwmark uint32) Bind {
	listen_ip4 := Af.ListenIPv4
	listen_ip6 := Af.ListenIPv6
	ListenIP4, _ := netip.ParseAddr("0.0.0.0")
	if listen_ip4 != "" {
		if addr, err := netip.ParseAddr(listen_ip4); err == nil && addr.Is4() {
			ListenIP4 = addr
		}
	}
	ListenIP6, _ := netip.ParseAddr("::")
	if listen_ip6 != "" {
		if addr, err := netip.ParseAddr(listen_ip6); err == nil && addr.Is6() {
			ListenIP6 = addr
		}
	}
	return NewStdNetBindAf(Af.IPv4, Af.IPv6, ListenIP4.As4(), ListenIP6.As16(), fwmark)
}
