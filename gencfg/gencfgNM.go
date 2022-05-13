/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package gencfg

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KusakabeSi/EtherGuard-VPN/conn"
	"github.com/KusakabeSi/EtherGuard-VPN/device"
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/path"
	"github.com/KusakabeSi/EtherGuard-VPN/tap"
	yaml "gopkg.in/yaml.v2"
)

func printNMCinfig() {
	tconfig := NMCfg{}
	tconfig.EdgeNodes = make(map[mtypes.Vertex]edge_raw_info)
	tconfig.EdgeNodes[mtypes.Vertex(1)] = edge_raw_info{Endpoint: "1.example.com:3456"}

	tconfig.EdgeNodes[mtypes.Vertex(2)] = edge_raw_info{Endpoint: "2.example.com:4567"}
	tconfig.DistanceMatrix = `X 1 2
1 0 1
2 1 0`
	toprint, _ := yaml.Marshal(tconfig)
	fmt.Print(string(toprint))
}

func GenNMCfg(NMCinfigPath string, enableP2P bool, printExample bool) (err error) {
	NMCfg := NMCfg{}
	if printExample {
		printNMCinfig()
		return
	}
	err = mtypes.ReadYaml(NMCinfigPath, &NMCfg)
	if err != nil {
		return err
	}
	os.Chdir(filepath.Dir(NMCinfigPath))

	err = os.MkdirAll(NMCfg.ConfigOutputDir, 0o700)
	if err != nil {
		return err
	}

	var fileWriter bulkFileWriter
	fileWriter.files = make(map[string]fileWriterfile)
	fileWriter.ow = NMCfg.ConfigOutputDirOW

	if NMCfg.EdgeConfigTemplate != "" {
		var econfig mtypes.EdgeConfig
		err = mtypes.ReadYaml(NMCfg.EdgeConfigTemplate, &econfig)
		if err != nil {
			fmt.Printf("Error read config: %v\t%v\n", NMCfg.EdgeConfigTemplate, err)
			return err
		}
	}
	if len(NMCfg.NetworkName) > 10 {
		return fmt.Errorf("name too long")
	}
	allowed := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_-"
	for _, c := range []byte(NMCfg.NetworkName) {
		if !strings.Contains(allowed, string(c)) {
			return fmt.Errorf("name can only contain %v", allowed)
		}
	}

	g, _ := path.NewGraph(0, false, mtypes.GraphRecalculateSetting{}, mtypes.NTPInfo{}, mtypes.LoggerInfo{LogInternal: false})
	edges := []mtypes.PongMsg{}
	if NMCfg.DistanceMatrix != "" {
		edges, err = path.ParseDistanceMatrix(NMCfg.DistanceMatrix)
		if err != nil {
			return err
		}
	} else {
		for S, edgeinfoS := range NMCfg.EdgeNodes {
			for D, edgeinfoD := range NMCfg.EdgeNodes {
				if S == D {
					continue
				}
				if len(edgeinfoS.Endpoint)+len(edgeinfoD.Endpoint) == 0 {
					continue
				}
				edges = append(edges, mtypes.PongMsg{
					Src_nodeID:     S,
					Dst_nodeID:     D,
					Timediff:       1,
					TimeToAlive:    99999,
					AdditionalCost: 0,
				})
			}
		}
	}
	g.UpdateLatencyMulti(edges, false, false)
	all_verts := g.Vertices()
	MaxNodeID := mtypes.Vertex(0)
	edge_infos := make(map[mtypes.Vertex]edge_info)
	for NodeID, edgeinfo := range NMCfg.EdgeNodes {
		endpoint := edgeinfo.Endpoint
		if _, has := all_verts[NodeID]; !has {
			return fmt.Errorf("not found in DistanceMatrix: NodeID %v", NodeID)
		}
		if !all_verts[NodeID] {
			return fmt.Errorf("duplicate definition: NodeID %v ", NodeID)
		}
		if endpoint != "" {
			_, _, err = conn.LookupIP(endpoint, conn.EnabledAf46, 0)
			if err != nil {
				return err
			}
		}
		pri, pub := device.RandomKeyPair()
		edge_infos[NodeID] = edge_info{
			Endpoint:      endpoint,
			PrivKey:       pri.ToString(),
			PubKey:        pub.ToString(),
			ConnectedEdge: make(map[mtypes.Vertex]bool),
		}
		all_verts[NodeID] = false
		if NodeID > MaxNodeID {
			MaxNodeID = NodeID
		}
	}
	for nid, notinconf := range all_verts {
		if notinconf {
			return fmt.Errorf("NodeID %v exists in DistanceMatrix but not in config", nid)
		}
	}
	for _, edge := range edges {
		s := edge.Src_nodeID
		d := edge.Dst_nodeID
		if len(edge_infos[s].Endpoint)+len(edge_infos[d].Endpoint) == 0 {
			return fmt.Errorf("there are an edge between node [%v , %v], but non of them have endpoint", s, d)
		}
		edge_infos[s].ConnectedEdge[d] = true
		edge_infos[d].ConnectedEdge[s] = true
	}
	if NMCfg.EdgeNode.MacPrefix != "" {
		_, err = tap.GetMacAddr(NMCfg.EdgeNode.MacPrefix, uint32(MaxNodeID))
		if err != nil {
			return err
		}
	} else {
		pbyte := mtypes.RandomBytes(4, []byte{0xaa, 0xbb, 0xcc, 0xdd})
		pbyte[0] &^= 0b00000001
		pbyte[0] |= 0b00000010
		NMCfg.EdgeNode.MacPrefix = fmt.Sprintf("%02X:%02X:%02X:%02X", pbyte[0], pbyte[1], pbyte[2], pbyte[3])
	}

	dist, dist_noAC, next, err := g.FloydWarshall(false)
	g.SetNHTable(next)
	if err != nil {
		fmt.Println("Error:", err)
	}
	if NMCfg.DistanceMatrix != "" && !enableP2P {
		nhTableStr, _ := yaml.Marshal(next)
		fmt.Println(string(nhTableStr))
	}
	all_vert := g.Vertices()
	fmt.Printf("Distance With Additional Cost\n")
	if NMCfg.DistanceMatrix != "" {
		for u := range all_vert {
			for v := range all_vert {
				if u != v {
					path, err := g.Path(u, v)
					if err != nil {
						return fmt.Errorf("couldn't find path from %v to %v: %v", u, v, err)
					}
					fmt.Printf("%d -> %d\t%3f\t%v\n", u, v, dist[u][v], path)
				}
			}
		}
	}
	fmt.Printf("Distance Without Additional Cost\n")
	if NMCfg.DistanceMatrix != "" {
		for u := range all_vert {
			for v := range all_vert {
				if u != v {
					path, err := g.Path(u, v)
					if err != nil {
						return fmt.Errorf("couldn't find path from %v to %v: %v", u, v, err)
					}
					fmt.Printf("%d -> %d\t%3f\t%v\n", u, v, dist_noAC[u][v], path)
				}
			}
		}
	}
	econfig, err := GetExampleEdgeConf(NMCfg.EdgeConfigTemplate, false)
	if err != nil {
		if enableP2P {
			econfig.DynamicRoute.AdditionalCost = 1000
		}
	}
	econfig.DynamicRoute.P2P.UseP2P = enableP2P
	econfig.DynamicRoute.SuperNode.UseSuperNode = false
	econfig.NextHopTable = next
	if enableP2P {
		econfig.NextHopTable = make(mtypes.NextHopTable)
	}
	ModeIDmax := 0
	for id, _ := range NMCfg.NetworkName {
		if ModeIDmax < id {
			ModeIDmax = id
		}
	}
	IPv4Block := NMCfg.EdgeNode.IPv4Range
	if IPv4Block != "" {
		_, _, err = tap.GetIP(4, IPv4Block, uint32(ModeIDmax))
		if err != nil {
			return err
		}
	}

	IPv6Block := NMCfg.EdgeNode.IPv6Range
	if IPv6Block != "" {
		_, _, err = tap.GetIP(6, IPv6Block, uint32(ModeIDmax))
		if err != nil {
			return err
		}
	}

	IPv6LLBlock := NMCfg.EdgeNode.IPv6LLRange
	if IPv6LLBlock != "" {
		_, _, err = tap.GetIP(6, IPv6LLBlock, uint32(ModeIDmax))
		if err != nil {
			return err
		}
	}
	econfig.DynamicRoute.NTPConfig.Servers = make([]string, 0)
	econfig.DynamicRoute.SuperNode.PSKey = ""
	econfig.DynamicRoute.SuperNode.EndpointV4 = ""
	econfig.DynamicRoute.SuperNode.EndpointV6 = ""
	econfig.DynamicRoute.SuperNode.PubKeyV4 = ""
	econfig.DynamicRoute.SuperNode.PubKeyV6 = ""
	econfig.DynamicRoute.SuperNode.EndpointEdgeAPIUrl = ""

	var pskdb device.PSKDB
	for NodeID, Edge := range edge_infos {
		econfig.NodeName = NMCfg.NetworkName
		econfig.Interface.Name = NMCfg.NetworkName
		econfig.Interface.MacAddrPrefix = NMCfg.EdgeNode.MacPrefix
		econfig.Interface.IPv4CIDR = NMCfg.EdgeNode.IPv4Range
		econfig.Interface.IPv6CIDR = NMCfg.EdgeNode.IPv6Range
		econfig.Interface.IPv6LLPrefix = NMCfg.EdgeNode.IPv6LLRange
		econfig.PrivKey = Edge.PrivKey
		econfig.NodeID = NodeID
		idstr := fmt.Sprintf("%0"+strconv.Itoa(len(MaxNodeID.ToString()))+"d", NodeID)
		if NMCfg.NetworkIFNameID {
			econfig.NodeName += idstr
			econfig.Interface.Name += idstr
		}
		PersistentKeepalive := uint32(30)
		econfig.ListenPort = 0
		if Edge.Endpoint != "" {
			ps := strings.Split(Edge.Endpoint, ":")
			pss := ps[len(ps)-1]
			port, err := strconv.ParseUint(pss, 10, 16)
			if err != nil {
				return err
			}
			econfig.ListenPort = int(port)
			PersistentKeepalive = 0
		}
		econfig.Peers = make([]mtypes.PeerInfo, 0)
		for CNodeID, _ := range Edge.ConnectedEdge {
			econfig.Peers = append(econfig.Peers, mtypes.PeerInfo{
				NodeID:              CNodeID,
				PubKey:              edge_infos[CNodeID].PubKey,
				PSKey:               pskdb.GetPSK(NodeID, CNodeID).ToString(),
				EndPoint:            edge_infos[CNodeID].Endpoint,
				PersistentKeepalive: PersistentKeepalive,
				Static:              true,
			})
		}
		mtypesBytes, _ := yaml.Marshal(econfig)
		fileWriter.WriteFile(filepath.Join(NMCfg.ConfigOutputDir, NMCfg.NetworkName+"_edge"+idstr+".yaml"), mtypesBytes, 0o600)
	}
	err = fileWriter.Commit()
	return err

}
