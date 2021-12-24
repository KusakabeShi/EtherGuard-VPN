# Etherguard

[English](#) | [中文](README_zh.md)

[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](code_of_conduct.md)

A Full Mesh Layer2 VPN based on wireguard-go  

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
        You may need std mode if you want to run Etherguard under WSL. (default "linux")
  -cfgmode string
        Running mode for generated config. [none|super|p2p]
  -config string
        Config path for the interface.
  -example
        Print example config
  -help
        Show this help
  -mode string
        Running mode. [super|edge|solve|gencfg]
  -no-uapi
        Disable UAPI
        With UAPI, you can check etherguard status by "wg" command
  -version
        Show version
```

## Working Mode

Mode        | Description
------------|:-----
Static Mode | No dynamic routing, no handshake server.<br>Similar to original wireguard , all configs are static<br>[Detail](example_config/static_mode/README.md)
Super Mode | Inspired by [n2n](https://github.com/ntop/n2n). There 2 types of node: SuperNode and EdgeNode<br>EdgeNode must connect to SuperNode first，get connection info of other EdgeNode from the SuperNode<br>The SuperNode runs [Floyd-Warshall Algorithm](https://en.wikipedia.org/wiki/Floyd–Warshall_algorithm)，and distribute the result to all other EdgeNodes.<br>[Detail](example_config/super_mode/README.md)
P2P Mode | Inspired by [tinc](https://github.com/gsliepen/tinc), There are no SuperNode. All EdgeNode will exchange information each other.<br>EdgeNodes are keep trying to connect each other, and notify all other peers success or not.<br>All edges runs [Floyd-Warshall Algorithm](https://en.wikipedia.org/wiki/Floyd–Warshall_algorithm) locally and find the best route by it self.<br>**Not recommend to use this mode in production environment, not test yet.**<br>[Detail](example_config/p2p_mode/README.md)

## Quick start

[Super mode quick start](example_config/super_mode/README.md)

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
