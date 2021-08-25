package path

import (
	"bytes"
	"encoding/gob"
	"strconv"
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
	Node_id       config.Vertex `struc:"uint32"`
	PeerStateHash [32]byte
	NhStateHash   [32]byte
	Name          string
}

func (c *RegisterMsg) ToString() string {
	return "RegisterMsg Node_id:" + c.Node_id.ToString()
}

func ParseRegisterMsg(bin []byte) (StructPlace RegisterMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

type UpdatePeerMsg struct {
	State_hash [32]byte `struc:"[32]uint8"`
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
	State_hash [32]byte `struc:"[32]uint8"`
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
	RequestID  uint32        `struc:"uint32"`
	Src_nodeID config.Vertex `struc:"uint32"`
	Time       time.Time     `struc:"uint64"`
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
	RequestID  uint32
	Src_nodeID config.Vertex `struc:"uint32"`
	Dst_nodeID config.Vertex `struc:"uint32"`
	Timediff   time.Duration `struc:"int64"`
}

func (c *PongMsg) ToString() string {
	return "PongMsg SID:" + c.Src_nodeID.ToString() + " DID:" + c.Dst_nodeID.ToString() + " Timediff:" + c.Timediff.String() + " RequestID:" + strconv.Itoa(int(c.RequestID))
}

func ParsePongMsg(bin []byte) (StructPlace PongMsg, err error) {
	var b bytes.Buffer
	b.Write(bin)
	d := gob.NewDecoder(&b)
	err = d.Decode(&StructPlace)
	return
}

type QueryPeerMsg struct {
	Request_ID uint32 `struc:"uint32"`
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
	Request_ID uint32        `struc:"uint32"`
	NodeID     config.Vertex `struc:"uint32"`
	PubKey     [32]byte      `struc:"[32]uint8"`
	PSKey      [32]byte      `struc:"[32]uint8"`
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

type SUPER_Events struct {
	Event_server_pong            chan PongMsg
	Event_server_register        chan RegisterMsg
	Event_server_NhTable_changed chan struct{}
}
