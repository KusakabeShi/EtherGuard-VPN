// +build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"errors"
	"fmt"
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
	v1 := config.Vertex(1)
	v2 := config.Vertex(2)
	tconfig := config.EdgeConfig{
		Interface: config.InterfaceConf{
			Itype:         "stdio",
			Name:          "tap1",
			VPPIfaceID:    5,
			VPPBridgeID:   4242,
			MacAddrPrefix: "AA:BB:CC:DD",
			MTU:           1416,
			RecvAddr:      "127.0.0.1:4001",
			SendAddr:      "127.0.0.1:5001",
			L2HeaderMode:  "nochg",
		},
		NodeID:       1,
		NodeName:     "Node01",
		DefaultTTL:   200,
		L2FIBTimeout: 3600,
		PrivKey:      "6GyDagZKhbm5WNqMiRHhkf43RlbMJ34IieTlIuvfJ1M=",
		ListenPort:   3001,
		LogLevel: config.LoggerInfo{
			LogLevel:    "error",
			LogTransit:  true,
			LogControl:  true,
			LogNormal:   true,
			LogInternal: true,
			LogNTP:      false,
		},
		DynamicRoute: config.DynamicRouteInfo{
			SendPingInterval: 20,
			PeerAliveTimeout: 30,
			DupCheckTimeout:  40,
			ConnTimeOut:      30,
			ConnNextTry:      5,
			SaveNewPeers:     true,
			SuperNode: config.SuperInfo{
				UseSuperNode:         true,
				PSKey:                "iPM8FXfnHVzwjguZHRW9bLNY+h7+B1O2oTJtktptQkI=",
				ConnURLV4:            "127.0.0.1:3000",
				PubKeyV4:             "LJ8KKacUcIoACTGB/9Ed9w0osrJ3WWeelzpL2u4oUic=",
				ConnURLV6:            "[::1]:3000",
				PubKeyV6:             "HCfL6YJtpJEGHTlJ2LgVXIWKB/K95P57LHTJ42ZG8VI=",
				APIUrl:               "http://127.0.0.1:3000/api",
				SuperNodeInfoTimeout: 50,
			},
			P2P: config.P2Pinfo{
				UseP2P:           true,
				SendPeerInterval: 20,
				GraphRecalculateSetting: config.GraphRecalculateSetting{
					StaticMode:                false,
					JitterTolerance:           20,
					JitterToleranceMultiplier: 1.1,
					NodeReportTimeout:         40,
					TimeoutCheckInterval:      5,
					RecalculateCoolDown:       5,
				},
			},
			NTPconfig: config.NTPinfo{
				UseNTP:           true,
				MaxServerUse:     5,
				SyncTimeInterval: 3600,
				NTPTimeout:       3,
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
		NextHopTable: config.NextHopTable{
			config.Vertex(1): {
				config.Vertex(2): &v2,
			},
			config.Vertex(2): {
				config.Vertex(1): &v1,
			},
		},
		ResetConnInterval: 86400,
		Peers: []config.PeerInfo{
			{
				NodeID:   2,
				PubKey:   "dHeWQtlTPQGy87WdbUARS4CtwVaR2y7IQ1qcX4GKSXk=",
				PSKey:    "juJMQaGAaeSy8aDsXSKNsPZv/nFiPj4h/1G70tGYygs=",
				EndPoint: "127.0.0.1:3002",
				Static:   true,
			},
		},
	}
	g := path.NewGraph(3, false, tconfig.DynamicRoute.P2P.GraphRecalculateSetting, tconfig.DynamicRoute.NTPconfig, tconfig.LogLevel)

	g.UpdateLatency(1, 2, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(2, 1, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(2, 3, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(3, 2, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(2, 4, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(4, 2, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(3, 4, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(4, 3, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(5, 3, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(3, 5, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(6, 4, path.S2TD(0.5), 0, false, false)
	g.UpdateLatency(4, 6, path.S2TD(0.5), 0, false, false)
	_, next, _ := g.FloydWarshall(false)
	tconfig.NextHopTable = next
	toprint, _ := yaml.Marshal(tconfig)
	fmt.Print(string(toprint))
	return
}

func Edge(configPath string, useUAPI bool, printExample bool, bindmode string) (err error) {
	if printExample {
		printExampleEdgeConf()
		return nil
	}
	var econfig config.EdgeConfig
	//printExampleConf()
	//return

	err = readYaml(configPath, &econfig)
	if err != nil {
		fmt.Printf("Error read config: %v\t%v\n", configPath, err)
		return err
	}

	NodeName := econfig.NodeName
	if len(NodeName) > 32 {
		return errors.New("Node name can't longer than 32 :" + NodeName)
	}
	var logLevel int
	switch econfig.LogLevel.LogLevel {
	case "verbose", "debug":
		logLevel = device.LogLevelVerbose
	case "error":
		logLevel = device.LogLevelError
	case "silent":
		logLevel = device.LogLevelSilent
	default:
		logLevel = device.LogLevelError
	}
	logger := device.NewLogger(
		logLevel,
		fmt.Sprintf("(%s) ", NodeName),
	)

	if err != nil {
		logger.Errorf("UAPI listen error: %v", err)
		os.Exit(ExitSetupFailed)
		return
	}

	var thetap tap.Device
	// open TUN device (or use supplied fd)
	switch econfig.Interface.Itype {
	case "dummy":
		thetap, err = tap.CreateDummyTAP()
	case "stdio":
		thetap, err = tap.CreateStdIOTAP(econfig.Interface, econfig.NodeID)
	case "udpsock":
		thetap, err = tap.CreateUDPSockTAP(econfig.Interface, econfig.NodeID)
	case "tcpsock":
		thetap, err = tap.CreateSockTAP(econfig.Interface, "tcp", econfig.NodeID, econfig.LogLevel)
	case "unixsock":
		thetap, err = tap.CreateSockTAP(econfig.Interface, "unix", econfig.NodeID, econfig.LogLevel)
	case "unixgramsock":
		thetap, err = tap.CreateSockTAP(econfig.Interface, "unixgram", econfig.NodeID, econfig.LogLevel)
	case "unixpacketsock":
		thetap, err = tap.CreateSockTAP(econfig.Interface, "unixpacket", econfig.NodeID, econfig.LogLevel)
	case "fd":
		thetap, err = tap.CreateFdTAP(econfig.Interface, econfig.NodeID)
	case "vpp":
		thetap, err = tap.CreateVppTAP(econfig.Interface, econfig.NodeID, econfig.LogLevel.LogLevel)
	case "tap":
		thetap, err = tap.CreateTAP(econfig.Interface, econfig.NodeID)
	default:
		return errors.New("Unknow interface type:" + econfig.Interface.Itype)
	}
	if err != nil {
		logger.Errorf("Failed to create TAP device: %v", err)
		os.Exit(ExitSetupFailed)
	}

	if econfig.DefaultTTL <= 0 {
		return errors.New("DefaultTTL must > 0")
	}

	////////////////////////////////////////////////////
	// Config
	if econfig.DynamicRoute.P2P.UseP2P == false && econfig.DynamicRoute.SuperNode.UseSuperNode == false {
		econfig.LogLevel.LogNTP = false // NTP in static mode is useless
	}
	graph := path.NewGraph(3, false, econfig.DynamicRoute.P2P.GraphRecalculateSetting, econfig.DynamicRoute.NTPconfig, econfig.LogLevel)
	graph.SetNHTable(econfig.NextHopTable, [32]byte{})

	the_device := device.NewDevice(thetap, econfig.NodeID, conn.NewDefaultBind(true, true, bindmode), logger, graph, false, configPath, &econfig, nil, nil, Version)
	defer the_device.Close()
	pk, err := device.Str2PriKey(econfig.PrivKey)
	if err != nil {
		fmt.Println("Error decode base64 ", err)
		return err
	}
	the_device.SetPrivateKey(pk)
	the_device.IpcSet("fwmark=0\n")
	the_device.IpcSet("listen_port=" + strconv.Itoa(econfig.ListenPort) + "\n")
	the_device.IpcSet("replace_peers=true\n")
	for _, peerconf := range econfig.Peers {
		pk, err := device.Str2PubKey(peerconf.PubKey)
		if err != nil {
			fmt.Println("Error decode base64 ", err)
			return err
		}
		the_device.NewPeer(pk, peerconf.NodeID, false)
		if peerconf.EndPoint != "" {
			peer := the_device.LookupPeer(pk)
			err = peer.SetEndpointFromConnURL(peerconf.EndPoint, 0, peerconf.Static)
			if err != nil {
				logger.Errorf("Failed to set endpoint %v: %w", peerconf.EndPoint, err)
				return err
			}
		}
	}

	if econfig.DynamicRoute.SuperNode.UseSuperNode {
		S4 := true
		S6 := true
		if econfig.DynamicRoute.SuperNode.ConnURLV4 != "" {
			pk, err := device.Str2PubKey(econfig.DynamicRoute.SuperNode.PubKeyV4)
			if err != nil {
				fmt.Println("Error decode base64 ", err)
				return err
			}
			psk, err := device.Str2PSKey(econfig.DynamicRoute.SuperNode.PSKey)
			if err != nil {
				fmt.Println("Error decode base64 ", err)
				return err
			}
			peer, err := the_device.NewPeer(pk, config.SuperNodeMessage, true)
			if err != nil {
				return err
			}
			peer.SetPSK(psk)
			err = peer.SetEndpointFromConnURL(econfig.DynamicRoute.SuperNode.ConnURLV4, 4, false)
			if err != nil {
				logger.Errorf("Failed to set endpoint for supernode v4 %v: %v", econfig.DynamicRoute.SuperNode.ConnURLV4, err)
				S4 = false
			}
		}
		if econfig.DynamicRoute.SuperNode.ConnURLV6 != "" {
			pk, err := device.Str2PubKey(econfig.DynamicRoute.SuperNode.PubKeyV6)
			if err != nil {
				fmt.Println("Error decode base64 ", err)
			}
			psk, err := device.Str2PSKey(econfig.DynamicRoute.SuperNode.PSKey)
			if err != nil {
				fmt.Println("Error decode base64 ", err)
				return err
			}
			peer, err := the_device.NewPeer(pk, config.SuperNodeMessage, true)
			if err != nil {
				return err
			}
			peer.SetPSK(psk)
			peer.StaticConn = false
			peer.ConnURL = econfig.DynamicRoute.SuperNode.ConnURLV6
			err = peer.SetEndpointFromConnURL(econfig.DynamicRoute.SuperNode.ConnURLV6, 6, false)
			if err != nil {
				logger.Errorf("Failed to set endpoint for supernode v6 %v: %v", econfig.DynamicRoute.SuperNode.ConnURLV6, err)
				S6 = false
			}
			if !(S4 || S6) {
				return errors.New("Failed to connect to supernode.")
			}
		}
		the_device.Event_Supernode_OK <- struct{}{}
	}

	logger.Verbosef("Device started")

	errs := make(chan error)
	term := make(chan os.Signal, 1)

	if useUAPI {
		startUAPI(NodeName, logger, the_device, errs)
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
