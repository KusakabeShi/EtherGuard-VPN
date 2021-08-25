### Requirement

Install VPP and Go
```bash
echo "deb [trusted=yes] https://packagecloud.io/fdio/release/ubuntu focal main" > /etc/apt/sources.list.d/99fd.io.list
curl -L https://packagecloud.io/fdio/release/gpgkey | sudo apt-key add -
add-apt-repository ppa:longsleep/golang-backports
apt-get -y update
apt-get install vpp vpp-plugin-core python3-vpp-api vpp-dbg vpp-dev libmemif libmemif-dev wireguard-tools golang-go build-essential golang-go
```

### Build
```bash
export CGO_CFLAGS="-I/usr/include/memif"
make
```