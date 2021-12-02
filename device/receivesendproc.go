package device

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io/ioutil"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/path"
	"github.com/KusakabeSi/EtherGuard-VPN/tap"
	"github.com/golang-jwt/jwt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

func (device *Device) SendPacket(peer *Peer, usage path.Usage, packet []byte, offset int) {
	if peer == nil {
		return
	} else if peer.endpoint == nil {
		return
	}
	if usage == path.NormalPacket && len(packet)-path.EgHeaderLen <= 12 {
		if device.LogLevel.LogNormal {
			fmt.Println("Normal: Invalid packet: Ethernet packet too small")
		}
		return
	}

	if device.LogLevel.LogNormal {
		EgHeader, _ := path.NewEgHeader(packet[:path.EgHeaderLen])
		if usage == path.NormalPacket && EgHeader.GetSrc() == device.ID {
			dst_nodeID := EgHeader.GetDst()
			packet_len := len(packet) - path.EgHeaderLen
			fmt.Println("Normal: Send Normal packet To:" + peer.GetEndpointDstStr() + " SrcID:" + device.ID.ToString() + " DstID:" + dst_nodeID.ToString() + " Len:" + strconv.Itoa(packet_len))
			packet := gopacket.NewPacket(packet[path.EgHeaderLen:], layers.LayerTypeEthernet, gopacket.Default)
			fmt.Println(packet.Dump())
		}
	}
	if device.LogLevel.LogControl {
		if usage != path.NormalPacket {
			if peer.GetEndpointDstStr() != "" {
				fmt.Println("Control: Send To:" + peer.GetEndpointDstStr() + " " + device.sprint_received(usage, packet[path.EgHeaderLen:]))
			}
		}
	}

	var elem *QueueOutboundElement
	elem = device.NewOutboundElement()
	copy(elem.buffer[offset:offset+len(packet)], packet)
	elem.Type = usage
	elem.packet = elem.buffer[offset : offset+len(packet)]
	if peer.isRunning.Get() {
		peer.StagePacket(elem)
		elem = nil
		peer.SendStagedPackets()
	}
}

func (device *Device) BoardcastPacket(skip_list map[mtypes.Vertex]bool, usage path.Usage, packet []byte, offset int) { // Send packet to all connected peers
	send_list := device.graph.GetBoardcastList(device.ID)
	for node_id, _ := range skip_list {
		send_list[node_id] = false
	}
	device.peers.RLock()
	for node_id, should_send := range send_list {
		if should_send {
			peer_out, _ := device.peers.IDMap[node_id]
			device.SendPacket(peer_out, usage, packet, offset)
		}
	}
	device.peers.RUnlock()
}

func (device *Device) SpreadPacket(skip_list map[mtypes.Vertex]bool, usage path.Usage, packet []byte, offset int) { // Send packet to all peers no matter it is alive
	device.peers.RLock()
	for peer_id, peer_out := range device.peers.IDMap {
		if _, ok := skip_list[peer_id]; ok {
			if device.LogLevel.LogTransit {
				fmt.Printf("Transit: Skipped Spread Packet packet through %d to %d\n", device.ID, peer_out.ID)
			}
			continue
		}
		device.SendPacket(peer_out, usage, packet, MessageTransportOffsetContent)
	}
	device.peers.RUnlock()
}

func (device *Device) TransitBoardcastPacket(src_nodeID mtypes.Vertex, in_id mtypes.Vertex, usage path.Usage, packet []byte, offset int) {
	node_boardcast_list := device.graph.GetBoardcastThroughList(device.ID, in_id, src_nodeID)
	device.peers.RLock()
	for peer_id := range node_boardcast_list {
		peer_out := device.peers.IDMap[peer_id]
		if device.LogLevel.LogTransit {
			fmt.Printf("Transit: Transfer packet from %d through %d to %d\n", in_id, device.ID, peer_out.ID)
		}
		device.SendPacket(peer_out, usage, packet, offset)
	}
	device.peers.RUnlock()
}

func (device *Device) Send2Super(usage path.Usage, packet []byte, offset int) {
	device.peers.RLock()
	if device.DRoute.SuperNode.UseSuperNode {
		for _, peer_out := range device.peers.SuperPeer {
			/*if device.LogTransit {
				fmt.Printf("Send to supernode %s\n", peer_out.endpoint.DstToString())
			}*/
			device.SendPacket(peer_out, usage, packet, offset)
		}
	}
	device.peers.RUnlock()
}

func (device *Device) CheckNoDup(packet []byte) bool {
	hasher := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	hasher.Write(packet)
	crc32result := hasher.Sum32()
	_, ok := device.DupData.Get(crc32result)
	device.DupData.Set(crc32result, true)
	return !ok
}

func (device *Device) process_received(msg_type path.Usage, peer *Peer, body []byte) (err error) {
	if device.IsSuperNode {
		switch msg_type {
		case path.Register:
			if content, err := mtypes.ParseRegisterMsg(body); err == nil {
				return device.server_process_RegisterMsg(peer, content)
			}
		case path.PongPacket:
			if content, err := mtypes.ParsePongMsg(body); err == nil {
				return device.server_process_Pong(peer, content)
			}
		default:
			err = errors.New("Not a valid msg_type")
		}
	} else {
		switch msg_type {
		case path.UpdatePeer:
			if content, err := mtypes.ParseUpdatePeerMsg(body); err == nil {
				go device.process_UpdatePeerMsg(peer, content)
			}
		case path.UpdateNhTable:
			if content, err := mtypes.ParseUpdateNhTableMsg(body); err == nil {
				go device.process_UpdateNhTableMsg(peer, content)
			}
		case path.UpdateError:
			if content, err := mtypes.ParseUpdateErrorMsg(body); err == nil {
				device.process_UpdateErrorMsg(peer, content)
			}
		case path.PingPacket:
			if content, err := mtypes.ParsePingMsg(body); err == nil {
				return device.process_ping(peer, content)
			}
		case path.PongPacket:
			if content, err := mtypes.ParsePongMsg(body); err == nil {
				return device.process_pong(peer, content)
			}
		case path.QueryPeer:
			if content, err := mtypes.ParseQueryPeerMsg(body); err == nil {
				return device.process_RequestPeerMsg(content)
			}
		case path.BroadcastPeer:
			if content, err := mtypes.ParseBoardcastPeerMsg(body); err == nil {
				return device.process_BoardcastPeerMsg(peer, content)
			}
		default:
			err = errors.New("Not a valid msg_type")
		}
	}
	return
}

func (device *Device) sprint_received(msg_type path.Usage, body []byte) string {
	switch msg_type {
	case path.Register:
		if content, err := mtypes.ParseRegisterMsg(body); err == nil {
			return content.ToString()
		}
		return "RegisterMsg: Parse failed"
	case path.UpdatePeer:
		if content, err := mtypes.ParseUpdatePeerMsg(body); err == nil {
			return content.ToString()
		}
		return "UpdatePeerMsg: Parse failed"
	case path.UpdateNhTable:
		if content, err := mtypes.ParseUpdateNhTableMsg(body); err == nil {
			return content.ToString()
		}
		return "UpdateNhTableMsg: Parse failed"
	case path.UpdateError:
		if content, err := mtypes.ParseUpdateErrorMsg(body); err == nil {
			return content.ToString()
		}
		return "UpdateErrorMsg: Parse failed"
	case path.PingPacket:
		if content, err := mtypes.ParsePingMsg(body); err == nil {
			return content.ToString()
		}
		return "PingPacketMsg: Parse failed"
	case path.PongPacket:
		if content, err := mtypes.ParsePongMsg(body); err == nil {
			return content.ToString()
		}
		return "PongPacketMsg: Parse failed"
	case path.QueryPeer:
		if content, err := mtypes.ParseQueryPeerMsg(body); err == nil {
			return content.ToString()
		}
		return "QueryPeerMsg: Parse failed"
	case path.BroadcastPeer:
		if content, err := mtypes.ParseBoardcastPeerMsg(body); err == nil {
			return content.ToString()
		}
		return "BoardcastPeerMsg: Parse failed"
	default:
		return "UnknownMsg: Not a valid msg_type"
	}
}

func (device *Device) GeneratePingPacket(src_nodeID mtypes.Vertex, request_reply int) ([]byte, path.Usage, error) {
	body, err := mtypes.GetByte(&mtypes.PingMsg{
		Src_nodeID:   src_nodeID,
		Time:         device.graph.GetCurrentTime(),
		RequestReply: request_reply,
	})
	if err != nil {
		return nil, path.PingPacket, err
	}
	buf := make([]byte, path.EgHeaderLen+len(body))
	header, _ := path.NewEgHeader(buf[0:path.EgHeaderLen])
	if err != nil {
		return nil, path.PingPacket, err
	}
	header.SetDst(mtypes.ControlMessage)
	header.SetTTL(0)
	header.SetSrc(device.ID)
	header.SetPacketLength(uint16(len(body)))
	copy(buf[path.EgHeaderLen:], body)
	return buf, path.PingPacket, nil
}

func (device *Device) SendPing(peer *Peer, times int, replies int, interval float64) {
	for i := 0; i < times; i++ {
		packet, usage, _ := device.GeneratePingPacket(device.ID, replies)
		device.SendPacket(peer, usage, packet, MessageTransportOffsetContent)
		time.Sleep(path.S2TD(interval))
	}
}

func compareVersion(v1 string, v2 string) bool {
	if strings.Contains(v1, "-") {
		v1 = strings.Split(v1, "-")[0]
	}
	if strings.Contains(v2, "-") {
		v2 = strings.Split(v2, "-")[0]
	}
	return v1 == v2
}

func (device *Device) server_process_RegisterMsg(peer *Peer, content mtypes.RegisterMsg) error {
	UpdateErrorMsg := mtypes.ServerCommandMsg{
		Node_id:   peer.ID,
		Action:    mtypes.NoAction,
		ErrorCode: 0,
		ErrorMsg:  "",
	}
	if peer.ID != content.Node_id {
		UpdateErrorMsg = mtypes.ServerCommandMsg{
			Node_id:   peer.ID,
			Action:    mtypes.ThrowError,
			ErrorCode: int(syscall.EPERM),
			ErrorMsg:  fmt.Sprintf("Your nodeID: %v is not match with registered nodeID: %v", content.Node_id, peer.ID),
		}
	}
	if compareVersion(content.Version, device.Version) == false {
		UpdateErrorMsg = mtypes.ServerCommandMsg{
			Node_id:   peer.ID,
			Action:    mtypes.ThrowError,
			ErrorCode: int(syscall.ENOSYS),
			ErrorMsg:  fmt.Sprintf("Your version: \"%v\" is not compatible with our version: \"%v\"", content.Version, device.Version),
		}
	}
	if UpdateErrorMsg.Action != mtypes.NoAction {
		body, err := mtypes.GetByte(&UpdateErrorMsg)
		if err != nil {
			return err
		}
		buf := make([]byte, path.EgHeaderLen+len(body))
		header, err := path.NewEgHeader(buf[:path.EgHeaderLen])
		header.SetSrc(device.ID)
		header.SetTTL(device.DefaultTTL)
		header.SetPacketLength(uint16(len(body)))
		copy(buf[path.EgHeaderLen:], body)
		header.SetDst(mtypes.SuperNodeMessage)
		device.SendPacket(peer, path.UpdateError, buf, MessageTransportOffsetContent)
		return nil
	}
	device.Event_server_register <- content
	return nil
}

func (device *Device) server_process_Pong(peer *Peer, content mtypes.PongMsg) error {
	device.Event_server_pong <- content
	return nil
}

func (device *Device) process_ping(peer *Peer, content mtypes.PingMsg) error {
	Timediff := device.graph.GetCurrentTime().Sub(content.Time).Seconds()
	peer.SingleWayLatency = Timediff

	PongMSG := mtypes.PongMsg{
		Src_nodeID:     content.Src_nodeID,
		Dst_nodeID:     device.ID,
		Timediff:       Timediff,
		TimeToAlive:    device.DRoute.PeerAliveTimeout,
		AdditionalCost: device.AdditionalCost,
	}
	if device.DRoute.P2P.UseP2P && time.Now().After(device.graph.NhTableExpire) {
		device.graph.UpdateLatency(content.Src_nodeID, device.ID, PongMSG.Timediff, device.DRoute.PeerAliveTimeout, device.AdditionalCost, true, false)
	}
	body, err := mtypes.GetByte(&PongMSG)
	if err != nil {
		return err
	}
	buf := make([]byte, path.EgHeaderLen+len(body))
	header, err := path.NewEgHeader(buf[:path.EgHeaderLen])
	header.SetSrc(device.ID)
	header.SetTTL(device.DefaultTTL)
	header.SetPacketLength(uint16(len(body)))
	copy(buf[path.EgHeaderLen:], body)
	if device.DRoute.SuperNode.UseSuperNode {
		header.SetDst(mtypes.SuperNodeMessage)
		device.Send2Super(path.PongPacket, buf, MessageTransportOffsetContent)
	}
	if device.DRoute.P2P.UseP2P {
		header.SetDst(mtypes.ControlMessage)
		device.SpreadPacket(make(map[mtypes.Vertex]bool), path.PongPacket, buf, MessageTransportOffsetContent)
	}
	go device.SendPing(peer, content.RequestReply, 0, 3)
	return nil
}

func (device *Device) process_pong(peer *Peer, content mtypes.PongMsg) error {
	if device.DRoute.P2P.UseP2P {
		if time.Now().After(device.graph.NhTableExpire) {
			device.graph.UpdateLatency(content.Src_nodeID, content.Dst_nodeID, content.Timediff, device.DRoute.PeerAliveTimeout, content.AdditionalCost, true, false)
		}
		if !peer.AskedForNeighbor {
			QueryPeerMsg := mtypes.QueryPeerMsg{
				Request_ID: uint32(device.ID),
			}
			body, err := mtypes.GetByte(&QueryPeerMsg)
			if err != nil {
				return err
			}
			buf := make([]byte, path.EgHeaderLen+len(body))
			header, err := path.NewEgHeader(buf[:path.EgHeaderLen])
			header.SetSrc(device.ID)
			header.SetTTL(device.DefaultTTL)
			header.SetPacketLength(uint16(len(body)))
			copy(buf[path.EgHeaderLen:], body)
			device.SendPacket(peer, path.QueryPeer, buf, MessageTransportOffsetContent)
		}
	}
	return nil
}

func (device *Device) process_UpdatePeerMsg(peer *Peer, content mtypes.UpdatePeerMsg) error {
	var send_signal bool
	if device.DRoute.SuperNode.UseSuperNode {
		if peer.ID != mtypes.SuperNodeMessage {
			if device.LogLevel.LogControl {
				fmt.Println("Control: Ignored UpdateErrorMsg. Not from supernode.")
			}
			return nil
		}
		if bytes.Equal(device.peers.Peer_state[:], content.State_hash[:]) {
			if device.LogLevel.LogControl {
				fmt.Println("Control: Same PeerState Hash, skip download nhTable")
			}
			return nil
		}
		var peer_infos mtypes.API_Peers

		downloadurl := device.DRoute.SuperNode.APIUrl + "/peerinfo?NodeID=" + strconv.Itoa(int(device.ID)) + "&PubKey=" + url.QueryEscape(device.staticIdentity.publicKey.ToString()) + "&State=" + url.QueryEscape(string(content.State_hash[:]))
		if device.LogLevel.LogControl {
			fmt.Println("Control: Download peerinfo from :" + downloadurl)
		}
		client := http.Client{
			Timeout: 30 * time.Second,
		}
		resp, err := client.Get(downloadurl)
		if err != nil {
			device.log.Errorf(err.Error())
			return err
		}
		defer resp.Body.Close()
		allbytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			device.log.Errorf(err.Error())
			return err
		}
		if resp.StatusCode != 200 {
			device.log.Errorf("Control: Download peerinfo result failed: " + strconv.Itoa(resp.StatusCode) + " " + string(allbytes))
			return nil
		}
		if device.LogLevel.LogControl {
			fmt.Println("Control: Download peerinfo result :" + string(allbytes))
		}
		if err := json.Unmarshal(allbytes, &peer_infos); err != nil {
			device.log.Errorf("JSON decode error:", err.Error())
			return err
		}

		for nodeID, thepeer := range device.peers.IDMap {
			pk := thepeer.handshake.remoteStatic
			psk := thepeer.handshake.presharedKey
			if val, ok := peer_infos[pk.ToString()]; ok {
				if val.NodeID != nodeID {
					device.RemovePeer(pk)
					continue
				} else if val.PSKey != psk.ToString() {
					device.RemovePeer(pk)
					continue
				}
			} else {
				device.RemovePeer(pk)
				continue
			}
		}

		for PubKey, peerinfo := range peer_infos {
			sk, err := Str2PubKey(PubKey)
			if err != nil {
				device.log.Errorf("Error decode base64:", err)
				continue
			}
			if bytes.Equal(sk[:], device.staticIdentity.publicKey[:]) {
				continue
			}
			thepeer := device.LookupPeer(sk)
			if thepeer == nil { //not exist in local
				if len(peerinfo.Connurl.ExternalV4)+len(peerinfo.Connurl.ExternalV6)+len(peerinfo.Connurl.LocalV4)+len(peerinfo.Connurl.LocalV6) == 0 {
					continue
				}
				if device.LogLevel.LogControl {
					fmt.Println("Control: Add new peer to local ID:" + peerinfo.NodeID.ToString() + " PubKey:" + PubKey)
				}
				if device.graph.Weight(device.ID, peerinfo.NodeID, false) == path.Infinity { // add node to graph
					device.graph.UpdateLatency(device.ID, peerinfo.NodeID, path.Infinity, 0, device.AdditionalCost, true, false)
				}
				if device.graph.Weight(peerinfo.NodeID, device.ID, false) == path.Infinity { // add node to graph
					device.graph.UpdateLatency(peerinfo.NodeID, device.ID, path.Infinity, 0, device.AdditionalCost, true, false)
				}
				device.NewPeer(sk, peerinfo.NodeID, false)
				thepeer = device.LookupPeer(sk)
			}
			if peerinfo.PSKey != "" {
				pk, err := Str2PSKey(peerinfo.PSKey)
				if err != nil {
					device.log.Errorf("Error decode base64:", err)
					continue
				}
				thepeer.SetPSK(pk)
			}

			thepeer.endpoint_trylist.UpdateSuper(*peerinfo.Connurl, !device.EdgeConfig.DynamicRoute.SuperNode.SkipLocalIP)
			if !thepeer.IsPeerAlive() {
				//Peer died, try to switch to this new endpoint
				send_signal = true
			}
		}
		device.peers.Peer_state = content.State_hash
		if send_signal {
			device.event_tryendpoint <- struct{}{}
		}
	}
	return nil
}

func (device *Device) process_UpdateNhTableMsg(peer *Peer, content mtypes.UpdateNhTableMsg) error {
	if device.DRoute.SuperNode.UseSuperNode {
		if peer.ID != mtypes.SuperNodeMessage {
			if device.LogLevel.LogControl {
				fmt.Println("Control: Ignored UpdateErrorMsg. Not from supernode.")
			}
			return nil
		}
		if bytes.Equal(device.graph.NhTableHash[:], content.State_hash[:]) {
			if device.LogLevel.LogControl {
				fmt.Println("Control: Same nhTable Hash, skip download nhTable")
			}
			device.graph.NhTableExpire = time.Now().Add(device.graph.SuperNodeInfoTimeout)
			return nil
		}
		var NhTable mtypes.NextHopTable
		if bytes.Equal(device.graph.NhTableHash[:], content.State_hash[:]) {
			return nil
		}
		downloadurl := device.DRoute.SuperNode.APIUrl + "/nhtable?NodeID=" + strconv.Itoa(int(device.ID)) + "&PubKey=" + url.QueryEscape(device.staticIdentity.publicKey.ToString()) + "&State=" + url.QueryEscape(string(content.State_hash[:]))
		if device.LogLevel.LogControl {
			fmt.Println("Control: Download NhTable from :" + downloadurl)
		}
		client := http.Client{
			Timeout: 30 * time.Second,
		}
		resp, err := client.Get(downloadurl)
		if err != nil {
			device.log.Errorf(err.Error())
			return err
		}
		defer resp.Body.Close()
		allbytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			device.log.Errorf(err.Error())
			return err
		}
		if resp.StatusCode != 200 {
			device.log.Errorf("Control: Download peerinfo result failed: " + strconv.Itoa(resp.StatusCode) + " " + string(allbytes))
			return nil
		}
		if device.LogLevel.LogControl {
			fmt.Println("Control: Download NhTable result :" + string(allbytes))
		}
		if err := json.Unmarshal(allbytes, &NhTable); err != nil {
			device.log.Errorf("JSON decode error:", err.Error())
			return err
		}
		device.graph.SetNHTable(NhTable, content.State_hash)
	}
	return nil
}

func (device *Device) process_UpdateErrorMsg(peer *Peer, content mtypes.ServerCommandMsg) error {
	if peer.ID != mtypes.SuperNodeMessage {
		if device.LogLevel.LogControl {
			fmt.Println("Control: Ignored UpdateErrorMsg. Not from supernode.")
		}
		return nil
	}
	device.log.Errorf(strconv.Itoa(int(content.ErrorCode)) + ": " + content.ErrorMsg)
	if content.Action == mtypes.Shutdown {
		device.closed <- 0
	} else if content.Action == mtypes.ThrowError {
		device.closed <- content.ErrorCode
	} else if content.Action == mtypes.Panic {
		panic(content.ToString())
	}
	return nil
}

func (device *Device) process_RequestPeerMsg(content mtypes.QueryPeerMsg) error { //Send all my peers to all my peers
	if device.DRoute.P2P.UseP2P {
		device.peers.RLock()
		for pubkey, peer := range device.peers.keyMap {
			if peer.ID >= mtypes.Special_NodeID {
				continue
			}
			if peer.endpoint == nil {
				// I don't have the infomation of this peer, skip
				continue
			}
			if !peer.IsPeerAlive() {
				// peer died, skip
				continue
			}

			peer.handshake.mutex.RLock()
			response := mtypes.BoardcastPeerMsg{
				Request_ID: content.Request_ID,
				NodeID:     peer.ID,
				PubKey:     pubkey,
				ConnURL:    peer.endpoint.DstToString(),
			}
			peer.handshake.mutex.RUnlock()
			body, err := mtypes.GetByte(response)
			if err != nil {
				device.log.Errorf("Error at receivesendproc.go line221: ", err)
				continue
			}
			buf := make([]byte, path.EgHeaderLen+len(body))
			header, _ := path.NewEgHeader(buf[0:path.EgHeaderLen])
			header.SetDst(mtypes.ControlMessage)
			header.SetTTL(device.DefaultTTL)
			header.SetSrc(device.ID)
			header.SetPacketLength(uint16(len(body)))
			copy(buf[path.EgHeaderLen:], body)
			device.SpreadPacket(make(map[mtypes.Vertex]bool), path.BroadcastPeer, buf, MessageTransportOffsetContent)
		}
		device.peers.RUnlock()
	}
	return nil
}

func (device *Device) process_BoardcastPeerMsg(peer *Peer, content mtypes.BoardcastPeerMsg) error {
	if device.DRoute.P2P.UseP2P {
		var pk NoisePublicKey
		if content.Request_ID == uint32(device.ID) {
			peer.AskedForNeighbor = true
		}
		if bytes.Equal(content.PubKey[:], device.staticIdentity.publicKey[:]) {
			return nil
		}
		copy(pk[:], content.PubKey[:])
		thepeer := device.LookupPeer(pk)
		if thepeer == nil { //not exist in local
			if device.LogLevel.LogControl {
				fmt.Println("Control: Add new peer to local ID:" + content.NodeID.ToString() + " PubKey:" + pk.ToString())
			}
			if device.graph.Weight(device.ID, content.NodeID, false) == path.Infinity { // add node to graph
				device.graph.UpdateLatency(device.ID, content.NodeID, path.Infinity, 0, device.AdditionalCost, true, false)
			}
			if device.graph.Weight(content.NodeID, device.ID, false) == path.Infinity { // add node to graph
				device.graph.UpdateLatency(content.NodeID, device.ID, path.Infinity, 0, device.AdditionalCost, true, false)
			}
			device.NewPeer(pk, content.NodeID, false)
		}
		if !thepeer.IsPeerAlive() {
			//Peer died, try to switch to this new endpoint
			thepeer.endpoint_trylist.UpdateP2P(content.ConnURL) //another gorouting will process it
			device.event_tryendpoint <- struct{}{}
		}

	}
	return nil
}

func (device *Device) RoutineSetEndpoint() {
	if !(device.DRoute.P2P.UseP2P || device.DRoute.SuperNode.UseSuperNode) {
		return
	}
	for {
		NextRun := false
		<-device.event_tryendpoint
		for _, thepeer := range device.peers.IDMap {
			if thepeer.LastPacketReceivedAdd1Sec.Load().(*time.Time).Add(path.S2TD(device.DRoute.PeerAliveTimeout)).After(time.Now()) {
				//Peer alives
				continue
			} else {
				FastTry, connurl := thepeer.endpoint_trylist.GetNextTry()
				if connurl == "" {
					continue
				}
				err := thepeer.SetEndpointFromConnURL(connurl, thepeer.ConnAF, thepeer.StaticConn) //trying to bind first url in the list and wait ConnNextTry seconds
				if err != nil {
					device.log.Errorf("Bind " + connurl + " failed!")
					thepeer.endpoint_trylist.Delete(connurl)
					continue
				}
				if FastTry {
					NextRun = true
					go device.SendPing(thepeer, int(device.DRoute.ConnNextTry+1), 1, 1)
				}

			}
		}
	ClearChanLoop:
		for {
			select {
			case <-device.event_tryendpoint:
			default:
				break ClearChanLoop
			}
		}
		time.Sleep(path.S2TD(device.DRoute.ConnNextTry))
		if device.LogLevel.LogInternal {
			fmt.Printf("Internal: RoutineSetEndpoint: NextRun:%v\n", NextRun)
		}
		if NextRun {
			device.event_tryendpoint <- struct{}{}
		}
	}
}

func (device *Device) RoutineDetectOfflineAndTryNextEndpoint() {
	if !(device.DRoute.P2P.UseP2P || device.DRoute.SuperNode.UseSuperNode) {
		return
	}
	if device.DRoute.ConnTimeOut == 0 {
		return
	}
	for {
		device.event_tryendpoint <- struct{}{}
		time.Sleep(path.S2TD(device.DRoute.ConnTimeOut))
	}
}

func (device *Device) RoutineSendPing() {
	if !(device.DRoute.P2P.UseP2P || device.DRoute.SuperNode.UseSuperNode) {
		return
	}
	for {
		packet, usage, _ := device.GeneratePingPacket(device.ID, 0)
		device.SpreadPacket(make(map[mtypes.Vertex]bool), usage, packet, MessageTransportOffsetContent)
		time.Sleep(path.S2TD(device.DRoute.SendPingInterval))
	}
}

func (device *Device) RoutineRegister() {
	if !(device.DRoute.SuperNode.UseSuperNode) {
		return
	}
	_ = <-device.Event_Supernode_OK
	for {
		body, _ := mtypes.GetByte(mtypes.RegisterMsg{
			Node_id:       device.ID,
			PeerStateHash: device.peers.Peer_state,
			NhStateHash:   device.graph.NhTableHash,
			Version:       device.Version,
			JWTSecret:     device.JWTSecret,
			HttpPostCount: device.HttpPostCount,
		})
		buf := make([]byte, path.EgHeaderLen+len(body))
		header, _ := path.NewEgHeader(buf[0:path.EgHeaderLen])
		header.SetDst(mtypes.SuperNodeMessage)
		header.SetTTL(0)
		header.SetSrc(device.ID)
		header.SetPacketLength(uint16(len(body)))
		copy(buf[path.EgHeaderLen:], body)
		device.Send2Super(path.Register, buf, MessageTransportOffsetContent)
		time.Sleep(path.S2TD(device.DRoute.SendPingInterval))
	}
}

func (device *Device) RoutinePostPeerInfo() {
	if !(device.DRoute.SuperNode.UseSuperNode) {
		return
	}
	if device.DRoute.SuperNode.HttpPostInterval <= 0 {
		return
	}
	for {
		// Stat all latency
		device.peers.RLock()
		pongs := make([]mtypes.PongMsg, 0, len(device.peers.IDMap))
		for id, peer := range device.peers.IDMap {
			device.peers.RUnlock()
			if peer.IsPeerAlive() {
				pong := mtypes.PongMsg{
					RequestID:   0,
					Src_nodeID:  device.ID,
					Dst_nodeID:  id,
					Timediff:    peer.SingleWayLatency,
					TimeToAlive: time.Now().Sub(*peer.LastPacketReceivedAdd1Sec.Load().(*time.Time)).Seconds() + device.DRoute.PeerAliveTimeout,
				}
				pongs = append(pongs, pong)
				if device.LogLevel.LogControl {
					fmt.Println("Control: Pack to: Post body " + pong.ToString())
				}
			}
			device.peers.RLock()
		}
		device.peers.RUnlock()
		// Prepare post paramater and post body
		LocalV4s := make(map[string]float64)
		LocalV6s := make(map[string]float64)
		if !device.peers.LocalV4.Equal(net.IP{}) {
			LocalV4 := net.UDPAddr{
				IP:   device.peers.LocalV4,
				Port: int(device.net.port),
			}

			LocalV4s[LocalV4.String()] = 100
		}
		if !device.peers.LocalV6.Equal(net.IP{}) {
			LocalV6 := net.UDPAddr{
				IP:   device.peers.LocalV6,
				Port: int(device.net.port),
			}
			LocalV4s[LocalV6.String()] = 100
		}

		body, _ := mtypes.GetByte(mtypes.API_report_peerinfo{
			Pongs:    pongs,
			LocalV4s: LocalV4s,
			LocalV6s: LocalV6s,
		})
		body = mtypes.Gzip(body)
		bodyhash := base64.StdEncoding.EncodeToString(body)
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, mtypes.API_report_peerinfo_jwt_claims{
			PostCount: device.HttpPostCount,
			BodyHash:  bodyhash,
		})
		tokenString, err := token.SignedString(device.JWTSecret[:])
		// Construct post request
		client := &http.Client{}
		downloadurl := device.DRoute.SuperNode.APIUrl + "/post/nodeinfo"
		req, err := http.NewRequest("POST", downloadurl, bytes.NewReader(body))
		q := req.URL.Query()
		q.Add("NodeID", device.ID.ToString())
		q.Add("JWTSig", tokenString)
		req.URL.RawQuery = q.Encode()
		req.Header.Set("Content-Type", "application/binary")
		device.HttpPostCount += 1
		if device.LogLevel.LogControl {
			fmt.Println("Control: Post to " + req.URL.String())
		}
		resp, err := client.Do(req)
		if err != nil {
			device.log.Errorf("RoutinePostPeerInfo: " + err.Error())
		} else {
			if device.LogLevel.LogControl {
				res, _ := ioutil.ReadAll(resp.Body)
				fmt.Println("Control: Post result " + string(res))
			}
			resp.Body.Close()
		}

		time.Sleep(mtypes.S2TD(device.DRoute.SuperNode.HttpPostInterval * 0.8))
	}
}

func (device *Device) RoutineRecalculateNhTable() {
	if device.graph.TimeoutCheckInterval == 0 {
		return
	}

	if !device.DRoute.P2P.UseP2P {
		return
	}
	for {
		if time.Now().After(device.graph.NhTableExpire) {
			if device.graph.CheckAnyShouldUpdate() {
				device.graph.RecalculateNhTable(false)
			}
		}
		time.Sleep(device.graph.TimeoutCheckInterval)
	}

}

func (device *Device) RoutineSpreadAllMyNeighbor() {
	if !device.DRoute.P2P.UseP2P {
		return
	}
	for {
		device.process_RequestPeerMsg(mtypes.QueryPeerMsg{
			Request_ID: uint32(mtypes.Broadcast),
		})
		time.Sleep(path.S2TD(device.DRoute.P2P.SendPeerInterval))
	}
}

func (device *Device) RoutineResetConn() {
	if device.ResetConnInterval <= 0.01 {
		return
	}
	for {
		for _, peer := range device.peers.keyMap {
			if !peer.StaticConn { //Do not reset connecton for dynamic peer
				continue
			}
			if peer.ConnURL == "" {
				continue
			}
			err := peer.SetEndpointFromConnURL(peer.ConnURL, peer.ConnAF, peer.StaticConn)
			if err != nil {
				device.log.Errorf("Failed to bind "+peer.ConnURL, err)
				continue
			}
		}
		time.Sleep(path.S2TD(device.ResetConnInterval))
	}
}

func (device *Device) RoutineClearL2FIB() {
	if device.fibTimeout <= 0.01 {
		return
	}
	timeout := path.S2TD(device.fibTimeout)
	for {
		device.l2fib.Range(func(k interface{}, v interface{}) bool {
			val := v.(*IdAndTime)
			if time.Now().After(val.Time.Add(timeout)) {
				mac := k.(tap.MacAddress)
				device.l2fib.Delete(k)
				if device.LogLevel.LogInternal {
					fmt.Printf("Internal: L2FIB [%v -> %v] deleted.\n", mac.String(), val.ID)
				}
			}
			return true
		})
		time.Sleep(timeout)
	}
}
