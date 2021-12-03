package mtypes

import (
	"math"
	"strconv"
)

// Nonnegative integer ID of vertex
type Vertex uint16

const (
	Broadcast        Vertex = math.MaxUint16 - iota // Normal boardcast, boardcast with route table
	ControlMessage   Vertex = math.MaxUint16 - iota // p2p mode: boardcast to every know peer and prevent dup. super mode: send to supernode
	SuperNodeMessage Vertex = math.MaxUint16 - iota
	BrokenMessage    Vertex = math.MaxUint16 - iota
	Special_NodeID   Vertex = BrokenMessage
)

type EdgeConfig struct {
	Interface         InterfaceConf
	NodeID            Vertex
	NodeName          string
	PostScript        string
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
	PostScript              string
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
	NodeID         Vertex
	Name           string
	PubKey         string
	PSKey          string
	AdditionalCost float64
	SkipLocalIP    bool
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
	AdditionalCost   float64
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
	SkipLocalIP          bool
	HttpPostInterval     float64
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
	TimeoutCheckInterval      float64
	RecalculateCoolDown       float64
}

type DistTable map[Vertex]map[Vertex]float64
type NextHopTable map[Vertex]map[Vertex]*Vertex

type API_connurl struct {
	ExternalV4 map[string]float64
	ExternalV6 map[string]float64
	LocalV4    map[string]float64
	LocalV6    map[string]float64
}

func (Connurl *API_connurl) IsEmpty() bool {
	return len(Connurl.ExternalV4)+len(Connurl.ExternalV6)+len(Connurl.LocalV4)+len(Connurl.LocalV6) == 0
}

func (Connurl *API_connurl) GetList(UseLocal bool) (ret map[string]float64) {
	ret = make(map[string]float64)
	if UseLocal {
		if Connurl.LocalV4 != nil {
			for k, v := range Connurl.LocalV4 {
				ret[k] = v
			}
		}
		if Connurl.LocalV6 != nil {
			for k, v := range Connurl.LocalV6 {
				ret[k] = v
			}
		}
	}
	if Connurl.ExternalV4 != nil {
		for k, v := range Connurl.ExternalV4 {
			ret[k] = v
		}
	}
	if Connurl.ExternalV6 != nil {
		for k, v := range Connurl.ExternalV6 {
			ret[k] = v
		}
	}
	return
}

type API_Peerinfo struct {
	NodeID  Vertex
	PSKey   string
	Connurl *API_connurl
}

type API_Peers map[string]API_Peerinfo // map[PubKey]API_Peerinfo

type JWTSecret [32]byte

const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
