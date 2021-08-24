/* SPDX-License-Identifier: MIT
 *
 * Copyright (C) 2017-2021 WireGuard LLC. All Rights Reserved.
 */

package device

import (
	"encoding/base64"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	"github.com/KusakabeSi/EtherGuardVPN/conn"
	"github.com/KusakabeSi/EtherGuardVPN/path"
	"github.com/KusakabeSi/EtherGuardVPN/ratelimiter"
	"github.com/KusakabeSi/EtherGuardVPN/rwcancel"
	"github.com/KusakabeSi/EtherGuardVPN/tap"
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
		IDMap        map[config.Vertex]*Peer
		SuperPeer    map[NoisePublicKey]*Peer
		Peer_state   [32]byte
	}
	event_tryendpoint chan struct{}
	ResetConnInterval float64

	EdgeConfigPath  string
	EdgeConfig      *config.EdgeConfig
	SuperConfigPath string
	SuperConfig     *config.SuperConfig

	Event_server_register        chan path.RegisterMsg
	Event_server_pong            chan path.PongMsg
	Event_server_NhTable_changed chan struct{}
	Event_save_config            chan struct{}

	indexTable    IndexTable
	cookieChecker CookieChecker

	MsgCount    uint32
	IsSuperNode bool
	ID          config.Vertex
	graph       *path.IG
	l2fib       sync.Map
	LogTransit  bool
	LogControl  bool
	DRoute      config.DynamicRouteInfo
	DupData     fixed_time_cache.Cache

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
	closed   chan struct{}
	log      *Logger
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
	if id == path.SuperNodeMessage {
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

	publicKey := sk.publicKey()
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

func NewDevice(tapDevice tap.Device, id config.Vertex, bind conn.Bind, logger *Logger, graph *path.IG, IsSuperNode bool, configpath string, econfig *config.EdgeConfig, sconfig *config.SuperConfig, superevents *path.SUPER_Events) *Device {
	device := new(Device)
	device.state.state = uint32(deviceStateDown)
	device.closed = make(chan struct{})
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
	device.peers.IDMap = make(map[config.Vertex]*Peer)
	device.peers.SuperPeer = make(map[NoisePublicKey]*Peer)
	device.IsSuperNode = IsSuperNode
	device.ID = id
	device.graph = graph

	device.rate.limiter.Init()
	device.indexTable.Init()
	device.PopulatePools()
	if IsSuperNode {
		device.Event_server_pong = superevents.Event_server_pong
		device.Event_server_register = superevents.Event_server_register
		device.Event_server_NhTable_changed = superevents.Event_server_NhTable_changed
		device.LogTransit = sconfig.LogLevel.LogTransit
		device.LogControl = sconfig.LogLevel.LogControl
		device.SuperConfig = sconfig
		device.SuperConfigPath = configpath
		go device.RoutineRecalculateNhTable()
	} else {
		device.EdgeConfigPath = configpath
		device.EdgeConfig = econfig
		device.DRoute = econfig.DynamicRoute
		device.DupData = *fixed_time_cache.NewCache(path.S2TD(econfig.DynamicRoute.DupCheckTimeout), false, path.S2TD(60))
		device.event_tryendpoint = make(chan struct{}, 1<<6)
		device.Event_save_config = make(chan struct{}, 1<<5)
		device.LogTransit = econfig.LogLevel.LogTransit
		device.LogControl = econfig.LogLevel.LogControl
		device.ResetConnInterval = device.EdgeConfig.ResetConnInterval
		go device.RoutineSetEndpoint()
		go device.RoutineRegister()
		go device.RoutineSendPing()
		go device.RoutineRecalculateNhTable()
		go device.RoutineSpreadAllMyNeighbor()
		go device.RoutineResetConn()
	}
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

func (device *Device) LookupPeerIDAtConfig(pk NoisePublicKey) (ID config.Vertex, err error) {
	var peerlist []config.PeerInfo
	if device.IsSuperNode {
		if device.SuperConfig == nil {
			return 0, errors.New("Superconfig is nil")
		}
		peerlist = device.SuperConfig.Peers
	} else {
		if device.EdgeConfig == nil {
			return 0, errors.New("EdgeConfig is nil")
		}
		peerlist = device.EdgeConfig.Peers
	}

	pkstr := PubKey2Str(pk)
	for _, peerinfo := range peerlist {
		if peerinfo.PubKey == pkstr {
			return peerinfo.NodeID, nil
		}
	}
	return 0, errors.New("Peer not found in the config file.")
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

func PubKey2Str(pk NoisePublicKey) (result string) {
	result = string(base64.StdEncoding.EncodeToString(pk[:]))
	return
}

func PriKey2Str(pk NoisePrivateKey) (result string) {
	result = string(base64.StdEncoding.EncodeToString(pk[:]))
	return
}
func PSKeyStr(pk NoisePresharedKey) (result string) {
	result = string(base64.StdEncoding.EncodeToString(pk[:]))
	return
}

func Str2PubKey(k string) (pk NoisePublicKey) {
	sk_slice, _ := base64.StdEncoding.DecodeString(k)
	copy(pk[:], sk_slice)
	return
}

func Str2PriKey(k string) (pk NoisePrivateKey) {
	sk_slice, _ := base64.StdEncoding.DecodeString(k)
	copy(pk[:], sk_slice)
	return
}

func Str2PSKey(k string) (pk NoisePresharedKey) {
	sk_slice, _ := base64.StdEncoding.DecodeString(k)
	copy(pk[:], sk_slice)
	return
}

func (device *Device) GetIPMap() map[config.Vertex]*Peer {
	return device.peers.IDMap
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
	device.peers.IDMap = make(map[config.Vertex]*Peer)
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

func (device *Device) Wait() chan struct{} {
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
