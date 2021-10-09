package tap

import (
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/KusakabeSi/EtherGuardVPN/config"
)

type SockServerTap struct {
	name     string
	mtu      int
	protocol string
	server   *net.Listener
	connRx   *net.Conn
	connTx   *net.Conn
	static   bool
	loglevel config.LoggerInfo

	closed bool
	events chan Event
}

// New creates and returns a new TUN interface for the application.
func CreateSockTAP(iconfig config.InterfaceConf, protocol string, NodeID config.Vertex, loglevel config.LoggerInfo) (tapdev Device, err error) {
	// Setup TUN Config

	tap := &SockServerTap{
		name:     iconfig.Name,
		mtu:      1500,
		protocol: protocol,
		server:   nil,
		connRx:   nil,
		connTx:   nil,
		static:   false,
		closed:   false,
		loglevel: loglevel,
		events:   make(chan Event, 1<<5),
	}

	if iconfig.RecvAddr == "" && iconfig.SendAddr == "" {
		return nil, errors.New("At least one of RecvAddr or SendAddr required.")
	}

	if iconfig.RecvAddr != "" {
		server, err := net.Listen(protocol, iconfig.RecvAddr)
		if err != nil {
			return nil, err
		}
		tap.server = &server
		go tap.RoutineAcceptConnection()
	}

	if iconfig.SendAddr != "" {
		client, err := net.Dial(protocol, iconfig.SendAddr)
		if err != nil {
			if tap.server != nil {
				(*tap.server).Close()
			}
			return nil, err
		}
		tap.connTx = &client
		tap.static = true
		if tap.server == nil {
			tap.connRx = &client
		}
	}

	tapdev = tap
	tapdev.Events() <- EventUp
	return
}

func (tap *SockServerTap) RoutineAcceptConnection() {
	if tap.server == nil {
		return
	}
	for {
		conn, err := (*tap.server).Accept()
		if tap.closed == true {
			return
		}
		if err != nil {
			if tap.loglevel.LogInternal {
				fmt.Printf("Internal: Accept error %v\n", err)
			}
			continue
		}
		if tap.loglevel.LogInternal {
			fmt.Printf("Internal: New connection accepted from %v\n", conn.RemoteAddr())
		}
		if tap.connRx != nil {
			if tap.loglevel.LogInternal {
				fmt.Printf("Internal: Old connection %v closed due to new connection\n", (*tap.connRx).RemoteAddr())
			}
			(*tap.connRx).Close()
		}

		tap.connRx = &conn
		if tap.static == false {
			tap.connTx = &conn
		}
	}
}

// SetMTU sets the Maximum Tansmission Unit Size for a
// Packet on the interface.

func (tap *SockServerTap) Read(buf []byte, offset int) (size int, err error) {
	if tap.closed {
		return 0, errors.New("Tap closed")
	}
	if tap.connRx == nil {
		time.Sleep(time.Second)
		return 0, nil
	}
	size, err = (*tap.connRx).Read(buf[offset:])
	if err != nil && tap.server != nil {
		if tap.loglevel.LogInternal {
			fmt.Printf("Internal: Connection closed: %v\n", (*tap.connRx).RemoteAddr())
		}
		tap.connRx = nil
		return 0, nil
	}
	return
} // read a packet from the device (without any additional headers)
func (tap *SockServerTap) Write(buf []byte, offset int) (size int, err error) {
	if tap.closed {
		return 0, errors.New("Tap closed")
	}
	if tap.connTx == nil {
		return
	}
	size, err = (*tap.connTx).Write(buf[offset:])
	if serr, ok := err.(*net.OpError); ok && tap.server != nil {
		if serr.Err.Error() == "use of closed network connection" || serr.Err.Error() == "EOF" {
			tap.connTx = nil
			return 0, nil
		}
	}
	return
} // writes a packet to the device (without any additional headers)
func (tap *SockServerTap) Flush() error {
	return nil
} // flush all previous writes to the device
func (tap *SockServerTap) MTU() (int, error) {
	return tap.mtu, nil
} // returns the MTU of the device
func (tap *SockServerTap) Name() (string, error) {
	return tap.name, nil
} // fetches and returns the current name
func (tap *SockServerTap) Events() chan Event {
	return tap.events
} // returns a constant channel of events related to the device
func (tap *SockServerTap) Close() error {
	tap.events <- EventDown
	tap.closed = true
	if tap.connRx != nil {
		(*tap.connRx).Close()
	}
	if tap.connTx != nil {
		(*tap.connTx).Close()
	}
	if tap.server != nil {
		(*tap.server).Close()
	}
	close(tap.events)
	return nil
} // stops the device and closes the event channel
