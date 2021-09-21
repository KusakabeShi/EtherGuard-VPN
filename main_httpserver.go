package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net"
	"strconv"
	"time"

	"net/http"

	"github.com/KusakabeSi/EtherGuardVPN/config"
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
	http_PeerInfoStr   []byte

	http_PeerID2PubKey map[config.Vertex]string

	http_passwords       config.Passwords
	http_StateExpire     time.Time
	http_StateString_tmp []byte

	http_PeerState map[string]*PeerState //the state hash reported by peer
	http_sconfig   *config.SuperConfig

	http_sconfig_path string
	http_econfig_tmp  *config.EdgeConfig
)

type HttpState struct {
	PeerInfo map[config.Vertex]HttpPeerInfo
	Infinity float64
	Edges    map[config.Vertex]map[config.Vertex]float64
	NhTable  config.NextHopTable
	Dist     config.DistTable
}

type HttpPeerInfo struct {
	Name string
}

type PeerState struct {
	NhTableState  [32]byte
	PeerInfoState [32]byte
}

type client struct {
	ConnV4  net.Addr
	ConnV6  net.Addr
	InterV4 []net.Addr
	InterV6 []net.Addr
	notify4 string
	notify6 string
}

func get_api_peers() (api_peerinfo config.API_Peers, api_peerinfo_str_byte []byte, StateHash [32]byte, changed bool) {
	api_peerinfo = make(config.API_Peers)
	for _, peerinfo := range http_sconfig.Peers {
		api_peerinfo[peerinfo.PubKey] = config.API_Peerinfo{
			NodeID:  peerinfo.NodeID,
			PubKey:  peerinfo.PubKey,
			PSKey:   peerinfo.PSKey,
			Connurl: make(map[string]bool),
		}
		connV4 := http_device4.GetConnurl(peerinfo.NodeID)
		connV6 := http_device6.GetConnurl(peerinfo.NodeID)
		api_peerinfo[peerinfo.PubKey].Connurl[connV4] = true
		api_peerinfo[peerinfo.PubKey].Connurl[connV6] = true
		delete(api_peerinfo[peerinfo.PubKey].Connurl, "")
	}
	api_peerinfo_str_byte, _ = json.Marshal(&api_peerinfo)
	hash_raw := md5.Sum(append(api_peerinfo_str_byte, http_HashSalt...))
	hash_str := hex.EncodeToString(hash_raw[:])
	copy(StateHash[:], []byte(hash_str))
	if bytes.Equal(http_PeerInfo_hash[:], StateHash[:]) == false {
		changed = true
	}
	return
}

func get_peerinfo(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	PubKeyA, has := params["PubKey"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found"))
		return
	}
	StateA, has := params["State"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found"))
		return
	}
	PubKey := PubKeyA[0]
	State := StateA[0]
	if bytes.Equal(http_PeerInfo_hash[:], []byte(State)) {
		if state := http_PeerState[PubKey]; state != nil {
			copy(http_PeerState[PubKey].PeerInfoState[:], State)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(http_PeerInfoStr))
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Not found"))
}

func get_nhtable(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	PubKeyA, has := params["PubKey"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found"))
		return
	}
	StateA, has := params["State"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found"))
		return
	}
	PubKey := PubKeyA[0]
	State := StateA[0]
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
	w.Write([]byte("Not found"))
}

func get_info(w http.ResponseWriter, r *http.Request) {
	params := r.URL.Query()
	PasswordA, has := params["Password"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found"))
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
			NhTable:  http_graph.GetNHTable(),
			Infinity: path.Infinity,
			Edges:    http_graph.GetEdges(),
			Dist:     http_graph.GetDtst(),
		}
		for _, peerinfo := range http_sconfig.Peers {
			hs.PeerInfo[peerinfo.NodeID] = HttpPeerInfo{
				Name: peerinfo.Name,
			}
		}
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
		w.Write([]byte("Not found"))
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
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("NodeID exists"))
			return
		}
		if peerinfo.Name == Name {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("Node name exists"))
			return
		}
		if peerinfo.PubKey == PubKey {
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte("PubKey exists"))
			return
		}
	}
	super_peeradd(config.SuperPeerInfo{
		NodeID: NodeID,
		Name:   Name,
		PubKey: PubKey,
		PSKey:  PSKey,
	})
	configbytes, _ := yaml.Marshal(http_sconfig)
	ioutil.WriteFile(http_sconfig_path, configbytes, 0644)
}

func peerdel(w http.ResponseWriter, r *http.Request) { //Waiting for test
	params := r.URL.Query()
	toDelete := config.Boardcast

	PasswordA, has := params["Password"]
	PubKey := ""
	if has {
		password := PasswordA[0]
		if password == http_passwords.AddPeer {
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
	if toDelete == config.Boardcast {
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
