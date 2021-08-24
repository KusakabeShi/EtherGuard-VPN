package tap

import (
	"context"
	"path"

	"errors"
	"fmt"
	"log"
	"os"
	"sync"

	"git.fd.io/govpp.git"
	"git.fd.io/govpp.git/adapter/socketclient"
	"git.fd.io/govpp.git/binapi/ethernet_types"
	interfaces "git.fd.io/govpp.git/binapi/interface"
	"git.fd.io/govpp.git/binapi/interface_types"
	"git.fd.io/govpp.git/binapi/l2"
	"git.fd.io/govpp.git/binapi/memif"
	"git.fd.io/govpp.git/extras/libmemif"

	"github.com/KusakabeSi/EtherGuardVPN/config"
	logger "github.com/sirupsen/logrus"
)

const (
	ENV_VPP_MEMIF_SOCKET_DIR = "VPP_MEMIF_SOCKET_DIR"
	ENV_VPP_SOCKET_PATH      = "VPP_API_SOCKET_PATH"
)

var (
	//read from env
	vppMemifSocketDir = "/var/run/eggo-vpp"
	vppApiSocketPath  = socketclient.DefaultSocketName // Path to VPP binary API socket file, default is /run/vpp/api.

	//internal
	NumQueues       = uint8(1)
	onConnectWg     sync.WaitGroup
	tunErrorChannel chan error
)

type VppTap struct {
	name          string
	mtu           int
	ifuid         uint32
	memifSockPath string
	SwIfIndex     interface_types.InterfaceIndex
	secret        string
	memif         *libmemif.Memif
	RxQueues      int
	RxintCh       <-chan uint8
	RxintChNext   chan uint8
	RxintErrCh    <-chan error
	TxQueues      int
	TxCount       uint
	logger        *logger.Logger
	errors        chan error // async error handling
	events        chan Event
}

// New creates and returns a new TUN interface for the application.
func CreateVppTAP(iconfig config.InterfaceConf, loglevel string) (tapdev Device, err error) {
	// Setup TUN Config
	// Set logger
	log := logger.New()
	log.Out = os.Stdout
	log.Level = func() logger.Level {
		switch loglevel {
		case "verbose", "debug":
			return logger.DebugLevel
		case "error":
			return logger.ErrorLevel
		case "silent":
			return logger.PanicLevel
		}
		return logger.ErrorLevel
	}()
	libmemif.SetLogger(log)
	if os.Getenv(ENV_VPP_MEMIF_SOCKET_DIR) != "" {
		vppMemifSocketDir = os.Getenv(ENV_VPP_MEMIF_SOCKET_DIR)
	}
	if os.Getenv(ENV_VPP_SOCKET_PATH) != "" {
		vppApiSocketPath = os.Getenv(ENV_VPP_SOCKET_PATH)
	}
	if err := os.MkdirAll(vppMemifSocketDir, 0755); err != nil {
		log.Fatalln("ERROR: Failed to create VPP memif socket folder " + vppMemifSocketDir)
		return nil, err
	}
	// connect to VPP
	conn, err := govpp.Connect(vppApiSocketPath)
	if err != nil {
		log.Fatalln("ERROR: Connecting to VPP failed:", err)
		return nil, err
	}
	defer conn.Disconnect()

	// create a channel
	ch, err := conn.NewAPIChannel()
	if err != nil {
		log.Fatalln("ERROR: creating channel failed:", err)
		return nil, err
	}
	defer ch.Close()

	if err := ch.CheckCompatiblity(&memif.MemifSocketFilenameAddDel{}, &memif.MemifCreate{}, &memif.MemifDelete{}); err != nil {
		return nil, err
	}
	if err := ch.CheckCompatiblity(&interfaces.SwInterfaceSetFlags{}, &interfaces.SwInterfaceSetMtu{}); err != nil {
		return nil, err
	}
	if err := ch.CheckCompatiblity(&l2.L2fibAddDel{}, &l2.SwInterfaceSetL2Bridge{}); err != nil {
		return nil, err
	}

	memifservice := memif.NewServiceClient(conn)
	l2service := l2.NewServiceClient(conn)
	interfacservice := interfaces.NewServiceClient(conn)

	IfMacAddr, err := GetMacAddr(iconfig.MacAddrPrefix, iconfig.VPPIfaceID)
	if err != nil {
		log.Fatalln("ERROR: Failed parse mac address:", iconfig.MacAddrPrefix)
		return nil, err
	}
	vppIfMacAddr := ethernet_types.MacAddress(IfMacAddr)

	tap := &VppTap{
		name:          iconfig.Name,
		mtu:           iconfig.MTU,
		ifuid:         iconfig.VPPIfaceID,
		SwIfIndex:     0,
		memifSockPath: path.Join(vppMemifSocketDir, iconfig.Name+".sock"),
		secret:        config.RandomStr(16, iconfig.Name),
		logger:        log,
		errors:        make(chan error, 1<<5),
		events:        make(chan Event, 1<<4),
	}
	// create memif socket id 1 filename /tmp/icmp-responder-example

	_, err = memifservice.MemifSocketFilenameAddDel(context.Background(), &memif.MemifSocketFilenameAddDel{
		IsAdd:          true,
		SocketID:       iconfig.VPPIfaceID,
		SocketFilename: tap.memifSockPath,
	})
	if err != nil {
		return nil, err
	}

	// create interface memif id 1 socket-id 1 slave secret secret no-zero-copy

	memifCreateReply, err := memifservice.MemifCreate(context.Background(), &memif.MemifCreate{
		Role:       memif.MEMIF_ROLE_API_SLAVE,
		Mode:       memif.MEMIF_MODE_API_ETHERNET,
		RxQueues:   NumQueues, // MEMIF_DEFAULT_RX_QUEUES
		TxQueues:   NumQueues, // MEMIF_DEFAULT_TX_QUEUES
		ID:         tap.ifuid,
		SocketID:   tap.ifuid,
		RingSize:   1024, // MEMIF_DEFAULT_RING_SIZE
		BufferSize: 2048, // MEMIF_DEFAULT_BUFFER_SIZE 2048
		NoZeroCopy: true,
		HwAddr:     vppIfMacAddr,
		Secret:     tap.secret,
	})
	if err != nil {
		return nil, err
	}

	tap.SwIfIndex = memifCreateReply.SwIfIndex

	// set int state memif1/1 up

	_, err = interfacservice.SwInterfaceSetFlags(context.Background(), &interfaces.SwInterfaceSetFlags{
		SwIfIndex: tap.SwIfIndex,
		Flags:     interface_types.IF_STATUS_API_FLAG_ADMIN_UP,
	})
	if err != nil {
		return nil, err
	}

	//set interface l2 bridge memif1/1 4242
	_, err = l2service.SwInterfaceSetL2Bridge(context.Background(), &l2.SwInterfaceSetL2Bridge{
		RxSwIfIndex: tap.SwIfIndex,
		BdID:        iconfig.VPPBridgeID,
		PortType:    l2.L2_API_PORT_TYPE_NORMAL,
		Shg:         0,
		Enable:      true,
	})
	if err != nil {
		return nil, err
	}
	//init libmemif
	libmemif.Init(tap.name)
	onConnectWg.Add(1)
	memifCallbacks := &libmemif.MemifCallbacks{
		OnConnect:    OnConnect,
		OnDisconnect: OnDisconnect,
	}
	// Prepare memif1 configuration.
	memifConfig := &libmemif.MemifConfig{
		MemifMeta: libmemif.MemifMeta{
			IfName:         tap.name,
			ConnID:         tap.ifuid,
			SocketFilename: tap.memifSockPath,
			Secret:         tap.secret,
			IsMaster:       true,
			Mode:           libmemif.IfModeEthernet,
		},
		MemifShmSpecs: libmemif.MemifShmSpecs{
			NumRxQueues:  NumQueues,
			NumTxQueues:  NumQueues,
			BufferSize:   2048,
			Log2RingSize: 10,
		},
	}
	// Create memif1 interface.
	memif, err := libmemif.CreateInterface(memifConfig, memifCallbacks)
	if err != nil {
		tap.logger.Errorf("libmemif.CreateInterface() error: %v\n", err)
		return nil, err
	}
	onConnectWg.Wait()
	_, err = interfacservice.SwInterfaceSetMtu(context.Background(), &interfaces.SwInterfaceSetMtu{
		SwIfIndex: tap.SwIfIndex,
		Mtu:       []uint32{uint32(tap.mtu)},
	})
	if err != nil {
		return nil, err
	}
	tap.memif = memif
	details, err := memif.GetDetails()
	tap.RxQueues = len(details.RxQueues)
	tap.RxintCh = memif.GetInterruptChan()
	tap.RxintErrCh = memif.GetInterruptErrorChan()
	tap.TxQueues = len(details.TxQueues)
	tapdev.Events() <- EventUp
	return
}

// SetMTU sets the Maximum Tansmission Unit Size for a
// Packet on the interface.

func (tap *VppTap) Read(buf []byte, offset int) (n int, err error) {
	select {
	case err = <-tap.RxintErrCh:
		tap.logger.Errorf("libmemif.Memif.RxintErr() error: %v\n", err)
		return 0, err
	case err = <-tap.errors:
		if err == nil {
			err = errors.New("Device closed")
		}
		tap.logger.Errorf("tun error: %v\n", err)
		return 0, err
	case queueID := <-tap.RxintCh:
		select {
		case tap.RxintChNext <- queueID:
			{
				// Use non-blocking write to prevent program stuck
			}
		default:
			tap.logger.Debugln("Buffer full")
		}

	case queueID := <-tap.RxintChNext:
		packets, err := tap.memif.RxBurst(queueID, 1)
		if err != nil {
			tap.logger.Errorf("libmemif.Memif.RxBurst() error: %v\n", err)
			return 0, err
		}
		if len(packets) == 0 {
			// No more packets to read until the next interrupt.
			return 0, nil
		}
		for _, packetData := range packets {
			select {
			case tap.RxintChNext <- queueID:
				{
					// Use non-blocking write to prevent program stuck
					// repeatedly call RxBurst() until returns an empty slice of packets
				}
			default:
				tap.logger.Debugln("Buffer full")
			}
			n = copy(buf[offset:], packetData)
		}
	}
	return
} // read a packet from the device (without any additional headers)
func (tap *VppTap) Write(buf []byte, offset int) (size int, err error) {
	queueID := tap.getTxQueueID()
	buf = buf[offset:]
	n, err := tap.memif.TxBurst(queueID, []libmemif.RawPacketData{buf})
	return len(buf) * int(n), err
} // writes a packet to the device (without any additional headers)
func (tap *VppTap) Flush() error {
	return nil
} // flush all previous writes to the device
func (tap *VppTap) MTU() (int, error) {
	return tap.mtu, nil
} // returns the MTU of the device
func (tap *VppTap) Name() (string, error) {
	return tap.name, nil
} // fetches and returns the current name
func (tap *VppTap) Events() chan Event {
	return tap.events
} // returns a constant channel of events related to the device
func (tap *VppTap) Close() error {
	// connect to VPP
	conn, err := govpp.Connect(vppApiSocketPath)
	if err != nil {
		log.Fatalln("ERROR: connecting to VPP failed:", err)
	}
	defer conn.Disconnect()

	// create a channel
	ch, err := conn.NewAPIChannel()
	if err != nil {
		log.Fatalln("ERROR: creating channel failed:", err)
	}
	defer ch.Close()

	memifservice := memif.NewServiceClient(conn)

	tap.memif.Close()
	libmemif.Cleanup()
	// delete interface memif memif1/1
	_, err = memifservice.MemifDelete(context.Background(), &memif.MemifDelete{
		SwIfIndex: tap.SwIfIndex,
	})
	// delete memif socket id 1
	_, err = memifservice.MemifSocketFilenameAddDel(context.Background(), &memif.MemifSocketFilenameAddDel{
		IsAdd:          false,
		SocketID:       tap.ifuid,
		SocketFilename: tap.memifSockPath,
	})
	tap.events <- EventDown
	close(tap.events)
	return nil
} // stops the device and closes the event channel

// OnConnect is called when a memif connection gets established.
func OnConnect(memif *libmemif.Memif) (err error) {
	details, err := memif.GetDetails()
	if err != nil {
		fmt.Printf("libmemif.GetDetails() error: %v\n", err)
	}
	fmt.Printf("memif %s has been connected: %+v\n", memif.IfName, details)

	onConnectWg.Done()

	return nil
}

// OnDisconnect is called when a memif connection is lost.
func OnDisconnect(memif *libmemif.Memif) (err error) {
	tunErrorChannel <- errors.New(fmt.Sprintf("memif %s has been disconnected", memif.IfName))
	return nil
}

func (tun *VppTap) getTxQueueID() uint8 {
	if tun.TxQueues == 1 {
		return 0
	}
	tun.TxCount++
	return uint8(tun.TxCount % uint(tun.TxQueues))
}
