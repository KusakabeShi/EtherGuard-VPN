package path

import (
	"encoding/binary"
	"errors"

	"github.com/KusakabeSi/EtherGuardVPN/config"
)

const EgHeaderLen = 12

type EgHeader struct {
	buf []byte
}

type Usage uint8

const (
	NornalPacket Usage = iota
	Register           //Register to server

	UpdatePeer //Comes from server
	UpdateNhTable

	PingPacket //Comes from other peer
	PongPacket //Send to everyone, include server
	RequestPeer
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
	return config.Vertex(binary.BigEndian.Uint32(e.buf[0:4]))
}
func (e EgHeader) SetDst(node_ID config.Vertex) {
	binary.BigEndian.PutUint32(e.buf[0:4], uint32(node_ID))
}

func (e EgHeader) GetSrc() config.Vertex {
	return config.Vertex(binary.BigEndian.Uint32(e.buf[4:8]))
}
func (e EgHeader) SetSrc(node_ID config.Vertex) {
	binary.BigEndian.PutUint32(e.buf[4:8], uint32(node_ID))
}

func (e EgHeader) GetTTL() uint8 {
	return e.buf[8]
}
func (e EgHeader) SetTTL(ttl uint8) {

	e.buf[8] = ttl
}

func (e EgHeader) GetUsage() Usage {
	return Usage(e.buf[9])
}
func (e EgHeader) SetUsage(usage Usage) {
	e.buf[9] = uint8(usage)
}

func (e EgHeader) GetPacketLength() uint16 {
	return binary.BigEndian.Uint16(e.buf[10:12])
}
func (e EgHeader) SetPacketLength(length uint16) {
	binary.BigEndian.PutUint16(e.buf[10:12], length)
}
