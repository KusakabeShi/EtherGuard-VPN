/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package device

import (
	"bytes"
	"container/list"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KusakabeSi/EtherGuard-VPN/conn"
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"gopkg.in/yaml.v2"
)

type endpoint_tryitem struct {
	URL      string
	lastTry  time.Time
	firstTry time.Time
}

type endpoint_trylist struct {
	sync.RWMutex
	timeout      time.Duration
	peer         *Peer
	trymap_super map[string]*endpoint_tryitem
	trymap_p2p   map[string]*endpoint_tryitem
}

func NewEndpoint_trylist(peer *Peer, timeout time.Duration) *endpoint_trylist {
	return &endpoint_trylist{
		timeout:      timeout,
		peer:         peer,
		trymap_super: make(map[string]*endpoint_tryitem),
		trymap_p2p:   make(map[string]*endpoint_tryitem),
	}
}

func (et *endpoint_trylist) UpdateSuper(urls mtypes.API_connurl, UseLocalIP bool) {
	et.Lock()
	defer et.Unlock()
	newmap_super := make(map[string]*endpoint_tryitem)
	if urls.IsEmpty() {
		if et.peer.device.LogLevel.LogInternal {
			fmt.Printf("Internal: Peer %v : Reset trylist(super) %v\n", et.peer.ID.ToString(), "nil")
		}
	}
	for url, it := range urls.GetList(UseLocalIP) {
		if url == "" {
			continue
		}
		_, err := conn.LookupIP(url, 0)
		if err != nil {
			if et.peer.device.LogLevel.LogInternal {
				fmt.Printf("Internal: Peer %v : Update trylist(super) %v error: %v\n", et.peer.ID.ToString(), url, err)
			}
			continue
		}
		if val, ok := et.trymap_super[url]; ok {
			if et.peer.device.LogLevel.LogInternal {
				fmt.Printf("Internal: Peer %v : Update trylist(super) %v\n", et.peer.ID.ToString(), url)
			}
			newmap_super[url] = val
		} else {
			if et.peer.device.LogLevel.LogInternal {
				fmt.Printf("Internal: Peer %v : New trylist(super) %v\n", et.peer.ID.ToString(), url)
			}
			newmap_super[url] = &endpoint_tryitem{
				URL:      url,
				lastTry:  time.Time{}.Add(mtypes.S2TD(it)),
				firstTry: time.Time{},
			}
		}
	}
	et.trymap_super = newmap_super
}

func (et *endpoint_trylist) UpdateP2P(url string) {
	_, err := conn.LookupIP(url, 0)
	if err != nil {
		return
	}
	et.Lock()
	defer et.Unlock()
	if _, ok := et.trymap_p2p[url]; !ok {
		if et.peer.device.LogLevel.LogInternal {
			fmt.Printf("Internal: Peer %v : Add trylist(p2p) %v\n", et.peer.ID.ToString(), url)
		}
		et.trymap_p2p[url] = &endpoint_tryitem{
			URL:      url,
			lastTry:  time.Now(),
			firstTry: time.Time{},
		}
	}
}

func (et *endpoint_trylist) Delete(url string) {
	et.Lock()
	defer et.Unlock()
	delete(et.trymap_super, url)
	delete(et.trymap_p2p, url)
}

func (et *endpoint_trylist) GetNextTry() (bool, string) {
	et.RLock()
	defer et.RUnlock()
	var smallest *endpoint_tryitem
	FastTry := true
	for _, v := range et.trymap_super {
		if smallest == nil || smallest.lastTry.After(v.lastTry) {
			smallest = v
		}
	}
	for url, v := range et.trymap_p2p {
		if v.firstTry.After(time.Time{}) && v.firstTry.Add(et.timeout).Before(time.Now()) {
			if et.peer.device.LogLevel.LogInternal {
				fmt.Printf("Internal: Peer %v : Delete trylist(p2p) %v\n", et.peer.ID.ToString(), url)
			}
			delete(et.trymap_p2p, url)
		}
		if smallest == nil || smallest.lastTry.After(v.lastTry) {
			smallest = v
		}
	}
	if smallest == nil {
		return false, ""
	}
	smallest.lastTry = time.Now()
	if !smallest.firstTry.After(time.Time{}) {
		smallest.firstTry = time.Now()
	}
	if smallest.firstTry.Add(et.timeout).Before(time.Now()) {
		FastTry = false
	}
	return FastTry, smallest.URL
}

type Peer struct {
	isRunning        AtomicBool
	sync.RWMutex     // Mostly protects endpoint, but is generally taken whenever we modify peer
	keypairs         Keypairs
	handshake        Handshake
	device           *Device
	endpoint         conn.Endpoint
	endpoint_trylist *endpoint_trylist

	LastPacketReceivedAdd1Sec atomic.Value // *time.Time

	SingleWayLatency atomic.Value
	stopping         sync.WaitGroup // routines pending stop

	ID               mtypes.Vertex
	AskedForNeighbor bool
	StaticConn       bool //if true, this peer will not write to config file when roaming, and the endpoint will be reset periodically
	ConnURL          string
	ConnAF           int //0: both, 4: ipv4 only, 6: ipv6 only

	// These fields are accessed with atomic operations, which must be
	// 64-bit aligned even on 32-bit platforms. Go guarantees that an
	// allocated struct will be 64-bit aligned. So we place
	// atomically-accessed fields up front, so that they can share in
	// this alignment before smaller fields throw it off.
	stats struct {
		txBytes           uint64 // bytes send to peer (endpoint)
		rxBytes           uint64 // bytes received from peer
		lastHandshakeNano int64  // nano seconds since epoch
	}

	disableRoaming bool

	timers struct {
		retransmitHandshake     *Timer
		sendKeepalive           *Timer
		newHandshake            *Timer
		zeroKeyMaterial         *Timer
		persistentKeepalive     *Timer
		handshakeAttempts       uint32
		needAnotherKeepalive    AtomicBool
		sentLastMinuteHandshake AtomicBool
	}

	state struct {
		sync.Mutex // protects against concurrent Start/Stop
	}

	queue struct {
		staged   chan *QueueOutboundElement // staged packets before a handshake is available
		outbound *autodrainingOutboundQueue // sequential ordering of udp transmission
		inbound  *autodrainingInboundQueue  // sequential ordering of tun writing
	}

	cookieGenerator             CookieGenerator
	trieEntries                 list.List
	persistentKeepaliveInterval uint32 // accessed atomically
}

func (device *Device) NewPeer(pk NoisePublicKey, id mtypes.Vertex, isSuper bool, PersistentKeepalive uint32) (*Peer, error) {
	if !isSuper {
		if id < mtypes.NodeID_Special {
			//pass check
		} else {
			return nil, errors.New(fmt.Sprint("ID ", uint32(id), " is a special NodeID"))
		}
	} else {
		if id == mtypes.NodeID_SuperNode {
			//pass check
		} else {
			return nil, errors.New(fmt.Sprint("ID", uint32(id), "is not a supernode NodeID"))
		}
	}

	if device.isClosed() {
		return nil, errors.New("device closed")
	}
	// lock resources
	device.staticIdentity.RLock()
	defer device.staticIdentity.RUnlock()

	device.peers.Lock()
	defer device.peers.Unlock()

	// check if over limit
	if len(device.peers.keyMap) >= MaxPeers {
		return nil, errors.New("too many peers")
	}

	// create peer
	if device.LogLevel.LogInternal {
		fmt.Println("Internal: Create peer with ID : " + id.ToString() + " and PubKey:" + pk.ToString())
	}
	peer := new(Peer)
	atomic.SwapUint32(&peer.persistentKeepaliveInterval, PersistentKeepalive)
	peer.LastPacketReceivedAdd1Sec.Store(&time.Time{})
	peer.Lock()
	defer peer.Unlock()

	peer.cookieGenerator.Init(pk)
	peer.device = device
	peer.endpoint_trylist = NewEndpoint_trylist(peer, mtypes.S2TD(device.EdgeConfig.DynamicRoute.PeerAliveTimeout))
	peer.SingleWayLatency.Store(mtypes.Infinity)
	peer.queue.outbound = newAutodrainingOutboundQueue(device)
	peer.queue.inbound = newAutodrainingInboundQueue(device)
	peer.queue.staged = make(chan *QueueOutboundElement, QueueStagedSize)
	// map public key
	oldpeer, ok := device.peers.keyMap[pk]
	if ok {
		if oldpeer.ID != id {
			oldpeer = nil
		}
		return oldpeer, fmt.Errorf("adding existing peer pubkey: %v", pk.ToString())
	}
	_, ok = device.peers.IDMap[id]
	if ok {
		return nil, fmt.Errorf("adding existing peer id: %v", id)
	}
	peer.ID = id

	// pre-compute DH
	handshake := &peer.handshake
	handshake.mutex.Lock()
	handshake.precomputedStaticStatic = device.staticIdentity.privateKey.sharedSecret(pk)
	handshake.remoteStatic = pk
	handshake.mutex.Unlock()

	// reset endpoint
	peer.endpoint = nil

	// add
	if id == mtypes.NodeID_SuperNode { // To communicate with supernode
		device.peers.SuperPeer[pk] = peer
		device.peers.keyMap[pk] = peer
	} else { // Regular peer, other edgenodes
		device.peers.keyMap[pk] = peer
		device.peers.IDMap[id] = peer
	}

	// start peer
	peer.timersInit()
	if peer.device.isUp() {
		peer.Start()
	}
	return peer, nil
}

func (peer *Peer) IsPeerAlive() bool {
	PeerAliveTimeout := mtypes.S2TD(peer.device.EdgeConfig.DynamicRoute.PeerAliveTimeout)
	if peer.endpoint == nil {
		return false
	}
	if peer.LastPacketReceivedAdd1Sec.Load().(*time.Time).Add(PeerAliveTimeout).Before(time.Now()) {
		return false
	}
	return true
}

func (peer *Peer) SendBuffer(buffer []byte) error {
	peer.device.net.RLock()
	defer peer.device.net.RUnlock()

	if peer.device.isClosed() {
		return nil
	}

	peer.RLock()
	defer peer.RUnlock()

	if peer.endpoint == nil {
		return errors.New("no known endpoint for peer")
	}

	err := peer.device.net.bind.Send(buffer, peer.endpoint)
	if err == nil {
		atomic.AddUint64(&peer.stats.txBytes, uint64(len(buffer)))
	}
	return err
}

func (peer *Peer) String() string {
	// The awful goo that follows is identical to:
	//
	//   base64Key := base64.StdEncoding.EncodeToString(peer.handshake.remoteStatic[:])
	//   abbreviatedKey := base64Key[0:4] + "…" + base64Key[39:43]
	//   return fmt.Sprintf("peer(%s)", abbreviatedKey)
	//
	// except that it is considerably more efficient.
	src := peer.handshake.remoteStatic
	b64 := func(input byte) byte {
		return input + 'A' + byte(((25-int(input))>>8)&6) - byte(((51-int(input))>>8)&75) - byte(((61-int(input))>>8)&15) + byte(((62-int(input))>>8)&3)
	}
	b := []byte("peer(____…____)")
	const first = len("peer(")
	const second = len("peer(____…")
	b[first+0] = b64((src[0] >> 2) & 63)
	b[first+1] = b64(((src[0] << 4) | (src[1] >> 4)) & 63)
	b[first+2] = b64(((src[1] << 2) | (src[2] >> 6)) & 63)
	b[first+3] = b64(src[2] & 63)
	b[second+0] = b64(src[29] & 63)
	b[second+1] = b64((src[30] >> 2) & 63)
	b[second+2] = b64(((src[30] << 4) | (src[31] >> 4)) & 63)
	b[second+3] = b64((src[31] << 2) & 63)
	return string(b)
}

func (peer *Peer) Start() {
	// should never start a peer on a closed device
	if peer.device.isClosed() {
		return
	}

	// prevent simultaneous start/stop operations
	peer.state.Lock()
	defer peer.state.Unlock()

	if peer.isRunning.Get() {
		return
	}

	device := peer.device
	device.log.Verbosef("%v - Starting", peer)

	// reset routine state
	peer.stopping.Wait()
	peer.stopping.Add(2)

	peer.handshake.mutex.Lock()
	peer.handshake.lastSentHandshake = time.Now().Add(-(RekeyTimeout + time.Second))
	peer.handshake.mutex.Unlock()

	peer.device.queue.encryption.wg.Add(1) // keep encryption queue open for our writes

	peer.timersStart()

	device.flushInboundQueue(peer.queue.inbound)
	device.flushOutboundQueue(peer.queue.outbound)
	go peer.RoutineSequentialSender()
	go peer.RoutineSequentialReceiver()

	peer.isRunning.Set(true)
}

func (peer *Peer) ZeroAndFlushAll() {
	device := peer.device

	// clear key pairs

	keypairs := &peer.keypairs
	keypairs.Lock()
	device.DeleteKeypair(keypairs.previous)
	device.DeleteKeypair(keypairs.current)
	device.DeleteKeypair(keypairs.loadNext())
	keypairs.previous = nil
	keypairs.current = nil
	keypairs.storeNext(nil)
	keypairs.Unlock()

	// clear handshake state

	handshake := &peer.handshake
	handshake.mutex.Lock()
	device.indexTable.Delete(handshake.localIndex)
	handshake.Clear()
	handshake.mutex.Unlock()

	peer.FlushStagedPackets()
}

func (peer *Peer) ExpireCurrentKeypairs() {
	handshake := &peer.handshake
	handshake.mutex.Lock()
	peer.device.indexTable.Delete(handshake.localIndex)
	handshake.Clear()
	peer.handshake.lastSentHandshake = time.Now().Add(-(RekeyTimeout + time.Second))
	handshake.mutex.Unlock()

	keypairs := &peer.keypairs
	keypairs.Lock()
	if keypairs.current != nil {
		atomic.StoreUint64(&keypairs.current.sendNonce, RejectAfterMessages)
	}
	if keypairs.next != nil {
		next := keypairs.loadNext()
		atomic.StoreUint64(&next.sendNonce, RejectAfterMessages)
	}
	keypairs.Unlock()
}

func (peer *Peer) Stop() {
	peer.state.Lock()
	defer peer.state.Unlock()

	if !peer.isRunning.Swap(false) {
		return
	}

	peer.device.log.Verbosef("%v - Stopping", peer)

	peer.timersStop()
	// Signal that RoutineSequentialSender and RoutineSequentialReceiver should exit.
	peer.queue.inbound.c <- nil
	peer.queue.outbound.c <- nil
	peer.stopping.Wait()
	peer.device.queue.encryption.wg.Done() // no more writes to encryption queue from us

	peer.ZeroAndFlushAll()
}

func (peer *Peer) SetPSK(psk NoisePresharedKey) {
	if !peer.device.IsSuperNode && peer.ID < mtypes.NodeID_Special && peer.device.EdgeConfig.DynamicRoute.P2P.UseP2P {
		peer.device.log.Verbosef("Preshared keys disabled in P2P mode.")
		return
	}
	peer.handshake.mutex.Lock()
	peer.handshake.presharedKey = psk
	peer.handshake.mutex.Unlock()
}

func (peer *Peer) SetEndpointFromConnURL(connurl string, af int, static bool) error {
	peer.StaticConn = static
	peer.ConnURL = connurl
	peer.ConnAF = af

	if peer.device.LogLevel.LogInternal {
		fmt.Println("Internal: Set endpoint to " + connurl + " for NodeID:" + peer.ID.ToString())
	}
	var err error
	connurl, err = conn.LookupIP(connurl, af)
	if err != nil {
		return err
	}

	endpoint, err := peer.device.net.bind.ParseEndpoint(connurl)
	if err != nil {
		return err
	}
	peer.StaticConn = static
	peer.ConnURL = connurl
	peer.SetEndpointFromPacket(endpoint)
	return nil
}

func (peer *Peer) SetEndpointFromPacket(endpoint conn.Endpoint) {
	if peer.disableRoaming {
		return
	}
	peer.Lock()
	defer peer.Unlock()
	if peer.ID == mtypes.NodeID_SuperNode {
		conn, err := net.Dial("udp", endpoint.DstToString())
		if err != nil {
			if peer.device.LogLevel.LogControl {
				fmt.Printf("Control: Set endpoint to peer %v failed: %v", peer.ID, err)
			}
			return
		}
		defer conn.Close()
		if err == nil {
			IP := conn.LocalAddr().(*net.UDPAddr).IP
			if ip4 := IP.To4(); ip4 != nil {
				peer.device.peers.LocalV4 = ip4
			} else {
				peer.device.peers.LocalV6 = IP
			}
		}
	}
	peer.device.SaveToConfig(peer, endpoint)
	peer.endpoint = endpoint

}

func (peer *Peer) GetEndpointSrcStr() string {
	if peer.endpoint == nil {
		return ""
	}
	return peer.endpoint.SrcToString()
}

func (peer *Peer) GetEndpointDstStr() string {
	if peer.endpoint == nil {
		return ""
	}
	return peer.endpoint.DstToString()
}

func (device *Device) SaveToConfig(peer *Peer, endpoint conn.Endpoint) {
	if device.IsSuperNode { //Can't use in super mode
		return
	}
	if peer.StaticConn { //static conn do not write new endpoint to config
		return
	}
	if !device.EdgeConfig.DynamicRoute.P2P.UseP2P { //Must in p2p mode
		return
	}
	if peer.endpoint != nil && peer.endpoint.DstIP().Equal(endpoint.DstIP()) { //endpoint changed
		return
	}

	url := endpoint.DstToString()
	foundInFile := false
	pubkeystr := peer.handshake.remoteStatic.ToString()
	pskstr := peer.handshake.presharedKey.ToString()
	if bytes.Equal(peer.handshake.presharedKey[:], make([]byte, 32)) {
		pskstr = ""
	}
	for _, peerfile := range device.EdgeConfig.Peers {
		if peerfile.NodeID == peer.ID && peerfile.PubKey == pubkeystr {
			foundInFile = true
			if !peerfile.Static {
				peerfile.EndPoint = url
			}
		} else if peerfile.NodeID == peer.ID || peerfile.PubKey == pubkeystr {
			panic("Found NodeID match " + peer.ID.ToString() + ", but PubKey Not match %s enrties in config file" + pubkeystr)
		}
	}
	if !foundInFile {
		device.EdgeConfig.Peers = append(device.EdgeConfig.Peers, mtypes.PeerInfo{
			NodeID:   peer.ID,
			PubKey:   pubkeystr,
			PSKey:    pskstr,
			EndPoint: url,
			Static:   false,
		})
	}
	go device.SaveConfig()
}

func (device *Device) SaveConfig() {
	if device.EdgeConfig.DynamicRoute.SaveNewPeers {
		configbytes, _ := yaml.Marshal(device.EdgeConfig)
		ioutil.WriteFile(device.EdgeConfigPath, configbytes, 0644)
	}
}
