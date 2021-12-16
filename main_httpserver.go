/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 Kusakabe Si. All Rights Reserved.
 */

package main

import (
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"net/http"
	"net/url"

	"github.com/golang-jwt/jwt"
	"golang.org/x/crypto/sha3"

	"github.com/KusakabeSi/EtherGuard-VPN/conn"
	"github.com/KusakabeSi/EtherGuard-VPN/device"
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/path"
	yaml "gopkg.in/yaml.v2"
)

type http_shared_objects struct {
	http_graph         *path.IG
	http_device4       *device.Device
	http_device6       *device.Device
	http_HashSalt      []byte
	http_NhTable_Hash  string
	http_PeerInfo_hash string
	http_NhTableStr    []byte
	http_PeerInfo      mtypes.API_Peers
	http_super_chains  *mtypes.SUPER_Events
	http_pskdb         device.PSKDB

	http_passwords       mtypes.Passwords
	http_StateExpire     time.Time
	http_StateString_tmp []byte

	http_PeerID2Info map[mtypes.Vertex]mtypes.SuperPeerInfo
	http_PeerState   map[string]*PeerState //the state hash reported by peer
	http_PeerIPs     map[string]*HttpPeerLocalIP

	http_sconfig *mtypes.SuperConfig

	http_sconfig_path string
	http_econfig_tmp  *mtypes.EdgeConfig

	sync.RWMutex
}

var (
	httpobj http_shared_objects
)

type HttpPeerLocalIP struct {
	LocalIPv4 map[string]float64
	LocalIPv6 map[string]float64
}

type HttpState struct {
	PeerInfo map[mtypes.Vertex]HttpPeerInfo
	Infinity float64
	Edges    map[mtypes.Vertex]map[mtypes.Vertex]float64
	Edges_Nh map[mtypes.Vertex]map[mtypes.Vertex]float64
	NhTable  mtypes.NextHopTable
	Dist     mtypes.DistTable
}

type HttpPeerInfo struct {
	Name     string
	LastSeen string
}

type PeerState struct {
	NhTableState          atomic.Value // string
	PeerInfoState         atomic.Value // string
	SuperParamState       atomic.Value // string
	SuperParamStateClient atomic.Value // string
	JETSecret             atomic.Value // mtypes.JWTSecret
	httpPostCount         atomic.Value // uint64
	LastSeen              atomic.Value // time.Time
}

func extractParamsStr(params url.Values, key string, w http.ResponseWriter) (string, error) {
	valA, has := params[key]
	if !has {
		errstr := fmt.Sprintf("Paramater %v: Missing paramater.", key)
		if w != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(errstr))
		}
		return "", fmt.Errorf(errstr)
	}
	return valA[0], nil
}

func extractParamsFloat(params url.Values, key string, bitSize int, w http.ResponseWriter) (float64, error) {
	val, err := extractParamsStr(params, key, w)
	if err != nil {
		return 0, err
	}
	ret, err := strconv.ParseFloat(val, 64)
	if err != nil {
		errstr := fmt.Sprintf("Paramater %v: Can't convert to type float%v", key, bitSize)
		if w != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(errstr))
		}
		return 0, fmt.Errorf(errstr)
	}
	return ret, nil
}

func extractParamsUint(params url.Values, key string, bitSize int, w http.ResponseWriter) (uint64, error) {
	val, err := extractParamsStr(params, key, w)
	if err != nil {
		return 0, err
	}
	ret, err := strconv.ParseUint(val, 10, bitSize)
	if err != nil {
		errstr := fmt.Sprintf("Paramater %v: Can't convert to type uint%v", key, bitSize)
		if w != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(errstr))
		}
		return 0, fmt.Errorf(errstr)
	}
	return ret, nil
}

func extractParamsVertex(params url.Values, key string, w http.ResponseWriter) (mtypes.Vertex, error) {
	val, err := extractParamsUint(params, key, 16, w)
	if err != nil {
		return mtypes.NodeID_Invalid, err
	}
	return mtypes.Vertex(val), nil
}

func get_api_peers(old_State_hash string) (api_peerinfo mtypes.API_Peers, StateHash string, changed bool) {
	// No lock
	api_peerinfo = make(mtypes.API_Peers)
	for _, peerinfo := range httpobj.http_sconfig.Peers {
		connV4 := httpobj.http_device4.GetConnurl(peerinfo.NodeID)
		connV6 := httpobj.http_device6.GetConnurl(peerinfo.NodeID)

		if peerinfo.ExternalIP != "" {
			ExternalIP := peerinfo.ExternalIP
			if strings.Contains(ExternalIP, ":") {
				ExternalIP = fmt.Sprintf("[%v]", ExternalIP)
			}
			if strings.Contains(connV4, ":") {
				hostport := strings.Split(connV4, ":")
				ExternalIP = ExternalIP + ":" + hostport[len(hostport)-1]
				_, ExternalEndPoint_v4, err := conn.LookupIP(ExternalIP, 4)
				if err == nil {
					connV4 = ExternalEndPoint_v4
				}
			}
			if strings.Contains(connV6, ":") {
				hostport := strings.Split(connV6, ":")
				ExternalIP = ExternalIP + ":" + hostport[len(hostport)-1]
				_, ExternalEndPoint_v6, err := conn.LookupIP(ExternalIP, 6)
				if err == nil {
					connV6 = ExternalEndPoint_v6
				}
			}
		}

		if len(connV4)+len(connV6) == 0 {
			continue
		}
		api_peerinfo[peerinfo.PubKey] = mtypes.API_Peerinfo{
			NodeID:  peerinfo.NodeID,
			PSKey:   peerinfo.PSKey,
			Connurl: &mtypes.API_connurl{},
		}
		if httpobj.http_PeerState[peerinfo.PubKey].LastSeen.Load().(time.Time).Add(mtypes.S2TD(httpobj.http_sconfig.PeerAliveTimeout)).After(time.Now()) {
			if connV4 != "" {
				api_peerinfo[peerinfo.PubKey].Connurl.ExternalV4 = map[string]float64{connV4: 4}
			}
			if connV6 != "" {
				api_peerinfo[peerinfo.PubKey].Connurl.ExternalV6 = map[string]float64{connV6: 6}
			}
			if !peerinfo.SkipLocalIP {
				api_peerinfo[peerinfo.PubKey].Connurl.LocalV4 = httpobj.http_PeerIPs[peerinfo.PubKey].LocalIPv4
				api_peerinfo[peerinfo.PubKey].Connurl.LocalV6 = httpobj.http_PeerIPs[peerinfo.PubKey].LocalIPv6
			}
		}
	}
	api_peerinfo_str_byte, _ := json.Marshal(&api_peerinfo)
	hash_raw := md5.Sum(append(api_peerinfo_str_byte, httpobj.http_HashSalt...))
	hash_str := hex.EncodeToString(hash_raw[:])
	StateHash = hash_str
	if old_State_hash != StateHash {
		changed = true
	}
	return
}

func edge_get_superparams(w http.ResponseWriter, r *http.Request) {
	// Read all params
	params := r.URL.Query()
	PubKey, err := extractParamsStr(params, "PubKey", w)
	if err != nil {
		return
	}
	State, err := extractParamsStr(params, "State", w)
	if err != nil {
		return
	}
	NodeID, err := extractParamsVertex(params, "NodeID", w)
	if err != nil {
		return
	}
	if NodeID >= mtypes.NodeID_Special {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Paramater NodeID: Can't use special nodeID."))
		return
	}
	// Authentication
	httpobj.RLock()
	defer httpobj.RUnlock()
	if _, has := httpobj.http_PeerID2Info[NodeID]; !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater PubKey: NodeID and PubKey are not match"))
		return
	}
	if httpobj.http_PeerID2Info[NodeID].PubKey != PubKey {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater PubKey: NodeID and PubKey are not match"))
		return
	}

	if _, has := httpobj.http_PeerState[PubKey]; !has {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Paramater PubKey: Not found in httpobj.http_PeerState, this shouldn't happen. Please report to the author."))
		return
	}

	if httpobj.http_PeerState[PubKey].SuperParamState.Load().(string) != State {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater State: State not correct"))
		return
	}
	// Do something
	SuperParams := mtypes.API_SuperParams{
		SendPingInterval:  httpobj.http_sconfig.SendPingInterval,
		HttpPostInterval:  httpobj.http_sconfig.HttpPostInterval,
		PeerAliveTimeout:  httpobj.http_sconfig.PeerAliveTimeout,
		AdditionalCost:    httpobj.http_PeerID2Info[NodeID].AdditionalCost,
		DampingResistance: httpobj.http_sconfig.DampingResistance,
	}
	SuperParamStr, _ := json.Marshal(SuperParams)
	httpobj.http_PeerState[PubKey].SuperParamStateClient.Store(State)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(SuperParamStr))
}

func edge_get_peerinfo(w http.ResponseWriter, r *http.Request) {
	// Read all params
	params := r.URL.Query()
	PubKey, err := extractParamsStr(params, "PubKey", w)
	if err != nil {
		return
	}
	State, err := extractParamsStr(params, "State", w)
	if err != nil {
		return
	}
	NodeID, err := extractParamsVertex(params, "NodeID", w)
	if err != nil {
		return
	}
	if NodeID >= mtypes.NodeID_Special {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Paramater NodeID: Can't use special nodeID."))
		return
	}
	// Authentication
	httpobj.RLock()
	defer httpobj.RUnlock()
	if _, has := httpobj.http_PeerID2Info[NodeID]; !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater PubKey: NodeID and PubKey are not match"))
		return
	}
	if httpobj.http_PeerID2Info[NodeID].PubKey != PubKey {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater PubKey: NodeID and PubKey are not match"))
		return
	}
	if httpobj.http_PeerInfo_hash != State {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater State: State not correct"))
		return
	}
	if _, has := httpobj.http_PeerState[PubKey]; !has {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Paramater PubKey: Not found in httpobj.http_PeerState, this shouldn't happen. Please report to the author."))
		return
	}

	// Do something
	httpobj.http_PeerState[PubKey].PeerInfoState.Store(State)
	http_PeerInfo_2peer := make(mtypes.API_Peers)

	for PeerPubKey, peerinfo := range httpobj.http_PeerInfo {
		if httpobj.http_sconfig.UsePSKForInterEdge {
			if NodeID == peerinfo.NodeID {
				continue
			}
			PSK := httpobj.http_pskdb.GetPSK(NodeID, peerinfo.NodeID)
			peerinfo.PSKey = PSK.ToString()
		} else {
			peerinfo.PSKey = ""
		}
		if httpobj.http_PeerID2Info[NodeID].SkipLocalIP { // Clear all local IP
			peerinfo.Connurl.LocalV4 = make(map[string]float64)
			peerinfo.Connurl.LocalV6 = make(map[string]float64)
		}
		http_PeerInfo_2peer[PeerPubKey] = peerinfo
	}
	api_peerinfo_str_byte, _ := json.Marshal(&http_PeerInfo_2peer)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write(api_peerinfo_str_byte)
}

func edge_get_nhtable(w http.ResponseWriter, r *http.Request) {
	// Read all params
	params := r.URL.Query()
	PubKey, err := extractParamsStr(params, "PubKey", w)
	if err != nil {
		return
	}
	State, err := extractParamsStr(params, "State", w)
	if err != nil {
		return
	}
	NodeID, err := extractParamsVertex(params, "NodeID", w)
	if err != nil {
		return
	}
	if NodeID >= mtypes.NodeID_Special {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Paramater NodeID: Can't use special nodeID."))
		return
	}
	// Authentication
	httpobj.RLock()
	defer httpobj.RUnlock()
	if _, has := httpobj.http_PeerID2Info[NodeID]; !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater PubKey: NodeID and PubKey are not match"))
		return
	}
	if httpobj.http_PeerID2Info[NodeID].PubKey != PubKey {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater PubKey: NodeID and PubKey are not match"))
		return
	}
	if httpobj.http_NhTable_Hash != State {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater State: State not correct"))
		return
	}
	if _, has := httpobj.http_PeerState[PubKey]; !has {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Paramater PubKey: Not found in httpobj.http_PeerState, this shouldn't happen. Please report to the author."))
		return
	}

	httpobj.http_PeerState[PubKey].NhTableState.Store(State)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(httpobj.http_NhTableStr))
}

func edge_post_nodeinfo(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	NodeID, err := extractParamsVertex(params, "NodeID", w)
	if err != nil {
		return
	}
	PubKey, err := extractParamsStr(params, "PubKey", w)
	if err != nil {
		return
	}
	if NodeID >= mtypes.NodeID_Special {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Paramater NodeID: Can't use special nodeID."))
		return
	}

	JWTSig, err := extractParamsStr(params, "JWTSig", w)
	if err != nil {
		return
	}

	httpobj.RLock()
	defer httpobj.RUnlock()
	if _, has := httpobj.http_PeerID2Info[NodeID]; !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("NodeID and PunKey are not match"))
		return
	}
	if httpobj.http_PeerID2Info[NodeID].PubKey != PubKey {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("NodeID and PunKey are not match"))
		return
	}

	JWTSecret := httpobj.http_PeerState[PubKey].JETSecret
	httpPostCount := httpobj.http_PeerState[PubKey].httpPostCount

	client_body, err := ioutil.ReadAll(r.Body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Request body: Error reading request body: %v", err)))
		return
	}

	token_claims := mtypes.API_report_peerinfo_jwt_claims{}

	token, err := jwt.ParseWithClaims(string(JWTSig), &token_claims, func(token *jwt.Token) (interface{}, error) {
		// Don't forget to validate the alg is what you expect:
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", token.Header["alg"])
		}
		JWTSecretB := JWTSecret.Load().(mtypes.JWTSecret)
		return JWTSecretB[:], nil
	})
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Paramater JWTSig: Signature verification failed: %v", err)))
		return
	}
	if !token.Valid {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Paramater JWTSig: Signature verification failed: Invalid token"))
		return
	}

	client_PostCount := token_claims.PostCount
	client_body_hash := token_claims.BodyHash

	if client_PostCount < httpPostCount.Load().(uint64) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Request body: postcount too small: %v", httpPostCount)))
		return
	}

	calculated_body_hash := sha3.Sum512(client_body)
	if base64.StdEncoding.EncodeToString(calculated_body_hash[:]) == client_body_hash {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Request body: hash not match: %v", client_body_hash)))
		return
	}

	client_body, err = mtypes.GUzip(client_body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Request body: gzip unzip failed"))
		return
	}
	client_report, err := mtypes.ParseAPI_report_peerinfo(client_body)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Request body: Error parsing request body: %v", err)))
		return
	}

	httpobj.http_PeerIPs[PubKey].LocalIPv4 = client_report.LocalV4s
	httpobj.http_PeerIPs[PubKey].LocalIPv6 = client_report.LocalV6s
	httpobj.http_PeerState[PubKey].httpPostCount.Store(client_PostCount + 1)
	httpobj.http_PeerState[PubKey].LastSeen.Store(time.Now())

	applied_pones := make([]mtypes.PongMsg, 0, len(client_report.Pongs))
	for _, pong_msg := range client_report.Pongs {
		if pong_msg.Src_nodeID != NodeID {
			continue
		}

		if info, has := httpobj.http_PeerID2Info[pong_msg.Dst_nodeID]; has {
			AdditionalCost_use := info.AdditionalCost

			if AdditionalCost_use >= 0 {
				pong_msg.AdditionalCost = AdditionalCost_use
			}
			applied_pones = append(applied_pones, pong_msg)
			if httpobj.http_sconfig.LogLevel.LogControl {
				fmt.Printf("Control: Recv %v S:%v D:%v From: %v(HTTP) IP:%v\n", pong_msg.ToString(), pong_msg.Src_nodeID.ToString(), pong_msg.Dst_nodeID.ToString(), NodeID.ToString(), r.RemoteAddr)
			}
		}
	}
	changed := httpobj.http_graph.UpdateLatencyMulti(applied_pones, true, true)
	if changed {
		NhTable := httpobj.http_graph.GetNHTable(true)
		NhTablestr, _ := json.Marshal(NhTable)
		md5_hash_raw := md5.Sum(append(NhTablestr, httpobj.http_HashSalt...))
		new_hash_str := hex.EncodeToString(md5_hash_raw[:])
		httpobj.http_NhTable_Hash = new_hash_str
		httpobj.http_NhTableStr = NhTablestr
		PushNhTable(false)
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func checkPassword(s1 string, s2 string) bool {
	b1 := []byte(s1)
	b2 := []byte(s2)
	if len(b1) == 0 || len(b2) == 0 {
		return false
	}
	if len(b1) != len(b2) {
		aaa := 0
		for _, c := range b1 {
			if c != b2[0] {
				aaa += 1
			}
		}
		return false
	}
	pass := true
	for i, c := range b1 {
		if c != b2[i] {
			pass = false
		}
	}
	return pass
}

func manage_get_peerstate(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	password, err := extractParamsStr(params, "Password", w)
	if err != nil {
		return
	}
	if !checkPassword(password, httpobj.http_passwords.ShowState) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Paramater Password: Wrong password"))
		return
	}
	httpobj.RLock()
	defer httpobj.RUnlock()
	if time.Now().After(httpobj.http_StateExpire) {
		hs := HttpState{
			PeerInfo: make(map[mtypes.Vertex]HttpPeerInfo),
			NhTable:  httpobj.http_graph.GetNHTable(false),
			Infinity: mtypes.Infinity,
			Edges:    httpobj.http_graph.GetEdges(false, false),
			Edges_Nh: httpobj.http_graph.GetEdges(true, true),
			Dist:     httpobj.http_graph.GetDtst(),
		}

		for _, peerinfo := range httpobj.http_sconfig.Peers {
			LastSeenStr := httpobj.http_PeerState[peerinfo.PubKey].LastSeen.Load().(time.Time).String()
			hs.PeerInfo[peerinfo.NodeID] = HttpPeerInfo{
				Name:     peerinfo.Name,
				LastSeen: LastSeenStr,
			}
		}
		httpobj.http_StateExpire = time.Now().Add(5 * time.Second)
		httpobj.http_StateString_tmp, _ = json.Marshal(hs)
	}
	w.WriteHeader(http.StatusOK)
	w.Write(httpobj.http_StateString_tmp)
}

func manage_peeradd(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	password, err := extractParamsStr(params, "Password", w)
	if err != nil {
		return
	}
	if !checkPassword(password, httpobj.http_passwords.AddPeer) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Paramater Password: Wrong password"))
		return
	}

	r.ParseForm()
	NodeID, err := extractParamsVertex(r.Form, "NodeID", w)
	if err != nil {
		return
	}
	Name, err := extractParamsStr(r.Form, "Name", w)
	if err != nil {
		return
	}

	AdditionalCost, err := extractParamsFloat(r.Form, "AdditionalCost", 64, w)
	if err != nil {
		return
	}

	PubKey, err := extractParamsStr(r.Form, "PubKey", w)
	if err != nil {
		return
	}

	SkipLocalIPS, err := extractParamsStr(r.Form, "SkipLocalIP", w)
	if err != nil {
		return
	}

	SkipLocalIP := strings.EqualFold(SkipLocalIPS, "true")

	PSKey, _ := extractParamsStr(r.Form, "PSKey", nil)

	httpobj.Lock()
	defer httpobj.Unlock()

	for _, peerinfo := range httpobj.http_sconfig.Peers {
		if peerinfo.NodeID == NodeID {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Paramater NodeID: NodeID exists"))
			return
		}
		if peerinfo.Name == Name {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Paramater Name: Node name exists"))
			return
		}
		if peerinfo.PubKey == PubKey {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Paramater PubKey: PubKey exists"))
			return
		}
	}
	if httpobj.http_sconfig.GraphRecalculateSetting.StaticMode {
		NhTableStr := r.Form.Get("NextHopTable")
		if NhTableStr == "" {
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte("Paramater NextHopTable: Your NextHopTable is in static mode.\nPlease provide your new NextHopTable in \"NextHopTable\" parmater in json format"))
			return
		}
		var NewNhTable mtypes.NextHopTable
		err := json.Unmarshal([]byte(NhTableStr), &NewNhTable)
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte(fmt.Sprintf("Paramater NextHopTable: \"%v\", %v", NhTableStr, err)))
			return
		}
		err = checkNhTable(NewNhTable, append(httpobj.http_sconfig.Peers, mtypes.SuperPeerInfo{
			NodeID:         NodeID,
			Name:           Name,
			PubKey:         PubKey,
			PSKey:          PSKey,
			AdditionalCost: AdditionalCost,
			SkipLocalIP:    SkipLocalIP,
		}))
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte(fmt.Sprintf("Paramater nexthoptable: \"%v\", %v", NhTableStr, err)))
			return
		}
		httpobj.http_graph.SetNHTable(NewNhTable)
	}
	err = super_peeradd(mtypes.SuperPeerInfo{
		NodeID:         NodeID,
		Name:           Name,
		PubKey:         PubKey,
		PSKey:          PSKey,
		AdditionalCost: AdditionalCost,
		SkipLocalIP:    SkipLocalIP,
	})
	if err != nil {
		w.WriteHeader(http.StatusExpectationFailed)
		w.Write([]byte(fmt.Sprintf("Error creating peer: %v", err)))
		return
	}
	httpobj.http_sconfig.Peers = append(httpobj.http_sconfig.Peers, mtypes.SuperPeerInfo{
		NodeID:         NodeID,
		Name:           Name,
		PubKey:         PubKey,
		PSKey:          PSKey,
		AdditionalCost: AdditionalCost,
		SkipLocalIP:    SkipLocalIP,
	})
	mtypesBytes, _ := yaml.Marshal(httpobj.http_sconfig)
	ioutil.WriteFile(httpobj.http_sconfig_path, mtypesBytes, 0644)
	httpobj.http_econfig_tmp.NodeID = NodeID
	httpobj.http_econfig_tmp.NodeName = Name
	httpobj.http_econfig_tmp.PrivKey = "Your_Private_Key"
	httpobj.http_econfig_tmp.DynamicRoute.SuperNode.PSKey = PSKey
	httpobj.http_econfig_tmp.DynamicRoute.AdditionalCost = AdditionalCost
	httpobj.http_econfig_tmp.DynamicRoute.SuperNode.SkipLocalIP = SkipLocalIP
	httpobj.http_econfig_tmp.NextHopTable = make(mtypes.NextHopTable)
	httpobj.http_econfig_tmp.Peers = make([]mtypes.PeerInfo, 0)
	ret_str_byte, _ := yaml.Marshal(&httpobj.http_econfig_tmp)
	w.WriteHeader(http.StatusOK)
	w.Write(ret_str_byte)
}

func manage_peerupdate(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	toUpdate := mtypes.NodeID_Broadcast

	var err error
	var NodeID mtypes.Vertex

	password, err := extractParamsStr(params, "Password", w)
	if err != nil {
		return
	}
	if !checkPassword(password, httpobj.http_passwords.UpdatePeer) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Paramater Password: Wrong password"))
		return
	}
	NodeID, err = extractParamsVertex(params, "NodeID", w)
	if err != nil {
		return
	}
	toUpdate = NodeID
	httpobj.Lock()
	defer httpobj.Unlock()
	if _, has := httpobj.http_PeerID2Info[toUpdate]; !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("Paramater NodeID: \"%v\" not found", NodeID)))
		return
	}
	PubKey := httpobj.http_PeerID2Info[toUpdate].PubKey
	Updated_params := make(map[string]string)
	new_superpeerinfo := httpobj.http_PeerID2Info[toUpdate]
	r.ParseForm()
	AdditionalCost, err := extractParamsFloat(r.Form, "AdditionalCost", 64, nil)
	if err == nil {
		Updated_params["AdditionalCost"] = fmt.Sprintf("%v", AdditionalCost)
		new_superpeerinfo.AdditionalCost = AdditionalCost
	}
	SkipLocalIP, err := extractParamsStr(r.Form, "SkipLocalIP", nil)
	if err == nil {
		SkipLocalIPVal := strings.EqualFold(SkipLocalIP, "true")
		Updated_params["SkipLocalIP"] = fmt.Sprintf("%v", SkipLocalIPVal)
		new_superpeerinfo.SkipLocalIP = SkipLocalIPVal

	}
	if len(Updated_params) == 0 {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("NodeID: " + toUpdate.ToString() + " , no any paramater updated.\n"))
		return
	}

	httpobj.http_PeerID2Info[toUpdate] = new_superpeerinfo
	SuperParams := mtypes.API_SuperParams{
		SendPingInterval:  httpobj.http_sconfig.SendPingInterval,
		HttpPostInterval:  httpobj.http_sconfig.HttpPostInterval,
		PeerAliveTimeout:  httpobj.http_sconfig.PeerAliveTimeout,
		DampingResistance: httpobj.http_sconfig.DampingResistance,
		AdditionalCost:    new_superpeerinfo.AdditionalCost,
	}

	SuperParamStr, _ := json.Marshal(SuperParams)
	md5_hash_raw := md5.Sum(append(SuperParamStr, httpobj.http_HashSalt...))
	new_hash_str := hex.EncodeToString(md5_hash_raw[:])
	httpobj.http_PeerState[PubKey].SuperParamState.Store(new_hash_str)

	var peers_new []mtypes.SuperPeerInfo
	for _, peerinfo := range httpobj.http_sconfig.Peers {
		if peerinfo.NodeID == toUpdate {
			peers_new = append(peers_new, new_superpeerinfo)
		} else {
			peers_new = append(peers_new, peerinfo)
		}
	}
	httpobj.http_sconfig.Peers = peers_new
	mtypesBytes, _ := yaml.Marshal(httpobj.http_sconfig)
	ioutil.WriteFile(httpobj.http_sconfig_path, mtypesBytes, 0644)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("NodeID: " + toUpdate.ToString() + " updated following values:\n"))
	for k, v := range Updated_params {
		w.Write([]byte(fmt.Sprintf("%v = %v\n", k, v)))
	}
}

func manage_superupdate(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	var err error

	password, err := extractParamsStr(params, "Password", w)
	if err != nil {
		return
	}
	if !checkPassword(password, httpobj.http_passwords.UpdateSuper) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Paramater Password: Wrong password"))
		return
	}

	r.ParseForm()
	Updated_params := make(map[string]string)

	sconfig_temp := mtypes.SuperConfig{}
	sconfig_temp.PeerAliveTimeout = httpobj.http_sconfig.PeerAliveTimeout
	sconfig_temp.SendPingInterval = httpobj.http_sconfig.SendPingInterval
	sconfig_temp.HttpPostInterval = httpobj.http_sconfig.HttpPostInterval
	sconfig_temp.DampingResistance = httpobj.http_sconfig.DampingResistance

	PeerAliveTimeout, err := extractParamsFloat(r.Form, "PeerAliveTimeout", 64, nil)
	if err == nil {
		if PeerAliveTimeout <= 0 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("Paramater PeerAliveTimeout %v: Must > 0.\n", PeerAliveTimeout)))
			return
		}
		Updated_params["PeerAliveTimeout"] = fmt.Sprintf("%v", PeerAliveTimeout)
		sconfig_temp.PeerAliveTimeout = PeerAliveTimeout
	}

	DampingResistance, err := extractParamsFloat(r.Form, "DampingResistance", 64, nil)
	if err == nil {
		if DampingResistance < 0 || DampingResistance >= 1 {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("Paramater DampingResistance %v: Must in range [0,1)\n", DampingResistance)))
			return
		}
		Updated_params["DampingResistance"] = fmt.Sprintf("%v", DampingResistance)
		sconfig_temp.DampingResistance = DampingResistance
	}

	SendPingInterval, err := extractParamsFloat(r.Form, "SendPingInterval", 64, nil)
	if err == nil {
		if SendPingInterval <= 0 || SendPingInterval >= sconfig_temp.PeerAliveTimeout {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("Paramater SendPingInterval: Must > 0 and < %v(PeerAliveTimeout).\n", sconfig_temp.PeerAliveTimeout)))
			return
		}
		Updated_params["SendPingInterval"] = fmt.Sprintf("%v", SendPingInterval)
		sconfig_temp.SendPingInterval = SendPingInterval
	}
	HttpPostInterval, err := extractParamsFloat(r.Form, "HttpPostInterval", 64, nil)
	if err == nil {
		if SendPingInterval <= 0 || SendPingInterval >= sconfig_temp.PeerAliveTimeout {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(fmt.Sprintf("Paramater HttpPostInterval: Must > 0 and < %v(PeerAliveTimeout).\n", sconfig_temp.PeerAliveTimeout)))
			return
		}
		Updated_params["HttpPostInterval"] = fmt.Sprintf("%v", HttpPostInterval)
		sconfig_temp.HttpPostInterval = HttpPostInterval
	}

	if len(Updated_params) == 0 {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("SuperNode: no any paramater updated.\n"))
		return
	}

	httpobj.http_sconfig.PeerAliveTimeout = sconfig_temp.PeerAliveTimeout
	httpobj.http_sconfig.SendPingInterval = sconfig_temp.SendPingInterval
	httpobj.http_sconfig.HttpPostInterval = sconfig_temp.HttpPostInterval
	httpobj.http_sconfig.DampingResistance = sconfig_temp.DampingResistance

	SuperParams := mtypes.API_SuperParams{
		SendPingInterval:  httpobj.http_sconfig.SendPingInterval,
		HttpPostInterval:  httpobj.http_sconfig.HttpPostInterval,
		PeerAliveTimeout:  httpobj.http_sconfig.PeerAliveTimeout,
		DampingResistance: httpobj.http_sconfig.DampingResistance,
		AdditionalCost:    10,
	}
	httpobj.Lock()
	defer httpobj.Unlock()
	for _, peerinfo := range httpobj.http_PeerID2Info {
		SuperParams.AdditionalCost = peerinfo.AdditionalCost
		PubKey := peerinfo.PubKey
		SuperParamStr, _ := json.Marshal(SuperParams)
		md5_hash_raw := md5.Sum(append(SuperParamStr, httpobj.http_HashSalt...))
		new_hash_str := hex.EncodeToString(md5_hash_raw[:])
		httpobj.http_PeerState[PubKey].SuperParamState.Store(new_hash_str)
	}

	mtypesBytes, _ := yaml.Marshal(httpobj.http_sconfig)
	ioutil.WriteFile(httpobj.http_sconfig_path, mtypesBytes, 0644)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Supernode: updated following values:\n"))
	for k, v := range Updated_params {
		w.Write([]byte(fmt.Sprintf("%v = %v\n", k, v)))
	}
}

func manage_peerdel(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	toDelete := mtypes.NodeID_Broadcast

	var err error
	var NodeID mtypes.Vertex
	var PrivKey string
	var PubKey string
	password, pwderr := extractParamsStr(params, "Password", nil)
	httpobj.Lock()
	defer httpobj.Unlock()
	if pwderr == nil { // user provide the password
		if checkPassword(password, httpobj.http_passwords.DelPeer) {
			NodeID, err = extractParamsVertex(params, "NodeID", w)
			if err != nil {
				return
			}
			toDelete = NodeID
			if _, has := httpobj.http_PeerID2Info[toDelete]; !has {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(fmt.Sprintf("Paramater NodeID: \"%v\" not found", NodeID)))
				return
			}
		} else {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte("Paramater Password: Wrong password"))
			return
		}
	} else { // user don't provide the password
		PrivKey, err = extractParamsStr(params, "PrivKey", w)
		if err != nil {
			return
		}
		privk, err := device.Str2PriKey(PrivKey)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprintf("Paramater PrivKey: %v", err)))
			return
		}
		pubk := privk.PublicKey()
		PubKey = pubk.ToString()
		for _, peerinfo := range httpobj.http_sconfig.Peers {
			if peerinfo.PubKey == PubKey {
				toDelete = peerinfo.NodeID
			}
		}
		if toDelete == mtypes.NodeID_Broadcast {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(fmt.Sprintf("Paramater PrivKey: \"%v\" not found", PubKey)))
			return
		}
	}

	var peers_new []mtypes.SuperPeerInfo
	for _, peerinfo := range httpobj.http_sconfig.Peers {
		if peerinfo.NodeID == toDelete {
			super_peerdel(peerinfo.NodeID)
		} else {
			peers_new = append(peers_new, peerinfo)
		}
	}

	httpobj.http_sconfig.Peers = peers_new
	mtypesBytes, _ := yaml.Marshal(httpobj.http_sconfig)
	ioutil.WriteFile(httpobj.http_sconfig_path, mtypesBytes, 0644)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("NodeID: " + toDelete.ToString() + " deleted."))
}

func HttpServer(edgeListen string, manageListen string, apiprefix string, errchan chan error) {
	if len(apiprefix) > 0 && apiprefix[0] != '/' {
		apiprefix = "/" + apiprefix
	}
	if len(edgeListen) > 0 && edgeListen[0] != ':' {
		edgeListen = ":" + edgeListen
	}
	if len(manageListen) > 0 && manageListen[0] != ':' {
		manageListen = ":" + manageListen
	}
	if edgeListen == manageListen {
		mux := http.NewServeMux()
		mux.HandleFunc(apiprefix+"/edge/superparams", edge_get_superparams)
		mux.HandleFunc(apiprefix+"/edge/peerinfo", edge_get_peerinfo)
		mux.HandleFunc(apiprefix+"/edge/nhtable", edge_get_nhtable)
		mux.HandleFunc(apiprefix+"/edge/post/nodeinfo", edge_post_nodeinfo)
		mux.HandleFunc(apiprefix+"/manage/peer/add", manage_peeradd)
		mux.HandleFunc(apiprefix+"/manage/peer/del", manage_peerdel)
		mux.HandleFunc(apiprefix+"/manage/peer/update", manage_peerupdate)
		mux.HandleFunc(apiprefix+"/manage/super/state", manage_get_peerstate)
		mux.HandleFunc(apiprefix+"/manage/super/update", manage_superupdate)

		go func() {
			err := http.ListenAndServe(edgeListen, mux)
			if err != nil {
				errchan <- err
			}
		}()
		return
	} else {
		edgemux := http.NewServeMux()
		managemux := http.NewServeMux()
		edgemux.HandleFunc(apiprefix+"/edge/superparams", edge_get_superparams)
		edgemux.HandleFunc(apiprefix+"/edge/peerinfo", edge_get_peerinfo)
		edgemux.HandleFunc(apiprefix+"/edge/nhtable", edge_get_nhtable)
		edgemux.HandleFunc(apiprefix+"/edge/post/nodeinfo", edge_post_nodeinfo)
		managemux.HandleFunc(apiprefix+"/manage/peer/add", manage_peeradd)
		managemux.HandleFunc(apiprefix+"/manage/peer/del", manage_peerdel)
		managemux.HandleFunc(apiprefix+"/manage/peer/update", manage_peerupdate)
		managemux.HandleFunc(apiprefix+"/manage/super/state", manage_get_peerstate)
		managemux.HandleFunc(apiprefix+"/manage/super/update", manage_superupdate)

		go func() {
			err := http.ListenAndServe(edgeListen, edgemux)
			if err != nil {
				errchan <- err
			}
		}()

		if manageListen != "" {
			go func() {
				err := http.ListenAndServe(manageListen, managemux)
				if err != nil {
					errchan <- err
				}
			}()
		}
	}

}
