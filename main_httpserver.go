package main

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"strconv"
	"sync"
	"time"

	"net/http"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	"github.com/KusakabeSi/EtherGuardVPN/conn"
	"github.com/KusakabeSi/EtherGuardVPN/device"
	"github.com/KusakabeSi/EtherGuardVPN/path"
	yaml "gopkg.in/yaml.v2"
)

var (
	http_graph         *path.IG
	http_device4       *device.Device
	http_device6       *device.Device
	http_HashSalt      []byte
	http_NhTable_Hash  [32]byte
	http_PeerInfo_hash [32]byte
	http_NhTableStr    []byte
	http_PeerInfo      config.API_Peers
	http_super_chains  *path.SUPER_Events

	http_passwords       config.Passwords
	http_StateExpire     time.Time
	http_StateString_tmp []byte

	http_maps_lock   sync.RWMutex
	http_PeerID2Info map[config.Vertex]config.SuperPeerInfo
	http_PeerState   map[string]*PeerState //the state hash reported by peer
	http_PeerIPs     map[string]*HttpPeerLocalIP

	http_sconfig *config.SuperConfig

	http_sconfig_path string
	http_econfig_tmp  *config.EdgeConfig
)

type HttpPeerLocalIP struct {
	IPv4 net.UDPAddr
	IPv6 net.UDPAddr
}

type HttpState struct {
	PeerInfo map[config.Vertex]HttpPeerInfo
	Infinity float64
	Edges    map[config.Vertex]map[config.Vertex]float64
	Edges_Nh map[config.Vertex]map[config.Vertex]float64
	NhTable  config.NextHopTable
	Dist     config.DistTable
}

type HttpPeerInfo struct {
	Name     string
	LastSeen string
}

type PeerState struct {
	NhTableState  [32]byte
	PeerInfoState [32]byte
	LastSeen      time.Time
}

type client struct {
	ConnV4  net.Addr
	ConnV6  net.Addr
	InterV4 []net.Addr
	InterV6 []net.Addr
	notify4 string
	notify6 string
}

func get_api_peers(old_State_hash [32]byte) (api_peerinfo config.API_Peers, StateHash [32]byte, changed bool) {
	api_peerinfo = make(config.API_Peers)
	for _, peerinfo := range http_sconfig.Peers {
		api_peerinfo[peerinfo.PubKey] = config.API_Peerinfo{
			NodeID:  peerinfo.NodeID,
			PSKey:   peerinfo.PSKey,
			Connurl: make(map[string]float64),
		}
		http_maps_lock.RLock()
		if http_PeerState[peerinfo.PubKey].LastSeen.Add(path.S2TD(http_sconfig.GraphRecalculateSetting.NodeReportTimeout)).After(time.Now()) {
			connV4 := http_device4.GetConnurl(peerinfo.NodeID)
			connV6 := http_device6.GetConnurl(peerinfo.NodeID)
			api_peerinfo[peerinfo.PubKey].Connurl[connV4] = 4
			api_peerinfo[peerinfo.PubKey].Connurl[connV6] = 6
			L4Addr := http_PeerIPs[peerinfo.PubKey].IPv4
			L4IP := L4Addr.IP
			L4str := L4Addr.String()
			if L4str != connV4 && conn.ValidIP(L4IP) {
				api_peerinfo[peerinfo.PubKey].Connurl[L4str] = 14
			}
			L6Addr := http_PeerIPs[peerinfo.PubKey].IPv6
			L6IP := L6Addr.IP
			L6str := L6Addr.String()
			if L6str != connV6 && conn.ValidIP(L6IP) {
				api_peerinfo[peerinfo.PubKey].Connurl[L6str] = 16
			}
			delete(api_peerinfo[peerinfo.PubKey].Connurl, "")
		}
		http_maps_lock.RUnlock()
	}
	api_peerinfo_str_byte, _ := json.Marshal(&api_peerinfo)
	hash_raw := md5.Sum(append(api_peerinfo_str_byte, http_HashSalt...))
	hash_str := hex.EncodeToString(hash_raw[:])
	copy(StateHash[:], []byte(hash_str))
	if bytes.Equal(old_State_hash[:], StateHash[:]) == false {
		changed = true
	}
	return
}

func get_peerinfo(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	PubKeyA, has := params["PubKey"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Require PubKey."))
		return
	}
	StateA, has := params["State"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Require State."))
		return
	}
	NIDA, has := params["NodeID"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Require NodeID."))
		return
	}
	NID2, err := strconv.ParseUint(NIDA[0], 10, 16)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("%v", err)))
		return
	}
	PubKey := PubKeyA[0]
	State := StateA[0]
	NodeID := config.Vertex(NID2)
	http_maps_lock.RLock()
	defer http_maps_lock.RUnlock()
	if http_PeerID2Info[NodeID].PubKey != PubKey {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("NodeID and PunKey are not match"))
		return
	}

	if bytes.Equal(http_PeerInfo_hash[:], []byte(State)) {
		if state := http_PeerState[PubKey]; state != nil {
			copy(http_PeerState[PubKey].PeerInfoState[:], State)
			http_PeerInfo_2peer := make(config.API_Peers)

			for PeerPubKey, peerinfo := range http_PeerInfo {
				if http_sconfig.UsePSKForInterEdge {
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
					h.Write(http_HashSalt)
					bs := h.Sum(nil)
					var psk device.NoisePresharedKey
					copy(psk[:], bs[:])
					peerinfo.PSKey = psk.ToString()
				} else {
					peerinfo.PSKey = ""
				}
				http_PeerInfo_2peer[PeerPubKey] = peerinfo
			}
			api_peerinfo_str_byte, _ := json.Marshal(&http_PeerInfo_2peer)

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write(api_peerinfo_str_byte)
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("State not correct"))
}

func get_nhtable(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	PubKeyA, has := params["PubKey"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Require PubKey."))
		return
	}
	StateA, has := params["State"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Require State."))
		return
	}
	NIDA, has := params["NodeID"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Require NodeID."))
		return
	}
	NID2, err := strconv.ParseUint(NIDA[0], 10, 16)
	if err != nil {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(fmt.Sprintf("%v", err)))
		return
	}
	PubKey := PubKeyA[0]
	State := StateA[0]
	NodeID := config.Vertex(NID2)
	http_maps_lock.RLock()
	defer http_maps_lock.RUnlock()
	if http_PeerID2Info[NodeID].PubKey != PubKey {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("NodeID and PunKey are not match"))
		return
	}

	if bytes.Equal(http_NhTable_Hash[:], []byte(State)) {
		if state := http_PeerState[PubKey]; state != nil {
			copy(http_PeerState[PubKey].NhTableState[:], State)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(http_NhTableStr))
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("State not correct"))
}

func get_info(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	PasswordA, has := params["Password"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Require Password"))
		return
	}
	password := PasswordA[0]
	if password != http_passwords.ShowState {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Wrong password"))
		return
	}
	if time.Now().After(http_StateExpire) {
		hs := HttpState{
			PeerInfo: make(map[config.Vertex]HttpPeerInfo),
			NhTable:  http_graph.GetNHTable(false),
			Infinity: path.Infinity,
			Edges:    http_graph.GetEdges(false, false),
			Edges_Nh: http_graph.GetEdges(true, true),
			Dist:     http_graph.GetDtst(),
		}
		http_maps_lock.RLock()
		for _, peerinfo := range http_sconfig.Peers {
			LastSeenStr := http_PeerState[peerinfo.PubKey].LastSeen.String()
			hs.PeerInfo[peerinfo.NodeID] = HttpPeerInfo{
				Name:     peerinfo.Name,
				LastSeen: LastSeenStr,
			}
		}
		http_maps_lock.RUnlock()
		http_StateExpire = time.Now().Add(5 * time.Second)
		http_StateString_tmp, _ = json.Marshal(hs)
	}
	w.WriteHeader(http.StatusOK)
	w.Write(http_StateString_tmp)
	return
}

func peeradd(w http.ResponseWriter, r *http.Request) { //Waiting for test
	params := r.URL.Query()
	PasswordA, has := params["Password"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Require Password"))
		return
	}
	password := PasswordA[0]
	if password != http_passwords.AddPeer {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Wrong password"))
		return
	}

	r.ParseForm()
	NID, err := strconv.ParseUint(r.Form.Get("nodeid"), 10, 16)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprint(err)))
		return
	}
	NodeID := config.Vertex(NID)
	Name := r.Form.Get("name")
	if len(Name) <= 0 || len(Name) >= 15 {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte("Name too long or too short."))
		return
	}
	PubKey := r.Form.Get("pubkey")
	_, err = device.Str2PubKey(PubKey)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprint(err)))
		return
	}
	PSKey := r.Form.Get("pskey")
	_, err = device.Str2PSKey(PSKey)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprint(err)))
		return
	}
	for _, peerinfo := range http_sconfig.Peers {
		if peerinfo.NodeID == NodeID {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("NodeID exists"))
			return
		}
		if peerinfo.Name == Name {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("Node name exists"))
			return
		}
		if peerinfo.PubKey == PubKey {
			w.WriteHeader(http.StatusConflict)
			w.Write([]byte("PubKey exists"))
			return
		}
	}
	if http_sconfig.GraphRecalculateSetting.StaticMode == true {
		NhTableStr := r.Form.Get("nexthoptable")
		if NhTableStr == "" {
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte("Your NextHopTable is in static mode.\nPlease provide your new NextHopTable in \"nexthoptable\" parmater in json format"))
			return
		}
		var NewNhTable config.NextHopTable
		err := json.Unmarshal([]byte(NhTableStr), &NewNhTable)
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte(fmt.Sprintf("%v", err)))
			return
		}
		err = checkNhTable(NewNhTable, append(http_sconfig.Peers, config.SuperPeerInfo{
			NodeID: NodeID,
			Name:   Name,
			PubKey: PubKey,
			PSKey:  PSKey,
		}))
		if err != nil {
			w.WriteHeader(http.StatusExpectationFailed)
			w.Write([]byte(fmt.Sprintf("%v", err)))
			return
		}
		http_graph.SetNHTable(NewNhTable, [32]byte{})
	}
	super_peeradd(config.SuperPeerInfo{
		NodeID: NodeID,
		Name:   Name,
		PubKey: PubKey,
		PSKey:  PSKey,
	})
	http_sconfig.Peers = append(http_sconfig.Peers, config.SuperPeerInfo{
		NodeID: NodeID,
		Name:   Name,
		PubKey: PubKey,
		PSKey:  PSKey,
	})
	configbytes, _ := yaml.Marshal(http_sconfig)
	ioutil.WriteFile(http_sconfig_path, configbytes, 0644)
	http_econfig_tmp.NodeID = NodeID
	http_econfig_tmp.NodeName = Name
	http_econfig_tmp.PrivKey = "Your_Private_Key"
	http_econfig_tmp.DynamicRoute.SuperNode.PSKey = PSKey
	ret_str_byte, _ := yaml.Marshal(&http_econfig_tmp)
	w.WriteHeader(http.StatusOK)
	w.Write(ret_str_byte)
	return
}

func peerdel(w http.ResponseWriter, r *http.Request) { //Waiting for test
	params := r.URL.Query()
	toDelete := config.Broadcast

	PasswordA, has := params["Password"]
	PubKey := ""
	if has {
		password := PasswordA[0]
		if password == http_passwords.DelPeer {
			NodeIDA, has := params["nodeid"]
			if !has {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte("Need NodeID"))
				return
			}
			NID, err := strconv.ParseUint(NodeIDA[0], 10, 16)
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(fmt.Sprint(err)))
				return
			}
			NodeID := config.Vertex(NID)
			toDelete = NodeID
		}
	}

	PriKeyA, has := params["privkey"]
	if has && PriKeyA[0] != "" {
		PrivKey := PriKeyA[0]
		privk, err := device.Str2PriKey(PrivKey)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(fmt.Sprint(err)))
			return
		}
		pubk := privk.PublicKey()
		PubKey = pubk.ToString()
		for _, peerinfo := range http_sconfig.Peers {
			if peerinfo.PubKey == PubKey {
				toDelete = peerinfo.NodeID
			}
		}
	}
	if toDelete == config.Broadcast {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Wrong password"))
		return
	}

	var peers_new []config.SuperPeerInfo
	for _, peerinfo := range http_sconfig.Peers {
		if peerinfo.NodeID == toDelete {
			super_peerdel(peerinfo.NodeID)
		} else {
			peers_new = append(peers_new, peerinfo)
		}
	}

	http_sconfig.Peers = peers_new
	configbytes, _ := yaml.Marshal(http_sconfig)
	ioutil.WriteFile(http_sconfig_path, configbytes, 0644)
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(toDelete.ToString() + " deleted."))
	return
}

func HttpServer(http_port int, apiprefix string) {
	mux := http.NewServeMux()
	if apiprefix[0] != '/' {
		apiprefix = "/" + apiprefix
	}
	mux.HandleFunc(apiprefix+"/peerinfo", get_peerinfo)
	mux.HandleFunc(apiprefix+"/nhtable", get_nhtable)
	mux.HandleFunc(apiprefix+"/peerstate", get_info)
	mux.HandleFunc(apiprefix+"/peer/add", peeradd) //Waiting for test
	mux.HandleFunc(apiprefix+"/peer/del", peerdel) //Waiting for test
	http.ListenAndServe(":"+strconv.Itoa(http_port), mux)
}
