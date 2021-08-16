// +build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/KusakabeSi/EtherGuardVPN/conn"
	"github.com/KusakabeSi/EtherGuardVPN/device"
	"github.com/KusakabeSi/EtherGuardVPN/ipc"
	"github.com/KusakabeSi/EtherGuardVPN/path"
	"github.com/KusakabeSi/EtherGuardVPN/tap"
	yaml "gopkg.in/yaml.v2"
)

type edgeConfig struct {
	Interface    InterfaceConf
	NodeID       path.Vertex
	NodeName     string
	PrivKey      string
	ListenPort   int
	LogLevel     LoggerInfo
	SuperNode    SuperInfo
	NextHopTable path.NextHopTable
	Peers        []PeerInfo
}

type InterfaceConf struct {
	Itype         string
	IfaceID       int
	Name          string
	MacAddr       string
	MTU           int
	RecvAddr      string
	SendAddr      string
	HumanFriendly bool
}

type SuperInfo struct {
	Enable   bool
	PubKeyV4 string
	PubKeyV6 string
	RegURLV4 string
	RegURLV6 string
	APIUrl   string
}

type PeerInfo struct {
	NodeID   path.Vertex
	PubKey   string
	EndPoint string
}

func printExampleConf() {
	var config edgeConfig
	config.Peers = make([]PeerInfo, 3)
	var g path.IG
	g.Init(4)
	g.Edge(1, 2, 0.5)
	g.Edge(2, 1, 0.5)
	g.Edge(2, 3, 0.5)
	g.Edge(3, 2, 0.5)
	g.Edge(2, 4, 0.5)
	g.Edge(4, 2, 0.5)
	g.Edge(3, 4, 0.5)
	g.Edge(4, 3, 0.5)
	g.Edge(5, 3, 0.5)
	g.Edge(3, 5, 0.5)
	g.Edge(6, 4, 0.5)
	g.Edge(4, 6, 0.5)
	_, next := path.FloydWarshall(g)
	config.NextHopTable = next
	test, _ := yaml.Marshal(config)
	fmt.Print(string(test))
	return
}

func Edge(configPath string, useUAPI bool) (err error) {

	var config edgeConfig
	//printExampleConf()
	//return

	err = readYaml(configPath, &config)
	if err != nil {
		fmt.Printf("Error read config: %s :", configPath)
		fmt.Print(err)
		return err
	}

	interfaceName := config.NodeName

	var logLevel int
	switch config.LogLevel.LogLevel {
	case "verbose", "debug":
		logLevel = device.LogLevelVerbose
	case "error":
		logLevel = device.LogLevelError
	case "silent":
		logLevel = device.LogLevelSilent
	}

	logger := device.NewLogger(
		logLevel,
		fmt.Sprintf("(%s) ", interfaceName),
	)

	logger.Verbosef("Starting wireguard-go version %s", Version)

	if err != nil {
		logger.Errorf("UAPI listen error: %v", err)
		os.Exit(ExitSetupFailed)
		return
	}

	var thetap tap.Device
	// open TUN device (or use supplied fd)
	switch config.Interface.Itype {
	case "dummy":
		thetap, err = tap.CreateDummyTAP()
	case "stdio":
		thetap, err = tap.CreateStdIOTAP(config.Interface.Name, config.Interface.HumanFriendly)
	case "udpsock":
		{
			lis, _ := net.ResolveUDPAddr("udp", config.Interface.RecvAddr)
			sen, _ := net.ResolveUDPAddr("udp", config.Interface.SendAddr)
			thetap, err = tap.CreateUDPSockTAP(config.Interface.Name, lis, sen, config.Interface.HumanFriendly)
		}
	}

	if err != nil {
		logger.Errorf("Failed to create TUN device: %v", err)
		os.Exit(ExitSetupFailed)
	}

	////////////////////////////////////////////////////
	// Config
	the_device := device.NewDevice(thetap, config.NodeID, conn.NewDefaultBind(), logger)
	the_device.LogTransit = config.LogLevel.LogTransit
	the_device.NhTable = config.NextHopTable
	defer the_device.Close()
	var sk [32]byte
	sk_slice, _ := base64.StdEncoding.DecodeString(config.PrivKey)
	copy(sk[:], sk_slice)
	the_device.SetPrivateKey(sk)
	the_device.IpcSet("fwmark=0\n")
	the_device.IpcSet("listen_port=" + strconv.Itoa(config.ListenPort) + "\n")
	the_device.IpcSet("replace_peers=true\n")
	for _, peerconf := range config.Peers {
		sk_slice, _ = base64.StdEncoding.DecodeString(peerconf.PubKey)
		copy(sk[:], sk_slice)
		the_device.NewPeer(sk, peerconf.NodeID)
		if peerconf.EndPoint != "" {
			peer := the_device.LookupPeer(sk)
			endpoint, err := the_device.Bind().ParseEndpoint(peerconf.EndPoint)
			if err != nil {
				logger.Errorf("Failed to set endpoint %v: %w", peerconf.EndPoint, err)
				return err
			}
			peer.SetEndpointFromPacket(endpoint)
		}
	}

	logger.Verbosef("Device started")

	errs := make(chan error)
	term := make(chan os.Signal, 1)

	if useUAPI {

		fileUAPI, err := func() (*os.File, error) {
			uapiFdStr := os.Getenv(ENV_WP_UAPI_FD)
			if uapiFdStr == "" {
				return ipc.UAPIOpen(interfaceName)
			}

			// use supplied fd

			fd, err := strconv.ParseUint(uapiFdStr, 10, 32)
			if err != nil {
				return nil, err
			}
			return os.NewFile(uintptr(fd), ""), nil
		}()
		uapi, err := ipc.UAPIListen(interfaceName, fileUAPI)
		if err != nil {
			logger.Errorf("Failed to listen on uapi socket: %v", err)
			os.Exit(ExitSetupFailed)
		}

		go func() {
			for {
				conn, err := uapi.Accept()
				if err != nil {
					errs <- err
					return
				}
				go the_device.IpcHandle(conn)
			}
		}()
		defer uapi.Close()
		logger.Verbosef("UAPI listener started")
	}

	// wait for program to terminate

	signal.Notify(term, syscall.SIGTERM)
	signal.Notify(term, os.Interrupt)

	select {
	case <-term:
	case <-errs:
	case <-the_device.Wait():
	}

	logger.Verbosef("Shutting down")
	return
}
