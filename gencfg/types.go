/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package gencfg

import (
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
)

type SMCfg struct {
	ConfigOutputDir     string `yaml:"Config output dir"`
	SuperConfigTemplate string `yaml:"ConfigTemplate for super node"`
	EdgeConfigTemplate  string `yaml:"ConfigTemplate for edge node"`
	NetworkName         string `yaml:"Network name"`
	Supernode           struct {
		ListenPort       int    `yaml:"Listen port"`
		EdgeAPI_Prefix   string `yaml:"EdgeAPI prefix"`
		EndpointV4       string `yaml:"Endpoint(IPv4)(optional)"`
		EndpointV6       string `yaml:"Endpoint(IPv6)(optional)"`
		Endpoint_EdgeAPI string `yaml:"Endpoint(EdgeAPI)"`
	} `yaml:"Super Node"`
	EdgeNode struct {
		NodeIDs     string `yaml:"Node IDs"`
		MacPrefix   string `yaml:"MacAddress prefix"`
		IPv4Range   string `yaml:"IPv4 range"`
		IPv6Range   string `yaml:"IPv6 range"`
		IPv6LLRange string `yaml:"IPv6 LL range"`
	} `yaml:"Edge Node"`
}

type NMCfg struct {
	ConfigOutputDir    string `yaml:"Config output dir"`
	EdgeConfigTemplate string `yaml:"ConfigTemplate for edge node"`
	NetworkName        string `yaml:"Network name"`
	EdgeNode           struct {
		MacPrefix   string `yaml:"MacAddress prefix"`
		IPv4Range   string `yaml:"IPv4 range"`
		IPv6Range   string `yaml:"IPv6 range"`
		IPv6LLRange string `yaml:"IPv6 LL range"`
	} `yaml:"Edge Node"`
	EdgeNodes      map[mtypes.Vertex]edge_raw_info `yaml:"Edge Nodes"`
	DistanceMatrix string                          `yaml:"Distance matrix for all nodes"`
}

type PMCfg struct {
	ConfigOutputDir    string `yaml:"Config output dir"`
	EdgeConfigTemplate string `yaml:"ConfigTemplate for edge node"`
	NetworkName        string `yaml:"Network name"`
	EdgeNode           struct {
		MacPrefix   string `yaml:"MacAddress prefix"`
		IPv4Range   string `yaml:"IPv4 range"`
		IPv6Range   string `yaml:"IPv6 range"`
		IPv6LLRange string `yaml:"IPv6 LL range"`
	} `yaml:"Edge Node"`
	EdgeNodes map[mtypes.Vertex]edge_raw_info `yaml:"Edge Nodes"`
}

type edge_raw_info struct {
	Endpoint            string  `yaml:"Endpoint(optional)"`
	PersistentKeepalive uint32  `yaml:"PersistentKeepalive"`
	AdditionalCost      float64 `yaml:"AdditionalCost"`
}

type edge_info struct {
	Endpoint            string
	ConnectedEdge       map[mtypes.Vertex]bool
	PrivKey             string
	PubKey              string
	PersistentKeepalive uint32
}
