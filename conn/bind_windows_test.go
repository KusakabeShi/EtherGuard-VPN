package conn

import (
	"errors"
	"os"
	"testing"

	"golang.org/x/sys/windows"
)

func TestWinRingBindIntegrationOpenClose(t *testing.T) {
	if os.Getenv("ETHERGUARD_RIO_INTEGRATION") != "1" {
		t.Skip("set ETHERGUARD_RIO_INTEGRATION=1 to exercise Windows RIO")
	}
	bind, ok := NewDefaultBind(EnabledAf46, "", 0).(*WinRingBind)
	if !ok {
		t.Skip("Windows RIO is unavailable")
	}
	var fixedPort uint16
	for attempt := 1; attempt <= 10; attempt++ {
		receive, port, err := bind.Open(fixedPort)
		if err != nil {
			t.Fatalf("Open() attempt %d: %v", attempt, err)
		}
		if len(receive) != 2 || port == 0 {
			t.Fatalf("Open() attempt %d receive funcs=%d port=%d", attempt, len(receive), port)
		}
		if fixedPort == 0 {
			fixedPort = port
		} else if port != fixedPort {
			t.Fatalf("Open() attempt %d port=%d, want fixed port %d", attempt, port, fixedPort)
		}
		if err := bind.Close(); err != nil {
			t.Fatalf("Close() attempt %d: %v", attempt, err)
		}
	}
	if err := bind.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func withWinRIOAvailability(t *testing.T, available bool) {
	t.Helper()
	previous := winRIOAvailable
	winRIOAvailable = func() bool { return available }
	t.Cleanup(func() { winRIOAvailable = previous })
}

func TestNewDefaultBindStdHonorsAddressFamilies(t *testing.T) {
	tests := []struct {
		name string
		af   EnabledAf
	}{
		{name: "IPv4", af: EnabledAf4},
		{name: "IPv6", af: EnabledAf6},
		{name: "dual stack", af: EnabledAf46},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			bind := NewDefaultBind(test.af, "std", 42)
			std, ok := bind.(*StdNetBind)
			if !ok {
				t.Fatalf("NewDefaultBind returned %T, want *StdNetBind", bind)
			}
			if got := std.EnabledAf(); got.IPv4 != test.af.IPv4 || got.IPv6 != test.af.IPv6 {
				t.Fatalf("EnabledAf() = %+v, want %+v", got, test.af)
			}
			if std.fwmark != 42 {
				t.Fatalf("fwmark = %d, want 42", std.fwmark)
			}
		})
	}
}

func TestNewDefaultBindFallsBackWhenRIOIsUnavailable(t *testing.T) {
	withWinRIOAvailability(t, false)
	bind := NewDefaultBind(EnabledAf4, "", 0)
	if _, ok := bind.(*StdNetBind); !ok {
		t.Fatalf("NewDefaultBind returned %T, want *StdNetBind", bind)
	}
}

func TestNewDefaultBindUsesRIOWhenAvailable(t *testing.T) {
	withWinRIOAvailability(t, true)
	bind := NewDefaultBind(EnabledAf6, "", 0)
	ring, ok := bind.(*WinRingBind)
	if !ok {
		t.Fatalf("NewDefaultBind returned %T, want *WinRingBind", bind)
	}
	if got := ring.EnabledAf(); got.IPv4 || !got.IPv6 {
		t.Fatalf("EnabledAf() = %+v, want IPv6 only", got)
	}
}

func TestWinRingBindCloseBeforeOpenIsIdempotent(t *testing.T) {
	bind := &WinRingBind{use4: true}
	if err := bind.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := bind.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestWinRingCleanupResetsReusableState(t *testing.T) {
	ring := ringBuffer{
		head:       10,
		tail:       20,
		isFull:     true,
		overlapped: windows.Overlapped{Internal: 1, InternalHigh: 2},
	}
	ring.CloseAndZero()
	if ring.head != 0 || ring.tail != 0 || ring.isFull || ring.overlapped != (windows.Overlapped{}) {
		t.Fatalf("ring state was not reset: %+v", ring)
	}

	family := afWinRingBind{rq: 42, blackhole: true}
	family.CloseAndZero()
	if family.rq != 0 || family.blackhole {
		t.Fatalf("family state was not reset: rq=%v blackhole=%v", family.rq, family.blackhole)
	}
}

func TestWinRingFamilyClosesSocketBeforeRingResources(t *testing.T) {
	wantErr := errors.New("close socket failed")
	family := afWinRingBind{sock: 123, rq: 456, blackhole: true}
	var order []string
	err := family.closeAndZeroWith(
		func(socket windows.Handle) error {
			if socket != 123 {
				t.Fatalf("closed socket %v, want 123", socket)
			}
			order = append(order, "socket")
			return wantErr
		},
		func(ring *ringBuffer) {
			switch ring {
			case &family.rx:
				order = append(order, "rx")
			case &family.tx:
				order = append(order, "tx")
			default:
				t.Fatal("closed an unknown ring")
			}
		},
	)
	if !errors.Is(err, wantErr) {
		t.Fatalf("close error = %v, want %v", err, wantErr)
	}
	wantOrder := []string{"socket", "rx", "tx"}
	if len(order) != len(wantOrder) {
		t.Fatalf("cleanup order = %v, want %v", order, wantOrder)
	}
	for i := range wantOrder {
		if order[i] != wantOrder[i] {
			t.Fatalf("cleanup order = %v, want %v", order, wantOrder)
		}
	}
	if family.sock != 0 || family.rq != 0 || family.blackhole {
		t.Fatalf("family state was not reset: sock=%v rq=%v blackhole=%v", family.sock, family.rq, family.blackhole)
	}
}

func TestWinRingBindRetriesDualStackRandomPortConflicts(t *testing.T) {
	for _, retryErr := range []error{windows.WSAEADDRINUSE, windows.WSAEACCES} {
		t.Run(retryErr.Error(), func(t *testing.T) {
			attempt := 0
			var closed4, closed6 int
			bind := &WinRingBind{use4: true, use6: true}
			withWinRingOperations(t, bind,
				func(_ *afWinRingBind, family int32, sa windows.Sockaddr) (windows.Sockaddr, error) {
					switch family {
					case windows.AF_INET:
						attempt++
						return &windows.SockaddrInet4{Port: 40000 + attempt}, nil
					case windows.AF_INET6:
						port := sa.(*windows.SockaddrInet6).Port
						if attempt == 1 {
							return nil, retryErr
						}
						return &windows.SockaddrInet6{Port: port}, nil
					default:
						t.Fatalf("unexpected address family %d", family)
						return nil, windows.WSAEAFNOSUPPORT
					}
				},
				func(*afWinRingBind) error { return nil },
				func(family *afWinRingBind) error {
					switch family {
					case &bind.v4:
						closed4++
					case &bind.v6:
						closed6++
					default:
						t.Fatal("cleaned an unknown address family")
					}
					return nil
				},
			)

			recv, port, err := bind.Open(0)
			if err != nil {
				t.Fatal(err)
			}
			if attempt != 2 || port != 40002 || len(recv) != 2 {
				t.Fatalf("Open() attempts=%d port=%d receive funcs=%d", attempt, port, len(recv))
			}
			if closed4 != 1 || closed6 != 1 {
				t.Fatalf("retry cleanup IPv4=%d IPv6=%d, want 1 each", closed4, closed6)
			}
		})
	}
}

func TestWinRingBindDoesNotRetryFixedPortConflict(t *testing.T) {
	wantErr := windows.WSAEADDRINUSE
	var opens int
	bind := &WinRingBind{use4: true, use6: true}
	withWinRingOperations(t, bind,
		func(_ *afWinRingBind, family int32, sa windows.Sockaddr) (windows.Sockaddr, error) {
			opens++
			if family == windows.AF_INET {
				return &windows.SockaddrInet4{Port: sa.(*windows.SockaddrInet4).Port}, nil
			}
			return nil, wantErr
		},
		func(*afWinRingBind) error { return nil },
		func(*afWinRingBind) error { return nil },
	)
	if _, _, err := bind.Open(30001); !errors.Is(err, wantErr) {
		t.Fatalf("Open() error = %v, want %v", err, wantErr)
	}
	if opens != 2 {
		t.Fatalf("Open() family calls = %d, want 2", opens)
	}
}

func TestWinRingBindDoesNotRetryRandomPortNonConflict(t *testing.T) {
	wantErr := windows.WSAENETDOWN
	var opens int
	bind := &WinRingBind{use4: true, use6: true}
	withWinRingOperations(t, bind,
		func(_ *afWinRingBind, family int32, _ windows.Sockaddr) (windows.Sockaddr, error) {
			opens++
			if family == windows.AF_INET {
				return &windows.SockaddrInet4{Port: 40000}, nil
			}
			return nil, wantErr
		},
		func(*afWinRingBind) error { return nil },
		func(*afWinRingBind) error { return nil },
	)
	if _, _, err := bind.Open(0); !errors.Is(err, wantErr) {
		t.Fatalf("Open() error = %v, want %v", err, wantErr)
	}
	if opens != 2 {
		t.Fatalf("Open() family calls = %d, want 2", opens)
	}
}

func TestWinRingBindStopsWhenRetryCleanupFails(t *testing.T) {
	bindErr := windows.WSAEADDRINUSE
	closeErr := errors.New("cleanup failed")
	bind := &WinRingBind{use4: true, use6: true}
	var closes int
	withWinRingOperations(t, bind,
		func(_ *afWinRingBind, family int32, _ windows.Sockaddr) (windows.Sockaddr, error) {
			if family == windows.AF_INET {
				return &windows.SockaddrInet4{Port: 40000}, nil
			}
			return nil, bindErr
		},
		func(*afWinRingBind) error { return nil },
		func(*afWinRingBind) error {
			closes++
			if closes == 1 {
				return closeErr
			}
			return nil
		},
	)
	if _, _, err := bind.Open(0); !errors.Is(err, bindErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Open() error = %v, want bind and cleanup errors", err)
	}
	if closes != 4 {
		t.Fatalf("family cleanup calls = %d, want 4 (retry cleanup plus deferred cleanup)", closes)
	}
}

func TestWinRingBindBoundsRandomPortRetries(t *testing.T) {
	bind := &WinRingBind{use4: true, use6: true}
	var ipv4Opens, closes int
	withWinRingOperations(t, bind,
		func(_ *afWinRingBind, family int32, _ windows.Sockaddr) (windows.Sockaddr, error) {
			if family == windows.AF_INET {
				ipv4Opens++
				return &windows.SockaddrInet4{Port: 40000 + ipv4Opens}, nil
			}
			return nil, windows.WSAEADDRINUSE
		},
		func(*afWinRingBind) error { return nil },
		func(*afWinRingBind) error { closes++; return nil },
	)
	if _, _, err := bind.Open(0); !errors.Is(err, windows.WSAEADDRINUSE) {
		t.Fatalf("Open() error = %v, want %v", err, windows.WSAEADDRINUSE)
	}
	if ipv4Opens != maxRandomPortRetries+1 {
		t.Fatalf("IPv4 open attempts = %d, want %d", ipv4Opens, maxRandomPortRetries+1)
	}
	wantCloses := maxRandomPortRetries*2 + 2
	if closes != wantCloses {
		t.Fatalf("family cleanup calls = %d, want %d", closes, wantCloses)
	}
}

func withWinRingOperations(
	t *testing.T,
	bind *WinRingBind,
	open func(*afWinRingBind, int32, windows.Sockaddr) (windows.Sockaddr, error),
	insert func(*afWinRingBind) error,
	close func(*afWinRingBind) error,
) {
	t.Helper()
	bind.ops = &winRingOperations{openFamily: open, insertReceive: insert, closeFamily: close}
	t.Cleanup(func() { bind.ops = nil })
}
