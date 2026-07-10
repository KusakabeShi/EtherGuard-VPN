package tap

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KusakabeSi/EtherGuard-VPN/mtypes"
	"github.com/songgao/water"
	"golang.org/x/sys/windows/registry"
)

const windowsAdapterClass = `SYSTEM\CurrentControlSet\Control\Class\{4D36E972-E325-11CE-BFC1-08002BE10318}`
const windowsNetworkClass = `SYSTEM\CurrentControlSet\Control\Network\{4D36E972-E325-11CE-BFC1-08002BE10318}`

type windowsTAPDevice struct {
	handle    io.ReadWriteCloser
	name      string
	mtu       int
	events    chan Event
	stop      chan struct{}
	closeOnce sync.Once
	closeErr  error
	status    func(string) (bool, error)
	wait      sync.WaitGroup
}

func newWindowsTAPDevice(handle io.ReadWriteCloser, name string, mtu int) *windowsTAPDevice {
	return &windowsTAPDevice{
		handle: handle,
		name:   name,
		mtu:    mtu,
		events: make(chan Event, 5),
		stop:   make(chan struct{}),
		status: windowsInterfaceUp,
	}
}

func (tap *windowsTAPDevice) Read(buffer []byte, offset int) (int, error) {
	if offset < 0 || offset >= len(buffer) {
		return 0, io.ErrShortBuffer
	}
	return tap.handle.Read(buffer[offset:])
}

func (tap *windowsTAPDevice) Write(buffer []byte, offset int) (int, error) {
	if offset < 0 || offset >= len(buffer) {
		return 0, io.ErrShortBuffer
	}
	return tap.handle.Write(buffer[offset:])
}

func (tap *windowsTAPDevice) Flush() error {
	return nil
}

func (tap *windowsTAPDevice) MTU() (int, error) {
	return tap.mtu, nil
}

func (tap *windowsTAPDevice) Name() (string, error) {
	return tap.name, nil
}

func (tap *windowsTAPDevice) Events() chan Event {
	return tap.events
}

func (tap *windowsTAPDevice) Close() error {
	tap.closeOnce.Do(func() {
		close(tap.stop)
		tap.closeErr = tap.handle.Close()
		tap.wait.Wait()
		close(tap.events)
	})
	return tap.closeErr
}

func (tap *windowsTAPDevice) monitor() {
	tap.monitorWithInterval(time.Second)
}

func (tap *windowsTAPDevice) monitorWithInterval(interval time.Duration) {
	ticker := time.NewTicker(interval)
	tap.monitorWithTicks(ticker.C, ticker.Stop)
}

func (tap *windowsTAPDevice) monitorWithTicks(ticks <-chan time.Time, stopTicks func()) {
	tap.wait.Add(1)
	go func() {
		defer tap.wait.Done()
		defer stopTicks()
		var last bool
		var initialized bool
		for {
			up, err := tap.status(tap.name)
			if err != nil {
				up = false
			}
			if !initialized || up != last {
				event := Event(EventDown)
				if up {
					event = EventUp
				}
				select {
				case tap.events <- event:
				case <-tap.stop:
					return
				}
				last = up
				initialized = true
			}
			select {
			case _, ok := <-ticks:
				if !ok {
					return
				}
			case <-tap.stop:
				return
			}
		}
	}()
}

func windowsInterfaceUp(name string) (bool, error) {
	adapter, err := net.InterfaceByName(name)
	if err != nil {
		return false, err
	}
	return adapter.Flags&net.FlagUp != 0, nil
}

func windowsTAPRegistryKey(name string) (string, string, string, error) {
	root, err := registry.OpenKey(registry.LOCAL_MACHINE, windowsAdapterClass, registry.READ)
	if err != nil {
		return "", "", "", fmt.Errorf("open TAP adapter registry: %w", err)
	}
	defer root.Close()
	names, err := root.ReadSubKeyNames(-1)
	if err != nil {
		return "", "", "", fmt.Errorf("list TAP adapters: %w", err)
	}
	for _, subkey := range names {
		path := windowsAdapterClass + `\` + subkey
		key, openErr := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.READ)
		if openErr != nil {
			continue
		}
		component, _, componentErr := key.GetStringValue("ComponentId")
		instance, _, instanceErr := key.GetStringValue("NetCfgInstanceId")
		key.Close()
		if componentErr != nil || instanceErr != nil || !strings.EqualFold(component, "tap0901") {
			continue
		}
		connection, connectionErr := registry.OpenKey(registry.LOCAL_MACHINE, windowsNetworkClass+`\`+instance+`\Connection`, registry.READ)
		if connectionErr != nil {
			continue
		}
		friendlyName, _, nameErr := connection.GetStringValue("Name")
		connection.Close()
		if nameErr == nil && friendlyName == name {
			return path, instance, component, nil
		}
	}
	return "", "", "", fmt.Errorf("TAP-Windows6 adapter %q with ComponentId tap0901 was not found", name)
}

func setWindowsTAPMAC(name string, mac MacAddress) (string, string, error) {
	path, instance, component, err := windowsTAPRegistryKey(name)
	if err != nil {
		return "", "", err
	}
	key, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.SET_VALUE)
	if err != nil {
		return "", "", fmt.Errorf("open TAP adapter %q for writing: %w", name, err)
	}
	defer key.Close()
	value := strings.ToUpper(strings.ReplaceAll(mac.String(), ":", ""))
	if err := key.SetStringValue("NetworkAddress", value); err != nil {
		return "", "", fmt.Errorf("set TAP adapter MAC: %w", err)
	}
	return instance, component, nil
}

func windowsWaterConfig(name, component string) water.Config {
	config := water.Config{DeviceType: water.TAP}
	config.ComponentID = component
	config.InterfaceName = name
	return config
}

func runWindowsCommand(name string, arguments ...string) error {
	output, err := exec.Command(name, arguments...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s failed: %w: %s", name, strings.Join(arguments, " "), err, strings.TrimSpace(string(output)))
	}
	return nil
}

func windowsAdapterRestartScript(instance string) string {
	return fmt.Sprintf("$ErrorActionPreference = 'Stop'; $adapter = Get-WmiObject Win32_NetworkAdapter -Filter \"GUID='%s'\"; if (-not $adapter) { exit 2 }; $result = $adapter.Disable(); if (-not $result -or $result.ReturnValue -ne 0) { exit 1 }; [System.Threading.Thread]::Sleep(500); $result = $adapter.Enable(); if (-not $result -or $result.ReturnValue -ne 0) { exit 1 }", instance)
}

func restartWindowsAdapter(instance string) error {
	script := windowsAdapterRestartScript(instance)
	return runWindowsCommand("powershell.exe", "-NoLogo", "-NoProfile", "-Command", script)
}

func configureWindowsTAP(iconfig mtypes.InterfaceConf, nodeID mtypes.Vertex) error {
	return configureWindowsTAPWithRunner(iconfig, nodeID, runWindowsCommand)
}

func configureWindowsTAPWithRunner(iconfig mtypes.InterfaceConf, nodeID mtypes.Vertex, runner func(string, ...string) error) error {
	name := iconfig.Name
	mtuValue := int(iconfig.MTU)
	if mtuValue == 0 {
		mtuValue = 1500
	}
	mtu := strconv.Itoa(mtuValue)
	if iconfig.IPv4CIDR != "" {
		if err := runner("netsh.exe", "interface", "ipv4", "set", "subinterface", name, "mtu="+mtu, "store=persistent"); err != nil {
			return err
		}
	}
	if iconfig.IPv6CIDR != "" || iconfig.IPv6LLPrefix != "" {
		if err := runner("netsh.exe", "interface", "ipv6", "set", "subinterface", name, "mtu="+mtu, "store=persistent"); err != nil {
			return err
		}
	}
	if iconfig.IPv4CIDR != "" {
		ip, mask, err := GetIP(4, iconfig.IPv4CIDR, uint32(nodeID))
		if err != nil {
			return err
		}
		ip4 := ip.To4()
		if ip4 == nil && len(ip) >= net.IPv4len {
			ip4 = net.IP(ip[len(ip)-net.IPv4len:])
		}
		if err := runner("netsh.exe", "interface", "ipv4", "set", "address", "name="+name, "source=static", "address="+ip4.String(), "mask="+net.IP(mask).String(), "gateway=none"); err != nil {
			return err
		}
	}
	for _, cidr := range []string{iconfig.IPv6CIDR, iconfig.IPv6LLPrefix} {
		if cidr == "" {
			continue
		}
		ip, mask, err := GetIP(6, cidr, uint32(nodeID))
		if err != nil {
			return err
		}
		ones, _ := mask.Size()
		_ = runner("netsh.exe", "interface", "ipv6", "delete", "address", "interface="+name, "address="+ip.String())
		if err := runner("netsh.exe", "interface", "ipv6", "add", "address", "interface="+name, "address="+ip.String()+"/"+strconv.Itoa(ones), "store=persistent"); err != nil {
			return err
		}
	}
	return nil
}

func CreateTAP(iconfig mtypes.InterfaceConf, nodeID mtypes.Vertex) (Device, error) {
	if iconfig.Name == "" {
		return nil, errors.New("Windows TAP adapter name is empty")
	}
	mac, err := GetMacAddr(iconfig.MacAddrPrefix, uint32(nodeID))
	if err != nil {
		return nil, fmt.Errorf("invalid Windows TAP MAC: %w", err)
	}
	instance, component, err := setWindowsTAPMAC(iconfig.Name, mac)
	if err != nil {
		return nil, err
	}
	if err := restartWindowsAdapter(instance); err != nil {
		return nil, err
	}
	config := windowsWaterConfig(iconfig.Name, component)
	adapter, err := water.New(config)
	if err != nil {
		return nil, fmt.Errorf("open Windows TAP adapter %q: %w", iconfig.Name, err)
	}
	mtu := int(iconfig.MTU)
	if mtu == 0 {
		mtu = 1500
	}
	device := newWindowsTAPDevice(adapter, iconfig.Name, mtu)
	if err := configureWindowsTAP(iconfig, nodeID); err != nil {
		if closeErr := device.Close(); closeErr != nil {
			return nil, errors.Join(err, fmt.Errorf("close Windows TAP adapter after configuration failure: %w", closeErr))
		}
		return nil, err
	}
	device.monitor()
	return device, nil
}

func CreateFdTAP(iconfig mtypes.InterfaceConf, nodeID mtypes.Vertex) (Device, error) {
	return nil, errors.New("fd TAP is not supported on Windows")
}
