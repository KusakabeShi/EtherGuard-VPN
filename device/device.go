/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package device

import (
	"bytes"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KusakabeSi/EtherGuard-VPN/conn"
	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/KusakabeSi/EtherGuard-VPN/path"
	"github.com/KusakabeSi/EtherGuard-VPN/ratelimiter"
	"github.com/KusakabeSi/EtherGuard-VPN/rwcancel"
	"github.com/KusakabeSi/EtherGuard-VPN/tap"
	fixed_time_cache "github.com/KusakabeSi/go-cache"
)

type Device struct {
	state struct {
		// state holds the device's state. It is accessed atomically.
		// Use the device.deviceState method to read it.
		// device.deviceState does not acquire the mutex, so it captures only a snapshot.
		// During state transitions, the state variable is updated before the device itself.
		// The state is thus either the current state of the device or
		// the intended future state of the device.
		// For example, while executing a call to Up, state will be deviceStateUp.
		// There is no guarantee that that intended future state of the device
		// will become the actual state; Up can fail.
		// The device can also change state multiple times between time of check and time of use.
		// Unsynchronized uses of state must therefore be advisory/best-effort only.
		state uint32 // actually a deviceState, but typed uint32 for convenience
		// stopping blocks until all inputs to Device have been closed.
		stopping sync.WaitGroup
		// mu protects state changes.
		sync.Mutex
	}

	net struct {
		stopping sync.WaitGroup
		sync.RWMutex
		bind          conn.Bind // bind interface
		netlinkCancel *rwcancel.RWCancel
		port          uint16 // listening port
		fwmark        uint32 // mark value (0 = disabled)
	}

	staticIdentity struct {
		sync.RWMutex
		privateKey NoisePrivateKey
		publicKey  NoisePublicKey
	}

	rate struct {
		underLoadUntil int64
		limiter        ratelimiter.Ratelimiter
	}

	peers struct {
		sync.RWMutex // protects keyMap
		keyMap       map[NoisePublicKey]*Peer
		IDMap        map[mtypes.Vertex]*Peer
		SuperPeer    map[NoisePublicKey]*Peer
		LocalV4      net.IP
		LocalV6      net.IP
	}

	state_hashes mtypes.StateHash

	event_tryendpoint chan struct{}

	EdgeConfigPath  string
	EdgeConfig      *mtypes.EdgeConfig
	SuperConfigPath string
	SuperConfig     *mtypes.SuperConfig

	Chan_server_register    chan mtypes.RegisterMsg
	Chan_server_pong        chan mtypes.PongMsg
	Chan_save_config        chan struct{}
	Chan_Device_Initialized chan struct{}
	Chan_SendPingStart      chan struct{}
	Chan_SendRegisterStart  chan struct{}
	Chan_HttpPostStart      chan struct{}

	indexTable    IndexTable
	cookieChecker CookieChecker

	IsSuperNode bool
	ID          mtypes.Vertex
	graph       *path.IG
	l2fib       sync.Map
	LogLevel    mtypes.LoggerInfo
	DupData     fixed_time_cache.Cache
	Version     string

	HttpPostCount uint64
	JWTSecret     mtypes.JWTSecret

	pool struct {
		messageBuffers   *WaitPool
		inboundElements  *WaitPool
		outboundElements *WaitPool
	}

	queue struct {
		encryption *outboundQueue
		decryption *inboundQueue
		handshake  *handshakeQueue
	}

	tap struct {
		device tap.Device
		mtu    int32
	}

	ipcMutex sync.RWMutex
	closed   chan int
	log      *Logger
}

type IdAndTime struct {
	ID   mtypes.Vertex
	Time time.Time
}

// deviceState represents the state of a Device.
// There are three states: down, up, closed.
// Transitions:
//
//   down -----+
//     ↑↓      ↓
//     up -> closed
//
type deviceState uint32

//go:generate go run golang.org/x/tools/cmd/stringer -type deviceState -trimprefix=deviceState
const (
	deviceStateDown deviceState = iota
	deviceStateUp
	deviceStateClosed
)

// deviceState returns device.state.state as a deviceState
// See those docs for how to interpret this value.
func (device *Device) deviceState() deviceState {
	return deviceState(atomic.LoadUint32(&device.state.state))
}

// isClosed reports whether the device is closed (or is closing).
// See device.state.state comments for how to interpret this value.
func (device *Device) isClosed() bool {
	return device.deviceState() == deviceStateClosed
}

// isUp reports whether the device is up (or is attempting to come up).
// See device.state.state comments for how to interpret this value.
func (device *Device) isUp() bool {
	return device.deviceState() == deviceStateUp
}

// Must hold device.peers.Lock()
func removePeerLocked(device *Device, peer *Peer, key NoisePublicKey) {
	// stop routing and processing of packets
	peer.Stop()

	// remove from peer map
	id := peer.ID
	delete(device.peers.keyMap, key)
	if id == mtypes.NodeID_SuperNode {
		delete(device.peers.SuperPeer, key)
	} else {
		delete(device.peers.IDMap, id)
	}
}

// changeState attempts to change the device state to match want.
func (device *Device) changeState(want deviceState) (err error) {
	device.state.Lock()
	defer device.state.Unlock()
	old := device.deviceState()
	if old == deviceStateClosed {
		// once closed, always closed
		device.log.Verbosef("Interface closed, ignored requested state %s", want)
		return nil
	}
	switch want {
	case old:
		return nil
	case deviceStateUp:
		atomic.StoreUint32(&device.state.state, uint32(deviceStateUp))
		err = device.upLocked()
		if err == nil {
			break
		}
		fallthrough // up failed; bring the device all the way back down
	case deviceStateDown:
		atomic.StoreUint32(&device.state.state, uint32(deviceStateDown))
		errDown := device.downLocked()
		if err == nil {
			err = errDown
		}
	}
	device.log.Verbosef("Interface state was %s, requested %s, now %s", old, want, device.deviceState())
	return
}

// upLocked attempts to bring the device up and reports whether it succeeded.
// The caller must hold device.state.mu and is responsible for updating device.state.state.
func (device *Device) upLocked() error {
	if err := device.BindUpdate(); err != nil {
		device.log.Errorf("Unable to update bind: %v", err)
		return err
	}

	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.Start()
		if atomic.LoadUint32(&peer.persistentKeepaliveInterval) > 0 {
			peer.SendKeepalive()
		}
	}
	device.peers.RUnlock()
	return nil
}

// downLocked attempts to bring the device down.
// The caller must hold device.state.mu and is responsible for updating device.state.state.
func (device *Device) downLocked() error {
	err := device.BindClose()
	if err != nil {
		device.log.Errorf("Bind close failed: %v", err)
	}

	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.Stop()
	}
	device.peers.RUnlock()
	return err
}

func (device *Device) Up() error {
	return device.changeState(deviceStateUp)
}

func (device *Device) Down() error {
	return device.changeState(deviceStateDown)
}

func (device *Device) IsUnderLoad() bool {
	// check if currently under load
	now := time.Now()
	underLoad := len(device.queue.handshake.c) >= QueueHandshakeSize/8
	if underLoad {
		atomic.StoreInt64(&device.rate.underLoadUntil, now.Add(UnderLoadAfterTime).UnixNano())
		return true
	}
	// check if recently under load
	return atomic.LoadInt64(&device.rate.underLoadUntil) > now.UnixNano()
}

func (device *Device) SetPrivateKey(sk NoisePrivateKey) error {
	// lock required resources

	device.staticIdentity.Lock()
	defer device.staticIdentity.Unlock()

	if sk.Equals(device.staticIdentity.privateKey) {
		return nil
	}

	device.peers.Lock()
	defer device.peers.Unlock()

	lockedPeers := make([]*Peer, 0, len(device.peers.keyMap))
	for _, peer := range device.peers.keyMap {
		peer.handshake.mutex.RLock()
		lockedPeers = append(lockedPeers, peer)
	}

	// remove peers with matching public keys

	publicKey := sk.PublicKey()
	for key, peer := range device.peers.keyMap {
		if peer.handshake.remoteStatic.Equals(publicKey) {
			peer.handshake.mutex.RUnlock()
			removePeerLocked(device, peer, key)
			peer.handshake.mutex.RLock()
		}
	}

	// update key material

	device.staticIdentity.privateKey = sk
	device.staticIdentity.publicKey = publicKey
	device.cookieChecker.Init(publicKey)

	// do static-static DH pre-computations

	expiredPeers := make([]*Peer, 0, len(device.peers.keyMap))
	for _, peer := range device.peers.keyMap {
		handshake := &peer.handshake
		handshake.precomputedStaticStatic = device.staticIdentity.privateKey.sharedSecret(handshake.remoteStatic)
		expiredPeers = append(expiredPeers, peer)
	}

	for _, peer := range lockedPeers {
		peer.handshake.mutex.RUnlock()
	}
	for _, peer := range expiredPeers {
		peer.ExpireCurrentKeypairs()
	}

	return nil
}

func NewDevice(tapDevice tap.Device, id mtypes.Vertex, bind conn.Bind, logger *Logger, graph *path.IG, IsSuperNode bool, configpath string, econfig *mtypes.EdgeConfig, sconfig *mtypes.SuperConfig, superevents *mtypes.SUPER_Events, version string) *Device {
	device := new(Device)
	device.state.state = uint32(deviceStateDown)
	device.closed = make(chan int)
	device.log = logger
	device.net.bind = bind
	device.tap.device = tapDevice
	mtu, err := device.tap.device.MTU()
	if err != nil {
		device.log.Errorf("Trouble determining MTU, assuming default: %v", err)
		mtu = DefaultMTU
	}
	device.tap.mtu = int32(mtu)
	device.peers.keyMap = make(map[NoisePublicKey]*Peer)
	device.peers.IDMap = make(map[mtypes.Vertex]*Peer)
	device.peers.SuperPeer = make(map[NoisePublicKey]*Peer)
	device.IsSuperNode = IsSuperNode
	device.ID = id
	device.graph = graph
	device.Version = version
	device.JWTSecret = mtypes.ByteSlice2Byte32(mtypes.RandomBytes(32, []byte(fmt.Sprintf("%v", time.Now()))))

	device.state_hashes.NhTable.Store("")
	device.state_hashes.Peer.Store("")
	device.state_hashes.SuperParam.Store("")

	device.rate.limiter.Init()
	device.indexTable.Init()
	device.PopulatePools()
	if IsSuperNode {
		device.SuperConfigPath = configpath
		device.SuperConfig = sconfig
		device.EdgeConfig = &mtypes.EdgeConfig{}
		device.EdgeConfig.Interface.MTU = DefaultMTU
		device.Chan_server_pong = superevents.Event_server_pong
		device.Chan_server_register = superevents.Event_server_register
		device.LogLevel = sconfig.LogLevel
	} else {
		device.EdgeConfigPath = configpath
		device.EdgeConfig = econfig
		device.SuperConfig = &mtypes.SuperConfig{}
		device.DupData = *fixed_time_cache.NewCache(mtypes.S2TD(econfig.DynamicRoute.DupCheckTimeout), false, mtypes.S2TD(60))
		device.event_tryendpoint = make(chan struct{}, 1<<6)
		device.Chan_save_config = make(chan struct{}, 1<<5)
		device.Chan_Device_Initialized = make(chan struct{}, 1<<5)
		device.Chan_SendPingStart = make(chan struct{}, 1<<5)
		device.Chan_SendRegisterStart = make(chan struct{}, 1<<5)
		device.Chan_HttpPostStart = make(chan struct{}, 1<<5)
		device.LogLevel = econfig.LogLevel
		device.SuperConfig.DampingResistance = device.EdgeConfig.DynamicRoute.DampingResistance

	}

	go func() {
		<-device.Chan_Device_Initialized
		if device.LogLevel.LogInternal {
			fmt.Printf("Internal: initialized, start background loops\n")
		}
		if IsSuperNode {
			go device.RoutineResetEndpoint()
		} else {
			go device.RoutineTryReceivedEndpoint()
			go device.RoutineDetectOfflineAndTryNextEndpoint()
			go device.RoutineRegister(device.Chan_SendRegisterStart)
			go device.RoutineSendPing(device.Chan_SendPingStart)
			go device.RoutineSpreadAllMyNeighbor()
			go device.RoutineResetEndpoint()
			go device.RoutineClearL2FIB()
			go device.RoutineRecalculateNhTable()
			go device.RoutinePostPeerInfo(device.Chan_HttpPostStart)
		}
	}()

	// create queues

	device.queue.handshake = newHandshakeQueue()
	device.queue.encryption = newOutboundQueue()
	device.queue.decryption = newInboundQueue()

	// start workers

	cpus := runtime.NumCPU()
	device.state.stopping.Wait()
	device.queue.encryption.wg.Add(cpus) // One for each RoutineHandshake
	for i := 0; i < cpus; i++ {
		go device.RoutineEncryption(i + 1)
		go device.RoutineDecryption(i + 1)
		go device.RoutineHandshake(i + 1)
	}

	device.state.stopping.Add(1)      // RoutineReadFromTUN
	device.queue.encryption.wg.Add(1) // RoutineReadFromTUN
	go device.RoutineReadFromTUN()
	go device.RoutineTUNEventReader()

	return device
}

func (device *Device) LookupPeerIDAtConfig(pk NoisePublicKey) (ID mtypes.Vertex, err error) {
	if device.IsSuperNode {
		var peerlist []mtypes.SuperPeerInfo
		if device.SuperConfig == nil {
			return 0, errors.New("superconfig is nil")
		}
		peerlist = device.SuperConfig.Peers
		pkstr := pk.ToString()
		for _, peerinfo := range peerlist {
			if peerinfo.PubKey == pkstr {
				return peerinfo.NodeID, nil
			}
		}
	} else {
		var peerlist []mtypes.PeerInfo
		if device.EdgeConfig == nil {
			return 0, errors.New("edgeconfig is nil")
		}
		peerlist = device.EdgeConfig.Peers
		pkstr := pk.ToString()
		for _, peerinfo := range peerlist {
			if peerinfo.PubKey == pkstr {
				return peerinfo.NodeID, nil
			}
		}
	}

	return 0, errors.New("peer not found in the config file")
}

type VPair struct {
	s mtypes.Vertex
	d mtypes.Vertex
}
type PSKDB struct {
	db sync.Map
}

func (D *PSKDB) GetPSK(s mtypes.Vertex, d mtypes.Vertex) (psk NoisePresharedKey) {
	if s > d {
		s, d = d, s
	}
	vp := VPair{
		s: s,
		d: d,
	}
	pski, ok := D.db.Load(vp)
	if !ok {
		psk = RandomPSK()
		pski, _ = D.db.LoadOrStore(vp, psk)
		return pski.(NoisePresharedKey)
	}
	return pski.(NoisePresharedKey)
}

func (D *PSKDB) DelNode(n mtypes.Vertex) {
	D.db.Range(func(key, value interface{}) bool {
		vp := key.(VPair)
		if vp.s == n || vp.d == n {
			D.db.Delete(vp)
		}
		return true
	})
}

func (device *Device) LookupPeer(pk NoisePublicKey) *Peer {
	device.peers.RLock()
	defer device.peers.RUnlock()

	return device.peers.keyMap[pk]
}

func (device *Device) LookupPeerByStr(pks string) *Peer {
	var pk NoisePublicKey
	sk_slice, _ := base64.StdEncoding.DecodeString(pks)
	copy(pk[:], sk_slice)
	return device.LookupPeer(pk)
}

func (pk NoisePublicKey) ToString() string {
	if bytes.Equal(pk[:], make([]byte, len(pk))) {
		return ""
	}
	return string(base64.StdEncoding.EncodeToString(pk[:]))
}

func (pk NoisePrivateKey) ToString() (result string) {
	if bytes.Equal(pk[:], make([]byte, len(pk))) {
		return ""
	}
	return string(base64.StdEncoding.EncodeToString(pk[:]))
}
func (pk NoisePresharedKey) ToString() (result string) {
	if bytes.Equal(pk[:], make([]byte, len(pk))) {
		return ""
	}
	return string(base64.StdEncoding.EncodeToString(pk[:]))
}

func Str2PubKey(k string) (pk NoisePublicKey, err error) {
	if k == "" {
		err = errors.New("empty public key string")
		return
	}
	sk_slice, err := base64.StdEncoding.DecodeString(k)
	copy(pk[:], sk_slice)
	return
}

func Str2PriKey(k string) (pk NoisePrivateKey, err error) {
	if k == "" {
		err = errors.New("empty private key string")
		return
	}
	sk_slice, err := base64.StdEncoding.DecodeString(k)
	copy(pk[:], sk_slice)
	return
}

func Str2PSKey(k string) (pk NoisePresharedKey, err error) {
	if k == "" {
		return
	}
	sk_slice, err := base64.StdEncoding.DecodeString(k)
	copy(pk[:], sk_slice)
	return
}

func RandomKeyPair() (pri NoisePrivateKey, pub NoisePublicKey) {
	pri = mtypes.ByteSlice2Byte32(mtypes.RandomBytes(32, make([]byte, 32)))
	pub = pri.PublicKey()
	return
}

func RandomPSK() (pk NoisePresharedKey) {
	return mtypes.ByteSlice2Byte32(mtypes.RandomBytes(32, make([]byte, 32)))
}

func (device *Device) GetConnurl(v mtypes.Vertex) string {
	if peer, has := device.peers.IDMap[v]; has {
		if peer.endpoint != nil {
			return peer.endpoint.DstToString()
		}
	}
	return ""
}

func (device *Device) RemovePeerByID(id mtypes.Vertex) {
	device.peers.Lock()
	defer device.peers.Unlock()
	peer, ok := device.peers.IDMap[id]
	if ok {
		removePeerLocked(device, peer, peer.handshake.remoteStatic)
	}
}

func (device *Device) RemovePeer(key NoisePublicKey) {
	device.peers.Lock()
	defer device.peers.Unlock()
	// stop peer and remove from routing

	peer, ok := device.peers.keyMap[key]
	if ok {
		removePeerLocked(device, peer, key)
	}
}

func (device *Device) RemoveAllPeers() {
	device.peers.Lock()
	defer device.peers.Unlock()

	for key, peer := range device.peers.keyMap {
		removePeerLocked(device, peer, key)
	}

	device.peers.keyMap = make(map[NoisePublicKey]*Peer)
	device.peers.IDMap = make(map[mtypes.Vertex]*Peer)
}

func (device *Device) Close() {
	device.state.Lock()
	defer device.state.Unlock()
	if device.isClosed() {
		return
	}
	atomic.StoreUint32(&device.state.state, uint32(deviceStateClosed))
	device.log.Verbosef("Device closing")

	device.tap.device.Close()
	device.downLocked()

	// Remove peers before closing queues,
	// because peers assume that queues are active.
	device.RemoveAllPeers()

	// We kept a reference to the encryption and decryption queues,
	// in case we started any new peers that might write to them.
	// No new peers are coming; we are done with these queues.
	device.queue.encryption.wg.Done()
	device.queue.decryption.wg.Done()
	device.queue.handshake.wg.Done()
	device.state.stopping.Wait()

	device.rate.limiter.Close()

	device.log.Verbosef("Device closed")
	close(device.closed)
}

func (device *Device) Wait() chan int {
	return device.closed
}

func (device *Device) SendKeepalivesToPeersWithCurrentKeypair() {
	if !device.isUp() {
		return
	}

	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.keypairs.RLock()
		sendKeepalive := peer.keypairs.current != nil && !peer.keypairs.current.created.Add(RejectAfterTime).Before(time.Now())
		peer.keypairs.RUnlock()
		if sendKeepalive {
			peer.SendKeepalive()
		}
	}
	device.peers.RUnlock()
}

// closeBindLocked closes the device's net.bind.
// The caller must hold the net mutex.
func closeBindLocked(device *Device) error {
	var err error
	netc := &device.net
	if netc.netlinkCancel != nil {
		netc.netlinkCancel.Cancel()
	}
	if netc.bind != nil {
		err = netc.bind.Close()
	}
	netc.stopping.Wait()
	return err
}

func (device *Device) Bind() conn.Bind {
	device.net.Lock()
	defer device.net.Unlock()
	return device.net.bind
}

func (device *Device) BindSetMark(mark uint32) error {
	device.net.Lock()
	defer device.net.Unlock()

	// check if modified
	if device.net.fwmark == mark {
		return nil
	}

	// update fwmark on existing bind
	device.net.fwmark = mark
	if device.isUp() && device.net.bind != nil {
		if err := device.net.bind.SetMark(mark); err != nil {
			return err
		}
	}

	// clear cached source addresses
	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.Lock()
		defer peer.Unlock()
		if peer.endpoint != nil {
			peer.endpoint.ClearSrc()
		}
	}
	device.peers.RUnlock()

	return nil
}

func (device *Device) BindUpdate() error {
	device.net.Lock()
	defer device.net.Unlock()

	// close existing sockets
	if err := closeBindLocked(device); err != nil {
		return err
	}

	// open new sockets
	if !device.isUp() {
		return nil
	}

	// bind to new port
	var err error
	var recvFns []conn.ReceiveFunc
	netc := &device.net
	recvFns, netc.port, err = netc.bind.Open(netc.port)
	if err != nil {
		netc.port = 0
		return err
	}
	netc.netlinkCancel, err = device.startRouteListener(netc.bind)
	if err != nil {
		netc.bind.Close()
		netc.port = 0
		return err
	}

	// set fwmark
	if netc.fwmark != 0 {
		err = netc.bind.SetMark(netc.fwmark)
		if err != nil {
			return err
		}
	}

	// clear cached source addresses
	device.peers.RLock()
	for _, peer := range device.peers.keyMap {
		peer.Lock()
		defer peer.Unlock()
		if peer.endpoint != nil {
			peer.endpoint.ClearSrc()
		}
	}
	device.peers.RUnlock()

	// start receiving routines
	device.net.stopping.Add(len(recvFns))
	device.queue.decryption.wg.Add(len(recvFns)) // each RoutineReceiveIncoming goroutine writes to device.queue.decryption
	device.queue.handshake.wg.Add(len(recvFns))  // each RoutineReceiveIncoming goroutine writes to device.queue.handshake
	for _, fn := range recvFns {
		go device.RoutineReceiveIncoming(fn)
	}

	device.log.Verbosef("UDP bind has been updated")
	return nil
}

func (device *Device) BindClose() error {
	device.net.Lock()
	err := closeBindLocked(device)
	device.net.Unlock()
	return err
}
