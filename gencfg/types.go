/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package gencfg

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

type PMG struct {
	ConfigOutputDir    string `yaml:"Config output dir"`
	EdgeConfigTemplate string `yaml:"ConfigTemplate for edge node"`
	NetworkName        string `yaml:"Network name"`
	EdgeNodes          []struct {
		NodeID   string `yaml:"Node ID"`
		Endpoint string `yaml:"Endpoint(optional)"`
	} `yaml:"Edge Nodes"`
	DistanceMatrix string `yaml:"Distance matrix for all nodes"`
}
