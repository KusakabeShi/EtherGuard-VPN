package path

import (
	"bytes"
	"encoding/gob"
	"time"

	"github.com/KusakabeSi/EtherGuardVPN/config"
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
	Node_id config.Vertex
}

func ParseRegisterMsg(bin []byte) (RegisterMsg, error) {
	var StructPlace RegisterMsg
	var b bytes.Buffer
	d := gob.NewDecoder(&b)
	err := d.Decode(&StructPlace)
	return StructPlace, err
}

type UpdatePeerMsg struct {
	State_hash [32]byte
}

func ParseUpdatePeerMsg(bin []byte) (UpdatePeerMsg, error) {
	var StructPlace UpdatePeerMsg
	var b bytes.Buffer
	d := gob.NewDecoder(&b)
	err := d.Decode(&StructPlace)
	return StructPlace, err
}

type UpdateNhTableMsg struct {
	State_hash [32]byte
}

func ParseUpdateNhTableMsg(bin []byte) (UpdateNhTableMsg, error) {
	var StructPlace UpdateNhTableMsg
	var b bytes.Buffer
	d := gob.NewDecoder(&b)
	err := d.Decode(&StructPlace)
	return StructPlace, err
}

type PingMsg struct {
	Src_nodeID config.Vertex
	Time       time.Time
}

func ParsePingMsg(bin []byte) (PingMsg, error) {
	var StructPlace PingMsg
	var b bytes.Buffer
	d := gob.NewDecoder(&b)
	err := d.Decode(&StructPlace)
	return StructPlace, err
}

type PongMsg struct {
	Src_nodeID config.Vertex
	Dst_nodeID config.Vertex
	Timediff   time.Duration
}

func ParsePongMsg(bin []byte) (PongMsg, error) {
	var StructPlace PongMsg
	var b bytes.Buffer
	d := gob.NewDecoder(&b)
	err := d.Decode(&StructPlace)
	return StructPlace, err
}

type RequestPeerMsg struct {
	Request_ID uint32
}

func ParseRequestPeerMsg(bin []byte) (RequestPeerMsg, error) {
	var StructPlace RequestPeerMsg
	var b bytes.Buffer
	d := gob.NewDecoder(&b)
	err := d.Decode(&StructPlace)
	return StructPlace, err
}

type BoardcastPeerMsg struct {
	RequestID uint32
	NodeID    config.Vertex
	PubKey    [32]byte
	PSKey     [32]byte
	ConnURL   string
}

func ParseBoardcastPeerMsg(bin []byte) (BoardcastPeerMsg, error) {
	var StructPlace BoardcastPeerMsg
	var b bytes.Buffer
	d := gob.NewDecoder(&b)
	err := d.Decode(&StructPlace)
	return StructPlace, err
}

type SUPER_Events struct {
	Event_server_pong            chan PongMsg
	Event_server_register        chan RegisterMsg
	Event_server_NhTable_changed chan struct{}
}
