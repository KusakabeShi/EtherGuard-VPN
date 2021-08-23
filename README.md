### Build
```
echo "deb [trusted=yes] https://packagecloud.io/fdio/release/ubuntu focal main" > /etc/apt/sources.list.d/99fd.io.list
curl -L https://packagecloud.io/fdio/release/gpgkey | sudo apt-key add -
apt-get update
apt-get install vpp vpp-plugin-core python3-vpp-api vpp-dbg vpp-dev libmemif libmemif-dev wireguard-tools
export CGO_CFLAGS="-I/usr/include/memif"
make
```