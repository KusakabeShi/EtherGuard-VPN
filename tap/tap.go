/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package tap

import (
	"encoding/binary"
	"errors"
	"net"
	"strconv"
	"strings"
)

type Event int
type MacAddress [6]byte

func (mac *MacAddress) String() string {
	return net.HardwareAddr((*mac)[:]).String()
}

func GetDstMacAddr(packet []byte) (dstMacAddr MacAddress) {
	copy(dstMacAddr[:], packet[0:6])
	return
}

func GetSrcMacAddr(packet []byte) (srcMacAddr MacAddress) {
	copy(srcMacAddr[:], packet[6:12])
	return
}

func GetMacAddr(prefix string, uid uint32) (mac MacAddress, err error) {
	macprefix, _, err := prefixStr2prefix(prefix)
	if err != nil {
		return
	}
	idbuf := make([]byte, 4)
	binary.BigEndian.PutUint32(idbuf, uid)
	copy(mac[2:], idbuf)
	copy(mac[:], macprefix)
	if IsNotUnicast(mac) {
		err = errors.New("ERROR: MAC address can only set to unicast address")
		return
	}
	return
}

func prefixStr2prefix(prefix string) ([]uint8, uint32, error) {
	hexStrs := strings.Split(strings.ToLower(prefix), ":")
	retprefix := make([]uint8, len(hexStrs))
	maxID := uint32(1)<<((6-len(hexStrs))*8) - 1
	if len(hexStrs) < 2 || len(hexStrs) > 6 {
		return []uint8{}, 0, errors.New("Macaddr prefix length must between 2 and 6, " + prefix + " is " + strconv.Itoa(len(hexStrs)))
	}
	for index, hexstr := range hexStrs {
		value, err := strconv.ParseInt(hexstr, 16, 16)
		if err != nil {
			return []uint8{}, 0, err
		}
		retprefix[index] = uint8(value)
	}
	return retprefix, maxID, nil
}

func IsNotUnicast(mac_in MacAddress) bool {
	if mac_in[0]&1 == 0 { // Is unicast
		return false
	}
	return true
}

const (
	EventUp = 1 << iota
	EventDown
	EventMTUUpdate
)

type Device interface {
	Read([]byte, int) (int, error)  // read a packet from the device (without any additional headers)
	Write([]byte, int) (int, error) // writes a packet to the device (without any additional headers)
	Flush() error                   // flush all previous writes to the device
	MTU() (int, error)              // returns the MTU of the device
	Name() (string, error)          // fetches and returns the current name
	Events() chan Event             // returns a constant channel of events related to the device
	Close() error                   // stops the device and closes the event channel
}
