# Etherguard

[中文版README](README_zh.md)

A Full Mesh Layer2 VPN based on wireguard-go  

[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](code_of_conduct.md)

OSPF can find best route based on it's cost.  
But sometimes the latency are different in the packet goes and back.  
I'm thinking, is it possible to find the best route based on the **single-way latency**?  
For example, I have two routes A and B at node N1, both of them can reach my node N2. A goes fast, but B backs fast.  
My VPN can automatically send packet through route A at node N1, and the packet backs from route B.

Here is the solution. This VPN `Etherguard` can collect all the single-way latency from all nodes, and calculate the best route using [Floyd–Warshall algorithm](https://en.wikipedia.org/wiki/Floyd–Warshall_algorithm).

Worried about the clock not match so that the measure result are not correct? It doesn't matter, here is the proof (Mandarin):  [https://www.kskb.eu.org/2021/08/rootless-routerpart-3-etherguard.html](https://www.kskb.eu.org/2021/08/rootless-routerpart-3-etherguard.html)

## Usage

```bash
Usage of ./etherguard-go:
  -bind string
        UDP socket bind mode. [linux|std]
        You may need std mode if tou want to run Etherguard under WSL. (default "linux")
  -config string
        Config path.
  -example
        Print example config
  -help
        Show this help
  -mode string
        Running mode. [super|edge|solve]
  -no-uapi
        Do not use UAPI
        With UAPI, you can check etherguard status by `wg` command
  -version
        Show version
```

## Mode

1. Static Mode: Similar to original wireguard. [Introduction](example_config/static_mode/README.md).
2. Super Mode: Inspired by[n2n](https://github.com/ntop/n2n). [Introduction](example_config/super_mode/README.md).
3. P2P Mode: Inspired by[tinc](https://github.com/gsliepen/tinc). [Introduction](example_config/p2p_mode/README.md).

## Common Config Paramater

### Edge Config

1. `interface`
    1. `itype`: Interface type.
        1. `dummy`: Dymmy interface, drop any packet received. You need this if you want to setup it as a relay node.
        2. `stdio`: Wrtie to stdout，read from stdin.  
           Paramaters: `macaddrprefix`,`l2headermode`
        3. `udpsock`: Read/Write the raw packet to an udp socket.  
           Paramaters: `recvaddr`,`sendaddr`
        3. `tcpsock`: Read/Write the raw packet to a tcp socket.  
           Paramaters: `recvaddr`,`sendaddr`
        3. `unixsock`: Read/Write the raw packet to an unix socket.  
           Paramaters: `recvaddr`,`sendaddr`
        3. `fd`: Read/Write the raw packet to specific file descriptor.  
           Paramaters: None. But require environment variable `EG_FD_RX` and `EG_FD_TX`
        4. `vpp`: Integrate to VPP by libmemif.  
           Paramaters: `name`,`vppifaceid`,`vppbridgeid`,`macaddrprefix`,`mtu`
        5. `tap`: Read/Write to tap device from linux.  
           Paramaters: `name`,`macaddrprefix`,`vppifaceid`,`mtu`
    2. `name` : Device name
    3. `vppifaceid`: Interface ID。Muse be unique in same VPP runtime
    4. `vppbridgeid`: VPP Bridge ID. Fill 0 if you don't use it.
    5. `macaddrprefix`: Mac address Prefix.  
                        Real Mac address=[Prefix]:[NodeID].  
                        If you fill full mac address here, NodeID will be ignored.
    6. `recvaddr`: Listen address for `XXXsock` mode(server mode)
    7. `sendaddr`: Packet send address for `XXXsock` mode(client mode)
    8. `l2headermode`: For debug usage, for `stdio` mode only
        1. `nochg`: Do not change anything.
        2. `kbdbg`: Keyboard debug mode.  
                    Let me construct Layer 2 header by ascii character only.  
                    So that I can track the packet flow with `loglevel` option.
        3. `noL2`: Remove all Layer 2 header, all boardcast
2. `nodeid`: NodeID. Must be unique in the whole Etherguard network.
3. `nodename`: Node Name.
4. `defaultttl`: Default TTL(etherguard layer. not affect ethernet layer)
5. `l2fibtimeout`: The timeout(in seconds) of the MacAddr-> NodeID lookup table
5. `privkey`: Private key. Same spec as wireguard.
5. `listenport`: UDP lesten port
6. `loglevel`: Log Level
    1. `loglevel`: `debug`,`error`,`slient` for wirefuard logger.
    2. `logtransit`: Log packets that neither the source or distenation is self.
    3. `logcontrol`: Log for all Control Message.
    4. `lognormal`: Log packets that either the source or distenation is self.
    5. `logntp`: NTP related logs.
7. `dynamicroute`: Log for dynamic route.
    1. `sendpinginterval`: Send `Ping` interval
    2. `dupchecktimeout`: Duplication chack timeout.
    3. `conntimeout`: Connection timeout.
    4. `savenewpeers`: Save peer info to local file.
    5. `supernode`: See [Super Mode](example_config/super_mode/README.md)
    6. `p2p` See [P2P Mode](example_config/p2p_mode/README.md)
    7. `ntpconfig`: NTP related settings
        1. `usentp`: USE NTP or not.
        2. `maxserveruse`: How many NTP servers should we use at once.  
           First time we will measure lentancy for all NTP server, next time it will use only fastest server.
        3. `synctimeinterval`: NTP sync interval.
        4. `ntptimeout`: NTP timeout
        5. `servers`: NTP server list
8. `nexthoptable`: Nexthop table。Only static mode use it. See [Static Mode](example_config/super_mode/README.md)
9. `resetconninterval`: Reset the endpoint for peers. You may need this if that peer use DDNS.
10. `peers`: Peer info.
    1. `nodeid`: Node ID.
    2. `pubkey`: Public key.
    3. `pskey`: Preshared key. Not implement yet.
    4. `endpoint`: Peer enddpoint. Will be overwrite if the peer roaming unless static=true.
    5. `static`: Do not overwrite by roaming and reset the connection every `resetconninterval` seconds.

### Super config

See [Super Mode](example_config/super_mode/README.md).

## Build

### No-vpp version

Build Etherguard.  

Install Go 1.16

```bash
add-apt-repository ppa:longsleep/golang-backports
apt-get -y update
apt-get install -y wireguard-tools golang-go build-essential git
```

Build

```bash
make
```

### VPP version

Build Etherguard with VPP integrated.  
You need libmemif.so installed to run this version.

Install VPP and libmemif

```bash
echo "deb [trusted=yes] https://packagecloud.io/fdio/release/ubuntu focal main" > /etc/apt/sources.list.d/99fd.io.list
curl -L https://packagecloud.io/fdio/release/gpgkey | sudo apt-key add -
apt-get -y update
apt-get install -y vpp vpp-plugin-core python3-vpp-api vpp-dbg vpp-dev libmemif libmemif-dev
```

Build

```bash
make vpp
```
