// +build ignore

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2019-2021 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"io"
	"log"
	"net"
	"net/http"

	"github.com/KusakabeSi/EtherGuardVPN/conn"
	"github.com/KusakabeSi/EtherGuardVPN/device"
	"github.com/KusakabeSi/EtherGuardVPN/tun/netstack"
)

func main() {
	tun, tnet, err := netstack.CreateNetTUN(
		[]net.IP{net.ParseIP("192.168.4.29")},
		[]net.IP{net.ParseIP("8.8.8.8"), net.ParseIP("8.8.4.4")},
		1420,
	)
	if err != nil {
		log.Panic(err)
	}
	dev := device.NewDevice(tun, conn.NewDefaultBind(), device.NewLogger(device.LogLevelVerbose, ""))
	dev.IpcSet(`private_key=a8dac1d8a70a751f0f699fb14ba1cff7b79cf4fbd8f09f44c6e6a90d0369604f
public_key=25123c5dcd3328ff645e4f2a3fce0d754400d3887a0cb7c56f0267e20fbf3c5b
endpoint=163.172.161.0:12912
allowed_ip=0.0.0.0/0
persistent_keepalive_interval=25
`)
	dev.Up()
	listener, err := tnet.ListenTCP(&net.TCPAddr{Port: 80})
	if err != nil {
		log.Panicln(err)
	}
	http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
		log.Printf("> %s - %s - %s", request.RemoteAddr, request.URL.String(), request.UserAgent())
		io.WriteString(writer, "Hello from userspace TCP!")
	})
	err = http.Serve(listener, nil)
	if err != nil {
		log.Panicln(err)
	}
}
