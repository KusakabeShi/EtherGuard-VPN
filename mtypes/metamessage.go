package mtypes

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt"
)

func GetByte(structIn interface{}) (bb []byte, err error) {
	var b bytes.Buffer
	e := gob.NewEncoder(&b)
	if err := e.Encode(structIn); err != nil {
		panic(err)
	}
	bb = b.Bytes()
	return
}

const Infinity = float64(99999)

type RegisterMsg struct {
	Node_id             Vertex
	Version             string
	PeerStateHash       string
	NhStateHash         string
	SuperParamStateHash string
	JWTSecret           JWTSecret
	HttpPostCount       uint64
}

func Hash2Str(h string) string {
	n := 10
	if len(h) > n-3 {
		return h[:n] + "..."
	}
	return "\"" + h + "\""
}

func (c *RegisterMsg) ToString() string {
	return fmt.Sprint("RegisterMsg Node_id:"+c.Node_id.ToString(), " Version:"+c.Version, " PeerHash:"+Hash2Str(c.PeerStateHash), " NhHash:"+Hash2Str(c.NhStateHash), " SuperParamHash:"+Hash2Str(c.SuperParamStateHash))
}

func ParseRegisterMsg(bin []byte) (StructPlace RegisterMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

type ServerCommand int

const (
	NoAction ServerCommand = iota
	Shutdown
	ThrowError
	Panic
	UpdatePeer
	UpdateNhTable
	UpdateSuperParams
)

func (a *ServerCommand) ToString() string {
	switch *a {
	case Shutdown:
		return "Shutdown"
	case ThrowError:
		return "ThrowError"
	case Panic:
		return "Panic"
	case UpdatePeer:
		return "UpdatePeer"
	case UpdateNhTable:
		return "UpdateNhTable"
	case UpdateSuperParams:
		return "UpdateSuperParams"
	default:
		return "Unknown"
	}
}

type ServerUpdateMsg struct {
	Node_id Vertex
	Action  ServerCommand
	Code    int
	Params  string
}

func ParseServerUpdateMsg(bin []byte) (StructPlace ServerUpdateMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

func (c *ServerUpdateMsg) ToString() string {
	return "ServerUpdateMsg Node_id:" + c.Node_id.ToString() + " Action:" + c.Action.ToString() + " Code:" + strconv.Itoa(int(c.Code)) + " Params: " + c.Params
}

type PingMsg struct {
	RequestID    uint32
	Src_nodeID   Vertex
	Time         time.Time
	RequestReply int
}

func (c *PingMsg) ToString() string {
	return "PingMsg SID:" + c.Src_nodeID.ToString() + " Time:" + c.Time.String() + " RequestID:" + strconv.Itoa(int(c.RequestID))
}

func ParsePingMsg(bin []byte) (StructPlace PingMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

type PongMsg struct {
	RequestID      uint32
	Src_nodeID     Vertex
	Dst_nodeID     Vertex
	Timediff       float64
	TimeToAlive    float64
	AdditionalCost float64
}

func (c *PongMsg) ToString() string {
	return "PongMsg SID:" + c.Src_nodeID.ToString() + " DID:" + c.Dst_nodeID.ToString() + " Timediff:" + S2TD(c.Timediff).String() + " TTL:" + S2TD(c.TimeToAlive).String() + " RequestID:" + strconv.Itoa(int(c.RequestID))
}

func ParsePongMsg(bin []byte) (StructPlace PongMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

type QueryPeerMsg struct {
	Request_ID uint32
}

func (c *QueryPeerMsg) ToString() string {
	return "QueryPeerMsg Request_ID:" + strconv.Itoa(int(c.Request_ID))
}

func ParseQueryPeerMsg(bin []byte) (StructPlace QueryPeerMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

type BoardcastPeerMsg struct {
	Request_ID uint32
	NodeID     Vertex
	PubKey     [32]byte
	ConnURL    string
}

func (c *BoardcastPeerMsg) ToString() string {
	return "BoardcastPeerMsg Request_ID:" + strconv.Itoa(int(c.Request_ID)) + " NodeID:" + c.NodeID.ToString() + " ConnURL:" + c.ConnURL
}

func ParseBoardcastPeerMsg(bin []byte) (StructPlace BoardcastPeerMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

type API_report_peerinfo struct {
	Pongs    []PongMsg
	LocalV4s map[string]float64
	LocalV6s map[string]float64
}

func ParseAPI_report_peerinfo(bin []byte) (StructPlace API_report_peerinfo, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

type API_report_peerinfo_jwt_claims struct {
	PostCount uint64
	BodyHash  string
	jwt.StandardClaims
}

type SUPER_Events struct {
	Event_server_pong     chan PongMsg
	Event_server_register chan RegisterMsg
}
