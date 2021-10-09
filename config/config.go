package config

import (
	"crypto/rand"
	"math"
	"strconv"
)

// Nonnegative integer ID of vertex
type Vertex uint16

const (
	Broadcast        Vertex = math.MaxUint16 - iota // Normal boardcast, boardcast with route table
	ControlMessage   Vertex = math.MaxUint16 - iota // p2p mode: boardcast to every know peer and prevent dup. super mode: send to supernode
	SuperNodeMessage Vertex = math.MaxUint16 - iota
	Special_NodeID   Vertex = SuperNodeMessage
)

type EdgeConfig struct {
	Interface         InterfaceConf
	NodeID            Vertex
	NodeName          string
	DefaultTTL        uint8
	L2FIBTimeout      float64
	PrivKey           string
	ListenPort        int
	LogLevel          LoggerInfo
	DynamicRoute      DynamicRouteInfo
	NextHopTable      NextHopTable
	ResetConnInterval float64
	Peers             []PeerInfo
}

type SuperConfig struct {
	NodeName                string
	PrivKeyV4               string
	PrivKeyV6               string
	ListenPort              int
	LogLevel                LoggerInfo
	RePushConfigInterval    float64
	Passwords               Passwords
	GraphRecalculateSetting GraphRecalculateSetting
	NextHopTable            NextHopTable
	EdgeTemplate            string
	UsePSKForInterEdge      bool
	Peers                   []SuperPeerInfo
}

type Passwords struct {
	ShowState string
	AddPeer   string
	DelPeer   string
}

type InterfaceConf struct {
	Itype         string
	Name          string
	VPPIfaceID    uint32
	VPPBridgeID   uint32
	MacAddrPrefix string
	MTU           int
	RecvAddr      string
	SendAddr      string
	L2HeaderMode  string
}

type PeerInfo struct {
	NodeID   Vertex
	PubKey   string
	PSKey    string
	EndPoint string
	Static   bool
}

type SuperPeerInfo struct {
	NodeID Vertex
	Name   string
	PubKey string
	PSKey  string
}

type LoggerInfo struct {
	LogLevel    string
	LogTransit  bool
	LogControl  bool
	LogNormal   bool
	LogInternal bool
	LogNTP      bool
}

func (v *Vertex) ToString() string {
	switch *v {
	case Broadcast:
		return "Boardcast"
	case ControlMessage:
		return "Control"
	case SuperNodeMessage:
		return "Super"
	default:
		return strconv.Itoa(int(*v))
	}
}

type DynamicRouteInfo struct {
	SendPingInterval float64
	PeerAliveTimeout float64
	DupCheckTimeout  float64
	ConnTimeOut      float64
	ConnNextTry      float64
	SaveNewPeers     bool
	SuperNode        SuperInfo
	P2P              P2Pinfo
	NTPconfig        NTPinfo
}

type NTPinfo struct {
	UseNTP           bool
	MaxServerUse     int
	SyncTimeInterval float64
	NTPTimeout       float64
	Servers          []string
}

type SuperInfo struct {
	UseSuperNode         bool
	PSKey                string
	ConnURLV4            string
	PubKeyV4             string
	ConnURLV6            string
	PubKeyV6             string
	APIUrl               string
	SuperNodeInfoTimeout float64
}

type P2Pinfo struct {
	UseP2P                  bool
	SendPeerInterval        float64
	GraphRecalculateSetting GraphRecalculateSetting
}

type GraphRecalculateSetting struct {
	StaticMode                bool
	JitterTolerance           float64
	JitterToleranceMultiplier float64
	NodeReportTimeout         float64
	RecalculateCoolDown       float64
}

type DistTable map[Vertex]map[Vertex]float64
type NextHopTable map[Vertex]map[Vertex]*Vertex

type API_Peerinfo struct {
	NodeID  Vertex
	PSKey   string
	Connurl map[string]int
}

type API_Peers map[string]API_Peerinfo // map[PubKey]API_Peerinfo

const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"

func RandomStr(length int, defaults string) string {
	bytes := make([]byte, length)

	if _, err := rand.Read(bytes); err != nil {
		return defaults
	}

	for i, b := range bytes {
		bytes[i] = chars[b%byte(len(chars))]
	}

	return string(bytes)
}
