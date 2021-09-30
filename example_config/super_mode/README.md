# Etherguard
[中文版](README_zh.md)

This is the documentation of the super_mode of this example_config
Before reading this, I'd like to suggest you read the [static mode](../static_mode/README.md) first.

## Super mode

Super mode are inspired by [n2n](https://github.com/ntop/n2n)  
We have two types of node, we called it super node and edge node.

All edge nodes have to connect to super node, exchange data and UDP hole punch each other by super node.  
The super node runs the [Floyd-Warshall Algorithm](https://en.wikipedia.org/wiki/Floyd–Warshall_algorithm)， and distribute the result to all edge node.

In the super mode of the edge node, the `nexthoptable` and `peers` section are useless. All infos are download from super node.  
Meanwhile, super node will generate pre shared key for inter-edge communication(if `usepskforinteredge` enabled).
```golang
psk = shs256("PubkeyPeerA" + "PubkeyPeerB" + "Chef Special and Featured in the season see salt")[:32]
```

### SuperMsg
There are new type of DstID called `SuperMsg`(65534). All packets sends to and receive from super node are using this packet type.  
This packet will not send to any other edge node, just like `DstID == self.NodeID`

## Control Message
In Super mode, Beside `Normal Packet`. We introduce a new packet type called `Control Message`. In Super mode, we will not relay any control message. We just receive or send it to target directly.  
We list all the control message we use in the super mode below.

### Register
This control message works like this picture:
![Workflow of Register](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS01.png)  

1. edge node send Register to the super node  
2. Supernode knows it's external IP and port number
3. Update it to database and distribute `UpdatePeerMsg` to all edges
4. Other edges get the notification, download the updated peer infos from supernode via HTTP API

### Ping/Pong
While edges get the peer infos, edges will start trying to talk each other directly like this picture:
![Workflow of Ping/Pong](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS02.png)  

1. Send `Ping` to all other edges with local time with TTL=0
2. Received a `Ping`, Subtract the peer time from local time, we get a single way latency.
3. Send a `Pong` to supernode, let supernode calculate the NextHopTable
4. Wait the supernode push `UpdateNhTable` message and download it.

### UpdateNhTable
While supernode get a `Pong` message, it will run the [Floyd-Warshall Algorithm](https://en.wikipedia.org/wiki/Floyd–Warshall_algorithm) to calculate the NextHopTable
![image](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS03.png)  
If there are any changes of this table, it will distribute `UpdateNhTable` to all edges to till then download the latest NextHopTable via HTTP API as soon as possible.

### UpdateError
Notify edges that an error has occurred, and close the edge  
It occurs when the version number is not match with supernode, or the NodeID of the edge is configured incorrectly, or the edge is deleted.

### HTTP API 
Why we use HTTP API instead of pack all information in the `UpdateXXX`?  
Because UDP is an unreliable protocol, there is an limit on the amount of content that can be carried.  
But the peer list contains all the peer information, the length is not fixed, it may exceed  
So we use `UpdateXXX` to tell we have a update, please download the latest information from supernode via HTTP API as soon as possible.
And `UpdateXXX` itself is not reliable, maybe it didn't reach the edge node at all.  
So the information of `UpdateXXX` carries the `state hash`. Bring it when with HTTP API. When the super node receives the HTTP API and sees the `state hash`, it knows that the edge node has received the `UpdateXXX`.  
Otherwise, it will send `UpdateXXX` to the node again after few seconds.

## HTTP Guest API
HTTP also has some APIs for the front-end to help manage the entire network

### peerstate  

```bash
curl "http://127.0.0.1:3000/api/peerstate?Password=passwd"
```  
It can show some information such as single way latency or last seen time.   
We can visualize it by Force-directed graph drawing.  

There is an `Infinity` section in the json response. It should be 9999. It means infinity if the number larger than it.  
Cuz json can't present infinity so that I use this trick.  
While we see the latency larger than this, we doesn't need to draw lines in this two nodes.

Example return value:
```json
{
  "PeerInfo": {
    "1": {
      "Name": "hk",
      "LastSeen": "2021-09-29 11:23:22.854700559 +0000 UTC m=+28740.116476977"
    },
    "1001": {
      "Name": "relay_kr",
      "LastSeen": "2021-09-29 11:23:21.277417897 +0000 UTC m=+28738.539194315"
    },
    "121": {
      "Name": "za_north",
      "LastSeen": "0001-01-01 00:00:00 +0000 UTC"
    },
    "33": {
      "Name": "us_west",
      "LastSeen": "2021-09-29 11:23:13.257033252 +0000 UTC m=+28730.518809670"
    },
    "49": {
      "Name": "us_east",
      "LastSeen": "2021-09-29 11:23:16.606165241 +0000 UTC m=+28733.867941659"
    },
    "51": {
      "Name": "ca_central",
      "LastSeen": "0001-01-01 00:00:00 +0000 UTC"
    },
    "65": {
      "Name": "fr",
      "LastSeen": "2021-09-29 11:23:19.4084596 +0000 UTC m=+28736.670236018"
    },
    "81": {
      "Name": "au_central",
      "LastSeen": "0001-01-01 00:00:00 +0000 UTC"
    },
    "89": {
      "Name": "uae_north",
      "LastSeen": "0001-01-01 00:00:00 +0000 UTC"
    },
    "9": {
      "Name": "jp_east",
      "LastSeen": "2021-09-29 11:23:16.669505147 +0000 UTC m=+28733.931281565"
    },
    "97": {
      "Name": "br_south",
      "LastSeen": "0001-01-01 00:00:00 +0000 UTC"
    }
  },
  "Infinity": 99999,
  "Edges": {
    "1": {
      "1001": 0.033121187,
      "33": 0.075653164,
      "49": 0.100471502,
      "65": 0.065714769,
      "9": 0.022864241
    },
    "1001": {
      "1": 0.018561948,
      "33": 0.064077348,
      "49": 0.094459818,
      "65": 0.079481599,
      "9": 0.011163433
    },
    "33": {
      "1": 0.075263428,
      "1001": 0.070029457,
      "49": 0.032631349,
      "65": 0.045575061,
      "9": 0.050444255
    },
    "49": {
      "1": 0.100271358,
      "1001": 0.100182834,
      "33": 0.034563118,
      "65": 0.017950046,
      "9": 0.07510982
    },
    "65": {
      "1": 0.114219741,
      "1001": 0.132759205,
      "33": 0.095265063,
      "49": 0.067413235,
      "9": 0.127562362
    },
    "9": {
      "1": 0.026909699,
      "1001": 0.022555855,
      "33": 0.056469043,
      "49": 0.090400723,
      "65": 0.08525314
    }
  },
  "NhTable": {
    "1": {
      "1001": 1001,
      "33": 33,
      "49": 49,
      "65": 65,
      "9": 9
    },
    "1001": {
      "1": 1,
      "33": 33,
      "49": 49,
      "65": 65,
      "9": 9
    },
    "33": {
      "1": 1,
      "1001": 1001,
      "49": 49,
      "65": 65,
      "9": 9
    },
    "49": {
      "1": 1,
      "1001": 9,
      "33": 33,
      "65": 65,
      "9": 9
    },
    "65": {
      "1": 1,
      "1001": 1001,
      "33": 33,
      "49": 49,
      "9": 9
    },
    "9": {
      "1": 1,
      "1001": 1001,
      "33": 33,
      "49": 33,
      "65": 65
    }
  },
  "Dist": {
    "1": {
      "1": 0,
      "1001": 0.033121187,
      "33": 0.075119328,
      "49": 0.102236885,
      "65": 0.074688856,
      "9": 0.022473723
    },
    "1001": {
      "1": 0.018561948,
      "1001": 0,
      "33": 0.064077348,
      "49": 0.094459818,
      "65": 0.079481599,
      "9": 0.011163433
    },
    "33": {
      "1": 0.075263428,
      "1001": 0.070029457,
      "33": 0,
      "49": 0.032631349,
      "65": 0.045575061,
      "9": 0.050444255
    },
    "49": {
      "1": 0.100271358,
      "1001": 0.097665675,
      "33": 0.034563118,
      "49": 0,
      "65": 0.017950046,
      "9": 0.07510982
    },
    "65": {
      "1": 0.114219741,
      "1001": 0.132759205,
      "33": 0.095265063,
      "49": 0.067413235,
      "65": 0,
      "9": 0.127562362
    },
    "9": {
      "1": 0.026909699,
      "1001": 0.022555855,
      "33": 0.056469043,
      "49": 0.089100392,
      "65": 0.08525314,
      "9": 0
    }
  }
}
```

Section meaning:  
1. PeerInfo: NodeID，Name，LastSeen
2. Edges: The **Single way latency**，9999 or missing means unreachable(UDP hole punching failed)
3. NhTable: Calculate result.
4. Dist: The latency of **packet through Etherguard**

### peeradd
We can add new edges with this API without restart the supernode

Exanple:  
```
curl -X POST "http://127.0.0.1:3000/api/peer/add?Password=passwd_addpeer" \
                           -H "Content-Type: application/x-www-form-urlencoded" \
                           -d "nodeid=100&name=Node_100&pubkey=6SuqwPH9pxGigtZDNp3PABZYfSEzDaBSwuThsUUAcyM="
```

Parameter:
1. URL query: Password: Password. Configured in the config file.
1. Post body:
    1. nodeid: Node ID
    1. pubkey: Public Key
    1. pskey: Pre shared Key

Return value:
1. http code != 200: Error reason  
2. http code == 200，An example edge config.  
    * generate by contents in `edgetemplate` with custom data (nodeid/name/pubkey)
    * Convenient for users to copy and paste

```yaml
interface:
  itype: stdio
  name: tap1
  vppifaceid: 1
  vppbridgeid: 4242
  macaddrprefix: AA:BB:CC:DD
  mtu: 1416
  recvaddr: 127.0.0.1:4001
  sendaddr: 127.0.0.1:5001
  l2headermode: kbdbg
nodeid: 100
nodename: Node_100
defaultttl: 200
privkey: Your_Private_Key
listenport: 3001
loglevel:
  loglevel: normal
  logtransit: true
  logcontrol: true
  lognormal: true
  logntp: true
dynamicroute:
  sendpinginterval: 16
  peeralivetimeout: 30
  dupchecktimeout: 40
  conntimeout: 30
  connnexttry: 5
  savenewpeers: true
  supernode:
    usesupernode: true
    pskey: ""
    connurlv4: 127.0.0.1:3000
    pubkeyv4: LJ8KKacUcIoACTGB/9Ed9w0osrJ3WWeelzpL2u4oUic=
    connurlv6: ""
    pubkeyv6: HCfL6YJtpJEGHTlJ2LgVXIWKB/K95P57LHTJ42ZG8VI=
    apiurl: http://127.0.0.1:3000/api
    supernodeinfotimeout: 50
  p2p:
    usep2p: false
    sendpeerinterval: 20
    graphrecalculatesetting:
      jittertolerance: 20
      jittertolerancemultiplier: 1.1
      nodereporttimeout: 40
      recalculatecooldown: 5
  ntpconfig:
    usentp: true
    maxserveruse: 8
    synctimeinterval: 3600
    ntptimeout: 3
    servers:
    - time.google.com
    - time1.google.com
    - time2.google.com
    - time3.google.com
    - time4.google.com
    - time1.facebook.com
    - time2.facebook.com
    - time3.facebook.com
    - time4.facebook.com
    - time5.facebook.com
    - time.cloudflare.com
    - time.apple.com
    - time.asia.apple.com
    - time.euro.apple.com
    - time.windows.com
nexthoptable: {}
resetconninterval: 86400
peers: []
```

### peerdel  
Delete peer

There are two deletion modes, namely password deletion and private key deletion.  
Designed to be used by administrators, or for people who join the network and want to leave the network.  

Use Password to delete any node. Take the newly added node above as an example, use this API to delete the node
```
curl "http://127.0.0.1:3000/api/peer/del?Password=passwd_delpeer&nodeid=100"
```

We can also use privkey to delete, the same as above, but use privkey parameter only.
```
curl "http://127.0.0.1:3000/api/peer/del?privkey=IJtpnkm9ytbuCukx4VBMENJKuLngo9KSsS1D60BqonQ="
```

Parameter:
1. URL query: 
    1. Password: Password: Password. Configured in the config file.
    1. nodeid: Node ID that you want to delete
    1. privkey: The private key of the edge

Return value:
1. http code != 200: Error reason  
2. http code == 200: Success message

## Config Parameters

### Super mode of edge node
1. `usesupernode`: Whether to enable Super mode
1. `pskey`: Pre shared Key used to establish connection with supernode
1. `connurlv4`: IPv4 connection address of the Super node
1. `pubkeyv4`: IPv4 key of Super node
1. `connurlv6`: IPv6 connection address of the Super node
1. `pubkeyv6`: IPv6 key of Super node
1. `apiurl`: HTTP(S) API connection address of Super node
1. `supernodeinfotimeout`: Supernode Timeout

### Super node it self

1. nodename: node name
1. privkeyv4: private key for ipv4
1. privkeyv6: private key for ipv6
1. listenport: listen udp port number
1. loglevel: Refer to [README.md](../README.md)
1. repushconfiginterval: re-push interval of `UpdateXXX` messages
1. passwords: HTTP API password
    1. showstate: node information
    1. addpeer: add peer
    1. delpeer: delete peer
1. graphrecalculatesetting: Some parameters related to [Floyd-Warshall algorithm](https://zh.wikipedia.org/zh-tw/Floyd-Warshall algorithm)
    1. staticmode: Disable the Floyd-Warshall algorithm and only use the nexthoptable loaded at the beginning.  
                   Supernode is only used to assist hole punching
    1. recalculatecooldown: Floyd-Warshal is O(n^3) time complexity algorithm, which cannot be calculated too often. Set a cooling time
    1. jittertolerance: jitter tolerance, after receiving Pong, one 37ms and one 39ms will not trigger recalculation
    1. jittertolerancemultiplier: the same is the jitter tolerance, but high ping allows more errors
                                    https://www.desmos.com/calculator/raoti16r5n
    1. nodereporttimeout: The timeout of the received `Pong` packet. Change back to Infinity after timeout.
1. nexthoptable: only works in `staticmode==true`, set nexthoptable manually
1. edgetemplate: for `addpeer` API. Refer to this configuration file and show a sample configuration file of the edge to the user
1. usepskforinteredge: Whether to enable pre-share key communication between edges. If enabled, supernode will generate PSKs for edges  automatically
1. peers: Peer list, refer to [README.md](../README.md)
    1. nodeid: Peer's node ID
    1. name: Peer name (displayed on the front end)
    1. pubkey: peer public key
    1. pskey: preshared key The PSK that this peer connects to this Supernode

## V4 V6 Two Keys
Why we split IPv4 and IPv6 into two session? 
Because of this situation

![OneChannel](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS04.png)

In this case, SuperNode does not know the external ipv4 address of Node02 and cannot help Node1 and Node2 to UDP hole punch.

![TwoChannel](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS05.png)

So like this, both V4 and V6 establish a session, so that both V4 and V6 can be taken care of at the same time.

## UDP hole punch reachability
For different NAT type, the UDP hole punch reachability can refer this table.([Origin](https://dh2i.com/kbs/kbs-2961448-understanding-different-nat-types-and-hole-punching/))

![reachability between NAT types](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS06.png)  

And if both sides are using ConeNAT, it's not gerenteed to punch success. It depends on the topology and the devices attributes.  
Like the section 3.5 in [this article](https://bford.info/pub/net/p2pnat/#SECTION00035000000000000000), we can't punch success.

## Notice for Relay node
Unlike n2n, our supernode do not relay any packet for edges.  
If the edge punch failed and no any route available, it's just unreachable. In this case we need to setup a relay node.

Relay node is a regular edge in public network, but `interface=dummy`.  

And we have to note that **do not** use 127.0.0.1 to connect to supernode.  
Because supernode well distribute the source IP of the nodes to all other edges. But 127.0.0.1 is not accessible from other edge.  

![Setup relay node](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS07.png)

To avoid this issue, please use the external IP of the supernode in the edge config.

## Quick start
Run this example_config (please open three terminals):
```bash
./etherguard-go -config example_config/super_mode/s1.yaml -mode super
./etherguard-go -config example_config/super_mode/n1.yaml -mode edge
./etherguard-go -config example_config/super_mode/n2.yaml -mode edge
```
Because it is in `stdio` mode, stdin will be read into the VPN network  
Please type in one of the edge windows
```
b1aaaaaaaaaa
```
b1 will be converted into a 12byte layer 2 header, b is the broadcast address `FF:FF:FF:FF:FF:FF`, 1 is the ordinary MAC address `AA:BB:CC:DD:EE:01`, aaaaaaaaaa is the payload, and then feed it into the VPN  
You should be able to see the string b1aaaaaaaaaa on another window. The first 12 bytes are converted back

## Next: [P2P Mode](../p2p_mode/README.md)