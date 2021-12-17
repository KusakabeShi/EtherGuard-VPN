/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package gencfg

import (
	"fmt"
	"io/fs"

	"github.com/KusakabeSi/EtherGuard-VPN/device"
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/path"
)

func GetExampleEdgeConf(templatePath string, getDemo bool) (mtypes.EdgeConfig, error) {
	econfig := mtypes.EdgeConfig{}
	var err error
	if templatePath != "" {
		err = mtypes.ReadYaml(templatePath, &econfig)
		if err == nil {
			return econfig, nil
		}
	}
	v1 := mtypes.Vertex(1)
	v2 := mtypes.Vertex(2)
	econfig = mtypes.EdgeConfig{
		Interface: mtypes.InterfaceConf{
			IType:         "tap",
			Name:          "tap1",
			VPPIFaceID:    1,
			VPPBridgeID:   4242,
			MacAddrPrefix: "AA:BB:CC:DD",
			MTU:           device.DefaultMTU,
			RecvAddr:      "127.0.0.1:4001",
			SendAddr:      "127.0.0.1:5001",
			L2HeaderMode:  "nochg",
		},
		NodeID:       1,
		NodeName:     "Node01",
		PostScript:   "",
		DefaultTTL:   200,
		L2FIBTimeout: 3600,
		PrivKey:      "6GyDagZKhbm5WNqMiRHhkf43RlbMJ34IieTlIuvfJ1M=",
		ListenPort:   0,
		AfPrefer:     4,
		LogLevel: mtypes.LoggerInfo{
			LogLevel:    "error",
			LogTransit:  false,
			LogControl:  true,
			LogNormal:   false,
			LogInternal: true,
			LogNTP:      true,
		},
		DynamicRoute: mtypes.DynamicRouteInfo{
			SendPingInterval:     16,
			PeerAliveTimeout:     70,
			DupCheckTimeout:      40,
			TimeoutCheckInterval: 20,
			ConnNextTry:          5,
			AdditionalCost:       10,
			DampingResistance:    0.95,
			SaveNewPeers:         true,
			SuperNode: mtypes.SuperInfo{
				UseSuperNode:         true,
				PSKey:                "iPM8FXfnHVzwjguZHRW9bLNY+h7+B1O2oTJtktptQkI=",
				EndpointV4:           "127.0.0.1:3000",
				PubKeyV4:             "LJ8KKacUcIoACTGB/9Ed9w0osrJ3WWeelzpL2u4oUic=",
				EndpointV6:           "[::1]:3000",
				PubKeyV6:             "HCfL6YJtpJEGHTlJ2LgVXIWKB/K95P57LHTJ42ZG8VI=",
				EndpointEdgeAPIUrl:   "http://127.0.0.1:3000/eg_api",
				SuperNodeInfoTimeout: 50,
				SkipLocalIP:          false,
				AdditionalLocalIP:    []string{"11.11.11.11:11111"},
			},
			P2P: mtypes.P2PInfo{
				UseP2P:           false,
				SendPeerInterval: 20,
				GraphRecalculateSetting: mtypes.GraphRecalculateSetting{
					StaticMode:                false,
					JitterTolerance:           50,
					JitterToleranceMultiplier: 1.1,
					TimeoutCheckInterval:      5,
					RecalculateCoolDown:       5,
					ManualLatency: mtypes.DistTable{
						mtypes.Vertex(1): {
							mtypes.Vertex(2): 2,
						},
						mtypes.Vertex(2): {
							mtypes.Vertex(1): 2,
						},
					},
				},
			},
			NTPConfig: mtypes.NTPInfo{
				UseNTP:           true,
				MaxServerUse:     8,
				SyncTimeInterval: 604800,
				NTPTimeout:       3,
				Servers: []string{
					"time.google.com",
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
					"time.windows.com",
					"pool.ntp.org",
					"0.pool.ntp.org",
					"1.pool.ntp.org",
					"2.pool.ntp.org",
					"3.pool.ntp.org",
				},
			},
		},
		NextHopTable: mtypes.NextHopTable{
			mtypes.Vertex(1): {
				mtypes.Vertex(2): v2,
			},
			mtypes.Vertex(2): {
				mtypes.Vertex(1): v1,
			},
		},
		ResetEndPointInterval: 600,
		Peers: []mtypes.PeerInfo{
			{
				NodeID:              2,
				PubKey:              "dHeWQtlTPQGy87WdbUARS4CtwVaR2y7IQ1qcX4GKSXk=",
				PSKey:               "juJMQaGAaeSy8aDsXSKNsPZv/nFiPj4h/1G70tGYygs=",
				EndPoint:            "127.0.0.1:3002",
				PersistentKeepalive: 30,
				Static:              true,
			},
		},
	}
	if getDemo {
		g, _ := path.NewGraph(3, false, mtypes.GraphRecalculateSetting{}, mtypes.NTPInfo{}, mtypes.LoggerInfo{})
		g.UpdateLatency(1, 2, 0.5, 99999, 0, false, false)
		g.UpdateLatency(2, 1, 0.5, 99999, 0, false, false)
		g.UpdateLatency(2, 3, 0.5, 99999, 0, false, false)
		g.UpdateLatency(3, 2, 0.5, 99999, 0, false, false)
		g.UpdateLatency(2, 4, 0.5, 99999, 0, false, false)
		g.UpdateLatency(4, 2, 0.5, 99999, 0, false, false)
		g.UpdateLatency(3, 4, 0.5, 99999, 0, false, false)
		g.UpdateLatency(4, 3, 0.5, 99999, 0, false, false)
		g.UpdateLatency(5, 3, 0.5, 99999, 0, false, false)
		g.UpdateLatency(3, 5, 0.5, 99999, 0, false, false)
		g.UpdateLatency(6, 4, 0.5, 99999, 0, false, false)
		g.UpdateLatency(4, 6, 0.5, 99999, 0, false, false)
		_, next, _ := g.FloydWarshall(false)
		econfig.NextHopTable = next

	} else {
		econfig.Peers = []mtypes.PeerInfo{}
		econfig.NextHopTable = make(mtypes.NextHopTable)
		econfig.DynamicRoute.P2P.GraphRecalculateSetting.ManualLatency = make(mtypes.DistTable)
		econfig.DynamicRoute.SuperNode.EndpointV4 = ""
		econfig.DynamicRoute.SuperNode.EndpointV6 = ""
		econfig.DynamicRoute.SuperNode.AdditionalLocalIP = make([]string, 0)
	}
	return econfig, &fs.PathError{Path: "", Err: fmt.Errorf("no path provided")}
}

func GetExampleSuperConf(templatePath string, getDemo bool) (mtypes.SuperConfig, error) {
	sconfig := mtypes.SuperConfig{}
	var err error
	if templatePath != "" {
		err = mtypes.ReadYaml(templatePath, &sconfig)
		if err == nil {
			return sconfig, nil
		}
	}

	v1 := mtypes.Vertex(1)
	v2 := mtypes.Vertex(2)

	random_passwd := mtypes.RandomStr(8, "passwd")

	sconfig = mtypes.SuperConfig{
		NodeName:             "NodeSuper",
		PostScript:           "",
		PrivKeyV4:            "mL5IW0GuqbjgDeOJuPHBU2iJzBPNKhaNEXbIGwwYWWk=",
		PrivKeyV6:            "+EdOKIoBp/EvIusHDsvXhV1RJYbyN3Qr8nxlz35wl3I=",
		ListenPort:           3000,
		ListenPort_EdgeAPI:   "3000",
		ListenPort_ManageAPI: "3000",
		API_Prefix:           "/eg_api",
		LogLevel: mtypes.LoggerInfo{
			LogLevel:    "normal",
			LogTransit:  false,
			LogControl:  true,
			LogNormal:   false,
			LogInternal: true,
			LogNTP:      true,
		},
		RePushConfigInterval:  30,
		PeerAliveTimeout:      70,
		DampingResistance:     0.9,
		HttpPostInterval:      50,
		SendPingInterval:      15,
		ResetEndPointInterval: 600,
		Passwords: mtypes.Passwords{
			ShowState:   random_passwd + "_showstate",
			AddPeer:     random_passwd + "_addpeer",
			DelPeer:     random_passwd + "_delpeer",
			UpdatePeer:  random_passwd + "_updatepeer",
			UpdateSuper: random_passwd + "_updatesuper",
		},
		GraphRecalculateSetting: mtypes.GraphRecalculateSetting{
			StaticMode: false,
			ManualLatency: mtypes.DistTable{
				mtypes.Vertex(1): {
					mtypes.Vertex(2): 1.14,
				},
				mtypes.Vertex(2): {
					mtypes.Vertex(1): 5.14,
				},
			},
			JitterTolerance:           30,
			JitterToleranceMultiplier: 1.01,
			TimeoutCheckInterval:      5,
			RecalculateCoolDown:       5,
		},
		NextHopTable: mtypes.NextHopTable{
			mtypes.Vertex(1): {
				mtypes.Vertex(2): v2,
			},
			mtypes.Vertex(2): {
				mtypes.Vertex(1): v1,
			},
		},
		EdgeTemplate:       "example_config/super_mode/n1.yaml",
		UsePSKForInterEdge: true,
		Peers: []mtypes.SuperPeerInfo{
			{
				NodeID:         1,
				Name:           "Node_01",
				PubKey:         "ZqzLVSbXzjppERslwbf2QziWruW3V/UIx9oqwU8Fn3I=",
				PSKey:          "iPM8FXfnHVzwjguZHRW9bLNY+h7+B1O2oTJtktptQkI=",
				AdditionalCost: 10,
			},
			{
				NodeID:         2,
				Name:           "Node_02",
				PubKey:         "dHeWQtlTPQGy87WdbUARS4CtwVaR2y7IQ1qcX4GKSXk=",
				PSKey:          "juJMQaGAaeSy8aDsXSKNsPZv/nFiPj4h/1G70tGYygs=",
				AdditionalCost: 10,
			},
		},
	}
	if !getDemo {
		sconfig.Peers = []mtypes.SuperPeerInfo{}
		sconfig.NextHopTable = make(mtypes.NextHopTable)
		sconfig.GraphRecalculateSetting.ManualLatency = make(mtypes.DistTable)
	}
	return sconfig, &fs.PathError{Path: "", Err: fmt.Errorf("no path provided")}
}
