/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/KusakabeSi/EtherGuardVPN/conn"
	"github.com/KusakabeSi/EtherGuardVPN/device"
	"github.com/KusakabeSi/EtherGuardVPN/ipc"

	"github.com/KusakabeSi/EtherGuardVPN/tun"
)

const (
	ExitSetupSuccess = 0
	ExitSetupFailed  = 1
)

func main() {
	if len(os.Args) != 2 {
		os.Exit(ExitSetupFailed)
	}
	interfaceName := os.Args[1]

	fmt.Fprintln(os.Stderr, "Warning: this is a test program for Windows, mainly used for debugging this Go package. For a real WireGuard for Windows client, the repo you want is <https://git.zx2c4.com/wireguard-windows/>, which includes this code as a module.")

	logger := device.NewLogger(
		device.LogLevelVerbose,
		fmt.Sprintf("(%s) ", interfaceName),
	)
	logger.Verbosef("Starting wireguard-go version %s", Version)

	tun, err := tun.CreateTUN(interfaceName, 0)
	if err == nil {
		realInterfaceName, err2 := tun.Name()
		if err2 == nil {
			interfaceName = realInterfaceName
		}
	} else {
		logger.Errorf("Failed to create TUN device: %v", err)
		os.Exit(ExitSetupFailed)
	}

	device := device.NewDevice(tun, conn.NewDefaultBind(), logger)
	err = device.Up()
	if err != nil {
		logger.Errorf("Failed to bring up device: %v", err)
		os.Exit(ExitSetupFailed)
	}
	logger.Verbosef("Device started")

	uapi, err := ipc.UAPIListen(interfaceName)
	if err != nil {
		logger.Errorf("Failed to listen on uapi socket: %v", err)
		os.Exit(ExitSetupFailed)
	}

	errs := make(chan error)
	term := make(chan os.Signal, 1)

	go func() {
		for {
			conn, err := uapi.Accept()
			if err != nil {
				errs <- err
				return
			}
			go device.IpcHandle(conn)
		}
	}()
	logger.Verbosef("UAPI listener started")

	// wait for program to terminate

	signal.Notify(term, os.Interrupt)
	signal.Notify(term, os.Kill)
	signal.Notify(term, syscall.SIGTERM)

	select {
	case <-term:
	case <-errs:
	case <-device.Wait():
	}

	// clean up

	uapi.Close()
	device.Close()

	logger.Verbosef("Shutting down")
}
