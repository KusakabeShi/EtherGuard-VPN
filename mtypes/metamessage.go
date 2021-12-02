package mtypes

import (
	"bytes"
	"encoding/base64"
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

type RegisterMsg struct {
	Node_id       Vertex
	Version       string
	PeerStateHash [32]byte
	NhStateHash   [32]byte
	JWTSecret     JWTSecret
	HttpPostCount uint64
}

func Hash2Str(h []byte) string {
	for _, v := range h {
		if v != 0 {
			return base64.StdEncoding.EncodeToString(h)[:10] + "..."
		}
	}
	return "\"\""
}

func (c *RegisterMsg) ToString() string {
	return fmt.Sprint("RegisterMsg Node_id:"+c.Node_id.ToString(), " Version:"+c.Version, " PeerHash:"+Hash2Str(c.PeerStateHash[:]), " NhHash:"+Hash2Str(c.NhStateHash[:]))
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
)

func (a *ServerCommand) ToString() string {
	if *a == Shutdown {
		return "Shutdown"
	} else if *a == ThrowError {
		return "ThrowError"
	} else if *a == Panic {
		return "Panic"
	}
	return "Unknown"
}

type ServerCommandMsg struct {
	Node_id   Vertex
	Action    ServerCommand
	ErrorCode int
	ErrorMsg  string
}

func ParseUpdateErrorMsg(bin []byte) (StructPlace ServerCommandMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

func (c *ServerCommandMsg) ToString() string {
	return "ServerCommandMsg Node_id:" + c.Node_id.ToString() + " Action:" + c.Action.ToString() + " ErrorCode:" + strconv.Itoa(int(c.ErrorCode)) + " ErrorMsg " + c.ErrorMsg
}

type UpdatePeerMsg struct {
	State_hash [32]byte
}

func (c *UpdatePeerMsg) ToString() string {
	return "UpdatePeerMsg State_hash:" + string(c.State_hash[:])
}

func ParseUpdatePeerMsg(bin []byte) (StructPlace UpdatePeerMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

type UpdateNhTableMsg struct {
	State_hash [32]byte
}

func (c *UpdateNhTableMsg) ToString() string {
	return "UpdateNhTableMsg State_hash:" + string(c.State_hash[:])
}

func ParseUpdateNhTableMsg(bin []byte) (StructPlace UpdateNhTableMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
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
