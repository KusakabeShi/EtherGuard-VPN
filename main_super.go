/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package main

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/google/shlex"

	"github.com/KusakabeSi/EtherGuard-VPN/conn"
	"github.com/KusakabeSi/EtherGuard-VPN/device"
	"github.com/KusakabeSi/EtherGuard-VPN/gencfg"
	"github.com/KusakabeSi/EtherGuard-VPN/ipc"
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/path"
	"github.com/KusakabeSi/EtherGuard-VPN/tap"
	yaml "gopkg.in/yaml.v2"
)

func checkNhTable(NhTable mtypes.NextHopTable, peers []mtypes.SuperPeerInfo) error {
	allpeer := make(map[mtypes.Vertex]bool, len(peers))
	for _, peer1 := range peers {
		allpeer[peer1.NodeID] = true
	}
	for _, peer1 := range peers {
		for _, peer2 := range peers {
			if peer1.NodeID == peer2.NodeID {
				continue
			}
			id1 := peer1.NodeID
			id2 := peer2.NodeID
			if dst, has := NhTable[id1]; has {
				if next, has2 := dst[id2]; has2 {
					if _, hasa := allpeer[next]; hasa {

					} else {
						return fmt.Errorf("NextHopTable[%v][%v]=%v which is not in the peer list", id1, id2, next)
					}
				} else {
					return fmt.Errorf("NextHopTable[%v][%v] not found", id1, id2)
				}
			} else {
				return fmt.Errorf("NextHopTable[%v] not found", id1)
			}
		}
	}
	return nil
}

func printExampleSuperConf() {
	sconfig, _ := gencfg.GetExampleSuperConf("", true)
	scprint, _ := yaml.Marshal(sconfig)
	fmt.Print(string(scprint))
}

func Super(configPath string, useUAPI bool, printExample bool, bindmode string) (err error) {
	if printExample {
		printExampleSuperConf()
		return nil
	}
	var sconfig mtypes.SuperConfig

	err = mtypes.ReadYaml(configPath, &sconfig)
	if err != nil {
		fmt.Printf("Error read config: %v\t%v\n", configPath, err)
		return err
	}
	httpobj.http_sconfig = &sconfig
	http_econfig_tmp, _ := gencfg.GetExampleEdgeConf(sconfig.EdgeTemplate, true)
	httpobj.http_econfig_tmp = &http_econfig_tmp
	NodeName := sconfig.NodeName
	if len(NodeName) > 32 {
		return errors.New("Node name can't longer than 32 :" + NodeName)
	}
	if sconfig.PeerAliveTimeout <= 0 {
		return fmt.Errorf("PeerAliveTimeout must > 0 : %v", sconfig.PeerAliveTimeout)
	}
	if sconfig.HttpPostInterval < 0 {
		return fmt.Errorf("HttpPostInterval must >= 0 : %v", sconfig.HttpPostInterval)
	} else if sconfig.HttpPostInterval > sconfig.PeerAliveTimeout {
		return fmt.Errorf("HttpPostInterval must <= PeerAliveTimeout : %v", sconfig.HttpPostInterval)
	}
	if sconfig.SendPingInterval <= 0 {
		return fmt.Errorf("SendPingInterval must > 0 : %v", sconfig.SendPingInterval)
	}
	if sconfig.RePushConfigInterval <= 0 {
		return fmt.Errorf("RePushConfigInterval must > 0 : %v", sconfig.RePushConfigInterval)
	}
	if sconfig.DampingResistance < 0 || sconfig.DampingResistance >= 1 {
		return fmt.Errorf("DampingResistance must in range [0,1) : %v", sconfig.DampingResistance)
	}

	var logLevel int
	switch sconfig.LogLevel.LogLevel {
	case "verbose", "debug":
		logLevel = device.LogLevelVerbose
	case "error":
		logLevel = device.LogLevelError
	case "silent":
		logLevel = device.LogLevelSilent
	default:
		logLevel = device.LogLevelError
	}

	logger4 := device.NewLogger(
		logLevel,
		fmt.Sprintf("(%s) ", NodeName+"_v4"),
	)
	logger6 := device.NewLogger(
		logLevel,
		fmt.Sprintf("(%s) ", NodeName+"_v6"),
	)

	httpobj.http_sconfig_path = configPath
	httpobj.http_PeerState = make(map[string]*PeerState)
	httpobj.http_PeerIPs = make(map[string]*HttpPeerLocalIP)
	httpobj.http_PeerID2Info = make(map[mtypes.Vertex]mtypes.SuperPeerInfo)
	httpobj.http_HashSalt = []byte(mtypes.RandomStr(32, fmt.Sprintf("%v", time.Now())))
	httpobj.http_passwords = sconfig.Passwords

	httpobj.http_super_chains = &mtypes.SUPER_Events{
		Event_server_pong:     make(chan mtypes.PongMsg, 1<<5),
		Event_server_register: make(chan mtypes.RegisterMsg, 1<<5),
	}
	httpobj.http_graph, err = path.NewGraph(3, true, sconfig.GraphRecalculateSetting, mtypes.NTPInfo{}, mtypes.LoggerInfo{})
	if err != nil {
		return err
	}
	httpobj.http_graph.SetNHTable(httpobj.http_sconfig.NextHopTable)
	if sconfig.GraphRecalculateSetting.StaticMode {
		err = checkNhTable(httpobj.http_sconfig.NextHopTable, sconfig.Peers)
		if err != nil {
			return err
		}
	}
	thetap4, _ := tap.CreateDummyTAP()
	httpobj.http_device4 = device.NewDevice(thetap4, mtypes.NodeID_SuperNode, conn.NewDefaultBind(true, false, bindmode), logger4, httpobj.http_graph, true, configPath, nil, &sconfig, httpobj.http_super_chains, Version)
	defer httpobj.http_device4.Close()
	thetap6, _ := tap.CreateDummyTAP()
	httpobj.http_device6 = device.NewDevice(thetap6, mtypes.NodeID_SuperNode, conn.NewDefaultBind(false, true, bindmode), logger6, httpobj.http_graph, true, configPath, nil, &sconfig, httpobj.http_super_chains, Version)
	defer httpobj.http_device6.Close()
	if sconfig.PrivKeyV4 != "" {
		pk4, err := device.Str2PriKey(sconfig.PrivKeyV4)
		if err != nil {
			fmt.Println("Error decode base64 ", err)
			return err
		}
		httpobj.http_device4.SetPrivateKey(pk4)
		httpobj.http_device4.IpcSet("fwmark=0\n")
		httpobj.http_device4.IpcSet("listen_port=" + strconv.Itoa(sconfig.ListenPort) + "\n")
		httpobj.http_device4.IpcSet("replace_peers=true\n")
	}

	if sconfig.PrivKeyV6 != "" {
		pk6, err := device.Str2PriKey(sconfig.PrivKeyV6)
		if err != nil {
			fmt.Println("Error decode base64 ", err)
			return err
		}
		httpobj.http_device6.SetPrivateKey(pk6)
		httpobj.http_device6.IpcSet("fwmark=0\n")
		httpobj.http_device6.IpcSet("listen_port=" + strconv.Itoa(sconfig.ListenPort) + "\n")
		httpobj.http_device6.IpcSet("replace_peers=true\n")
	}

	for _, peerconf := range sconfig.Peers {
		err := super_peeradd(peerconf)
		if err != nil {
			return err
		}
	}
	logger4.Verbosef("Device4 started")
	logger6.Verbosef("Device6 started")

	errs := make(chan error, 1<<3)
	term := make(chan os.Signal, 1)
	if useUAPI {
		uapi4, err := startUAPI(NodeName+"_v4", logger4, httpobj.http_device4, errs)
		if err != nil {
			return err
		}
		defer uapi4.Close()
		uapi6, err := startUAPI(NodeName+"_v6", logger6, httpobj.http_device6, errs)
		if err != nil {
			return err
		}
		defer uapi6.Close()
	}

	go Event_server_event_hendler(httpobj.http_graph, httpobj.http_super_chains)
	go RoutinePushSettings(mtypes.S2TD(sconfig.RePushConfigInterval))
	go RoutineTimeoutCheck()
	HttpServer(sconfig.ListenPort_EdgeAPI, sconfig.ListenPort_ManageAPI, sconfig.API_Prefix, errs)

	if sconfig.PostScript != "" {
		envs := make(map[string]string)
		envs["EG_MODE"] = "super"
		envs["EG_NODE_NAME"] = sconfig.NodeName
		cmdarg, err := shlex.Split(sconfig.PostScript)
		if err != nil {
			return fmt.Errorf("error parse PostScript %v", err)
		}
		if sconfig.LogLevel.LogInternal {
			fmt.Printf("PostScript: exec.Command(%v)\n", cmdarg)
		}
		cmd := exec.Command(cmdarg[0], cmdarg[1:]...)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("exec.Command(%v) failed with %v", cmdarg, err)
		}
		if sconfig.LogLevel.LogInternal {
			fmt.Printf("PostScript output: %s\n", string(out))
		}
	}

	SdNotify, err := mtypes.SdNotify(false, mtypes.SdNotifyReady)
	if sconfig.LogLevel.LogInternal {
		fmt.Printf("Internal: SdNotify:%v err:%v\n", SdNotify, err)
	}

	signal.Notify(term, syscall.SIGTERM)
	signal.Notify(term, os.Interrupt)
	select {
	case <-term:
	case <-errs:
	case <-httpobj.http_device4.Wait():
	case <-httpobj.http_device6.Wait():
	}
	logger4.Verbosef("Shutting down")
	return
}

func super_peeradd(peerconf mtypes.SuperPeerInfo) error {
	// No lock, lock before call me
	pk, err := device.Str2PubKey(peerconf.PubKey)
	if err != nil {
		return fmt.Errorf("error decode base64 :%v", err)
	}
	if httpobj.http_sconfig.PrivKeyV4 != "" {
		var psk device.NoisePresharedKey
		if peerconf.PSKey != "" {
			psk, err = device.Str2PSKey(peerconf.PSKey)
			if err != nil {
				return fmt.Errorf("error decode base64 :%v", err)
			}
		}
		peer4, err := httpobj.http_device4.NewPeer(pk, peerconf.NodeID, false, 0)
		if err != nil {
			return fmt.Errorf("error create peer id :%v", err)
		}
		peer4.StaticConn = false
		if peerconf.PSKey != "" {
			peer4.SetPSK(psk)
		}
		if peerconf.EndPoint != "" {
			err = peer4.SetEndpointFromConnURL(peerconf.EndPoint, 4, false)
			if err != nil {
				if httpobj.http_sconfig.LogLevel.LogInternal {
					fmt.Printf("Internal: Set endpoint failed:%v\n", err)
				}
			}
		}
	}
	if httpobj.http_sconfig.PrivKeyV6 != "" {
		var psk device.NoisePresharedKey
		if peerconf.PSKey != "" {
			psk, err = device.Str2PSKey(peerconf.PSKey)
			if err != nil {
				return fmt.Errorf("error decode base64 :%v", err)
			}
		}
		peer6, err := httpobj.http_device6.NewPeer(pk, peerconf.NodeID, false, 0)
		if err != nil {
			return fmt.Errorf("error create peer id :%v", err)
		}
		peer6.StaticConn = false
		if peerconf.PSKey != "" {
			peer6.SetPSK(psk)
		}
		if peerconf.EndPoint != "" {
			err = peer6.SetEndpointFromConnURL(peerconf.EndPoint, 6, false)
			if err != nil {
				if httpobj.http_sconfig.LogLevel.LogInternal {
					fmt.Printf("Internal: Set endpoint failed:%v\n", err)
				}
			}
		}
	}
	httpobj.http_PeerID2Info[peerconf.NodeID] = peerconf

	SuperParams := mtypes.API_SuperParams{
		SendPingInterval: httpobj.http_sconfig.SendPingInterval,
		HttpPostInterval: httpobj.http_sconfig.HttpPostInterval,
		PeerAliveTimeout: httpobj.http_sconfig.PeerAliveTimeout,
		AdditionalCost:   peerconf.AdditionalCost,
	}

	SuperParamStr, _ := json.Marshal(SuperParams)
	md5_hash_raw := md5.Sum(append(SuperParamStr, httpobj.http_HashSalt...))
	new_hash_str := hex.EncodeToString(md5_hash_raw[:])

	PS := PeerState{}
	PS.NhTableState.Store("")              // string
	PS.PeerInfoState.Store("")             // string
	PS.SuperParamState.Store(new_hash_str) // string
	PS.SuperParamStateClient.Store("")     // string
	PS.JETSecret.Store(mtypes.JWTSecret{}) // mtypes.JWTSecret
	PS.httpPostCount.Store(uint64(0))      // uint64
	PS.LastSeen.Store(time.Time{})         // time.Time
	httpobj.http_PeerState[peerconf.PubKey] = &PS

	httpobj.http_PeerIPs[peerconf.PubKey] = &HttpPeerLocalIP{}
	return nil
}

func super_peerdel(toDelete mtypes.Vertex) {
	// No lock, lock before call me
	if _, has := httpobj.http_PeerID2Info[toDelete]; !has {
		return
	}
	PubKey := httpobj.http_PeerID2Info[toDelete].PubKey
	httpobj.http_pskdb.DelNode(toDelete)
	delete(httpobj.http_PeerState, PubKey)
	delete(httpobj.http_PeerIPs, PubKey)
	delete(httpobj.http_PeerID2Info, toDelete)
	go super_peerdel_notify(toDelete, PubKey)
}

func super_peerdel_notify(toDelete mtypes.Vertex, PubKey string) {
	ServerUpdateMsg := mtypes.ServerUpdateMsg{
		Node_id: toDelete,
		Action:  mtypes.Shutdown,
		Code:    int(syscall.ENOENT),
		Params:  "You've been removed from supernode.",
	}
	for i := 0; i < 10; i++ {
		body, _ := mtypes.GetByte(&ServerUpdateMsg)
		buf := make([]byte, path.EgHeaderLen+len(body))
		header, _ := path.NewEgHeader(buf[:path.EgHeaderLen], device.DefaultMTU)
		header.SetSrc(mtypes.NodeID_SuperNode)
		copy(buf[path.EgHeaderLen:], body)
		header.SetDst(toDelete)

		peer4 := httpobj.http_device4.LookupPeerByStr(PubKey)
		httpobj.http_device4.SendPacket(peer4, path.ServerUpdate, 0, buf, device.MessageTransportOffsetContent)

		peer6 := httpobj.http_device6.LookupPeerByStr(PubKey)
		httpobj.http_device6.SendPacket(peer6, path.ServerUpdate, 0, buf, device.MessageTransportOffsetContent)
		time.Sleep(mtypes.S2TD(0.1))
	}
	httpobj.http_device4.RemovePeerByID(toDelete)
	httpobj.http_device6.RemovePeerByID(toDelete)
	httpobj.http_graph.RemoveVirt(toDelete, true, false)
}

func Event_server_event_hendler(graph *path.IG, events *mtypes.SUPER_Events) {
	for {
		select {
		case reg_msg := <-events.Event_server_register:
			var should_push_peer bool
			var should_push_nh bool
			var should_push_superparams bool
			NodeID := reg_msg.Node_id
			httpobj.RLock()
			PubKey := httpobj.http_PeerID2Info[NodeID].PubKey
			if reg_msg.Node_id < mtypes.NodeID_Special {
				httpobj.http_PeerState[PubKey].LastSeen.Store(time.Now())
				httpobj.http_PeerState[PubKey].JETSecret.Store(reg_msg.JWTSecret)
				httpobj.http_PeerState[PubKey].httpPostCount.Store(reg_msg.HttpPostCount)
				if httpobj.http_PeerState[PubKey].NhTableState.Load().(string) != reg_msg.NhStateHash {
					httpobj.http_PeerState[PubKey].NhTableState.Store(reg_msg.NhStateHash)
					should_push_nh = true
				}
				if httpobj.http_PeerState[PubKey].PeerInfoState.Load().(string) != reg_msg.PeerStateHash {
					httpobj.http_PeerState[PubKey].PeerInfoState.Store(reg_msg.PeerStateHash)
					should_push_peer = true
				}
				if httpobj.http_PeerState[PubKey].SuperParamStateClient.Load().(string) != reg_msg.SuperParamStateHash {
					httpobj.http_PeerState[PubKey].SuperParamStateClient.Store(reg_msg.SuperParamStateHash)
					should_push_superparams = true
				}
			}
			var peer_state_changed bool

			httpobj.http_PeerInfo, httpobj.http_PeerInfo_hash, peer_state_changed = get_api_peers(httpobj.http_PeerInfo_hash)
			if should_push_peer || peer_state_changed {
				PushPeerinfo(false)
			}
			if should_push_nh {
				PushNhTable(false)
			}
			if should_push_superparams {
				PushServerParams(false)
			}
			httpobj.RUnlock()
		case pong_msg := <-events.Event_server_pong:
			var changed bool
			httpobj.RLock()
			if pong_msg.Src_nodeID < mtypes.NodeID_Special && pong_msg.Dst_nodeID < mtypes.NodeID_Special {
				AdditionalCost_use := httpobj.http_PeerID2Info[pong_msg.Dst_nodeID].AdditionalCost
				if AdditionalCost_use < 0 {
					pong_msg.AdditionalCost = AdditionalCost_use
				}
				changed = httpobj.http_graph.UpdateLatencyMulti([]mtypes.PongMsg{pong_msg}, true, true)
			} else {
				changed = httpobj.http_graph.RecalculateNhTable(true)

			}
			if changed {
				NhTable := graph.GetNHTable(true)
				NhTablestr, _ := json.Marshal(NhTable)
				md5_hash_raw := md5.Sum(append(NhTablestr, httpobj.http_HashSalt...))
				new_hash_str := hex.EncodeToString(md5_hash_raw[:])
				httpobj.http_NhTable_Hash = new_hash_str
				httpobj.http_NhTableStr = NhTablestr
				PushNhTable(false)
			}
			httpobj.RUnlock()
		}
	}
}

func RoutinePushSettings(interval time.Duration) {
	force := false
	var lastforce time.Time
	for {
		if time.Now().After(lastforce.Add(interval)) {
			lastforce = time.Now()
			force = true
		} else {
			force = false
		}
		PushNhTable(force)
		PushPeerinfo(false)
		PushServerParams(false)
		time.Sleep(mtypes.S2TD(1))
	}
}

func RoutineTimeoutCheck() {
	for {
		httpobj.http_super_chains.Event_server_register <- mtypes.RegisterMsg{
			Node_id: mtypes.NodeID_SuperNode,
			Version: "dummy",
		}
		httpobj.http_super_chains.Event_server_pong <- mtypes.PongMsg{
			RequestID:  0,
			Src_nodeID: mtypes.NodeID_SuperNode,
			Dst_nodeID: mtypes.NodeID_SuperNode,
		}
		time.Sleep(httpobj.http_graph.TimeoutCheckInterval)
	}
}

func PushNhTable(force bool) {
	// No lock
	body, err := mtypes.GetByte(mtypes.ServerUpdateMsg{
		Node_id: mtypes.NodeID_SuperNode,
		Action:  mtypes.UpdateNhTable,
		Code:    0,
		Params:  string(httpobj.http_NhTable_Hash[:]),
	})
	if err != nil {
		fmt.Println("Error get byte")
		return
	}
	buf := make([]byte, path.EgHeaderLen+len(body))
	header, _ := path.NewEgHeader(buf[:path.EgHeaderLen], device.DefaultMTU)
	header.SetDst(mtypes.NodeID_SuperNode)
	header.SetSrc(mtypes.NodeID_SuperNode)
	copy(buf[path.EgHeaderLen:], body)
	for pkstr, peerstate := range httpobj.http_PeerState {
		isAlive := peerstate.LastSeen.Load().(time.Time).Add(mtypes.S2TD(httpobj.http_sconfig.PeerAliveTimeout)).After(time.Now())
		if !isAlive && !force {
			continue
		}
		if force || peerstate.NhTableState.Load().(string) != httpobj.http_NhTable_Hash {
			if peer := httpobj.http_device4.LookupPeerByStr(pkstr); peer != nil && peer.GetEndpointDstStr() != "" {
				httpobj.http_device4.SendPacket(peer, path.ServerUpdate, 0, buf, device.MessageTransportOffsetContent)
			}
			if peer := httpobj.http_device6.LookupPeerByStr(pkstr); peer != nil && peer.GetEndpointDstStr() != "" {
				httpobj.http_device6.SendPacket(peer, path.ServerUpdate, 0, buf, device.MessageTransportOffsetContent)
			}
		}
	}
}

func PushPeerinfo(force bool) {
	//No lock
	body, err := mtypes.GetByte(mtypes.ServerUpdateMsg{
		Node_id: mtypes.NodeID_SuperNode,
		Action:  mtypes.UpdatePeer,
		Code:    0,
		Params:  string(httpobj.http_PeerInfo_hash[:]),
	})
	if err != nil {
		fmt.Println("Error get byte")
		return
	}
	buf := make([]byte, path.EgHeaderLen+len(body))
	header, _ := path.NewEgHeader(buf[:path.EgHeaderLen], device.DefaultMTU)
	header.SetDst(mtypes.NodeID_SuperNode)
	header.SetSrc(mtypes.NodeID_SuperNode)
	copy(buf[path.EgHeaderLen:], body)
	for pkstr, peerstate := range httpobj.http_PeerState {
		isAlive := peerstate.LastSeen.Load().(time.Time).Add(mtypes.S2TD(httpobj.http_sconfig.PeerAliveTimeout)).After(time.Now())
		if !isAlive && !force {
			continue
		}
		if force || peerstate.PeerInfoState.Load().(string) != httpobj.http_PeerInfo_hash {
			if peer := httpobj.http_device4.LookupPeerByStr(pkstr); peer != nil {
				httpobj.http_device4.SendPacket(peer, path.ServerUpdate, 0, buf, device.MessageTransportOffsetContent)
			}
			if peer := httpobj.http_device6.LookupPeerByStr(pkstr); peer != nil {
				httpobj.http_device6.SendPacket(peer, path.ServerUpdate, 0, buf, device.MessageTransportOffsetContent)
			}
		}
	}
}

func PushServerParams(force bool) {
	//No lock
	for pkstr, peerstate := range httpobj.http_PeerState {
		isAlive := peerstate.LastSeen.Load().(time.Time).Add(mtypes.S2TD(httpobj.http_sconfig.PeerAliveTimeout)).After(time.Now())
		if !isAlive && !force {
			continue
		}
		if force || peerstate.SuperParamState.Load().(string) != peerstate.SuperParamStateClient.Load().(string) {

			body, err := mtypes.GetByte(mtypes.ServerUpdateMsg{
				Node_id: mtypes.NodeID_SuperNode,
				Action:  mtypes.UpdateSuperParams,
				Code:    0,
				Params:  peerstate.SuperParamState.Load().(string),
			})
			if err != nil {
				fmt.Println("Error get byte")
				return
			}
			buf := make([]byte, path.EgHeaderLen+len(body))
			header, _ := path.NewEgHeader(buf[:path.EgHeaderLen], device.DefaultMTU)
			header.SetDst(mtypes.NodeID_SuperNode)
			header.SetSrc(mtypes.NodeID_SuperNode)
			copy(buf[path.EgHeaderLen:], body)

			if peer := httpobj.http_device4.LookupPeerByStr(pkstr); peer != nil {
				httpobj.http_device4.SendPacket(peer, path.ServerUpdate, 0, buf, device.MessageTransportOffsetContent)
			}
			if peer := httpobj.http_device6.LookupPeerByStr(pkstr); peer != nil {
				httpobj.http_device6.SendPacket(peer, path.ServerUpdate, 0, buf, device.MessageTransportOffsetContent)
			}
		}
	}
}

func startUAPI(interfaceName string, logger *device.Logger, the_device *device.Device, errs chan error) (net.Listener, error) {
	fileUAPI, err := func() (*os.File, error) {
		uapiFdStr := os.Getenv(ENV_EG_UAPI_FD)
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
	if err != nil {
		fmt.Printf("Error create UAPI socket \n")
		return nil, err
	}
	uapi, err := ipc.UAPIListen(interfaceName, fileUAPI)
	if err != nil {
		logger.Errorf("Failed to listen on uapi socket: %v", err)
		return nil, err
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
	logger.Verbosef("UAPI listener started")
	return uapi, err
}
