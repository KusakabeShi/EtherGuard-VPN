package mtypes

import (
	"math"
	"strconv"
	"sync/atomic"
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
	Interface         InterfaceConf    `yaml:"Interface"`
	NodeID            Vertex           `yaml:"NodeID"`
	NodeName          string           `yaml:"NodeName"`
	PostScript        string           `yaml:"PostScript"`
	DefaultTTL        uint8            `yaml:"DefaultTTL"`
	L2FIBTimeout      float64          `yaml:"L2FIBTimeout"`
	PrivKey           string           `yaml:"PrivKey"`
	ListenPort        int              `yaml:"ListenPort"`
	LogLevel          LoggerInfo       `yaml:"LogLevel"`
	DynamicRoute      DynamicRouteInfo `yaml:"DynamicRoute"`
	NextHopTable      NextHopTable     `yaml:"NextHopTable"`
	ResetConnInterval float64          `yaml:"ResetConnInterval"`
	Peers             []PeerInfo       `yaml:"Peers"`
}

type SuperConfig struct {
	NodeName                string                  `yaml:"NodeName"`
	PostScript              string                  `yaml:"PostScript"`
	PrivKeyV4               string                  `yaml:"PrivKeyV4"`
	PrivKeyV6               string                  `yaml:"PrivKeyV6"`
	ListenPort              int                     `yaml:"ListenPort"`
	ListenPort_EdgeAPI      string                  `yaml:"ListenPort_EdgeAPI"`
	ListenPort_ManageAPI    string                  `yaml:"ListenPort_ManageAPI"`
	API_Prefix              string                  `yaml:"API_Prefix"`
	RePushConfigInterval    float64                 `yaml:"RePushConfigInterval"`
	HttpPostInterval        float64                 `yaml:"HttpPostInterval"`
	PeerAliveTimeout        float64                 `yaml:"PeerAliveTimeout"`
	SendPingInterval        float64                 `yaml:"SendPingInterval"`
	LogLevel                LoggerInfo              `yaml:"LogLevel"`
	Passwords               Passwords               `yaml:"Passwords"`
	GraphRecalculateSetting GraphRecalculateSetting `yaml:"GraphRecalculateSetting"`
	NextHopTable            NextHopTable            `yaml:"NextHopTable"`
	EdgeTemplate            string                  `yaml:"EdgeTemplate"`
	UsePSKForInterEdge      bool                    `yaml:"UsePSKForInterEdge"`
	Peers                   []SuperPeerInfo         `yaml:"Peers"`
}

type Passwords struct {
	ShowState   string `yaml:"ShowState"`
	AddPeer     string `yaml:"AddPeer"`
	DelPeer     string `yaml:"DelPeer"`
	UpdatePeer  string `yaml:"UpdatePeer"`
	UpdateSuper string `yaml:"UpdateSuper"`
}

type InterfaceConf struct {
	IType         string `yaml:"IType"`
	Name          string `yaml:"Name"`
	VPPIFaceID    uint32 `yaml:"VPPIFaceID"`
	VPPBridgeID   uint32 `yaml:"VPPBridgeID"`
	MacAddrPrefix string `yaml:"MacAddrPrefix"`
	IPv4CIDR      string `yaml:"IPv4CIDR"`
	IPv6CIDR      string `yaml:"IPv6CIDR"`
	IPv6LLPrefix  string `yaml:"IPv6LLPrefix"`
	MTU           int    `yaml:"MTU"`
	RecvAddr      string `yaml:"RecvAddr"`
	SendAddr      string `yaml:"SendAddr"`
	L2HeaderMode  string `yaml:"L2HeaderMode"`
}

type PeerInfo struct {
	NodeID   Vertex `yaml:"NodeID"`
	PubKey   string `yaml:"PubKey"`
	PSKey    string `yaml:"PSKey"`
	EndPoint string `yaml:"EndPoint"`
	Static   bool   `yaml:"Static"`
}

type SuperPeerInfo struct {
	NodeID         Vertex  `yaml:"NodeID"`
	Name           string  `yaml:"Name"`
	PubKey         string  `yaml:"PubKey"`
	PSKey          string  `yaml:"PSKey"`
	AdditionalCost float64 `yaml:"AdditionalCost"`
	SkipLocalIP    bool    `yaml:"SkipLocalIP"`
}

type LoggerInfo struct {
	LogLevel    string `yaml:"LogLevel"`
	LogTransit  bool   `yaml:"LogTransit"`
	LogControl  bool   `yaml:"LogControl"`
	LogNormal   bool   `yaml:"LogNormal"`
	LogInternal bool   `yaml:"LogInternal"`
	LogNTP      bool   `yaml:"LogNTP"`
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
	SendPingInterval float64   `yaml:"SendPingInterval"`
	PeerAliveTimeout float64   `yaml:"PeerAliveTimeout"`
	DupCheckTimeout  float64   `yaml:"DupCheckTimeout"`
	ConnTimeOut      float64   `yaml:"ConnTimeOut"`
	ConnNextTry      float64   `yaml:"ConnNextTry"`
	AdditionalCost   float64   `yaml:"AdditionalCost"`
	SaveNewPeers     bool      `yaml:"SaveNewPeers"`
	SuperNode        SuperInfo `yaml:"SuperNode"`
	P2P              P2PInfo   `yaml:"P2P"`
	NTPConfig        NTPInfo   `yaml:"NTPConfig"`
}

type NTPInfo struct {
	UseNTP           bool     `yaml:"UseNTP"`
	MaxServerUse     int      `yaml:"MaxServerUse"`
	SyncTimeInterval float64  `yaml:"SyncTimeInterval"`
	NTPTimeout       float64  `yaml:"NTPTimeout"`
	Servers          []string `yaml:"Servers"`
}

type SuperInfo struct {
	UseSuperNode         bool    `yaml:"UseSuperNode"`
	PSKey                string  `yaml:"PSKey"`
	EndpointV4           string  `yaml:"EndpointV4"`
	PubKeyV4             string  `yaml:"PubKeyV4"`
	EndpointV6           string  `yaml:"EndpointV6"`
	PubKeyV6             string  `yaml:"PubKeyV6"`
	EndpointEdgeAPIUrl   string  `yaml:"EndpointEdgeAPIUrl"`
	SkipLocalIP          bool    `yaml:"SkipLocalIP"`
	SuperNodeInfoTimeout float64 `yaml:"SuperNodeInfoTimeout"`
}

type P2PInfo struct {
	UseP2P                  bool                    `yaml:"UseP2P"`
	SendPeerInterval        float64                 `yaml:"SendPeerInterval"`
	GraphRecalculateSetting GraphRecalculateSetting `yaml:"GraphRecalculateSetting"`
}

type GraphRecalculateSetting struct {
	StaticMode                bool    `yaml:"StaticMode"`
	JitterTolerance           float64 `yaml:"JitterTolerance"`
	JitterToleranceMultiplier float64 `yaml:"JitterToleranceMultiplier"`
	TimeoutCheckInterval      float64 `yaml:"TimeoutCheckInterval"`
	RecalculateCoolDown       float64 `yaml:"RecalculateCoolDown"`
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

type API_SuperParams struct {
	SendPingInterval float64
	HttpPostInterval float64
	PeerAliveTimeout float64
	AdditionalCost   float64
}

type StateHash struct {
	Peer       atomic.Value //[32]byte
	SuperParam atomic.Value //[32]byte
	NhTable    atomic.Value //[32]byte
}

type API_Peers map[string]API_Peerinfo // map[PubKey]API_Peerinfo

type JWTSecret [32]byte

const chars = "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
