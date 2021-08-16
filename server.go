package main

import (
	"encoding/json"
	"fmt"
	"net"
	"strconv"

	"net/http"

	"github.com/KusakabeSi/EtherGuardVPN/path"
)

type client struct {
	ConnV4  net.Addr
	ConnV6  net.Addr
	InterV4 []net.Addr
	InterV6 []net.Addr
	notify4 string
	notify6 string
}

type action struct {
	Action  string `json:"a"`
	Node_ID int    `json:"id"`
	Name    string `json:"n"`
}

type serverConf struct {
	UDP_port   int    `json:"port"`
	CONN_url   string `json:"url"`
	USE_Oneway bool   `json:"use_oneway"`
}

type pathLentancy struct {
	NodeID_S  int     `json:"src"`
	NodeID_E  int     `json:"dst"`
	Latency   float64 `json:"ping"`
	Is_Oneway bool    `json:"oneway"`
}

var (
	clients   = []client{}
	graph     = path.IG{}
	node_num  = 10
	udp_port  = 9595
	http_port = 9595
)

func (c *client) hasV4() bool {
	return c.ConnV4.String() == ""
}
func (c *client) hasV6() bool {
	return c.ConnV6.String() == ""
}
func (c *client) online() bool {
	return c.hasV4() || c.hasV6()
}

func serv(conn net.Conn, version int) {
	buffer := make([]byte, 1024)

	_, err := conn.Read(buffer)
	if err != nil {
		fmt.Println(err)
	}
	incoming := string(buffer)
	fmt.Println("[INCOMING]", conn.RemoteAddr(), incoming)
	theaction := action{}
	err = json.Unmarshal(buffer, &theaction)
	if err != nil {
		fmt.Println("[Error]", err)
		return
	}

	if theaction.Action != "register" {
		fmt.Println("[Error]", "Unknow action", theaction.Action)
		return
	}
	if version == 4 {
		clients[theaction.Node_ID].ConnV4 = conn.RemoteAddr()
	} else if version == 6 {
		clients[theaction.Node_ID].ConnV6 = conn.RemoteAddr()
	}
	conn.Write([]byte("OK"))
	err = conn.Close()
	if err != nil {
		fmt.Println("[Error]", err)
		fmt.Println(err)
	}
}

func accept(listener net.Listener, version int) {
	for {
		conn, err := listener.Accept()
		if err != nil {
			fmt.Println(err)
			continue
		}
		serv(conn, version)
	}
}

// Server --
func RegisterServer() {
	/*
		graph.Init(node_num)
		clients = make([]client, node_num)

		addr4 := &net.UDPAddr{
			IP:   net.IPv4zero,
			Port: udp_port,
		}
		addr6 := &net.UDPAddr{
			IP:   net.IPv6zero,
			Port: udp_port,
		}



		// Connect to a DTLS server
		listener4, err4 := dtls.Listen("udp4", addr4)
		if err4 != nil {
			fmt.Println(err)
		}
		defer listener4.Close()
		listener6, err6 := dtls.Listen("udp6", addr6)
		if err6 != nil {
			fmt.Println(err)
		}
		defer listener6.Close()
		if err4 != nil && err6 != nil {
			fmt.Println("udp4 and udp6 both failed!")
			return
		}
		go accept(listener4, 4)
		go accept(listener6, 6)*/
}

func get_config(w http.ResponseWriter, r *http.Request) {
	rr, _ := json.Marshal(serverConf{
		UDP_port: udp_port,
		CONN_url: "https://example.com",
	})
	w.WriteHeader(http.StatusOK)
	w.Write(rr)
}

func get_neighbor(w http.ResponseWriter, r *http.Request) {
	rr, _ := json.Marshal(clients)
	w.WriteHeader(http.StatusOK)
	w.Write(rr)
}

func get_route(w http.ResponseWriter, r *http.Request) {
	dist, next := path.FloydWarshall(graph)
	rr, _ := json.Marshal(path.Fullroute{
		Dist: dist,
		Next: next,
	})
	w.WriteHeader(http.StatusOK)
	w.Write(rr)
}

func post_latency(w http.ResponseWriter, r *http.Request) {
	body := make([]byte, r.ContentLength)
	info := pathLentancy{}
	err := json.Unmarshal(body, &info)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(fmt.Sprint(err)))
		return
	}
	if info.Is_Oneway {
		graph.Edge(path.Vertex(info.NodeID_S), path.Vertex(info.NodeID_E), info.Latency)
	} else {
		graph.Edge(path.Vertex(info.NodeID_S), path.Vertex(info.NodeID_E), info.Latency/2)
		graph.Edge(path.Vertex(info.NodeID_E), path.Vertex(info.NodeID_S), info.Latency/2)
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("OK"))
}

func HttpServer() {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/neighbor/", get_neighbor)
	mux.HandleFunc("/api/route/", get_route)
	mux.HandleFunc("/api/latency/", post_latency)
	go http.ListenAndServe(":"+strconv.Itoa(http_port), mux)
}
