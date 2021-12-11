/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package gencfg

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"math"
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

func ParseIDs(s string) ([]int, int, int, error) {
	ret := make([]int, 0)
	if len(s) <= 3 {
		return ret, 0, 0, fmt.Errorf("Parse Error: %v", s)
	}
	if s[0] != '[' {
		return ret, 0, 0, fmt.Errorf("Parse Error: %v", s)
	}
	if s[len(s)-1] != ']' {
		return ret, 0, 0, fmt.Errorf("Parse Error: %v", s)
	}
	s = s[1 : len(s)-1]
	as := strings.Split(s, ",")
	min := math.MaxUint16
	max := 0
	for i, es := range as {
		if strings.Contains(es, "~") {
			esl := strings.SplitN(es, "~", 2)
			si, err := strconv.ParseInt(esl[0], 10, 16)
			if err != nil {
				return ret, min, max, err
			}
			ei, err := strconv.ParseInt(esl[1], 10, 16)
			if err != nil {
				return ret, min, max, err
			}
			if si >= ei {
				return ret, min, max, fmt.Errorf("end %v must > start %v", ei, si)
			}
			if int(si) < 0 {
				return ret, min, max, fmt.Errorf("node ID < 0 at element %v", i)
			}
			if min > int(si) {
				min = int(si)
			}
			if int(si) < max {
				return ret, min, max, fmt.Errorf("list out of order at the %vth element: %v", i, es)
			} else if int(si) == max {
				return ret, min, max, fmt.Errorf("duplicate id in the %vth element: %v", i, es)
			}
			max = int(ei)
			for ; si <= ei; si++ {
				ret = append(ret, int(si))
			}
		} else {
			si, err := strconv.ParseInt(es, 10, 16)
			if err != nil {
				return ret, min, max, err
			}
			if int(si) < max {
				return ret, min, max, fmt.Errorf("List out of order at the %vth element!", i)
			} else if int(si) == max {
				return ret, min, max, fmt.Errorf("duplicate id in the %vth element", i)
			}
			if min > int(si) {
				min = int(si)
			}
			max = int(si)
			ret = append(ret, int(si))
		}
	}
	return ret, min, max, nil
}

func printExampleSMCfg() {
	tconfig := SMCfg{}
	toprint, _ := yaml.Marshal(tconfig)
	fmt.Print(string(toprint))
}

func GenSuperCfg(SMCinfigPath string, printExample bool) (err error) {
	SMCfg := SMCfg{}
	if printExample {
		printExampleSMCfg()
		return
	}
	err = mtypes.ReadYaml(SMCinfigPath, &SMCfg)
	if err != nil {
		return err
	}

	os.Chdir(filepath.Dir(SMCinfigPath))

	err = os.MkdirAll(SMCfg.ConfigOutputDir, 0o700)
	if err != nil {
		return err
	}
	files, err := os.ReadDir(SMCfg.ConfigOutputDir)
	if err != nil {
		return err
	}
	if len(files) > 0 {
		return fmt.Errorf(SMCfg.ConfigOutputDir + " not empty")
	}

	if SMCfg.SuperConfigTemplate != "" {
		var sconfig mtypes.SuperConfig
		err = mtypes.ReadYaml(SMCfg.SuperConfigTemplate, &sconfig)
		if err != nil {
			fmt.Printf("Error read config: %v\t%v\n", SMCfg.SuperConfigTemplate, err)
			return err
		}
	}

	if SMCfg.EdgeConfigTemplate != "" {
		var econfig mtypes.EdgeConfig
		err = mtypes.ReadYaml(SMCfg.EdgeConfigTemplate, &econfig)
		if err != nil {
			fmt.Printf("Error read config: %v\t%v\n", SMCfg.EdgeConfigTemplate, err)
			return err
		}
	}

	sconfig := GetExampleSuperConf(SMCfg.SuperConfigTemplate, false)

	if len(SMCfg.NetworkName) > 10 {
		return fmt.Errorf("Name too long")
	}
	allowed := "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ_"
	for _, c := range []byte(SMCfg.NetworkName) {
		if strings.Contains(allowed, string(c)) == false {
			return fmt.Errorf("Name can only contain %v", allowed)
		}
	}

	ListenPort := fmt.Sprintf("%v", SMCfg.Supernode.ListenPort)

	API_Prefix := SMCfg.Supernode.EdgeAPI_Prefix
	EndpointV4 := SMCfg.Supernode.EndpointV4
	if EndpointV4 != "" {
		_, err = conn.LookupIP(EndpointV4+":"+ListenPort, 4)
		if err != nil {
			return err
		}

	}

	EndpointV6 := SMCfg.Supernode.EndpointV6
	if EndpointV6 != "" {
		if strings.Contains(EndpointV6, ":") && (EndpointV6[0] != '[' || EndpointV6[len(EndpointV6)-1] != ']') {
			return fmt.Errorf("Invalid IPv6 format, please use [%v] instead", EndpointV6)
		}
		_, err = conn.LookupIP(EndpointV6+":"+ListenPort, 6)
		if err != nil {
			return
		}
	} else if EndpointV4 == "" {
		return fmt.Errorf("Muse provide at lease v4 v6 address")
	}

	EndpointEdgeAPIUrl := SMCfg.Supernode.Endpoint_EdgeAPI

	sconfig.NodeName = SMCfg.NetworkName + "SP"
	sconfig.API_Prefix = API_Prefix
	sconfig.ListenPort, _ = strconv.Atoi(ListenPort)
	sconfig.ListenPort_EdgeAPI = ListenPort
	sconfig.ListenPort_ManageAPI = ListenPort
	sconfig.EdgeTemplate = SMCfg.EdgeConfigTemplate

	NodeIDs, _, ModeIDmax, err := ParseIDs(SMCfg.EdgeNode.NodeIDs)
	if err != nil {
		return
	}
	MacPrefix := SMCfg.EdgeNode.MacPrefix

	if MacPrefix != "" {
		_, err = tap.GetMacAddr(MacPrefix, uint32(ModeIDmax))
		if err != nil {
			return err
		}
	} else {
		pbyte := mtypes.RandomBytes(4, []byte{0xaa, 0xbb, 0xcc, 0xdd})
		pbyte[0] &^= 0b00000001
		pbyte[0] |= 0b00000010
		MacPrefix = fmt.Sprintf("%02X:%02X:%02X:%02X", pbyte[0], pbyte[1], pbyte[2], pbyte[3])
	}

	IPv4Block := SMCfg.EdgeNode.IPv4Range
	if IPv4Block != "" {
		_, _, err = tap.GetIP(4, IPv4Block, uint32(ModeIDmax))
		if err != nil {
			return err
		}
	}

	IPv6Block := SMCfg.EdgeNode.IPv6Range
	if IPv6Block != "" {
		_, _, err = tap.GetIP(6, IPv6Block, uint32(ModeIDmax))
		if err != nil {
			return err
		}
	}

	IPv6LLBlock := SMCfg.EdgeNode.IPv6LLRange
	if IPv6LLBlock != "" {
		_, _, err = tap.GetIP(6, IPv6LLBlock, uint32(ModeIDmax))
		if err != nil {
			return err
		}
	}

	SuperPeerInfo := make([]mtypes.SuperPeerInfo, 0, ModeIDmax)
	PrivKeyS4, PubKeyS4 := device.RandomKeyPair()
	PrivKeyS6, PubKeyS6 := device.RandomKeyPair()
	sconfig.PrivKeyV4 = PrivKeyS4.ToString()
	sconfig.PrivKeyV6 = PrivKeyS6.ToString()
	allec := make(map[mtypes.Vertex]mtypes.EdgeConfig)
	peerceconf := GetExampleEdgeConf(sconfig.EdgeTemplate, false)
	for _, ii := range NodeIDs {
		i := mtypes.Vertex(ii)
		PSKeyE := device.RandomPSK()
		PrivKeyE, PubKeyE := device.RandomKeyPair()
		idstr := fmt.Sprintf("%0"+strconv.Itoa(len(strconv.Itoa(ModeIDmax)))+"d", i)

		allec[i] = peerceconf
		if EndpointV4 != "" {
			peerceconf.DynamicRoute.SuperNode.EndpointV4 = EndpointV4 + ":" + ListenPort
		}
		if EndpointV6 != "" {
			peerceconf.DynamicRoute.SuperNode.EndpointV6 = EndpointV6 + ":" + ListenPort
		}
		peerceconf.DynamicRoute.SuperNode.EndpointEdgeAPIUrl = EndpointEdgeAPIUrl
		peerceconf.Interface.MacAddrPrefix = MacPrefix
		peerceconf.Interface.IPv4CIDR = IPv4Block
		peerceconf.Interface.IPv6CIDR = IPv6Block
		peerceconf.Interface.IPv6LLPrefix = IPv6LLBlock

		peerceconf.NodeID = i
		peerceconf.NodeName = SMCfg.NetworkName + idstr
		peerceconf.Interface.Name = SMCfg.NetworkName + idstr
		peerceconf.DynamicRoute.SuperNode.PubKeyV4 = PubKeyS4.ToString()
		peerceconf.DynamicRoute.SuperNode.PubKeyV6 = PubKeyS6.ToString()
		peerceconf.DynamicRoute.SuperNode.PSKey = PSKeyE.ToString()
		peerceconf.PrivKey = PrivKeyE.ToString()

		SuperPeerInfo = append(SuperPeerInfo, mtypes.SuperPeerInfo{
			NodeID:         i,
			Name:           SMCfg.NetworkName + idstr,
			PubKey:         PubKeyE.ToString(),
			PSKey:          PSKeyE.ToString(),
			AdditionalCost: peerceconf.DynamicRoute.AdditionalCost,
			SkipLocalIP:    peerceconf.DynamicRoute.SuperNode.SkipLocalIP,
		})
		mtypesBytes, _ := yaml.Marshal(peerceconf)
		ioutil.WriteFile(filepath.Join(SMCfg.ConfigOutputDir, SMCfg.NetworkName+"_edge"+idstr+".yaml"), mtypesBytes, 0o600)
		fmt.Println(filepath.Join(SMCfg.ConfigOutputDir, SMCfg.NetworkName+"_edge"+idstr+".yaml"))
	}
	sconfig.Peers = SuperPeerInfo
	mtypesBytes, _ := yaml.Marshal(sconfig)
	ioutil.WriteFile(filepath.Join(SMCfg.ConfigOutputDir, SMCfg.NetworkName+"_super.yaml"), mtypesBytes, 0o600)
	fmt.Println(filepath.Join(SMCfg.ConfigOutputDir, SMCfg.NetworkName+"_super.yaml"))
	return nil
}
