# Etherguard
[English](#) | [中文](README_zh.md)

## P2P Mode

P2P Mode is inspired by [tinc](https://github.com/gsliepen/tinc), There are no SuperNode. All EdgeNode will exchange information each other.  
EdgeNodes are keep trying to connect each other, and notify all other peers success or not.  
All edges runs [Floyd-Warshall Algorithm](https://en.wikipedia.org/wiki/Floyd–Warshall_algorithm) locally and find the best route by it self.  
**Not recommend to use this mode in production environment, not test yet.**

## Quick Start
First, edit the `gensp2p.yaml`

```yaml
Config output dir: /tmp/eg_gen_static   # Profile output location
ConfigTemplate for edge node: ""        # Profile Template
Network name: "EgNet"
Edge Node:
  MacAddress prefix: ""                 # Leave blank to generate randomly
  IPv4 range: 192.168.76.0/24           # By the way, the IP part can be omitted.
  IPv6 range: fd95:71cb:a3df:e586::/64  # The only purpose of this field is to call the ip command after startup to add an ip to the tap interface
  IPv6 LL range: fe80::a3df:0/112       # 
Edge Nodes:                             # Node related settings
  1:
    Endpoint(optional): 127.0.0.1:3001
  2:
    Endpoint(optional): 127.0.0.1:3002
  3:
    Endpoint(optional): 127.0.0.1:3003
  4:
    Endpoint(optional): 127.0.0.1:3004
  5:
    Endpoint(optional): 127.0.0.1:3005
  6:
    Endpoint(optional): 127.0.0.1:3006
```

Run this, it will generate the required configuration file
```
./etherguard-go -mode gencfg -cfgmode p2p -config example_config/p2p_mode/genp2p.yaml
```

Deploy these configuration files to the corresponding nodes, and then execute  
```
./etherguard-go -config [config path] -mode edge
```

you can turn off unnecessary logs to increase performance after it works.

[WIP]