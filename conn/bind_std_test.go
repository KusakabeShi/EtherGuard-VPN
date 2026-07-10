package conn

import (
	"net"
	"syscall"
	"testing"
)

func TestStdNetBindPreservesFixedPortWhenIPv4IsUnsupported(t *testing.T) {
	probe, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
	if err != nil {
		t.Skipf("IPv6 is unavailable: %v", err)
	}
	port := probe.LocalAddr().(*net.UDPAddr).Port
	probe.Close()

	original := listenNetFunc
	listenNetFunc = func(network string, listenIP net.IP, requestedPort int) (*net.UDPConn, int, error) {
		if network == "udp4" {
			return nil, 0, syscall.EAFNOSUPPORT
		}
		if requestedPort != port {
			t.Fatalf("IPv6 requested port = %d, want %d", requestedPort, port)
		}
		return original(network, listenIP, requestedPort)
	}
	t.Cleanup(func() { listenNetFunc = original })

	bind := NewStdNetBindAf(true, true, [4]byte{}, [16]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1}, 0)
	defer bind.Close()
	_, got, err := bind.Open(uint16(port))
	if err != nil {
		t.Fatal(err)
	}
	if got != uint16(port) {
		t.Fatalf("Open() port = %d, want %d", got, port)
	}
}
