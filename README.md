# Etherguard
[中文](README_zh.md)

WIP

## Build

### No-vpp version

#### Dependency
Go 1.16
```bash
add-apt-repository ppa:longsleep/golang-backports
apt-get -y update
apt-install -y wireguard-tools golang-go build-essential
```
#### Build
```bash
make
```

### VPP version

#### Dependency

VPP and libemif is requires

```
echo "deb [trusted=yes] https://packagecloud.io/fdio/release/ubuntu focal main" > /etc/apt/sources.list.d/99fd.io.list
curl -L https://packagecloud.io/fdio/release/gpgkey | sudo apt-key add -
apt-get -y update
apt-get install -y vpp vpp-plugin-core python3-vpp-api vpp-dbg vpp-dev libmemif libmemif-dev
```
#### Build
```bash
make vpp
```