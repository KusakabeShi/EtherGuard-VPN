/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package gencfg

import (
	"fmt"
	"io/fs"
	"io/ioutil"
	"os"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
)

type SMCfg struct {
	ConfigOutputDir     string `yaml:"Config output dir"`
	ConfigOutputDirOW   bool   `yaml:"Enable generated config overwrite"`
	SuperConfigTemplate string `yaml:"ConfigTemplate for super node"`
	EdgeConfigTemplate  string `yaml:"ConfigTemplate for edge node"`
	NetworkName         string `yaml:"Network name"`
	NetworkIFNameID     bool   `yaml:"Add NodeID to the interface name"`
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
	ConfigOutputDirOW  bool   `yaml:"Enable generated config overwrite"`
	EdgeConfigTemplate string `yaml:"ConfigTemplate for edge node"`
	NetworkName        string `yaml:"Network name"`
	NetworkIFNameID    bool   `yaml:"Add NodeID to the interface name"`
	EdgeNode           struct {
		MacPrefix   string `yaml:"MacAddress prefix"`
		IPv4Range   string `yaml:"IPv4 range"`
		IPv6Range   string `yaml:"IPv6 range"`
		IPv6LLRange string `yaml:"IPv6 LL range"`
	} `yaml:"Edge Node"`
	EdgeNodes      map[mtypes.Vertex]edge_raw_info `yaml:"Edge Nodes"`
	DistanceMatrix string                          `yaml:"Distance matrix for all nodes"`
}

type edge_raw_info struct {
	Endpoint string `yaml:"Endpoint(optional)"`
}

type edge_info struct {
	Endpoint      string
	ConnectedEdge map[mtypes.Vertex]bool
	PrivKey       string
	PubKey        string
}

type bulkFileWriter struct {
	files     map[string]fileWriterfile
	committed bool
	ow        bool
}

type fileWriterfile struct {
	content []byte
	perm    fs.FileMode
}

func (f *bulkFileWriter) WriteFile(path string, content []byte, perm fs.FileMode) {
	f.files[path] = fileWriterfile{
		content: content,
		perm:    perm,
	}
}

func (f *bulkFileWriter) Commit() error {
	if f.committed {
		return fmt.Errorf("fileWriter has been commited")
	}
	f.committed = true
	for path, file := range f.files {
		if !f.ow {
			if _, err := os.Stat(path); os.IsNotExist(err) {
				// path/to/whatever does not exist
			} else {
				return fmt.Errorf("file %v exists, overwrite disabled", path)
			}
		}

		if err := ioutil.WriteFile(path, file.content, file.perm); err != nil {
			return err
		} else {
			fmt.Println(path)
		}
	}
	return nil
}
