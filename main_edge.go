/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package main

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/google/shlex"

	"github.com/KusakabeSi/EtherGuard-VPN/conn"
	"github.com/KusakabeSi/EtherGuard-VPN/device"
	"github.com/KusakabeSi/EtherGuard-VPN/gencfg"
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/path"
	"github.com/KusakabeSi/EtherGuard-VPN/tap"
	yaml "gopkg.in/yaml.v2"
)

func printExampleEdgeConf() {
	tconfig := gencfg.GetExampleEdgeConf("")
	toprint, _ := yaml.Marshal(tconfig)
	fmt.Print(string(toprint))
}

func Edge(configPath string, useUAPI bool, printExample bool, bindmode string) (err error) {
	if printExample {
		printExampleEdgeConf()
		return nil
	}
	var econfig mtypes.EdgeConfig
	//printExampleConf()
	//return

	err = mtypes.ReadYaml(configPath, &econfig)
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
	switch econfig.Interface.IType {
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
		return errors.New("Unknow interface type:" + econfig.Interface.IType)
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
	graph := path.NewGraph(3, false, econfig.DynamicRoute.P2P.GraphRecalculateSetting, econfig.DynamicRoute.NTPConfig, econfig.LogLevel)
	graph.SetNHTable(econfig.NextHopTable)

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
		if econfig.DynamicRoute.SuperNode.EndpointV4 != "" {
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
			peer, err := the_device.NewPeer(pk, mtypes.SuperNodeMessage, true)
			if err != nil {
				return err
			}
			peer.SetPSK(psk)
			err = peer.SetEndpointFromConnURL(econfig.DynamicRoute.SuperNode.EndpointV4, 4, false)
			if err != nil {
				logger.Errorf("Failed to set endpoint for supernode v4 %v: %v", econfig.DynamicRoute.SuperNode.EndpointV4, err)
				S4 = false
			}
		}
		if econfig.DynamicRoute.SuperNode.EndpointV6 != "" {
			pk, err := device.Str2PubKey(econfig.DynamicRoute.SuperNode.PubKeyV6)
			if err != nil {
				fmt.Println("Error decode base64 ", err)
			}
			psk, err := device.Str2PSKey(econfig.DynamicRoute.SuperNode.PSKey)
			if err != nil {
				fmt.Println("Error decode base64 ", err)
				return err
			}
			peer, err := the_device.NewPeer(pk, mtypes.SuperNodeMessage, true)
			if err != nil {
				return err
			}
			peer.SetPSK(psk)
			peer.StaticConn = false
			peer.ConnURL = econfig.DynamicRoute.SuperNode.EndpointV6
			err = peer.SetEndpointFromConnURL(econfig.DynamicRoute.SuperNode.EndpointV6, 6, false)
			if err != nil {
				logger.Errorf("Failed to set endpoint for supernode v6 %v: %v", econfig.DynamicRoute.SuperNode.EndpointV6, err)
				S6 = false
			}
			if !(S4 || S6) {
				return errors.New("Failed to connect to supernode.")
			}
		}
		the_device.Chan_Supernode_OK <- struct{}{}
	}

	logger.Verbosef("Device started")

	errs := make(chan error)
	term := make(chan os.Signal, 1)

	if useUAPI {
		startUAPI(NodeName, logger, the_device, errs)
	}

	if econfig.PostScript != "" {
		envs := make(map[string]string)
		nid := econfig.NodeID
		nid_bytearr := []byte{0, 0}
		MacAddr, _ := tap.GetMacAddr(econfig.Interface.MacAddrPrefix, uint32(nid))
		binary.LittleEndian.PutUint16(nid_bytearr, uint16(nid))

		envs["EG_MODE"] = "edge"
		envs["EG_NODE_NAME"] = econfig.NodeName
		envs["EG_NODE_ID_INT_DEC"] = fmt.Sprintf("%d", nid)
		envs["EG_NODE_ID_BYTE0_DEC"] = fmt.Sprintf("%d", nid_bytearr[0])
		envs["EG_NODE_ID_BYTE1_DEC"] = fmt.Sprintf("%d", nid_bytearr[1])
		envs["EG_NODE_ID_INT_HEX"] = fmt.Sprintf("%x", nid)
		envs["EG_NODE_ID_BYTE0_HEX"] = fmt.Sprintf("%X", nid_bytearr[0])
		envs["EG_NODE_ID_BYTE1_HEX"] = fmt.Sprintf("%X", nid_bytearr[1])
		envs["EG_INTERFACE_NAME"] = econfig.Interface.Name
		envs["EG_INTERFACE_TYPE"] = econfig.Interface.IType
		envs["EG_INTERFACE_MAC_PREFIX"] = econfig.Interface.MacAddrPrefix
		envs["EG_INTERFACE_MAC_ADDR"] = MacAddr.String()

		cmdarg, err := shlex.Split(econfig.PostScript)
		if err != nil {
			return fmt.Errorf("Error parse PostScript %v\n", err)
		}
		if econfig.LogLevel.LogInternal {
			fmt.Printf("PostScript: exec.Command(%v)\n", cmdarg)
		}
		cmd := exec.Command(cmdarg[0], cmdarg[1:]...)
		cmd.Env = os.Environ()
		for k, v := range envs {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			return fmt.Errorf("exec.Command(%v) failed with %v\n", cmdarg, err)
		}
		if econfig.LogLevel.LogInternal {
			fmt.Printf("PostScript output: %s\n", string(out))
		}
	}
	mtypes.SdNotify(false, mtypes.SdNotifyReady)

	// wait for program to terminate
	signal.Notify(term, syscall.SIGTERM)
	signal.Notify(term, os.Interrupt)

	select {
	case <-term:
	case <-errs:
	case errcode := <-the_device.Wait():
		if errcode != 0 {
			return syscall.Errno(errcode)
		}
	}
	logger.Verbosef("Shutting down")
	return
}
