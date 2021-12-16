# Etherguard
[English](README.md) | [中文](#)

## Static Mode

沒有自動選路，沒有握手伺服器  
類似原本的wireguard，一切都要提前配置好  
路由表也是如此。您需要手動配置設定檔裡面的`NextHopTable`部分

這個模式下，不存在任何的Control Message，斷線偵測什麼的也不會有  
請務必保持提前定義好的拓樸。不然如果存在中轉，中轉節點斷了，部分連線就會中斷

## Quick Start
首先，按照需求修改`genstatic.yaml`

```yaml
Config output dir: /tmp/eg_gen_static   # 設定檔輸出位置
ConfigTemplate for edge node: ""        # 設定檔Template
Network name: "EgNet"
Edge Node:
  MacAddress prefix: ""                 # 留空隨機產生
  IPv4 range: 192.168.76.0/24           # 順帶一提，IP的部分可以直接省略沒關係  
  IPv6 range: fd95:71cb:a3df:e586::/64  # 這個欄位唯一的目的只是在啟動以後，調用ip命令，幫tap接口加個ip  
  IPv6 LL range: fe80::a3df:0/112       # 和VPN本身運作完全無關  
Edge Nodes:                             # 所有的節點相關設定
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
Distance matrix for all nodes: |-       # 左邊是起點，上面是終點，Inf代表此二節點不相連 ，數值代表相連。數值大小代表通過成本(通常是延遲)
  X 1   2   3   4   5   6
  1 0   1.0 Inf Inf Inf Inf
  2 1.0 0   1.0 1.0 Inf Inf
  3 Inf 1.0 0   1   1.0 Inf
  4 Inf 1.0 1.0 0   Inf 1.0
  5 Inf Inf 1.0 Inf 1.0 Inf
  6 Inf Inf Inf 1.0 Inf 1.0
```
接著執行這個，就會生成所需設定檔了。
```
./etherguard-go -mode gencfg -cfgmode static -config example_config/static_mode/genstatic.yaml
```

把這些設定檔部署去對應節點，然後再執行  
```
./etherguard-go -config [設定檔位置] -mode edge
```
就可以了

確認運作以後，可以關閉不必要的log增加性能

## Documentation

Static Mode的說明文件

這份[範例配置檔](./)的網路拓樸如圖所示

!["Topology"](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/static_mode/Example_static.png)

發出封包時，會設定起始ID=自己的Node ID，終點ID則是看Dst Mac Address。  
如果Dst MacAddr是廣播地址，或是不在自己的對應表裡面，就會設定終點=Broadcast

收到封包的時候，如果`dst==自己ID`，就會收下，不轉給任何人。  
同時還會看它的 Src Mac Address 和 Src NodeID ，並加入對應表  
這樣下次傳給他就可以直接傳給目標，而不用廣播給全節點了

所以設定檔中的轉發表如下表。格式是yaml的巢狀dictionary  
轉發/發送封包時，直接查詢`NhTable`  
就知道下面一個封包要轉給誰了

NextHopTable 是長這樣的資料結構，`NhTable[起點][終點]=下一跳`  

```yaml
NextHopTable:
  1:
    2: 2
    3: 2
  2:
    1: 1
    3: 3

  3:
    1: 2
    2: 2
```

### Broadcast 
比較特別的是`終點ID=Broadcast`的情況。

假設今天的狀況:我是4號，我收到`起點ID = 1，終點ID=Broadcast`的封包  
我應該只轉給6號就好，而不會轉給3號。  
因為3號會收到來自2號的封包，自己就不用重複遞送了

因此我有設計，如果`終點ID = Broadcast`，就會檢查Src到自己的所有鄰居，會不會經過自己  
**1 -> 6** 會經過自己: [1 2 4 6]  
**1 -> 3** 不會: [1 2 3]  
2號是封包來源跳過檢查  
就能知道我應該把封包轉送給6號，而不轉送給3號


### 小工具

如果懶的手算轉發表，本工具也能幫你算算

請先準備好一個txt檔，就叫他path.txt吧  
標記任2節點之間的單向延遲。`Inf`代表不可直連

```
X 1   2   3   4   5   6
1 0   1.0 Inf Inf Inf Inf
2 1.0 0   1.0 1.0 Inf Inf
3 Inf 1.0 0   1   1.0 Inf
4 Inf 1.0 1.0 0   Inf 1.0
5 Inf Inf 1.0 Inf 1.0 Inf
6 Inf Inf Inf 1.0 Inf 1.0
```

之後用這個指令就能輸出用Floyd Warshall算好的轉發表了，填入設定檔即可

### EdgeNode Config Parameter

<a name="EdgeConfig"></a>EdgeConfig    | Description
---------------------|:-----
[Interface](#Interface)| 接口相關設定。VPN有兩端，一端是VPN網路，另一端則是本地接口
NodeID               | 節點ID。節點之間辨識身分用的，同一網路內節點ID不能重複
NodeName             | 節點名稱
PostScript           | 初始化完畢之後要跑的腳本
DefaultTTL           | TTL，etherguard層使用，和乙太層不共通
L2FIBTimeout         | MacAddr-> NodeID 查找表的 timeout(秒) ，類似ARP table
PrivKey              | 私鑰，和wireguard規格一樣
ListenPort           | 監聽的udp埠
[LogLevel](#LogLevel)| 紀錄log
[DynamicRoute](../super_mode/README_zh.md#DynamicRoute)      | 動態路由相關設定<br>StaticMode用不到
NextHopTable          | 轉發表， 下一跳 = `NhTable[起點][終點]`<br>SuperMode以及P2PMode用不到
ResetEndPointInterval | 每隔一段時間就會重置連線，重新解析域名<br>只對標記為Static的Peer生效<br>如果有Endpoint是動態ip就要用這個
[Peers](#Peers)       | 鄰居節點。<br>SuperMode用不到，從SuperNode接收

<a name="Interface"></a>Interface      | Description
---------------|:-----
[IType](#IType)| 接口類型，意味著從VPN網路收到的封包要丟去哪邊
Name           | 裝置名稱
VPPIFaceID     | VPP 的 interface ID。同一個VPP runtime內不能重複
VPPBridgeID    | VPP 的網橋ID。不使用VPP網橋功能的話填0
MacAddrPrefix  | MAC地址前綴。真正的 MAC 地址=[前綴]:[NodeID]
IPv4CIDR       | 啟動以後，調用ip命令，幫tap接口加個ip。僅限tap有效
IPv4CIDR       | 啟動以後，調用ip命令，幫tap接口加個ip。僅限tap有效
IPv6LLPrefix   | 啟動以後，調用ip命令，幫tap接口加個ip。僅限tap有效
MTU            | 裝置MTU，僅限`tap` , `vpp` 模式有效
RecvAddr       | listen地址，收到的東西丟去 VPN 網路。僅限`*sock`生效
SendAddr       | 連線地址，VPN網路收到的東西丟去這個地址。僅限`*sock`生效
[L2HeaderMode](#L2HeaderMode)   | 僅限 `stdio` 生效。debug用途，有三種模式

<a name="IType"></a>IType      | Description
-----------|:-----
dummy      | 收到的封包直接丟棄，但幫忙轉發。作為中繼節點，本身不加入網路使用
stdio      | 收到的封包丟stdout，stdin進來的資料丟入vpn網路，debug用途<br>需要參數: `MacAddrPrefix` && `L2HeaderMode`
udpsock    | 收到的封包丟去一個udp socket<br>需要參數: `RecvAddr` && `SendAddr`
tcpsock    | 收到的封包丟去一個tcp socket<br>需要參數: `RecvAddr` \|\| `SendAddr`
unixsock   | 收到的封包丟去一個unix socket(SOCK_STREAM 模式)<br>需要參數: `RecvAddr` \|\| `SendAddr`
udpsock    | 收到的封包丟去一個unix socket(SOCK_DGRAM 模式)<br>需要參數: `RecvAddr` \|\| `SendAddr`
udpsock    | 收到的封包丟去一個unix socket(SOCK_SEQPACKET 模式)<br>需要參數: `RecvAddr` \|\| `SendAddr`
fd         | 收到的封包丟去一個特定的file descriptor<br>需要參數: 無. 但是使用環境變數 `EG_FD_RX` && `EG_FD_TX` 來指定
vpp        | 使用libmemif使vpp加入VPN網路<br>需要參數: `Name` && `VPPIFaceID` && `VPPBridgeID` && `MacAddrPrefix` && `MTU`
tap        | Linux的tap設備。讓linux加入VPN網路<br>需要參數: `Name` && `MacAddrPrefix` && `MTU`<br>可選參數:`IPv4CIDR` , `IPv6CIDR` , `IPv6LLPrefix`

<a name="L2HeaderMode"></a>L2HeaderMode   | Description
---------------|:-----
nochg          | 收到的封包丟stdout，stdin進來的資料丟入vpn網路，不對封包作任何更動
kbdbg          | 前 12byte 會用來做選路判斷<br>但是stdio模式下，使用鍵盤輸入一個Ethernet frame不太方便<br>此模式讓我快速產生Ethernet frame，debug更方便<br>`b`轉換成`FF:FF:FF:FF:FF:FF`<br>`2`轉換成 `AA:BB:CC:DD:EE:02`<br>輸入`b2aaaaa`就會變成`b"0xffffffffffffaabbccddee02aaaaa"`
noL2           | 讀取時拔掉L2 Header的模式<br>寫入時時一律使用廣播MacAddress

<a name="LogLevel"></a>LogLevel      | Description
------------|:-----
LogLevel    | wireguard原本的log紀錄器的loglevel<br>接受參數: `debug`,`error`,`slient`
LogTransit  | 轉送封包，也就是起點/終點都不是自己的封包的log
LogNormal   | 收發普通封包，起點是自己or終點是自己的log
LogControl  | Control Message的log
LogInternal | 一些內部事件的log
LogNTP      | NTP 同步時鐘相關的log

<a name="Peers"></a>Peers      | Description
--------------------|:-----
NodeID              | 對方的節點ID
PubKey              | 對方的公鑰
PSKey               | 對方的預共享金鑰
EndPoint            | 對方的連線地址。如果漫遊，而且`Static=false`會覆寫設定檔
PersistentKeepalive | wireguard的PersistentKeepalive參數
Static              | 關閉漫遊功能，每隔`ResetConnInterval`秒，重置回初始ip

#### Run example config

在**不同terminal**分別執行以下命令

```
./etherguard-go -config example_config/super_mode/EgNet_edge1.yaml -mode edge
./etherguard-go -config example_config/super_mode/EgNet_edge2.yaml -mode edge
./etherguard-go -config example_config/super_mode/EgNet_edge3.yaml -mode edge
./etherguard-go -config example_config/super_mode/EgNet_edge4.yaml -mode edge
./etherguard-go -config example_config/super_mode/EgNet_edge5.yaml -mode edge
./etherguard-go -config example_config/super_mode/EgNet_edge6.yaml -mode edge
```

因為本範例配置是stdio的kbdbg模式，stdin會讀入VPN網路  
請在其中一個edge視窗中鍵入
```
b1message
```
因為`L2HeaderMode`是`kbdbg`，所以b1會被轉換成 12byte 的layer 2 header，b是廣播地址`FF:FF:FF:FF:FF:FF`，1是普通地址`AA:BB:CC:DD:EE:01`，message是後面的payload，然後再丟入VPN  
此時應該要能夠在另一個視窗上看見字串b1message。前12byte被轉換回來了

## 下一篇: [Super Mode的運作](../super_mode/README_zh.md)