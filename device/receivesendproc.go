package device

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	"github.com/KusakabeSi/EtherGuardVPN/path"
	"gopkg.in/yaml.v2"
)

func (device *Device) SendPacket(peer *Peer, packet []byte, offset int) {
	if peer == nil {
		return
	}
	if device.LogControl {
		EgHeader, _ := path.NewEgHeader(packet[:path.EgHeaderLen])
		if EgHeader.GetUsage() != path.NornalPacket {
			device.MsgCount += 1
			EgHeader.SetMessageID(device.MsgCount)
			if peer.GetEndpointDstStr() != "" {
				fmt.Printf("Send MID:" + strconv.Itoa(int(device.MsgCount)) + " To:" + peer.GetEndpointDstStr() + " " + device.sprint_received(EgHeader.GetUsage(), packet[path.EgHeaderLen:]) + "\n")
			}
		}
	}
	var elem *QueueOutboundElement
	elem = device.NewOutboundElement()
	copy(elem.buffer[offset:offset+len(packet)], packet)
	elem.packet = elem.buffer[offset : offset+len(packet)]
	if peer.isRunning.Get() {
		peer.StagePacket(elem)
		elem = nil
		peer.SendStagedPackets()
	}
}

func (device *Device) BoardcastPacket(skip_list map[config.Vertex]bool, packet []byte, offset int) { // Send packet to all connected peers
	send_list := device.graph.GetBoardcastList(device.ID)
	for node_id, _ := range skip_list {
		send_list[node_id] = false
	}
	for node_id, should_send := range send_list {
		if should_send {
			device.SendPacket(device.peers.IDMap[node_id], packet, offset)
		}
	}
}

func (device *Device) SpreadPacket(skip_list map[config.Vertex]bool, packet []byte, offset int) { // Send packet to all peers no matter it is alive
	for peer_id, peer_out := range device.peers.IDMap {
		if _, ok := skip_list[peer_id]; ok {
			if device.LogTransit {
				fmt.Printf("Skipped Spread Packet packet through %d to %d\n", device.ID, peer_out.ID)
			}
			continue
		}
		device.SendPacket(peer_out, packet, MessageTransportOffsetContent)
	}
}

func (device *Device) TransitBoardcastPacket(src_nodeID config.Vertex, in_id config.Vertex, packet []byte, offset int) {
	node_boardcast_list := device.graph.GetBoardcastThroughList(device.ID, in_id, src_nodeID)
	for peer_id := range node_boardcast_list {
		peer_out := device.peers.IDMap[peer_id]
		if device.LogTransit {
			fmt.Printf("Transfer packet from %d through %d to %d\n", in_id, device.ID, peer_out.ID)
		}
		device.SendPacket(peer_out, packet, offset)
	}
}

func (device *Device) Send2Super(packet []byte, offset int) {
	if device.DRoute.SuperNode.UseSuperNode {
		for _, peer_out := range device.peers.SuperPeer {
			if device.LogTransit {
				fmt.Printf("Send to supernode %s\n", peer_out.endpoint.DstToString())
			}
			device.SendPacket(peer_out, packet, offset)
		}
	}
}

func (device *Device) CheckNoDup(packet []byte) bool {
	hasher := crc32.New(crc32.MakeTable(crc32.Castagnoli))
	hasher.Write(packet)
	crc32result := hasher.Sum32()
	_, ok := device.DupData.Get(crc32result)
	device.DupData.Set(crc32result, true)
	return !ok
}

func (device *Device) process_received(msg_type path.Usage, body []byte) (err error) {
	if device.IsSuperNode {
		switch msg_type {
		case path.Register:
			if content, err := path.ParseRegisterMsg(body); err == nil {
				return device.server_process_RegisterMsg(content)
			}
		case path.PongPacket:
			if content, err := path.ParsePongMsg(body); err == nil {
				return device.server_process_Pong(content)
			}
		default:
			err = errors.New("Not a valid msg_type")
		}
	} else {
		switch msg_type {
		case path.UpdatePeer:
			if content, err := path.ParseUpdatePeerMsg(body); err == nil {
				go device.process_UpdatePeerMsg(content)
			}
		case path.UpdateNhTable:
			if content, err := path.ParseUpdateNhTableMsg(body); err == nil {
				go device.process_UpdateNhTableMsg(content)
			}
		case path.PingPacket:
			if content, err := path.ParsePingMsg(body); err == nil {
				return device.process_ping(content)
			}
		case path.PongPacket:
			if content, err := path.ParsePongMsg(body); err == nil {
				return device.process_pong(content)
			}
		case path.RequestPeer:
			if content, err := path.ParseRequestPeerMsg(body); err == nil {
				return device.process_RequestPeerMsg(content)
			}
		case path.BoardcastPeer:
			if content, err := path.ParseBoardcastPeerMsg(body); err == nil {
				return device.process_BoardcastPeerMsg(content)
			}
		default:
			err = errors.New("Not a valid msg_type")
		}
	}
	return
}

func (device *Device) sprint_received(msg_type path.Usage, body []byte) (ret string) {
	switch msg_type {
	case path.Register:
		if content, err := path.ParseRegisterMsg(body); err == nil {
			ret = content.ToString()
		}
	case path.UpdatePeer:
		if content, err := path.ParseUpdatePeerMsg(body); err == nil {
			ret = content.ToString()
		}
	case path.UpdateNhTable:
		if content, err := path.ParseUpdateNhTableMsg(body); err == nil {
			ret = content.ToString()
		}
	case path.PingPacket:
		if content, err := path.ParsePingMsg(body); err == nil {
			ret = content.ToString()
		}
	case path.PongPacket:
		if content, err := path.ParsePongMsg(body); err == nil {
			ret = content.ToString()
		}
	case path.RequestPeer:
		if content, err := path.ParseRequestPeerMsg(body); err == nil {
			ret = content.ToString()
		}
	case path.BoardcastPeer:
		if content, err := path.ParseBoardcastPeerMsg(body); err == nil {
			ret = content.ToString()
		}
	default:
		ret = "Not a valid msg_type"
	}
	return
}

func (device *Device) server_process_RegisterMsg(content path.RegisterMsg) error {
	device.Event_server_register <- content
	return nil
}

func (device *Device) server_process_Pong(content path.PongMsg) error {
	device.Event_server_pong <- content
	return nil
}

func (device *Device) process_ping(content path.PingMsg) error {
	PongMSG := path.PongMsg{
		Src_nodeID: content.Src_nodeID,
		Dst_nodeID: device.ID,
		Timediff:   device.graph.GetCurrentTime().Sub(content.Time),
	}
	body, err := path.GetByte(&PongMSG)
	if err != nil {
		return err
	}
	buf := make([]byte, path.EgHeaderLen+len(body))
	header, err := path.NewEgHeader(buf[:path.EgHeaderLen])
	header.SetSrc(device.ID)
	header.SetTTL(200)
	header.SetUsage(path.PongPacket)
	header.SetPacketLength(uint16(len(body)))
	copy(buf[path.EgHeaderLen:], body)
	if device.DRoute.SuperNode.UseSuperNode {
		header.SetDst(path.SuperNodeMessage)
		device.Send2Super(buf, MessageTransportOffsetContent)
	}
	if device.DRoute.P2P.UseP2P {
		header.SetDst(path.ControlMessage)
		device.SpreadPacket(make(map[config.Vertex]bool), buf, MessageTransportOffsetContent)
	}
	return nil
}

func (device *Device) process_pong(content path.PongMsg) error {
	if device.DRoute.P2P.UseP2P {
		device.graph.UpdateLentancy(content.Src_nodeID, content.Dst_nodeID, content.Timediff, false)
	}
	return nil
}

func (device *Device) process_UpdatePeerMsg(content path.UpdatePeerMsg) error {
	var send_signal bool
	if device.DRoute.SuperNode.UseSuperNode {
		var peer_infos config.HTTP_Peers
		if bytes.Equal(device.peers.Peer_state[:], content.State_hash[:]) {
			return nil
		}

		downloadurl := device.DRoute.SuperNode.APIUrl + "/peerinfo?PubKey=" + url.QueryEscape(PubKey2Str(device.staticIdentity.publicKey)) + "&State=" + url.QueryEscape(string(content.State_hash[:]))
		if device.LogControl {
			fmt.Println("Download peerinfo from :" + downloadurl)
		}
		resp, err := http.Get(downloadurl)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		allbytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(allbytes, &peer_infos); err != nil {
			return err
		}

		for pubkey, peerinfo := range peer_infos.Peers {
			if len(peerinfo.Connurl) == 0 {
				return nil
			}
			sk := Str2PubKey(pubkey)
			if bytes.Equal(sk[:], device.staticIdentity.publicKey[:]) {
				continue
			}
			thepeer := device.LookupPeer(sk)
			if thepeer == nil { //not exist in local
				if device.graph.Weight(device.ID, peerinfo.NodeID) == path.Infinity { // add node to graph
					device.graph.UpdateLentancy(device.ID, peerinfo.NodeID, path.S2TD(path.Infinity), false)
				}
				if device.graph.Weight(peerinfo.NodeID, device.ID) == path.Infinity { // add node to graph
					device.graph.UpdateLentancy(peerinfo.NodeID, device.ID, path.S2TD(path.Infinity), false)
				}
				device.NewPeer(sk, peerinfo.NodeID)
				thepeer = device.LookupPeer(sk)
				if peerinfo.PSKey != "" {
					pk := Str2PSKey(peerinfo.PSKey)
					thepeer.handshake.presharedKey = pk
				}
			}

			if thepeer.LastPingReceived.Add(path.S2TD(device.DRoute.P2P.PeerAliveTimeout)).Before(time.Now()) {
				//Peer died, try to switch to this new endpoint
				for url, _ := range peerinfo.Connurl {
					thepeer.endpoint_trylist[url] = time.Time{} //another gorouting will process it
					send_signal = true
				}
			}
		}
		device.peers.Peer_state = content.State_hash
		if send_signal {
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
			if thepeer.LastPingReceived.Add(path.S2TD(device.DRoute.P2P.PeerAliveTimeout)).After(time.Now()) {
				//Peer alives
				for url := range thepeer.endpoint_trylist {
					delete(thepeer.endpoint_trylist, url)
				}
			} else {
				//Peer died, try to switch to this new endpoint
				for url, trytime := range thepeer.endpoint_trylist {
					if trytime.Sub(time.Time{}) != time.Duration(0) && time.Now().Sub(trytime) > path.S2TD(device.DRoute.ConnTimeOut) {
						delete(thepeer.endpoint_trylist, url)
					} else {
						endpoint, err := device.Bind().ParseEndpoint(url) //trying to bind first url in the list and wait device.DRoute.P2P.PeerAliveTimeout seconds
						if err != nil {
							device.log.Errorf("Can't bind " + url)
							delete(thepeer.endpoint_trylist, url)
						}
						if device.LogControl {
							fmt.Println("Set endpoint to " + endpoint.DstToString() + " for NodeID:" + strconv.Itoa(int(thepeer.ID)))
						}
						thepeer.SetEndpointFromPacket(endpoint)
						NextRun = true
						thepeer.endpoint_trylist[url] = time.Now()
						//Send Ping message to it
						packet, err := device.GeneratePingPacket(device.ID)
						device.SendPacket(thepeer, packet, MessageTransportOffsetContent)
						break
					}
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
		time.Sleep(path.S2TD(device.DRoute.P2P.PeerAliveTimeout))
		if NextRun {
			go device.SaveConfig()
			device.event_tryendpoint <- struct{}{}
		}
	}
}

func (device *Device) SaveConfig() {
	if device.DRoute.SaveNewPeers {
		configbytes, _ := yaml.Marshal(device.EdgeConfig)
		ioutil.WriteFile(device.EdgeConfigPath, configbytes, 0666)
	}
}

func (device *Device) RoutineSendPing() {
	if !(device.DRoute.P2P.UseP2P || device.DRoute.SuperNode.UseSuperNode) {
		return
	}
	for {
		packet, _ := device.GeneratePingPacket(device.ID)
		device.SpreadPacket(make(map[config.Vertex]bool), packet, MessageTransportOffsetContent)
		time.Sleep(path.S2TD(device.DRoute.SendPingInterval))
	}
}

func (device *Device) RoutineRegister() {
	if !(device.DRoute.SuperNode.UseSuperNode) {
		return
	}
	first := true
	for {
		body, _ := path.GetByte(path.RegisterMsg{
			Node_id: device.ID,
			Init:    first,
		})
		buf := make([]byte, path.EgHeaderLen+len(body))
		header, _ := path.NewEgHeader(buf[0:path.EgHeaderLen])
		header.SetDst(path.SuperNodeMessage)
		header.SetTTL(0)
		header.SetSrc(device.ID)
		header.SetUsage(path.Register)
		header.SetPacketLength(uint16(len(body)))
		copy(buf[path.EgHeaderLen:], body)
		device.Send2Super(buf, MessageTransportOffsetContent)
		time.Sleep(path.S2TD(device.DRoute.SendPingInterval))
		first = false
	}
}

func (device *Device) RoutineRecalculateNhTable() {
	if device.IsSuperNode {
		for {
			changed := device.graph.RecalculateNhTable(true)
			if changed {
				device.Event_server_NhTable_changed <- struct{}{}
			}
			time.Sleep(device.graph.NodeReportTimeout)
		}
	} else {
		if !device.DRoute.P2P.UseP2P {
			return
		}
		for {
			device.graph.RecalculateNhTable(false)
			time.Sleep(device.graph.NodeReportTimeout)
		}
	}

}

func (device *Device) GeneratePingPacket(src_nodeID config.Vertex) ([]byte, error) {
	body, err := path.GetByte(&path.PingMsg{
		Src_nodeID: src_nodeID,
		Time:       device.graph.GetCurrentTime(),
	})
	if err != nil {
		return nil, err
	}
	buf := make([]byte, path.EgHeaderLen+len(body))
	header, _ := path.NewEgHeader(buf[0:path.EgHeaderLen])
	if err != nil {
		return nil, err
	}
	header.SetDst(path.PingMessage)
	header.SetTTL(0)
	header.SetSrc(device.ID)
	header.SetUsage(path.PingPacket)
	header.SetPacketLength(uint16(len(body)))
	copy(buf[path.EgHeaderLen:], body)
	return buf, nil
}

func (device *Device) process_UpdateNhTableMsg(content path.UpdateNhTableMsg) error {
	if device.DRoute.SuperNode.UseSuperNode {
		if bytes.Equal(device.graph.NhTableHash[:], content.State_hash[:]) {
			return nil
		}
		var NhTable config.NextHopTable
		if bytes.Equal(device.graph.NhTableHash[:], content.State_hash[:]) {
			return nil
		}
		downloadurl := device.DRoute.SuperNode.APIUrl + "/nhtable?PubKey=" + url.QueryEscape(PubKey2Str(device.staticIdentity.publicKey)) + "&State=" + url.QueryEscape(string(content.State_hash[:]))
		if device.LogControl {
			fmt.Println("Download NhTable from :" + downloadurl)
		}
		resp, err := http.Get(downloadurl)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		allbytes, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(allbytes, &NhTable); err != nil {
			return err
		}
		device.graph.SetNHTable(NhTable, content.State_hash)
	}
	return nil
}

func (device *Device) process_RequestPeerMsg(content path.RequestPeerMsg) error {
	if device.DRoute.P2P.UseP2P {
		for pubkey, peer := range device.peers.keyMap {
			if peer.ID >= path.Special_NodeID {
				continue
			}
			response := path.BoardcastPeerMsg{
				Request_ID: content.Request_ID,
				NodeID:     peer.ID,
				PubKey:     pubkey,
				PSKey:      peer.handshake.presharedKey,
				ConnURL:    peer.endpoint.DstToString(),
			}
			body, err := path.GetByte(response)
			if err != nil {
				device.log.Errorf("Error at receivesendproc.go line221: ", err)
				continue
			}
			buf := make([]byte, path.EgHeaderLen+len(body))
			header, _ := path.NewEgHeader(buf[0:path.EgHeaderLen])
			header.SetDst(path.ControlMessage)
			header.SetTTL(200)
			header.SetSrc(device.ID)
			header.SetUsage(path.BoardcastPeer)
			header.SetPacketLength(uint16(len(body)))
			copy(buf[path.EgHeaderLen:], body)
			device.SpreadPacket(make(map[config.Vertex]bool), buf, MessageTransportOffsetContent)
		}
	}
	return nil
}

func (device *Device) process_BoardcastPeerMsg(content path.BoardcastPeerMsg) error {
	if device.DRoute.P2P.UseP2P {
		var sk NoisePublicKey
		copy(sk[:], content.PubKey[:])
		thepeer := device.LookupPeer(sk)
		if thepeer == nil { //not exist in local
			if device.graph.Weight(device.ID, content.NodeID) == path.Infinity { // add node to graph
				device.graph.UpdateLentancy(device.ID, content.NodeID, path.S2TD(path.Infinity), false)
			}
			if device.graph.Weight(content.NodeID, device.ID) == path.Infinity { // add node to graph
				device.graph.UpdateLentancy(content.NodeID, device.ID, path.S2TD(path.Infinity), false)
			}
			device.NewPeer(sk, content.NodeID)
			thepeer = device.LookupPeer(sk)
			var pk NoisePresharedKey
			copy(pk[:], content.PSKey[:])
			thepeer.handshake.presharedKey = pk
		}
		if thepeer.LastPingReceived.Add(path.S2TD(device.DRoute.P2P.PeerAliveTimeout)).Before(time.Now()) {
			//Peer died, try to switch to this new endpoint
			thepeer.endpoint_trylist[content.ConnURL] = time.Time{} //another gorouting will process it
		}

	}
	return nil
}
