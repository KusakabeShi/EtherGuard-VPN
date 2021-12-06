/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package main

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/KusakabeSi/EtherGuard-VPN/conn"
	"github.com/KusakabeSi/EtherGuard-VPN/device"
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/tap"
	yaml "gopkg.in/yaml.v2"
)

var gencfg_reader *bufio.Reader

func readFLn(promptF string, checkFn func(string) error, defaultAns func() string, args ...interface{}) string {
	defaultans := defaultAns()
	if defaultans != "" {
		fmt.Printf(promptF+" ("+defaultans+") :", args...)
	} else {
		fmt.Printf(promptF+" :", args...)
	}
	text, err := gencfg_reader.ReadString('\n')
	if err != nil {
		panic(err)
	}
	text = strings.Replace(text, "\n", "", -1)
	if text == "" {
		text = defaultans
	}
	if err := checkFn(text); err != nil {
		fmt.Println(err)
		return readFLn(promptF, checkFn, defaultAns, args...)
	}
	return text
}

func genSuperCfg() error {
	gencfg_reader = bufio.NewReader(os.Stdin)

	noCheck := func(string) error {
		return nil
	}

	noDefault := func() string {
		return ""
	}

	CfgSavePath := readFLn("Config save path", func(s string) (err error) {
		err = os.MkdirAll(s, 0o700)
		if err != nil {
			return
		}
		files, err := os.ReadDir(s)
		if err != nil {
			return
		}
		if len(files) > 0 {
			return fmt.Errorf(s + " not empty")
		}
		return
	}, func() string { return filepath.Join(".", "eg_generated_configs") })
	SuperTamplatePath := readFLn("SuperConfig template path(optional)", func(s string) (err error) {
		if s != "" {
			var sconfig mtypes.SuperConfig
			err = readYaml(s, &sconfig)
			if err != nil {
				fmt.Printf("Error read config: %v\t%v\n", s, err)
				return err
			}
		}
		return
	}, noDefault)
	EdgeTamplatePath := readFLn("EdgeTamplatePath template path(optional)", func(s string) (err error) {
		if s != "" {
			var econfig mtypes.EdgeConfig
			err = readYaml(s, &econfig)
			if err != nil {
				fmt.Printf("Error read config: %v\t%v\n", s, err)
				return err
			}
		}
		return
	}, noDefault)

	sconfig := getExampleSuperConf(SuperTamplatePath)

	NetworkName := readFLn("Network name", func(s string) (err error) {
		if len(s) > 10 {
			return fmt.Errorf("Name too long")
		}
		allowed := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_"
		for _, c := range []byte(s) {
			if strings.Contains(allowed, string(c)) == false {
				return fmt.Errorf("Name can only contain %v", allowed)
			}
		}
		return
	}, func() string { return "eg_net" })

	ListenPort := readFLn("SuperNode ListenPort", func(s string) (err error) {
		_, err = strconv.ParseUint(s, 10, 16)
		return
	}, func() string { return "12369" })

	API_Prefix := readFLn("EdgeAPI prefix", noCheck, func() string { return "/" + NetworkName + "/eg_api" })
	EndpointV4 := readFLn("IPv4/domain of your supernode (optional)", func(s string) (err error) {
		if s != "" {
			_, err = conn.LookupIP(s+":"+ListenPort, 4)
		}
		return
	}, noDefault)
	EndpointV6 := readFLn("IPv6/domain of your supernode (optional)", func(s string) (err error) {
		if s != "" {
			if strings.Contains(s, ":") && (s[0] != '[' || s[len(s)-1] != ']') {
				return fmt.Errorf("Invalid IPv6 format, please use [%v] instead", s)
			}
			_, err = conn.LookupIP(s+":"+ListenPort, 6)
		} else if EndpointV4 == "" {
			return fmt.Errorf("Muse provide at lease v4 v6 address")
		}
		return
	}, noDefault)
	EndpointEdgeAPIUrl := readFLn("URL(use domain would be better) of the EdgeAPI provided by supernode", noCheck, func() string { return fmt.Sprintf("http://%v:%v%v", EndpointV4, ListenPort, API_Prefix) })

	sconfig.NodeName = NetworkName + "SP"
	sconfig.API_Prefix = API_Prefix
	sconfig.ListenPort, _ = strconv.Atoi(ListenPort)
	sconfig.ListenPort_EdgeAPI = ListenPort
	sconfig.ListenPort_ManageAPI = ListenPort
	sconfig.NextHopTable = make(mtypes.NextHopTable)
	sconfig.EdgeTemplate = EdgeTamplatePath

	NodeNum := readFLn("Number of your nodes", func(s string) (err error) {
		_, err = strconv.ParseInt(s, 10, 16)
		return
	}, noDefault)
	MaxNode, _ := strconv.ParseInt(NodeNum, 10, 16)

	MacPrefix := readFLn("MacAddress Prefix", func(s string) (err error) {
		_, err = tap.GetMacAddr(s, uint32(MaxNode))
		return
	}, func() string {
		pbyte := mtypes.RandomBytes(4, []byte{0xaa, 0xbb, 0xcc, 0xdd})
		pbyte[0] &^= 0b00000001
		pbyte[0] |= 0b00000010
		return fmt.Sprintf("%X:%X:%X:%X", pbyte[0], pbyte[1], pbyte[2], pbyte[3])
	})
	IPv4Block := readFLn("IPv4 block(optional)", func(s string) (err error) {
		if s != "" {
			_, _, err = tap.GetIP(4, s, uint32(MaxNode))
		}
		return err
	}, noDefault)
	IPv6Block := readFLn("IPv6 block(optional)", func(s string) (err error) {
		if s != "" {
			_, _, err = tap.GetIP(6, s, uint32(MaxNode))
		}
		return err
	}, noDefault)
	IPv6LLBlock := readFLn("IPv6LL block(optional)", func(s string) (err error) {
		if s != "" {
			_, _, err = tap.GetIP(6, s, uint32(MaxNode))
		}
		return err
	}, noDefault)

	SuperPeerInfo := make([]mtypes.SuperPeerInfo, 0, MaxNode)
	PrivKeyS4 := device.NoisePrivateKey(mtypes.ByteSlice2Byte32(mtypes.RandomBytes(32, []byte{})))
	PubKeyS4 := PrivKeyS4.PublicKey()
	PrivKeyS6 := device.NoisePrivateKey(mtypes.ByteSlice2Byte32(mtypes.RandomBytes(32, []byte{})))
	PubKeyS6 := PrivKeyS6.PublicKey()
	sconfig.PrivKeyV4 = PrivKeyS4.ToString()
	sconfig.PrivKeyV6 = PrivKeyS6.ToString()
	allec := make(map[mtypes.Vertex]mtypes.EdgeConfig)
	peerceconf := getExampleEdgeConf(EdgeTamplatePath)
	peerceconf.Peers = []mtypes.PeerInfo{}
	peerceconf.NextHopTable = make(mtypes.NextHopTable)
	for i := mtypes.Vertex(1); i <= mtypes.Vertex(MaxNode); i++ {
		PSKeyE := device.NoisePresharedKey(mtypes.ByteSlice2Byte32(mtypes.RandomBytes(32, []byte{})))
		PrivKeyE := device.NoisePrivateKey(mtypes.ByteSlice2Byte32(mtypes.RandomBytes(32, []byte{})))
		PubKeyE := PrivKeyE.PublicKey()
		idstr := fmt.Sprintf("%0"+strconv.Itoa(len(NodeNum))+"d", i)

		allec[i] = peerceconf
		peerceconf.DynamicRoute.SuperNode.EndpointV4 = EndpointV4 + ":" + ListenPort
		peerceconf.DynamicRoute.SuperNode.EndpointV6 = EndpointV6 + ":" + ListenPort
		peerceconf.DynamicRoute.SuperNode.EndpointEdgeAPIUrl = EndpointEdgeAPIUrl
		peerceconf.Interface.MacAddrPrefix = MacPrefix
		peerceconf.Interface.IPv4CIDR = IPv4Block
		peerceconf.Interface.IPv6CIDR = IPv6Block
		peerceconf.Interface.IPv6LLPrefix = IPv6LLBlock

		peerceconf.NodeID = i
		peerceconf.NodeName = NetworkName + idstr
		peerceconf.Interface.Name = NetworkName + idstr
		peerceconf.DynamicRoute.SuperNode.PubKeyV4 = PubKeyS4.ToString()
		peerceconf.DynamicRoute.SuperNode.PubKeyV6 = PubKeyS6.ToString()
		peerceconf.DynamicRoute.SuperNode.PSKey = PSKeyE.ToString()
		peerceconf.PrivKey = PrivKeyE.ToString()

		SuperPeerInfo = append(SuperPeerInfo, mtypes.SuperPeerInfo{
			NodeID:         i,
			Name:           NetworkName + idstr,
			PubKey:         PubKeyE.ToString(),
			PSKey:          PSKeyE.ToString(),
			AdditionalCost: peerceconf.DynamicRoute.AdditionalCost,
			SkipLocalIP:    peerceconf.DynamicRoute.SuperNode.SkipLocalIP,
		})
		mtypesBytes, _ := yaml.Marshal(peerceconf)
		ioutil.WriteFile(filepath.Join(CfgSavePath, NetworkName+"_edge"+idstr+".yaml"), mtypesBytes, 0o600)
		fmt.Println(filepath.Join(CfgSavePath, NetworkName+"_edge"+idstr+".yaml"))
	}
	sconfig.Peers = SuperPeerInfo
	mtypesBytes, _ := yaml.Marshal(sconfig)
	ioutil.WriteFile(filepath.Join(CfgSavePath, NetworkName+"_super.yaml"), mtypesBytes, 0o600)
	fmt.Println(filepath.Join(CfgSavePath, NetworkName+"_super.yaml"))
	return nil
}
