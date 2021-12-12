package path

import (
	"encoding/binary"
	"errors"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
)

const EgHeaderLen =4

type EgHeader struct {
	buf []byte
}

type Usage uint8

const (
	MessageInitiationType Usage = iota
	MessageResponseType
	MessageCookieReplyType
	MessageTransportType

	NormalPacket

	Register     //Send to server
	ServerUpdate //Comes from server

	PingPacket //Comes from other peer
	PongPacket //Send to everyone, include server
	QueryPeer
	BroadcastPeer
)

func (v Usage) IsNormal() bool {
	return v == NormalPacket
}

func (v Usage) IsControl() bool {
	switch v {
	case Register:
		return true
	case ServerUpdate:
		return true
	case PingPacket:
		return true
	case PongPacket:
		return true
	case QueryPeer:
		return true
	case BroadcastPeer:
		return true
	default:
		return false
	}
}

func (v Usage) IsControl_Super2Edge() bool {
	switch v {
	case ServerUpdate:
		return true
	default:
		return false
	}
}

func (v Usage) IsControl_Edge2Super() bool {
	switch v {
	case Register:
		return true
	case PongPacket:
		return true
	default:
		return false
	}
}

func (v Usage) IsControl_Edge2Edge() bool {
	switch v {
	case PingPacket:
		return true
	case PongPacket:
		return true
	case QueryPeer:
		return true
	case BroadcastPeer:
		return true
	default:
		return false
	}
}

func NewEgHeader(pac []byte, mtu uint16) (e EgHeader, err error) {
	if len(pac) != EgHeaderLen {
		err = errors.New("invalid packet size")
		return
	}
	e.buf = pac
	return
}

func (e EgHeader) GetDst() mtypes.Vertex {
	return mtypes.Vertex(binary.BigEndian.Uint16(e.buf[0:2]))
}
func (e EgHeader) SetDst(node_ID mtypes.Vertex) {
	binary.BigEndian.PutUint16(e.buf[0:2], uint16(node_ID))
}

func (e EgHeader) GetSrc() mtypes.Vertex {
	return mtypes.Vertex(binary.BigEndian.Uint16(e.buf[2:4]))
}
func (e EgHeader) SetSrc(node_ID mtypes.Vertex) {
	binary.BigEndian.PutUint16(e.buf[2:4], uint16(node_ID))
}
