package path

import (
	"encoding/binary"
	"errors"

	"github.com/KusakabeSi/EtherGuardVPN/config"
)

const EgHeaderLen = 7

type EgHeader struct {
	buf []byte
}

type Usage uint8

const (
	MessageInitiationType Usage = iota
	MessageResponseType
	MessageCookieReplyType
	MessageTransportType

	NornalPacket
	Register //Register to server

	UpdatePeer //Comes from server
	UpdateNhTable
	UpdateError

	PingPacket //Comes from other peer
	PongPacket //Send to everyone, include server
	QueryPeer
	BoardcastPeer
)

func NewEgHeader(pac []byte) (e EgHeader, err error) {
	if len(pac) != EgHeaderLen {
		err = errors.New("Invalid packet size")
		return
	}
	e.buf = pac
	return
}

func (e EgHeader) GetDst() config.Vertex {
	return config.Vertex(binary.BigEndian.Uint16(e.buf[0:2]))
}
func (e EgHeader) SetDst(node_ID config.Vertex) {
	binary.BigEndian.PutUint16(e.buf[0:2], uint16(node_ID))
}

func (e EgHeader) GetSrc() config.Vertex {
	return config.Vertex(binary.BigEndian.Uint16(e.buf[2:4]))
}
func (e EgHeader) SetSrc(node_ID config.Vertex) {
	binary.BigEndian.PutUint16(e.buf[2:4], uint16(node_ID))
}

func (e EgHeader) GetTTL() uint8 {
	return e.buf[4]
}
func (e EgHeader) SetTTL(ttl uint8) {
	e.buf[4] = ttl
}

func (e EgHeader) GetPacketLength() uint16 {
	return binary.BigEndian.Uint16(e.buf[5:7])
}
func (e EgHeader) SetPacketLength(length uint16) {
	binary.BigEndian.PutUint16(e.buf[5:7], length)
}
