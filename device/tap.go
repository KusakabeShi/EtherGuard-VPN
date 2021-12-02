/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package device

import (
	"fmt"
	"sync/atomic"

	"github.com/KusakabeSi/EtherGuard-VPN/tap"
)

const DefaultMTU = 1420

func (device *Device) RoutineTUNEventReader() {
	device.log.Verbosef("Routine: event worker - started")

	for event := range device.tap.device.Events() {
		if event&tap.EventMTUUpdate != 0 {
			mtu, err := device.tap.device.MTU()
			if err != nil {
				device.log.Errorf("Failed to load updated MTU of device: %v", err)
				continue
			}
			if mtu < 0 {
				device.log.Errorf("MTU not updated to negative value: %v", mtu)
				continue
			}
			var tooLarge string
			if mtu > MaxContentSize {
				tooLarge = fmt.Sprintf(" (too large, capped at %v)", MaxContentSize)
				mtu = MaxContentSize
			}
			old := atomic.SwapInt32(&device.tap.mtu, int32(mtu))
			if int(old) != mtu {
				device.log.Verbosef("MTU updated: %v%s", mtu, tooLarge)
			}
		}

		if event&tap.EventUp != 0 {
			device.log.Verbosef("Interface up requested")
			device.Up()
		}

		if event&tap.EventDown != 0 {
			device.log.Verbosef("Interface down requested")
			device.Down()
		}
	}

	device.log.Verbosef("Routine: event worker - stopped")
}
