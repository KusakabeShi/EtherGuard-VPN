package main

import (
	"bytes"
	"encoding/json"
	"net"
	"strconv"
	"sync"
	"time"

	"net/http"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	"github.com/KusakabeSi/EtherGuardVPN/device"
	"github.com/KusakabeSi/EtherGuardVPN/path"
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
	http_PeerState     map[string]*PeerState
	http_PeerID2Map    map[config.Vertex]string
	http_PeerInfos     config.HTTP_Peers // nodeID name pubkey, preshared key and more
	http_peerinfos     sync.Map          // map[config.Vertex]string // nodeID and name, for guest visiting
	http_StatePWD      string
	http_StateExpire   time.Time
	http_StateString   []byte
)

type HttpState struct {
	PeerInfo map[config.Vertex]string
	Edges    map[config.Vertex]map[config.Vertex]float64
	NhTable  config.NextHopTable
	Dist     config.DistTable
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
	PwdA, has := params["Password"]
	if !has {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("Not found"))
		return
	}
	password := PwdA[0]
	if password != http_StatePWD {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte("Wrong password"))
		return
	}
	if time.Now().After(http_StateExpire) {
		hs := HttpState{
			PeerInfo: make(map[config.Vertex]string),
			NhTable:  http_graph.GetNHTable(false),
			Edges:    http_graph.GetEdges(),
			Dist:     http_graph.GetDtst(),
		}
		http_peerinfos.Range(func(key interface{}, value interface{}) bool {
			hs.PeerInfo[key.(config.Vertex)] = value.(string)
			return true
		})
		http_StateExpire = time.Now().Add(5 * time.Second)
		http_StateString, _ = json.Marshal(hs)
	}
	w.WriteHeader(http.StatusOK)
	w.Write(http_StateString)
	return
}

func HttpServer(http_port int, apiprefix string) {
	mux := http.NewServeMux()
	if apiprefix[0] != '/' {
		apiprefix = "/" + apiprefix
	}
	mux.HandleFunc(apiprefix+"/peerinfo", get_peerinfo)
	mux.HandleFunc(apiprefix+"/nhtable", get_nhtable)
	http.ListenAndServe(":"+strconv.Itoa(http_port), mux)
}
