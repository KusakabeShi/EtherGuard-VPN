package main

import (
	"bytes"
	"net"
	"strconv"

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
	http_PeerInfos     config.HTTP_Peers
)

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
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(http_NhTableStr))
			return
		}
	}
	w.WriteHeader(http.StatusNotFound)
	w.Write([]byte("Not found"))
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
