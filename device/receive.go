/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package device

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/crypto/chacha20poly1305"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	"github.com/KusakabeSi/EtherGuardVPN/conn"
	"github.com/KusakabeSi/EtherGuardVPN/path"
	"github.com/KusakabeSi/EtherGuardVPN/tap"
)

type QueueHandshakeElement struct {
	msgType  path.Usage
	packet   []byte
	endpoint conn.Endpoint
	buffer   *[MaxMessageSize]byte
}

type QueueInboundElement struct {
	Type path.Usage
	sync.Mutex
	buffer   *[MaxMessageSize]byte
	packet   []byte
	counter  uint64
	keypair  *Keypair
	endpoint conn.Endpoint
}

// clearPointers clears elem fields that contain pointers.
// This makes the garbage collector's life easier and
// avoids accidentally keeping other objects around unnecessarily.
// It also reduces the possible collateral damage from use-after-free bugs.
func (elem *QueueInboundElement) clearPointers() {
	elem.buffer = nil
	elem.packet = nil
	elem.keypair = nil
	elem.endpoint = nil
}

/* Called when a new authenticated message has been received
 *
 * NOTE: Not thread safe, but called by sequential receiver!
 */
func (peer *Peer) keepKeyFreshReceiving() {
	if peer.timers.sentLastMinuteHandshake.Get() {
		return
	}
	keypair := peer.keypairs.Current()
	if keypair != nil && keypair.isInitiator && time.Since(keypair.created) > (RejectAfterTime-KeepaliveTimeout-RekeyTimeout) {
		peer.timers.sentLastMinuteHandshake.Set(true)
		peer.SendHandshakeInitiation(false)
	}
}

/* Receives incoming datagrams for the device
 *
 * Every time the bind is updated a new routine is started for
 * IPv4 and IPv6 (separately)
 */
func (device *Device) RoutineReceiveIncoming(recv conn.ReceiveFunc) {
	recvName := recv.PrettyName()
	defer func() {
		device.log.Verbosef("Routine: receive incoming %s - stopped", recvName)
		device.queue.decryption.wg.Done()
		device.queue.handshake.wg.Done()
		device.net.stopping.Done()
	}()

	device.log.Verbosef("Routine: receive incoming %s - started", recvName)

	// receive datagrams until conn is closed

	buffer := device.GetMessageBuffer()

	var (
		err         error
		size        int
		endpoint    conn.Endpoint
		deathSpiral int
	)

	for {
		size, endpoint, err = recv(buffer[:])

		if err != nil {
			device.PutMessageBuffer(buffer)
			if errors.Is(err, net.ErrClosed) {
				return
			}
			device.log.Verbosef("Failed to receive %s packet: %v", recvName, err)
			if neterr, ok := err.(net.Error); ok && !neterr.Temporary() {
				return
			}
			if deathSpiral < 10 {
				deathSpiral++
				time.Sleep(time.Second / 3)
				buffer = device.GetMessageBuffer()
				continue
			}
			return
		}
		deathSpiral = 0

		if size < MinMessageSize {
			continue
		}

		// check size of packet

		packet := buffer[:size]
		msgType := path.Usage(packet[0])
		msgType_wg := msgType
		if msgType >= path.MessageTransportType {
			msgType_wg = path.MessageTransportType
		}

		var okay bool

		switch msgType_wg {

		// check if transport

		case path.MessageTransportType:

			// check size

			if len(packet) < MessageTransportSize {
				continue
			}

			// lookup key pair

			receiver := binary.LittleEndian.Uint32(
				packet[MessageTransportOffsetReceiver:MessageTransportOffsetCounter],
			)
			value := device.indexTable.Lookup(receiver)
			keypair := value.keypair
			if keypair == nil {
				continue
			}

			// check keypair expiry

			if keypair.created.Add(RejectAfterTime).Before(time.Now()) {
				continue
			}

			// create work element
			peer := value.peer
			elem := device.GetInboundElement()
			elem.Type = msgType
			elem.packet = packet
			elem.buffer = buffer
			elem.keypair = keypair
			elem.endpoint = endpoint
			elem.counter = 0
			elem.Mutex = sync.Mutex{}
			elem.Lock()

			// add to decryption queues
			if peer.isRunning.Get() {
				peer.queue.inbound.c <- elem
				device.queue.decryption.c <- elem
				buffer = device.GetMessageBuffer()
			} else {
				device.PutInboundElement(elem)
			}
			continue

		// otherwise it is a fixed size & handshake related packet

		case path.MessageInitiationType:
			okay = len(packet) == MessageInitiationSize

		case path.MessageResponseType:
			okay = len(packet) == MessageResponseSize

		case path.MessageCookieReplyType:
			okay = len(packet) == MessageCookieReplySize

		default:
			device.log.Verbosef("Received message with unknown type")
		}

		if okay {
			select {
			case device.queue.handshake.c <- QueueHandshakeElement{
				msgType:  msgType,
				buffer:   buffer,
				packet:   packet,
				endpoint: endpoint,
			}:
				buffer = device.GetMessageBuffer()
			default:
			}
		}
	}
}

func (device *Device) RoutineDecryption(id int) {
	var nonce [chacha20poly1305.NonceSize]byte

	defer device.log.Verbosef("Routine: decryption worker %d - stopped", id)
	device.log.Verbosef("Routine: decryption worker %d - started", id)

	for elem := range device.queue.decryption.c {
		// split message into fields
		counter := elem.packet[MessageTransportOffsetCounter:MessageTransportOffsetContent]
		content := elem.packet[MessageTransportOffsetContent:]

		// decrypt and release to consumer
		var err error
		elem.counter = binary.LittleEndian.Uint64(counter)
		// copy counter to nonce
		binary.LittleEndian.PutUint64(nonce[0x4:0xc], elem.counter)
		elem.packet, err = elem.keypair.receive.Open(
			content[:0],
			nonce[:],
			content,
			nil,
		)
		if err != nil {
			elem.packet = nil
		}
		elem.Unlock()
	}
}

/* Handles incoming packets related to handshake
 */
func (device *Device) RoutineHandshake(id int) {
	defer func() {
		device.log.Verbosef("Routine: handshake worker %d - stopped", id)
		device.queue.encryption.wg.Done()
	}()
	device.log.Verbosef("Routine: handshake worker %d - started", id)

	for elem := range device.queue.handshake.c {

		// handle cookie fields and ratelimiting

		switch elem.msgType {

		case path.MessageCookieReplyType:

			// unmarshal packet

			var reply MessageCookieReply
			reader := bytes.NewReader(elem.packet)
			err := binary.Read(reader, binary.LittleEndian, &reply)
			if err != nil {
				device.log.Verbosef("Failed to decode cookie reply")
				goto skip
			}

			// lookup peer from index

			entry := device.indexTable.Lookup(reply.Receiver)

			if entry.peer == nil {
				goto skip
			}

			// consume reply

			if peer := entry.peer; peer.isRunning.Get() {
				device.log.Verbosef("Receiving cookie response from %s", elem.endpoint.DstToString())
				if !peer.cookieGenerator.ConsumeReply(&reply) {
					device.log.Verbosef("Could not decrypt invalid cookie response")
				}
			}

			goto skip

		case path.MessageInitiationType, path.MessageResponseType:

			// check mac fields and maybe ratelimit

			if !device.cookieChecker.CheckMAC1(elem.packet) {
				device.log.Verbosef("Received packet with invalid mac1")
				goto skip
			}

			// endpoints destination address is the source of the datagram

			if device.IsUnderLoad() {

				// verify MAC2 field

				if !device.cookieChecker.CheckMAC2(elem.packet, elem.endpoint.DstToBytes()) {
					device.SendHandshakeCookie(&elem)
					goto skip
				}

				// check ratelimiter

				if !device.rate.limiter.Allow(elem.endpoint.DstIP()) {
					goto skip
				}
			}

		default:
			device.log.Errorf("Invalid packet ended up in the handshake queue")
			goto skip
		}

		// handle handshake initiation/response content

		switch elem.msgType {
		case path.MessageInitiationType:

			// unmarshal

			var msg MessageInitiation
			reader := bytes.NewReader(elem.packet)
			err := binary.Read(reader, binary.LittleEndian, &msg)
			if err != nil {
				device.log.Errorf("Failed to decode initiation message")
				goto skip
			}

			// consume initiation

			peer := device.ConsumeMessageInitiation(&msg)
			if peer == nil {
				device.log.Verbosef("Received invalid initiation message from %s", elem.endpoint.DstToString())
				goto skip
			}

			// update timers

			peer.timersAnyAuthenticatedPacketTraversal()
			peer.timersAnyAuthenticatedPacketReceived()

			// update endpoint
			peer.SetEndpointFromPacket(elem.endpoint)

			device.log.Verbosef("%v - Received handshake initiation", peer)
			atomic.AddUint64(&peer.stats.rxBytes, uint64(len(elem.packet)))

			peer.SendHandshakeResponse()

		case path.MessageResponseType:

			// unmarshal

			var msg MessageResponse
			reader := bytes.NewReader(elem.packet)
			err := binary.Read(reader, binary.LittleEndian, &msg)
			if err != nil {
				device.log.Errorf("Failed to decode response message")
				goto skip
			}

			// consume response

			peer := device.ConsumeMessageResponse(&msg)
			if peer == nil {
				device.log.Verbosef("Received invalid response message from %s", elem.endpoint.DstToString())
				goto skip
			}

			// update endpoint
			peer.SetEndpointFromPacket(elem.endpoint)

			device.log.Verbosef("%v - Received handshake response", peer)
			atomic.AddUint64(&peer.stats.rxBytes, uint64(len(elem.packet)))

			// update timers

			peer.timersAnyAuthenticatedPacketTraversal()
			peer.timersAnyAuthenticatedPacketReceived()

			// derive keypair

			err = peer.BeginSymmetricSession()

			if err != nil {
				device.log.Errorf("%v - Failed to derive keypair: %v", peer, err)
				goto skip
			}

			peer.timersSessionDerived()
			peer.timersHandshakeComplete()
			peer.SendKeepalive()
		}
	skip:
		device.PutMessageBuffer(elem.buffer)
	}
}

func (peer *Peer) RoutineSequentialReceiver() {
	device := peer.device
	var peer_out *Peer
	defer func() {
		device.log.Verbosef("%v - Routine: sequential receiver - stopped", peer)
		peer.stopping.Done()
	}()
	device.log.Verbosef("%v - Routine: sequential receiver - started", peer)

	for elem := range peer.queue.inbound.c {
		if elem == nil {
			return
		}
		var EgHeader path.EgHeader
		var err error
		var src_nodeID config.Vertex
		var dst_nodeID config.Vertex
		var packet_type path.Usage
		should_process := false
		should_receive := false
		should_transfer := false
		elem.Lock()
		if elem.packet == nil {
			// decryption failed
			goto skip
		}

		if !elem.keypair.replayFilter.ValidateCounter(elem.counter, RejectAfterMessages) {
			goto skip
		}

		peer.SetEndpointFromPacket(elem.endpoint)
		if peer.ReceivedWithKeypair(elem.keypair) {
			peer.timersHandshakeComplete()
			peer.SendStagedPackets()
		}

		peer.keepKeyFreshReceiving()
		peer.timersAnyAuthenticatedPacketTraversal()
		peer.timersAnyAuthenticatedPacketReceived()
		atomic.AddUint64(&peer.stats.rxBytes, uint64(len(elem.packet)+MinMessageSize))

		if len(elem.packet) == 0 {
			device.log.Verbosef("%v - Receiving keepalive packet", peer)
			goto skip
		}
		peer.timersDataReceived()

		if len(elem.packet) <= path.EgHeaderLen {
			device.log.Errorf("Invalid EgHeader from peer %v", peer)
			goto skip
		}
		EgHeader, err = path.NewEgHeader(elem.packet[0:path.EgHeaderLen]) // EG header
		src_nodeID = EgHeader.GetSrc()
		dst_nodeID = EgHeader.GetDst()
		elem.packet = elem.packet[:EgHeader.GetPacketLength()+path.EgHeaderLen] // EG header + true packet
		packet_type = elem.Type

		if device.IsSuperNode {
			peer.LastPingReceived = time.Now()
			switch dst_nodeID {
			case config.ControlMessage:
				should_process = true
			case config.SuperNodeMessage:
				should_process = true
			default:
				device.log.Errorf("Invalid dst_nodeID received. Check your code for bug")
			}
		} else {
			switch dst_nodeID {
			case config.Boardcast:
				should_receive = true
				should_transfer = true
			case config.SuperNodeMessage:
				should_process = true
			case config.ControlMessage:
				packet := elem.packet[path.EgHeaderLen:] //true packet
				if device.CheckNoDup(packet) {
					should_process = true
					should_transfer = true
				} else {
					should_process = false
					should_transfer = false
					if device.LogLevel.LogTransit {
						fmt.Printf("Transit: Duplicate packet received from %d through %d , src_nodeID = %d . Dropeed.\n", peer.ID, device.ID, src_nodeID)
					}
				}
			case device.ID:
				if packet_type == path.NornalPacket {
					should_receive = true
				} else {
					should_process = true
				}
			default:
				if device.graph.Next(device.ID, dst_nodeID) != nil {
					should_transfer = true
				} else {
					device.log.Verbosef("No route to peer ID %v", dst_nodeID)
				}
			}
		}
		if should_transfer {
			l2ttl := EgHeader.GetTTL()
			if l2ttl == 0 {
				device.log.Verbosef("TTL is 0 %v", dst_nodeID)
			} else {
				EgHeader.SetTTL(l2ttl - 1)
				if dst_nodeID == config.Boardcast { //Regular transfer algorithm
					device.TransitBoardcastPacket(src_nodeID, peer.ID, elem.Type, elem.packet, MessageTransportOffsetContent)
				} else if dst_nodeID == config.ControlMessage { // Control Message will try send to every know node regardless the connectivity
					skip_list := make(map[config.Vertex]bool)
					skip_list[src_nodeID] = true //Don't send to conimg peer and source peer
					skip_list[peer.ID] = true
					device.SpreadPacket(skip_list, elem.Type, elem.packet, MessageTransportOffsetContent)

				} else {
					next_id := device.graph.Next(device.ID, dst_nodeID)
					if next_id != nil {
						peer_out = device.peers.IDMap[*next_id]
						if device.LogLevel.LogTransit {
							fmt.Printf("Transit: Transfer packet from %d through %d to %d\n", peer.ID, device.ID, peer_out.ID)
						}
						device.SendPacket(peer_out, elem.Type, elem.packet, MessageTransportOffsetContent)
					}
				}
			}
		}

		if should_process {
			if packet_type != path.NornalPacket {
				if device.LogLevel.LogControl {
					if peer.GetEndpointDstStr() != "" {
						fmt.Println("Control: Received From:" + peer.GetEndpointDstStr() + " " + device.sprint_received(packet_type, elem.packet[path.EgHeaderLen:]))
					}
				}
				err = device.process_received(packet_type, peer, elem.packet[path.EgHeaderLen:])
				if err != nil {
					device.log.Errorf(err.Error())
				}
			}
		}

		if should_receive { // Write message to tap device
			if packet_type == path.NornalPacket {
				if device.LogLevel.LogNormal {
					fmt.Println("Normal: Reveived Normal packet From:" + peer.GetEndpointDstStr() + " SrcID:" + src_nodeID.ToString() + " DstID:" + dst_nodeID.ToString() + " Len:" + strconv.Itoa(len(elem.packet)))
				}
				if len(elem.packet) <= path.EgHeaderLen+12 {
					device.log.Errorf("Invalid normal packet from peer %v", peer.ID.ToString())
					goto skip
				}
				src_macaddr := tap.GetSrcMacAddr(elem.packet[path.EgHeaderLen:])
				if !tap.IsNotUnicast(src_macaddr) {
					actual, loaded := device.l2fib.LoadOrStore(src_macaddr, src_nodeID)
					if loaded {
						if actual.(config.Vertex) != src_nodeID {
							device.l2fib.Store(src_macaddr, src_nodeID) // Write to l2fib table
						}
					}
				}
				_, err = device.tap.device.Write(elem.buffer[:MessageTransportOffsetContent+len(elem.packet)], MessageTransportOffsetContent+path.EgHeaderLen)
				if err != nil && !device.isClosed() {
					device.log.Errorf("Failed to write packet to TUN device: %v", err)
				}
				if len(peer.queue.inbound.c) == 0 {
					err = device.tap.device.Flush()
					if err != nil {
						peer.device.log.Errorf("Unable to flush packets: %v", err)
					}
				}
			}
		}

	skip:
		device.PutMessageBuffer(elem.buffer)
		device.PutInboundElement(elem)
	}
}
