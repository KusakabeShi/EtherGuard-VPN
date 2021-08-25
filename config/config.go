package config

import (
	"crypto/rand"
	"strconv"
	"math"
)

const (
	Boardcast        Vertex = math.MaxUint32 - iota // Normal boardcast, boardcast with route table
	ControlMessage   Vertex = math.MaxUint32 - iota // p2p mode: boardcast to every know keer and prevent dup/ super mode: send to supernode
	PingMessage      Vertex = math.MaxUint32 - iota // boardsact to every know peer but don't transit
	SuperNodeMessage Vertex = math.MaxUint32 - iota
	Special_NodeID   Vertex = SuperNodeMessage
)

type EdgeConfig struct {
	Interface         InterfaceConf
	NodeID            Vertex
	NodeName          string
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
	StatePassword           string
	GraphRecalculateSetting GraphRecalculateSetting
	Peers                   []PeerInfo
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

type LoggerInfo struct {
	LogLevel   string
	LogTransit bool
	LogControl bool
	LogNormal  bool
}

// Nonnegative integer ID of vertex
type Vertex uint32

func (v *Vertex) ToString() string {
	switch *v{
	case Boardcast:
		return "B"
	case ControlMessage:
		return "C"
	case PingMessage:
		return "P"
	case SuperNodeMessage:
		return "S"
	default:
		return strconv.Itoa(int(*v))
	}
}

type DynamicRouteInfo struct {
	SendPingInterval float64
	DupCheckTimeout  float64
	ConnTimeOut      float64
	SaveNewPeers     bool
	SuperNode        SuperInfo
	P2P              P2Pinfo
	NTPconfig        NTPinfo
}

type NTPinfo struct {
	UseNTP       bool
	MaxServerUse int
	Servers      []string
}

type SuperInfo struct {
	UseSuperNode         bool
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
	PeerAliveTimeout        float64
	GraphRecalculateSetting GraphRecalculateSetting
}

type GraphRecalculateSetting struct {
	JitterTolerance           float64
	JitterToleranceMultiplier float64
	NodeReportTimeout         float64
	RecalculateCoolDown       float64
}

type DistTable map[Vertex]map[Vertex]float64
type NextHopTable map[Vertex]map[Vertex]*Vertex

type HTTP_Peerinfo struct {
	NodeID  Vertex
	PubKey  string
	PSKey   string
	Connurl map[string]bool
}
type HTTP_Peers map[string]HTTP_Peerinfo

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