package config

type EdgeConfig struct {
	Interface    InterfaceConf
	NodeID       Vertex
	NodeName     string
	PrivKey      string
	ListenPort   int
	LogLevel     LoggerInfo
	DynamicRoute DynamicRouteInfo
	NextHopTable NextHopTable
	Peers        []PeerInfo
}

type SuperConfig struct {
	NodeName                string
	PrivKeyV4               string
	PrivKeyV6               string
	ListenPort              int
	LogLevel                LoggerInfo
	RePushConfigInterval    float64
	GraphRecalculateSetting GraphRecalculateSetting
	Peers                   []PeerInfo
}

type InterfaceConf struct {
	Itype         string
	IfaceID       int
	Name          string
	MacAddr       string
	MTU           int
	RecvAddr      string
	SendAddr      string
	HumanFriendly bool
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
}

// Nonnegative integer ID of vertex
type Vertex uint32

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
type HTTP_Peers struct {
	Peers map[string]HTTP_Peerinfo
}
