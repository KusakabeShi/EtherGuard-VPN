package tap

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/songgao/water"
)

type fakeWindowsTAPHandle struct {
	readData  []byte
	written   []byte
	closeErr  error
	closeCall int
	readCall  int
	writeCall int
}

func TestWindowsWaterConfigPreservesRegistryComponentIDCase(t *testing.T) {
	config := windowsWaterConfig("tap1", "TAP0901")
	if config.DeviceType != water.TAP || config.InterfaceName != "tap1" || config.ComponentID != "TAP0901" {
		t.Fatalf("windowsWaterConfig() = %#v", config)
	}
}

func TestWindowsTAPIntegration(t *testing.T) {
	if os.Getenv("ETHERGUARD_TAP_INTEGRATION") != "1" {
		t.Skip("set ETHERGUARD_TAP_INTEGRATION=1 to use the installed TAP-Windows6 adapter")
	}
	device, err := CreateTAP(mtypes.InterfaceConf{
		Name:          "tap1",
		MacAddrPrefix: "02:00",
		IPv4CIDR:      "192.0.2.0/30",
		MTU:           1500,
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { device.Close() })
	if name, err := device.Name(); err != nil || name != "tap1" {
		t.Fatalf("Name() = %q, %v", name, err)
	}
	if mtu, err := device.MTU(); err != nil || mtu != 1500 {
		t.Fatalf("MTU() = %d, %v", mtu, err)
	}
}

func (handle *fakeWindowsTAPHandle) Read(buffer []byte) (int, error) {
	handle.readCall++
	return copy(buffer, handle.readData), nil
}

func (handle *fakeWindowsTAPHandle) Write(buffer []byte) (int, error) {
	handle.writeCall++
	handle.written = append(handle.written, buffer...)
	return len(buffer), nil
}

func (handle *fakeWindowsTAPHandle) Close() error {
	handle.closeCall++
	return handle.closeErr
}

func TestWindowsTAPReadHonorsOffset(t *testing.T) {
	handle := &fakeWindowsTAPHandle{readData: []byte{1, 2, 3}}
	device := newWindowsTAPDevice(handle, "tap1", 1500)
	buffer := bytes.Repeat([]byte{9}, 8)
	n, err := device.Read(buffer, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || !bytes.Equal(buffer, []byte{9, 9, 1, 2, 3, 9, 9, 9}) {
		t.Fatalf("Read() = %d, %v", n, buffer)
	}
}

func TestWindowsTAPWriteHonorsOffset(t *testing.T) {
	handle := &fakeWindowsTAPHandle{}
	device := newWindowsTAPDevice(handle, "tap1", 1500)
	n, err := device.Write([]byte{9, 9, 1, 2, 3}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 || !bytes.Equal(handle.written, []byte{1, 2, 3}) {
		t.Fatalf("Write() = %d, %v", n, handle.written)
	}
}

func TestWindowsTAPRejectsInvalidOffset(t *testing.T) {
	handle := &fakeWindowsTAPHandle{}
	device := newWindowsTAPDevice(handle, "tap1", 1500)
	if _, err := device.Read(make([]byte, 4), 5); !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("Read() error = %v, want io.ErrShortBuffer", err)
	}
	if _, err := device.Write(make([]byte, 4), -1); !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("Write() error = %v, want io.ErrShortBuffer", err)
	}
	if _, err := device.Read(make([]byte, 4), 4); !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("Read() at end error = %v, want io.ErrShortBuffer", err)
	}
	if _, err := device.Write(nil, 0); !errors.Is(err, io.ErrShortBuffer) {
		t.Fatalf("Write() with empty buffer error = %v, want io.ErrShortBuffer", err)
	}
	if handle.readCall != 0 || handle.writeCall != 0 {
		t.Fatalf("invalid offsets reached handle: reads=%d writes=%d", handle.readCall, handle.writeCall)
	}
}

func TestWindowsTAPCloseIsIdempotent(t *testing.T) {
	wantErr := errors.New("close failed")
	handle := &fakeWindowsTAPHandle{closeErr: wantErr}
	device := newWindowsTAPDevice(handle, "tap1", 1500)
	if err := device.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := device.Close(); !errors.Is(err, wantErr) {
		t.Fatalf("second Close() error = %v", err)
	}
	if handle.closeCall != 1 {
		t.Fatalf("Close() called handle %d times", handle.closeCall)
	}
}

func TestWindowsTAPBuildsWin7CompatibleNetshCommands(t *testing.T) {
	var commands [][]string
	runner := func(name string, arguments ...string) error {
		commands = append(commands, append([]string{name}, arguments...))
		return nil
	}
	err := configureWindowsTAPWithRunner(mtypes.InterfaceConf{
		Name:         "tap1",
		MTU:          1400,
		IPv4CIDR:     "192.0.2.0/30",
		IPv6CIDR:     "fd00::/64",
		IPv6LLPrefix: "fe80::/64",
	}, 2, runner)
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"netsh.exe", "interface", "ipv4", "set", "subinterface", "tap1", "mtu=1400", "store=persistent"},
		{"netsh.exe", "interface", "ipv6", "set", "subinterface", "tap1", "mtu=1400", "store=persistent"},
		{"netsh.exe", "interface", "ipv4", "set", "address", "name=tap1", "source=static", "address=192.0.2.2", "mask=255.255.255.252", "gateway=none"},
		{"netsh.exe", "interface", "ipv6", "delete", "address", "interface=tap1", "address=fd00::2"},
		{"netsh.exe", "interface", "ipv6", "add", "address", "interface=tap1", "address=fd00::2/64", "store=persistent"},
		{"netsh.exe", "interface", "ipv6", "delete", "address", "interface=tap1", "address=fe80::2"},
		{"netsh.exe", "interface", "ipv6", "add", "address", "interface=tap1", "address=fe80::2/64", "store=persistent"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestWindowsTAPUsesDefaultMTUInNetshCommands(t *testing.T) {
	var commands [][]string
	runner := func(name string, arguments ...string) error {
		commands = append(commands, append([]string{name}, arguments...))
		return nil
	}
	if err := configureWindowsTAPWithRunner(mtypes.InterfaceConf{Name: "tap1", IPv4CIDR: "192.0.2.0/30"}, 1, runner); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"netsh.exe", "interface", "ipv4", "set", "subinterface", "tap1", "mtu=1500", "store=persistent"},
		{"netsh.exe", "interface", "ipv4", "set", "address", "name=tap1", "source=static", "address=192.0.2.1", "mask=255.255.255.252", "gateway=none"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestWindowsTAPIPv6OnlySkipsIPv4Commands(t *testing.T) {
	var commands [][]string
	runner := func(name string, arguments ...string) error {
		commands = append(commands, append([]string{name}, arguments...))
		return nil
	}
	if err := configureWindowsTAPWithRunner(mtypes.InterfaceConf{Name: "tap1", IPv6CIDR: "fd00::/64"}, 2, runner); err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"netsh.exe", "interface", "ipv6", "set", "subinterface", "tap1", "mtu=1500", "store=persistent"},
		{"netsh.exe", "interface", "ipv6", "delete", "address", "interface=tap1", "address=fd00::2"},
		{"netsh.exe", "interface", "ipv6", "add", "address", "interface=tap1", "address=fd00::2/64", "store=persistent"},
	}
	if !reflect.DeepEqual(commands, want) {
		t.Fatalf("commands = %#v, want %#v", commands, want)
	}
}

func TestWindowsAdapterRestartScriptIsPowerShell2Compatible(t *testing.T) {
	script := windowsAdapterRestartScript("adapter-guid")
	for _, required := range []string{"$ErrorActionPreference = 'Stop'", "if (-not $result", "[System.Threading.Thread]::Sleep(500)"} {
		if !strings.Contains(script, required) {
			t.Fatalf("restart script %q does not contain %q", script, required)
		}
	}
	if strings.Contains(script, "Start-Sleep -Milliseconds") {
		t.Fatalf("restart script uses an unsupported sleep command: %q", script)
	}
}

func TestWindowsTAPMonitorDeduplicatesEvents(t *testing.T) {
	device := newWindowsTAPDevice(&fakeWindowsTAPHandle{}, "tap1", 1500)
	ticks := make(chan time.Time)
	states := []bool{false, true, true}
	device.status = func(string) (bool, error) {
		state := states[0]
		if len(states) > 1 {
			states = states[1:]
		}
		return state, nil
	}
	device.monitorWithTicks(ticks, func() {})
	defer device.Close()
	wantWindowsTAPEvent(t, device.Events(), EventDown)
	ticks <- time.Now()
	wantWindowsTAPEvent(t, device.Events(), EventUp)
	ticks <- time.Now()
	select {
	case event := <-device.Events():
		t.Fatalf("unexpected duplicate event %v", event)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestWindowsTAPMonitorTreatsStatusErrorsAsDown(t *testing.T) {
	device := newWindowsTAPDevice(&fakeWindowsTAPHandle{}, "tap1", 1500)
	ticks := make(chan time.Time)
	statusErr := errors.New("adapter disappeared")
	states := []struct {
		up  bool
		err error
	}{
		{up: true},
		{err: statusErr},
		{err: statusErr},
		{up: true},
	}
	device.status = func(string) (bool, error) {
		state := states[0]
		states = states[1:]
		return state.up, state.err
	}
	device.monitorWithTicks(ticks, func() {})
	defer device.Close()
	wantWindowsTAPEvent(t, device.Events(), EventUp)
	ticks <- time.Now()
	wantWindowsTAPEvent(t, device.Events(), EventDown)
	ticks <- time.Now()
	select {
	case event := <-device.Events():
		t.Fatalf("unexpected duplicate event %v", event)
	case <-time.After(50 * time.Millisecond):
	}
	ticks <- time.Now()
	wantWindowsTAPEvent(t, device.Events(), EventUp)
}

func TestWindowsTAPMonitorStopsWhenTicksClose(t *testing.T) {
	device := newWindowsTAPDevice(&fakeWindowsTAPHandle{}, "tap1", 1500)
	ticks := make(chan time.Time)
	stopped := make(chan struct{})
	device.status = func(string) (bool, error) { return true, nil }
	device.monitorWithTicks(ticks, func() { close(stopped) })
	wantWindowsTAPEvent(t, device.Events(), EventUp)
	close(ticks)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("monitor did not stop after its tick source closed")
	}
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}
}

func wantWindowsTAPEvent(t *testing.T, events <-chan Event, want Event) {
	t.Helper()
	select {
	case event := <-events:
		if event != want {
			t.Fatalf("event = %v, want %v", event, want)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for event %v", want)
	}
}
