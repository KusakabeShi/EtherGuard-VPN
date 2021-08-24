// +build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	"github.com/KusakabeSi/EtherGuardVPN/conn"
	"github.com/KusakabeSi/EtherGuardVPN/device"
	"github.com/KusakabeSi/EtherGuardVPN/path"
	"github.com/KusakabeSi/EtherGuardVPN/tap"
	yaml "gopkg.in/yaml.v2"
)

func printExampleEdgeConf() {
	tconfig := config.EdgeConfig{
		Interface: config.InterfaceConf{
			Itype:         "stdio",
			VPPIfaceID:    5,
			Name:          "tap1",
			MacAddrPrefix: "AA:BB:CC:DD:EE:FF",
			MTU:           1400,
			RecvAddr:      "127.0.0.1:4001",
			SendAddr:      "127.0.0.1:5001",
			L2HeaderMode:  "nochg",
		},
		NodeID:     1,
		NodeName:   "Node01",
		PrivKey:    "SM8pGjT0r8njy1/7ffN4wMwF7nnJ8UYSjGRWpCqo3ng=",
		ListenPort: 3001,
		LogLevel: config.LoggerInfo{
			LogLevel:   "normal",
			LogTransit: true,
			LogControl: true,
		},
		DynamicRoute: config.DynamicRouteInfo{
			SendPingInterval: 20,
			DupCheckTimeout:  40,
			ConnTimeOut:      30,
			SaveNewPeers:     true,
			SuperNode: config.SuperInfo{
				UseSuperNode:         true,
				ConnURLV4:            "127.0.0.1:3000",
				PubKeyV4:             "LJ8KKacUcIoACTGB/9Ed9w0osrJ3WWeelzpL2u4oUic=",
				ConnURLV6:            "[::1]:3000",
				PubKeyV6:             "HCfL6YJtpJEGHTlJ2LgVXIWKB/K95P57LHTJ42ZG8VI=",
				APIUrl:               "http://127.0.0.1:3000/api",
				SuperNodeInfoTimeout: 40,
			},
			P2P: config.P2Pinfo{
				UseP2P:           true,
				SendPeerInterval: 20,
				PeerAliveTimeout: 30,
				GraphRecalculateSetting: config.GraphRecalculateSetting{
					JitterTolerance:           20,
					JitterToleranceMultiplier: 1.1,
					NodeReportTimeout:         40,
					RecalculateCoolDown:       5,
				},
			},
			NTPconfig: config.NTPinfo{
				UseNTP:       true,
				MaxServerUse: 5,
				Servers: []string{"time.google.com",
					"time1.google.com",
					"time2.google.com",
					"time3.google.com",
					"time4.google.com",
					"time1.facebook.com",
					"time2.facebook.com",
					"time3.facebook.com",
					"time4.facebook.com",
					"time5.facebook.com",
					"time.cloudflare.com",
					"time.apple.com",
					"time.asia.apple.com",
					"time.euro.apple.com",
					"time.windows.com"},
			},
		},
		NextHopTable:      config.NextHopTable{},
		ResetConnInterval: 86400,
		Peers: []config.PeerInfo{
			{
				NodeID:   2,
				PubKey:   "NuYJ/3Ght+C4HovFq5Te/BrIazo6zwDJ8Bdu4rQCz0o=",
				EndPoint: "127.0.0.1:3002",
				Static:   true,
			},
		},
	}
	g := path.NewGraph(3, false, tconfig.DynamicRoute.P2P.GraphRecalculateSetting)

	g.UpdateLentancy(1, 2, path.S2TD(0.5), false)
	g.UpdateLentancy(2, 1, path.S2TD(0.5), false)
	g.UpdateLentancy(2, 3, path.S2TD(0.5), false)
	g.UpdateLentancy(3, 2, path.S2TD(0.5), false)
	g.UpdateLentancy(2, 4, path.S2TD(0.5), false)
	g.UpdateLentancy(4, 2, path.S2TD(0.5), false)
	g.UpdateLentancy(3, 4, path.S2TD(0.5), false)
	g.UpdateLentancy(4, 3, path.S2TD(0.5), false)
	g.UpdateLentancy(5, 3, path.S2TD(0.5), false)
	g.UpdateLentancy(3, 5, path.S2TD(0.5), false)
	g.UpdateLentancy(6, 4, path.S2TD(0.5), false)
	g.UpdateLentancy(4, 6, path.S2TD(0.5), false)
	_, next := path.FloydWarshall(g)
	tconfig.NextHopTable = next
	toprint, _ := yaml.Marshal(tconfig)
	fmt.Print(string(toprint))
	return
}

func Edge(configPath string, useUAPI bool, printExample bool) (err error) {
	if printExample {
		printExampleEdgeConf()
		return nil
	}
	var tconfig config.EdgeConfig
	//printExampleConf()
	//return

	err = readYaml(configPath, &tconfig)
	if err != nil {
		fmt.Printf("Error read config: %s :", configPath)
		fmt.Print(err)
		return err
	}

	interfaceName := tconfig.NodeName

	var logLevel int
	switch tconfig.LogLevel.LogLevel {
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

	if err != nil {
		logger.Errorf("UAPI listen error: %v", err)
		os.Exit(ExitSetupFailed)
		return
	}

	var thetap tap.Device
	// open TUN device (or use supplied fd)
	switch tconfig.Interface.Itype {
	case "dummy":
		thetap, err = tap.CreateDummyTAP()
	case "stdio":
		tconfig.Interface.VPPIfaceID = uint32(tconfig.NodeID)
		thetap, err = tap.CreateStdIOTAP(tconfig.Interface)
	case "udpsock":
		tconfig.Interface.VPPIfaceID = uint32(tconfig.NodeID)
		lis, _ := net.ResolveUDPAddr("udp", tconfig.Interface.RecvAddr)
		sen, _ := net.ResolveUDPAddr("udp", tconfig.Interface.SendAddr)
		thetap, err = tap.CreateUDPSockTAP(tconfig.Interface, lis, sen)
	case "vpp":
		thetap, err = tap.CreateVppTAP(tconfig.Interface, tconfig.LogLevel.LogLevel)
	case "tap":
		thetap, err = tap.CreateTAP(tconfig.Interface)
	default:
		return errors.New("Unknow interface type:" + tconfig.Interface.Itype)
	}
	if err != nil {
		logger.Errorf("Failed to create TAP device: %v", err)
		os.Exit(ExitSetupFailed)
	}

	////////////////////////////////////////////////////
	// Config
	graph := path.NewGraph(3, false, tconfig.DynamicRoute.P2P.GraphRecalculateSetting)
	graph.SetNHTable(tconfig.NextHopTable, [32]byte{})

	the_device := device.NewDevice(thetap, tconfig.NodeID, conn.NewDefaultBind(), logger, graph, false, configPath, &tconfig, nil, nil)
	defer the_device.Close()
	var sk [32]byte
	sk_slice, _ := base64.StdEncoding.DecodeString(tconfig.PrivKey)
	copy(sk[:], sk_slice)
	the_device.SetPrivateKey(sk)
	the_device.IpcSet("fwmark=0\n")
	the_device.IpcSet("listen_port=" + strconv.Itoa(tconfig.ListenPort) + "\n")
	the_device.IpcSet("replace_peers=true\n")
	for _, peerconf := range tconfig.Peers {
		sk_slice, _ = base64.StdEncoding.DecodeString(peerconf.PubKey)
		copy(sk[:], sk_slice)
		if peerconf.NodeID >= path.SuperNodeMessage {
			return errors.New(fmt.Sprintf("Invalid Node_id at peer %s\n", peerconf.PubKey))
		}
		the_device.NewPeer(sk, peerconf.NodeID)
		if peerconf.EndPoint != "" {
			peer := the_device.LookupPeer(sk)
			endpoint, err := the_device.Bind().ParseEndpoint(peerconf.EndPoint)
			if err != nil {
				logger.Errorf("Failed to set endpoint %v: %w", peerconf.EndPoint, err)
				return err
			}
			peer.StaticConn = peerconf.Static
			peer.ConnURL = peerconf.EndPoint
			peer.SetEndpointFromPacket(endpoint)
		}
	}

	if tconfig.DynamicRoute.SuperNode.UseSuperNode {
		if tconfig.DynamicRoute.SuperNode.ConnURLV4 != "" {
			sk_slice, _ = base64.StdEncoding.DecodeString(tconfig.DynamicRoute.SuperNode.PubKeyV4)
			copy(sk[:], sk_slice)
			endpoint, err := the_device.Bind().ParseEndpoint(tconfig.DynamicRoute.SuperNode.ConnURLV4)
			if err != nil {
				return err
			}
			peer, err := the_device.NewPeer(sk, path.SuperNodeMessage)
			if err != nil {
				return err
			}
			peer.StaticConn = false
			peer.ConnURL = tconfig.DynamicRoute.SuperNode.ConnURLV4
			peer.SetEndpointFromPacket(endpoint)
		}
		if tconfig.DynamicRoute.SuperNode.ConnURLV6 != "" {
			sk_slice, _ = base64.StdEncoding.DecodeString(tconfig.DynamicRoute.SuperNode.PubKeyV6)
			copy(sk[:], sk_slice)
			endpoint, err := the_device.Bind().ParseEndpoint(tconfig.DynamicRoute.SuperNode.ConnURLV6)
			if err != nil {
				return err
			}
			peer, err := the_device.NewPeer(sk, path.SuperNodeMessage)
			if err != nil {
				return err
			}
			peer.StaticConn = false
			peer.ConnURL = tconfig.DynamicRoute.SuperNode.ConnURLV6
			peer.SetEndpointFromPacket(endpoint)
		}
	}

	logger.Verbosef("Device started")

	errs := make(chan error)
	term := make(chan os.Signal, 1)

	if useUAPI {
		startUAPI(interfaceName, logger, the_device, errs)
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
