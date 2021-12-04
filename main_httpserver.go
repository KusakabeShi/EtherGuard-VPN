package main

import (
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"net/http"
	"net/url"

	"github.com/golang-jwt/jwt"
	"golang.org/x/crypto/sha3"

	"github.com/KusakabeSi/EtherGuard-VPN/device"
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/path"
	yaml "gopkg.in/yaml.v2"
)

type http_shared_objects struct {
	http_graph            *path.IG
	http_device4          *device.Device
	http_device6          *device.Device
	http_HashSalt         []byte
	http_NhTable_Hash     string
	http_PeerInfo_hash    string
	http_SuperParams_Hash string
	http_SuperParamsStr   []byte
	http_NhTableStr       []byte
	http_PeerInfo         mtypes.API_Peers
	http_super_chains     *mtypes.SUPER_Events

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
	NhTableState    string
	PeerInfoState   string
	SuperParamState string
	JETSecret       mtypes.JWTSecret
	httpPostCount   uint64
	LastSeen        time.Time
}

type client struct {
	ConnV4  net.Addr
	ConnV6  net.Addr
	InterV4 []net.Addr
	InterV6 []net.Addr
	notify4 string
	notify6 string
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
		return mtypes.BrokenMessage, err
	}
	return mtypes.Vertex(val), nil
}

func get_api_peers(old_State_hash string) (api_peerinfo mtypes.API_Peers, StateHash string, changed bool) {
	// No lock
	api_peerinfo = make(mtypes.API_Peers)
	for _, peerinfo := range httpobj.http_sconfig.Peers {
		api_peerinfo[peerinfo.PubKey] = mtypes.API_Peerinfo{
			NodeID:  peerinfo.NodeID,
			PSKey:   peerinfo.PSKey,
			Connurl: &mtypes.API_connurl{},
		}
		if httpobj.http_PeerState[peerinfo.PubKey].LastSeen.Add(mtypes.S2TD(httpobj.http_sconfig.PeerAliveTimeout)).After(time.Now()) {
			connV4 := httpobj.http_device4.GetConnurl(peerinfo.NodeID)
			connV6 := httpobj.http_device6.GetConnurl(peerinfo.NodeID)
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

func get_superparams(w http.ResponseWriter, r *http.Request) {
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
	if NodeID >= mtypes.Special_NodeID {
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

	if httpobj.http_SuperParams_Hash != State {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Paramater State: State not correct"))
		return
	}

	if _, has := httpobj.http_PeerState[PubKey]; has == false {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Paramater PubKey: Not found in httpobj.http_PeerState, this shouldn't happen. Please report to the author."))
		return
	}
	// Do something
	SuperParams := mtypes.API_SuperParams{
		SendPingInterval: httpobj.http_sconfig.SendPingInterval,
		HttpPostInterval: httpobj.http_sconfig.HttpPostInterval,
		PeerAliveTimeout: httpobj.http_sconfig.PeerAliveTimeout,
		AdditionalCost:   httpobj.http_PeerID2Info[NodeID].AdditionalCost,
	}
	SuperParamStr, _ := json.Marshal(SuperParams)
	httpobj.http_PeerState[PubKey].SuperParamState = State
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(SuperParamStr))
	return
}

func get_peerinfo(w http.ResponseWriter, r *http.Request) {
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
	if NodeID >= mtypes.Special_NodeID {
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
	if _, has := httpobj.http_PeerState[PubKey]; has == false {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Paramater PubKey: Not found in httpobj.http_PeerState, this shouldn't happen. Please report to the author."))
		return
	}

	// Do something
	httpobj.http_PeerState[PubKey].PeerInfoState = State
	http_PeerInfo_2peer := make(mtypes.API_Peers)

	for PeerPubKey, peerinfo := range httpobj.http_PeerInfo {
		if httpobj.http_sconfig.UsePSKForInterEdge {
			h := sha256.New()
			if NodeID > peerinfo.NodeID {
				h.Write([]byte(PubKey))
				h.Write([]byte(PeerPubKey))
			} else if NodeID < peerinfo.NodeID {
				h.Write([]byte(PeerPubKey))
				h.Write([]byte(PubKey))
			} else {
				continue
			}
			h.Write(httpobj.http_HashSalt)
			bs := h.Sum(nil)
			var psk device.NoisePresharedKey
			copy(psk[:], bs[:])
			peerinfo.PSKey = psk.ToString()
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
	return
}

func get_nhtable(w http.ResponseWriter, r *http.Request) {
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
	if NodeID >= mtypes.Special_NodeID {
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
	if _, has := httpobj.http_PeerState[PubKey]; has == false {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("Paramater PubKey: Not found in httpobj.http_PeerState, this shouldn't happen. Please report to the author."))
		return
	}

	httpobj.http_PeerState[PubKey].NhTableState = State
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(httpobj.http_NhTableStr))
	return

}

func get_peerstate(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	password, err := extractParamsStr(params, "Password", w)
	if err != nil {
		return
	}
	if password != httpobj.http_passwords.ShowState {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Wrong password"))
		return
	}
	httpobj.RLock()
	defer httpobj.RUnlock()
	if time.Now().After(httpobj.http_StateExpire) {
		hs := HttpState{
			PeerInfo: make(map[mtypes.Vertex]HttpPeerInfo),
			NhTable:  httpobj.http_graph.GetNHTable(false),
			Infinity: path.Infinity,
			Edges:    httpobj.http_graph.GetEdges(false, false),
			Edges_Nh: httpobj.http_graph.GetEdges(true, true),
			Dist:     httpobj.http_graph.GetDtst(),
		}

		for _, peerinfo := range httpobj.http_sconfig.Peers {
			LastSeenStr := httpobj.http_PeerState[peerinfo.PubKey].LastSeen.String()
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
	return
}

func post_nodeinfo(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()

	NodeID, err := extractParamsVertex(params, "NodeID", w)
	if err != nil {
		return
	}
	PubKey, err := extractParamsStr(params, "PubKey", w)
	if err != nil {
		return
	}
	if NodeID >= mtypes.Special_NodeID {
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
			return nil, fmt.Errorf("Unexpected signing method: %v", token.Header["alg"])
		}
		return JWTSecret[:], nil
	})
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Paramater JWTSig: Signature verification failed: %v", err)))
		return
	}
	if !token.Valid {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprintf("Paramater JWTSig: Signature verification failed: Invalid token")))
		return
	}

	client_PostCount := token_claims.PostCount
	client_body_hash := token_claims.BodyHash

	if client_PostCount < httpPostCount {
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
		w.Write([]byte(fmt.Sprintf("Request body: gzip unzip failed")))
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
	httpobj.http_PeerState[PubKey].httpPostCount = client_PostCount + 1

	applied_pones := make([]mtypes.PongMsg, 0, len(client_report.Pongs))
	for _, pong_msg := range client_report.Pongs {
		if pong_msg.Src_nodeID != NodeID {
			continue
		}

		if info, has := httpobj.http_PeerID2Info[pong_msg.Dst_nodeID]; !has {
			AdditionalCost_use := info.AdditionalCost

			if AdditionalCost_use >= 0 {
				pong_msg.AdditionalCost = AdditionalCost_use
			}
			applied_pones = append(applied_pones, pong_msg)
		}
	}
	changed := httpobj.http_graph.UpdateLatencyMulti(client_report.Pongs, true, true)
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
	w.Write([]byte(fmt.Sprintf("OK")))
	return
}

func peeradd(w http.ResponseWriter, r *http.Request) { //Waiting for test
	params := r.URL.Query()
	password, err := extractParamsStr(params, "Password", w)
	if err != nil {
		return
	}
	if password != httpobj.http_passwords.AddPeer {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Wrong password"))
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

	PSKey, err := extractParamsStr(r.Form, "PSKey", nil)

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
	if httpobj.http_sconfig.GraphRecalculateSetting.StaticMode == true {
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
	ret_str_byte, _ := yaml.Marshal(&httpobj.http_econfig_tmp)
	w.WriteHeader(http.StatusOK)
	w.Write(ret_str_byte)
	return
}

func peerdel(w http.ResponseWriter, r *http.Request) { //Waiting for test
	params := r.URL.Query()
	toDelete := mtypes.Broadcast

	var err error
	var NodeID mtypes.Vertex
	var PrivKey string
	var PubKey string
	password, pwderr := extractParamsStr(params, "Password", nil)
	httpobj.Lock()
	defer httpobj.Unlock()
	if pwderr == nil { // user provide the password
		if password == httpobj.http_passwords.DelPeer {
			NodeID, err = extractParamsVertex(params, "NodeID", w)
			if err != nil {
				return
			}
			toDelete = NodeID
			if _, has := httpobj.http_PeerID2Info[toDelete]; !has {
				w.WriteHeader(http.StatusNotFound)
				w.Write([]byte(fmt.Sprintf("Paramater NodeID: \"%v\" not found", PubKey)))
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
		if toDelete == mtypes.Broadcast {
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
	return
}

func HttpServer(http_port int, apiprefix string) {
	mux := http.NewServeMux()
	if apiprefix[0] != '/' {
		apiprefix = "/" + apiprefix
	}
	mux.HandleFunc(apiprefix+"/superparams", get_superparams)
	mux.HandleFunc(apiprefix+"/peerinfo", get_peerinfo)
	mux.HandleFunc(apiprefix+"/nhtable", get_nhtable)
	mux.HandleFunc(apiprefix+"/peerstate", get_peerstate)
	mux.HandleFunc(apiprefix+"/post/nodeinfo", post_nodeinfo)
	mux.HandleFunc(apiprefix+"/peer/add", peeradd) //Waiting for test
	mux.HandleFunc(apiprefix+"/peer/del", peerdel) //Waiting for test
	http.ListenAndServe(":"+strconv.Itoa(http_port), mux)
}
