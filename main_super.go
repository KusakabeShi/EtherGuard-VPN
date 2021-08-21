// +build !windows

/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package main

import (
	"bytes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	"github.com/KusakabeSi/EtherGuardVPN/conn"
	"github.com/KusakabeSi/EtherGuardVPN/device"
	"github.com/KusakabeSi/EtherGuardVPN/ipc"
	"github.com/KusakabeSi/EtherGuardVPN/path"
	"github.com/KusakabeSi/EtherGuardVPN/tap"
	yaml "gopkg.in/yaml.v2"
)

func printExampleSuperConf() {
	sconfig := config.SuperConfig{
		NodeName:   "NodeSuper",
		PrivKeyV4:  "SM8pGjT0r8njy1/7ffN4wMwF7nnJ8UYSjGRWpCqo3ng=",
		PrivKeyV6:  "SM8pGjT0r8njy1/7ffN4wMwF7nnJ8UYSjGRWpCqo3ng=",
		ListenPort: 3000,
		LogLevel: config.LoggerInfo{
			LogLevel:   "normal",
			LogTransit: true,
		},
		Peers: []config.PeerInfo{
			{
				NodeID:   2,
				PubKey:   "NuYJ/3Ght+C4HovFq5Te/BrIazo6zwDJ8Bdu4rQCz0o=",
				EndPoint: "127.0.0.1:3002",
				Static:   true,
			},
		},
		GraphRecalculateSetting: config.GraphRecalculateSetting{
			JitterTolerance:           20,
			JitterToleranceMultiplier: 1.1,
			NodeReportTimeout:         40,
			RecalculateCoolDown:       5,
		},
	}

	soprint, _ := yaml.Marshal(sconfig)
	fmt.Print(string(soprint))
	return
}

func Super(configPath string, useUAPI bool, printExample bool) (err error) {
	if printExample {
		printExampleSuperConf()
		return nil
	}
	var sconfig config.SuperConfig
	err = readYaml(configPath, &sconfig)
	if err != nil {
		fmt.Printf("Error read config: %s :", configPath)
		fmt.Print(err)
		return err
	}
	interfaceName := sconfig.NodeName
	var logLevel int
	switch sconfig.LogLevel.LogLevel {
	case "verbose", "debug":
		logLevel = device.LogLevelVerbose
	case "error":
		logLevel = device.LogLevelError
	case "silent":
		logLevel = device.LogLevelSilent
	}

	logger4 := device.NewLogger(
		logLevel,
		fmt.Sprintf("(%s) ", interfaceName+"_v4"),
	)
	logger6 := device.NewLogger(
		logLevel,
		fmt.Sprintf("(%s) ", interfaceName+"_v6"),
	)

	http_PeerState = make(map[string]*PeerState)
	http_PeerID2Map = make(map[config.Vertex]string)
	http_PeerInfos.Peers = make(map[string]config.HTTP_Peerinfo)

	super_chains := path.SUPER_Events{
		Event_server_pong:            make(chan path.PongMsg, 1<<5),
		Event_server_register:        make(chan path.RegisterMsg, 1<<5),
		Event_server_NhTable_changed: make(chan struct{}, 1<<4),
	}

	thetap, _ := tap.CreateDummyTAP()
	http_graph = path.NewGraph(3, true, sconfig.GraphRecalculateSetting)
	http_device4 = device.NewDevice(thetap, path.SuperNodeMessage, conn.NewCustomBind(true, false), logger4, http_graph, true, "", nil, &sconfig, &super_chains)
	http_device6 = device.NewDevice(thetap, path.SuperNodeMessage, conn.NewCustomBind(false, true), logger6, http_graph, true, "", nil, &sconfig, &super_chains)
	defer http_device4.Close()
	defer http_device6.Close()
	var sk [32]byte
	sk_slice, _ := base64.StdEncoding.DecodeString(sconfig.PrivKeyV4)
	copy(sk[:], sk_slice)
	http_device4.SetPrivateKey(sk)
	sk_slice, _ = base64.StdEncoding.DecodeString(sconfig.PrivKeyV6)
	copy(sk[:], sk_slice)
	http_device6.SetPrivateKey(sk)
	http_device4.IpcSet("fwmark=0\n")
	http_device6.IpcSet("fwmark=0\n")
	http_device4.IpcSet("listen_port=" + strconv.Itoa(sconfig.ListenPort) + "\n")
	http_device6.IpcSet("listen_port=" + strconv.Itoa(sconfig.ListenPort-1) + "\n")
	http_device4.IpcSet("replace_peers=true\n")
	http_device6.IpcSet("replace_peers=true\n")

	for _, peerconf := range sconfig.Peers {
		var pk device.NoisePublicKey

		pk_slice, err := base64.StdEncoding.DecodeString(peerconf.PubKey)
		if err != nil {
			fmt.Println("Error decode base64 ", err)
		}
		copy(pk[:], pk_slice)
		if peerconf.NodeID >= path.SuperNodeMessage {
			return errors.New(fmt.Sprintf("Invalid Node_id at peer %s\n", peerconf.PubKey))
		}
		http_PeerID2Map[peerconf.NodeID] = peerconf.PubKey
		http_PeerInfos.Peers[peerconf.PubKey] = config.HTTP_Peerinfo{
			NodeID:  peerconf.NodeID,
			PubKey:  peerconf.PubKey,
			PSKey:   peerconf.PSKey,
			Connurl: make(map[string]bool),
		}
		peer4, _ := http_device4.NewPeer(pk, peerconf.NodeID)
		peer6, _ := http_device6.NewPeer(pk, peerconf.NodeID)
		if peerconf.PSKey != "" {
			var psk device.NoisePresharedKey
			psk_slice, err := base64.StdEncoding.DecodeString(peerconf.PSKey)
			if err != nil {
				fmt.Println("Error decode base64 ", err)
			}
			copy(psk[:], psk_slice)
			peer4.SetPSK(psk)
			peer6.SetPSK(psk)
		}
		http_PeerState[peerconf.PubKey] = &PeerState{}
	}
	logger4.Verbosef("Device started")

	errs := make(chan error, 1<<3)
	term := make(chan os.Signal, 1)
	if useUAPI {
		uapi4, err := startUAPI(interfaceName+"_v4", logger4, http_device4, errs)
		if err != nil {
			return err
		}
		defer uapi4.Close()
		uapi6, err := startUAPI(interfaceName+"_v6", logger6, http_device6, errs)
		if err != nil {
			return err
		}
		defer uapi6.Close()
	}
	signal.Notify(term, syscall.SIGTERM)
	signal.Notify(term, os.Interrupt)

	go Event_server_event_hendler(http_graph, super_chains)
	go RoutinePushSettings(path.S2TD(sconfig.RePushConfigInterval))
	go HttpServer(sconfig.ListenPort, "/api")

	select {
	case <-term:
	case <-errs:
	case <-http_device4.Wait():
	case <-http_device6.Wait():
	}
	logger4.Verbosef("Shutting down")
	return
}

func Event_server_event_hendler(graph *path.IG, events path.SUPER_Events) {
	for {
		select {
		case reg_msg := <-events.Event_server_register:
			if reg_msg.Init == true {
				copy(http_PeerState[http_PeerID2Map[reg_msg.Node_id]].NhTableState[:], make([]byte, 32))
				copy(http_PeerState[http_PeerID2Map[reg_msg.Node_id]].PeerInfoState[:], make([]byte, 32))
			}
			PubKey := http_PeerID2Map[reg_msg.Node_id]
			if peer := http_device4.LookupPeerByStr(PubKey); peer != nil {
				if connstr := peer.GetEndpointDstStr(); connstr != "" {
					http_PeerInfos.Peers[PubKey].Connurl[connstr] = true
				}
			}
			if peer := http_device6.LookupPeerByStr(PubKey); peer != nil {
				if connstr := peer.GetEndpointDstStr(); connstr != "" {
					http_PeerInfos.Peers[PubKey].Connurl[connstr] = true
				}
			}
			http_PeerInfoStr, _ = json.Marshal(&http_PeerInfos)
			PeerInfo_hash_raw := md5.Sum(http_PeerInfoStr)
			PeerInfo_hash_str := hex.EncodeToString(PeerInfo_hash_raw[:])
			PeerInfo_hash_str_byte := []byte(PeerInfo_hash_str)
			if bytes.Equal(http_PeerInfo_hash[:], PeerInfo_hash_str_byte) == false {
				copy(http_PeerInfo_hash[:], PeerInfo_hash_str_byte)
				PushUpdate()
			}
		case <-events.Event_server_NhTable_changed:
			NhTable := graph.GetNHTable(false)
			NhTablestr, _ := json.Marshal(NhTable)
			md5_hash_raw := md5.Sum(http_NhTableStr)
			new_hash_str := hex.EncodeToString(md5_hash_raw[:])
			new_hash_str_byte := []byte(new_hash_str)
			copy(http_NhTable_Hash[:], new_hash_str_byte)
			http_NhTableStr = NhTablestr
			PushUpdate()
		case pong_msg := <-events.Event_server_pong:
			changed := graph.UpdateLentancy(pong_msg.Src_nodeID, pong_msg.Dst_nodeID, pong_msg.Timediff, true)
			if changed {
				NhTable := graph.GetNHTable(false)
				NhTablestr, _ := json.Marshal(NhTable)
				md5_hash_raw := md5.Sum(http_NhTableStr)
				new_hash_str := hex.EncodeToString(md5_hash_raw[:])
				new_hash_str_byte := []byte(new_hash_str)
				copy(http_NhTable_Hash[:], new_hash_str_byte)
				http_NhTableStr = NhTablestr
				PushNhTable()
			}
		}
	}
}

func RoutinePushSettings(interval time.Duration) {
	for {
		time.Sleep(interval)
		PushNhTable()
		PushUpdate()
	}
}

func PushNhTable() {
	body, err := path.GetByte(path.UpdateNhTableMsg{
		State_hash: http_NhTable_Hash,
	})
	if err != nil {
		fmt.Println("Error get byte")
		return
	}
	buf := make([]byte, path.EgHeaderLen+len(body))
	header, _ := path.NewEgHeader(buf[:path.EgHeaderLen])
	header.SetDst(path.SuperNodeMessage)
	header.SetPacketLength(uint16(len(body)))
	header.SetSrc(path.SuperNodeMessage)
	header.SetTTL(0)
	header.SetUsage(path.UpdateNhTable)
	copy(buf[path.EgHeaderLen:], body)
	for pkstr, peerstate := range http_PeerState {
		if peerstate.NhTableState != http_NhTable_Hash {
			if peer := http_device4.LookupPeerByStr(pkstr); peer != nil {
				http_device4.SendPacket(peer, buf, device.MessageTransportOffsetContent)
			}
			if peer := http_device6.LookupPeerByStr(pkstr); peer != nil {
				http_device6.SendPacket(peer, buf, device.MessageTransportOffsetContent)
			}
		}
	}
}

func PushUpdate() {
	body, err := path.GetByte(path.UpdatePeerMsg{
		State_hash: http_PeerInfo_hash,
	})
	if err != nil {
		fmt.Println("Error get byte")
		return
	}
	buf := make([]byte, path.EgHeaderLen+len(body))
	header, _ := path.NewEgHeader(buf[:path.EgHeaderLen])
	header.SetDst(path.SuperNodeMessage)
	header.SetPacketLength(uint16(len(body)))
	header.SetSrc(path.SuperNodeMessage)
	header.SetTTL(0)
	header.SetUsage(path.UpdatePeer)
	copy(buf[path.EgHeaderLen:], body)
	for pkstr, peerstate := range http_PeerState {
		if peerstate.PeerInfoState != http_PeerInfo_hash {
			if peer := http_device4.LookupPeerByStr(pkstr); peer != nil {
				http_device4.SendPacket(peer, buf, device.MessageTransportOffsetContent)
			}
			if peer := http_device6.LookupPeerByStr(pkstr); peer != nil {
				http_device6.SendPacket(peer, buf, device.MessageTransportOffsetContent)
			}
		}
	}
}

func startUAPI(interfaceName string, logger *device.Logger, the_device *device.Device, errs chan error) (net.Listener, error) {
	fileUAPI, err := func() (*os.File, error) {
		uapiFdStr := os.Getenv(ENV_WP_UAPI_FD)
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
	uapi, err := ipc.UAPIListen(interfaceName, fileUAPI)
	if err != nil {
		logger.Errorf("Failed to listen on uapi socket: %v", err)
		os.Exit(ExitSetupFailed)
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
