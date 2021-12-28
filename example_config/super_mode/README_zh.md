# Etherguard
[English](README.md) | [中文](#)

## Super Mode

此模式是受到[n2n](https://github.com/ntop/n2n)的啟發，分為SuperNode和EdgeNode兩種節點  
EdgeNode首先和SuperNode建立連線，藉由SuperNode交換其他EdgeNode的資訊  
由SuperNode執行[Floyd-Warshall演算法](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)，並把計算結果分發給EdgeNode  


## Quick start

首先按需求修改`gensuper.yaml`

```yaml
Config output dir: /tmp/eg_gen
ConfigTemplate for super node: ""
ConfigTemplate for edge node: ""
Network name: eg_net
Super Node:
  Listen port: 3456
  EdgeAPI prefix: /eg_net/eg_api
  Endpoint(IPv4)(optional): example.com
  Endpoint(IPv6)(optional): example.com
  Endpoint(EdgeAPI): http://example.com:3456/eg_net/eg_api
Edge Node:
  Node IDs: "[1~10,11,19,23,29,31,55~66,88~99]"
  MacAddress prefix: ""                 # 留空隨機產生
  IPv4 range: 192.168.76.0/24           # IP的部分可以直接省略沒關係
  IPv6 range: fd95:71cb:a3df:e586::/64  # 這個欄位唯一的目的只是在啟動以後，調用ip命令，幫tap接口加個ip  
  IPv6 LL range: fe80::a3df:0/112       # 和VPN本身運作完全無關  
```
接著執行這個，就會生成所需設定檔了。
```
$ ./etherguard-go -mode gencfg -cfgmode super -config example_config/super_mode/gensuper.yaml
```

把一個super，2個edge分別搬去三台機器  
或是2台機器，super和edge可以是同一台

在Supernode執行  
```
./etherguard-go -config [設定檔位置] -mode super
```
在EdgeNode執行  
```
./etherguard-go -config [設定檔位置] -mode edge
```

## Documentation

在了解Super Mode的運作之前，建議您先閱讀[Static Mode的運作](../static_mode/README_zh.md)方法，再閱讀本篇會比較好


在EdgeNode的SuperMode下，設定檔裡面的`NextHopTable`以及`Peers`是無效的。  
這些資訊都是從SuperNode上面下載  
同時，SuperNode會幫每個連線生成pre-shared key，分發給edge使用(如果啟用`UsePSKForInterEdge`的話)。  

### SuperMsg
但是比起StaticMode，SuperMode引入了一種新的 `終點ID` 叫做 `NodeID_SuperNode`。  
所有送往SuperNode的封包都會是這種類型。  
這種封包不會在EdgeNode之間傳播，收到也會不會轉給任何人，如同`終點ID == 自己`一般

## Control Message
從SuperMode開始，我們有了StaticMode不存在的Control Message。他會控制EtherGuard一些行為  
在SuperMode下，我們不會轉發任何控制消息。 我們只會直接接收或發送給目標。  
下面列出Super Mode會出現的Control message

### Register
具體運作方式類似這張圖  
![Register運作流程](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS01.png)  
1. EdgeNode發送`Register`給SuperNode 
2. SuperNode收到以後就知道這個EdgeNode的Endpoint IP和Port number。  
3. 更新進資料庫以後發布`UpdatePeerMsg`。  
4. 其他edge node收到以後就用HTTP EdgeAPI去下載完整的peer list。並且把自己沒有的peer通通加到本地

### Ping/Pong
有了peer list以後，接下來的運作方式類似這張圖  
![EGS02](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS02.png)  
Edge node 會嘗試向其他所有peer發送`Ping`，裡面會攜帶節點自己的時間  
`Ping` 封包的TTL=0 所以不會被轉發，只會抵達可以直連的節點  
收到`Ping`，就會產生一個`Pong`，並攜帶時間差。這個時間就是單向延遲  
但是他不會把`Pong`送回給原節點，而是送給Super node

### <a name="AdditionalCost"></a>AdditionalCost
有了各個節點的延遲以後，還不會立刻計算`Floyd-Warshall`，而是要先加上`AdditionalCost`  

以這張圖片的情境為例:  
![EGS08](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS08.png)  
Path    | Latency |Cost|Win
--------|:--------|:---|:--
A->B->C | 3ms     | 3  | O
A->C    | 4ms     | 4  |

但是這個情境，3ms 4ms 只相差1ms  
為了這1ms而多繞一趟實在浪費，而且轉發本身也要時間

每個節點有了`AdditionalCost`參數，就能設定經過這個節點轉發，所需額外增加的成本

假如ABC全部設定了`AdditionalCost=10`
Path    | Latency |AdditionalCost|Cost|Win
--------|:--------|:-------------|:---|:--
A->B->C | 3ms     | 20           | 23 |
A->C    | 4ms     | 10           | 14 | O

A->C 就換選擇直連，不會為了省下1ms而繞路  
這邊`AdditionalCost=10`可以解釋為: 必須能省下10ms，才會繞這條路

這個參數也有別的用途  
針對流量比較貴的節點，可以設定`AdditionalCost=10000`  
別人就不會走他中轉了，而是盡量繞別的路，或是直連  
除非別條路線全掛，只剩這挑Cost 10000的路線

還有一個用法，全部節點都設定`AdditionalCost=10000`  
無視延遲，全節點都盡量直連，打動失敗才繞路

### UpdateNhTable   
Super node收到節點們傳來的Pong以後，就知道他們的單向延遲了。接下來的運作方式類似這張圖  
![image](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS03.png)  
Super node收到Pong以後，就會更新它裡面的`Distance matrix`，並且重新計算轉發表  
如果有變動，就發布`UpdateNhTableMsg`  
其他edge node收到以後就用HTTP EdgeAPI去下載完整的轉發表

### ServerUpdate
通知EdgeNode有事情發生
1. 關閉EdgeNode程式  
    * 版本號不匹配
    * 該edge的NodeID配置錯誤
    * 該Edge被刪除
2. 通知EdgeNode有更新
    * UpdateNhTable
    * UpdatePeer
    * UpdateSuperParams


## HTTP EdgeAPI  
為什麼要用HTTP額外下載呢?直接`UpdateXXX`夾帶資訊不好嗎?  
因為udp是不可靠協議，能攜帶的內容量也有上限。  
但是peer list包含了全部的peer資訊，長度不是固定的，可能超過  
所以這樣設計，`UpdateXXX`單純只是告訴edge node有資訊更新，請速速用HTTP下載

而且`UpdateXXX`本身不可靠，說不定根本就沒抵達edge node。  
所以`UpdateXXX`這類資訊都帶了`state hash`。用HTTP API的時候要帶上  
這樣super node收到HTTP API看到`state hash`就知道這個edge node確實有收到`UpdateXXX`了。  
不然每隔一段時間就會重新發送`UpdateXXX`給該節點

預設配置是走HTTP。但為**了你的安全著想，建議使用nginx反代理成https**  
有想過SuperNode開發成直接支援https，但是證書動態更新太麻煩就沒有做了  

## HTTP Manage API
HTTP還有5個Manage API，給前端使用，幫助管理整個網路

### super/state  
```bash
curl "http://127.0.0.1:3456/eg_net/eg_api/manage/super/state?Password=passwd_showstate"
```  
可以給前端看的，用來顯示現在各節點之間的單向延遲狀況  
之後可以用來畫力導向圖。

這個json下載下來有一個叫做`infinity`的欄位，值應該永遠是9999  
因為json沒辦法表達無限大。所以大於這個數值的就是無限大，不可達的意思  
這個數值是編譯時決定的，一般不會動。但保留變更的彈性  
所以有這個欄位，前端顯示時看到數值大於這個，就視為不可達，不用畫線了

返回值範例:
```json
{
  "PeerInfo": {
    "1": {
      "Name": "Node_01",
      "LastSeen": "2021-12-05 21:21:56.039750832 +0000 UTC m=+23.401193649"
    },
    "2": {
      "Name": "Node_02",
      "LastSeen": "2021-12-05 21:21:57.711616169 +0000 UTC m=+25.073058986"
    }
  },
  "Infinity": 99999,
  "Edges": {
    "1": {
      "2": 0.002179297
    },
    "2": {
      "1": -0.00030252
    }
  },
  "Edges_Nh": {
    "1": {
      "2": 0.012179297
    },
    "2": {
      "1": 0.00969748
    }
  },
  "NhTable": {
    "1": {
      "2": 2
    },
    "2": {
      "1": 1
    }
  },
  "Dist": {
    "1": {
      "1": 0,
      "2": 0.012179297
    },
    "2": {
      "1": 0.00969748,
      "2": 0
    }
  }
}
```

欄位意義:  
1. PeerInfo: 節點id，名稱，上次上線時間
2. Edges: 節點**直連的延遲**，99999或是缺失代表不可達(打洞失敗)
3. Edges_Nh: 加上AdditionalCost之後的結果，也就是餵給 FloydWarshall(g) 的真正參數
3. NhTable: 計算結果
4. Dist: 節點走**Etherguard之後的延遲**

### peer/add
再來是新增peer，可以不用重啟Supernode就新增Peer

範例:  
```bash
curl -X POST "http://127.0.0.1:3456/eg_net/eg_api/manage/peer/add?Password=passwd_addpeer" \
 -H "Content-Type: application/x-www-form-urlencoded" \
 -d "NodeID=100&Name=Node_100&PubKey=DG%2FLq1bFpE%2F6109emAoO3iaC%2BshgWtdRaGBhW3soiSI%3D&AdditionalCost=1000&PSKey=w5t64vFEoyNk%2FiKJP3oeSi9eiGEiPteZmf2o0oI2q2U%3D&SkipLocalIP=false"
```
參數:
1. URL query: Password: 新增peer用的密碼，在設定檔配置
1. Post body:
    1. NodeID: Node ID
    1. Name: 節點名稱
    1. PubKey: Public Key
    1. PSKey: Pre shared Key
    1. AdditionalCost: 此節點進行封包轉發的額外成本。單位: 毫秒
    1. SkipLocalIP: 是否使該節點不使用Local IP
    1. nexthoptable: 如果你的super node的`graphrecalculatesetting`是static mode，那麼你需要在這提供一張新的`NextHopTable`，json格式

返回值:
1. http code != 200: 出錯原因  
2. http code == 200，一份edge的參考設定檔  
    * 會根據 `edgetemplate` 裡面的內容，再填入使用者的資訊(nodeid/name/pubkey)
    * 方便使用者複製貼上

### peer/del  
有兩種刪除模式，分別是使用Password刪除，以及使用privkey刪除。  
設計上分別是給管理員使用，或是給加入網路的人，想離開網路使用

使用Password刪除可以刪除任意節點，以上面新增的節點為例，使用這個API即可刪除剛剛新增的節點
```bash
curl "http://127.0.0.1:3456/eg_net/eg_api/manage/peer/del?Password=passwd_delpeer&NodeID=100"
```

也可以使用privkey刪除，同上，但是只要附上privkey參數就好
```bash
curl "http://127.0.0.1:3456/eg_net/eg_api/manage/peer/del?PrivKey=iquaLyD%2BYLzW3zvI0JGSed9GfDqHYMh%2FvUaU0PYVAbQ%3D"
```

參數:
1. URL query: 
    1. Password: 刪除peer用的密碼，在設定檔配置
    1. nodeid: 你想刪除的Node ID
    1. privkey: 該節點的私鑰

返回值:
1. http code != 200: 錯誤訊息
2. http code == 200: 被刪除的nodeID  

### peer/update
更新節點的一些參數
```bash
curl -X POST "http://127.0.0.1:3456/eg_net/eg_api/manage/peer/update?Password=passwd_updatepeer&NodeID=1" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "AdditionalCost=10&SkipLocalIP=false"
```

### super/update
更新SuperNode的一些參數
```bash
curl -X POST "http://127.0.0.1:3456/eg_net/eg_api/manage/super/update?Password=passwd_updatesuper" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "SendPingInterval=15&HttpPostInterval=60&PeerAliveTimeout=70&DampingFilterRadius=3"
```

### SuperNode Config Parameter

Key                 | Description
--------------------|:-----
NodeName            | 節點名稱
PostScript          | 初始化完畢之後要跑的腳本
PrivKeyV4           | IPv4通訊使用的私鑰
PrivKeyV6           | IPv6通訊使用的私鑰
ListenPort          | udp監聽埠
ListenPort_EdgeAPI  | HTTP EdgeAPI 的監聽埠
ListenPort_ManageAPI| HTTP ManageAPI 的監聽埠
API_Prefix          | HTTP API prefix
RePushConfigInterval| 重新push`UpdateXXX`的間格
HttpPostInterval    | EdgeNode 使用EdgeAPI回報狀態的頻率
PeerAliveTimeout    | 判定斷線Timeout
SendPingInterval    | EdgeNode 之間使用Ping/Pong測量延遲的間格
[LogLevel](../static_mode/README_zh.md#LogLevel)| 紀錄log
[Passwords](#Passwords) | HTTP ManageAPI 的密碼，5個API密碼是獨立的
[GraphRecalculateSetting](#GraphRecalculateSetting) | 一些和[Floyd-Warshall演算法](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)相關的參數
[NextHopTable](../static_mode/README_zh.md#NextHopTable) | StaticMode 模式下使用的轉發表
EdgeTemplate        | HTTP ManageAPI `peer/add` 返回的edge的參考設定檔
UsePSKForInterEdge  | 幫Edge生成PreSharedKey，供edge之間直接連線使用
[Peers](#EdgeNodes)     | EdgeNode資訊

<a name="Passwords"></a>Passwords      | Description
--------------------|:-----
ShowState   | HTTP ManageAPI `super/state` 的密碼
AddPeer     | HTTP ManageAPI `peer/add` 的密碼
DelPeer     | HTTP ManageAPI `peer/del` 的密碼
UpdatePeer  | HTTP ManageAPI `peer/update` 的密碼
UpdateSuper | HTTP ManageAPI `super/update` 的密碼

<a name="GraphRecalculateSetting"></a>GraphRecalculateSetting      | Description
--------------------|:-----
StaticMode                 | 關閉`Floyd-Warshall`演算法，只使用設定檔提供的NextHopTable`。SuperNode單純用來輔助打洞
ManualLatency              | 手動設定延遲，不採用EdgeNode回報的延遲(單位: 毫秒)<br> 特殊值65535匹配任何目標
JitterTolerance            | 抖動容許誤差，收到Pong以後，一個37ms，一個39ms，不會觸發重新計算<br>比較對象是上次更新使用的值。如果37 37 41 43 .. 100 ，每次變動一點點，總變動量超過域值還是會更新
JitterToleranceMultiplier  | 抖動容許誤差的放大係數，高ping的話允許更多誤差<br>https://www.desmos.com/calculator/raoti16r5n
DampingFilterRadius        | 防抖用低通濾波器的window半徑
TimeoutCheckInterval       | 週期性檢查節點的連線狀況，是否斷線需要重新規劃線路
RecalculateCoolDown        | Floyd-Warshal是O(n^3)時間複雜度，不能太常算。<br>設個冷卻時間<br>有節點加入/斷線觸發的重新計算，無視這個CoolDown

<a name="EdgeNodes"></a>Peers      | Description
--------------------|:-----
NodeID              | 節點ID
PubKey              | 公鑰
PSKey               | 預共享金鑰
[AdditionalCost](#AdditionalCost)      | 繞路成本(單位: 毫秒)<br>設定-1代表使用EdgeNode自身設定
SkipLocalIP         | 打洞時，不使用EdgeNode回報的本地IP，僅使用SuperNode蒐集到的外部IP
EndPoint            | SuperNode啟動時，主動向Edge連線的Endpoint
ExternalIP          | 針對沒開Nat Reflection，又要把SuperNode和EdgeNode跑在同一内網的情境使用<br>沒有Nat Reflection，SuperNode無法讀取內網EdgeNode的外部IP，只能手動指定了

### EdgeNode Config Parameter

#### [EdgeConfig Root](../static_mode/README_zh.md#EdgeConfig)

<a name="DynamicRoute"></a>DynamicRoute      | Description
--------------------|:-----
SendPingInterval     | 發送Ping訊息的間隔(秒)
PeerAliveTimeout     | 被標記為離線所需的無反應時間(秒)
TimeoutCheckInterval | 檢查間格(秒)，檢查是否有任何peer超時，若有就標記
ConnNextTry          | 被標記以後，嘗試下一個endpoint的間隔(秒)
DupCheckTimeout      | 重複封包檢查的timeout(秒)<br>完全相同的封包收第二次會被丟棄
[AdditionalCost](#AdditionalCost)     | 繞路成本(毫秒)。僅限SuperNode設定-1時生效
SaveNewPeers         | 是否把下載來的鄰居資訊存到本地設定檔裡面
[SuperNode](#SuperNode)          | SuperNode相關設定
[P2P](../p2p_mode/README_zh.md#P2P)                  | P2P相關設定，SuperMode用不到
[NTPConfig](#NTPConfig)          | NTP時間同步相關設定

<a name="SuperNode"></a>SuperNode      | Description
---------------------|:-----
UseSuperNode         | 是否啟用SuperNode
PSKey                | 和SuperNode通訊用的PreShared Key
EndpointV4           | SuperNode的IPv4 Endpoint
PubKeyV4             | SuperNode的IPv4公鑰
EndpointV6           | SuperNode的IPv6 Endpoint
PubKeyV6             | SuperNode的IPv6公鑰
EndpointEdgeAPIUrl   | SuperNode的EdgeAPI存取路徑
SkipLocalIP          | 不回報本地IP，避免和其他Edge內網直連
SuperNodeInfoTimeout | 實驗性選項，SuperNode離線超時，切換成P2P模式<br>需先打開P2P模式<br>`UseP2P=false`本選項無效<br>P2P模式尚未測試，穩定性未知，不推薦使用


<a name="NTPConfig"></a>NTPConfig      | Description
--------------------|:-----
UseNTP            | 是否使用NTP同步時間
MaxServerUse      | 向多少NTP伺服器發送請求
SyncTimeInterval  | 多久同步一次時間
NTPTimeout        | NTP伺服器連線Timeout
Servers           | NTP伺服器列表
   
## V4 V6 兩個公鑰
為什麼要分開IPv4和IPv6呢?  
因為有這種情況:

![OneChannel](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS04.png)

這樣的話SuperNode就不知道Node02的ipv4地址，就不能幫助Node1和Node2打洞了

![TwoChannel](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS05.png)

所以要像這樣，V4和V6都建立一條通道，才能讓V4和V6同時都被處理到

## 打洞可行性
對於不同的NAT type，打洞的可行性可以參考這張圖([出處](https://dh2i.com/kbs/kbs-2961448-understanding-different-nat-types-and-hole-punching/))

![reachability between NAT types](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS06.png)  

還有，就算雙方都是ConeNAT，也不保證100%成功。  
還得看NAT設備的支援情況，詳見[此文](https://bford.info/pub/net/p2pnat/#SECTION00035000000000000000)，裡面3.5章節描述的情況，也無法打洞成功

## Relay node
因為Etherguard的Supernode單純只負責幫忙打洞+計算[Floyd-Warshall](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)，並分發運算結果  
而他本身並不參與資料轉發。因此如上章節描述打洞失敗，且沒有任何可達路徑的話，就需要搭建relay node  
基本上任意一個節點有公網ip，就不用擔心沒有路徑可達了。但是還是說明一下

Relay node其實也是一個edge node，只不過被設定成為interface=dummy，不串接任何真實接口  
![EGS07](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS07.png)  
只是在設定時要注意，Supernode地只要設定成Supernode的**外網ip**。  
因為如果用127.0.0.1連接supernode，supernode看到封包的src IP就是127.0.0.1，就會把127.0.0.1分發給`Node_1`和`Node_2`  
`Node_1`和`Node_2`看到`Node_R`的連線地址是`127.0.0.1`，就連不上了

#### Run example config

在**不同terminal**分別執行以下命令

```bash
./etherguard-go -config example_config/super_mode/Node_super.yaml -mode super
./etherguard-go -config example_config/super_mode/Node_edge001.yaml -mode edge
./etherguard-go -config example_config/super_mode/Node_edge002.yaml -mode edge
```
因為是stdio模式，stdin會讀入VPN網路  
請在其中一個edge視窗中鍵入
```
b1aaaaaaaaaa
```
b1會被轉換成 12byte 的layer 2 header，b是廣播地址`FF:FF:FF:FF:FF:FF`，1是普通地址`AA:BB:CC:DD:EE:01`，aaaaaaaaaa是後面的payload，然後再丟入VPN  
此時應該要能夠在另一個視窗上看見字串b1aaaaaaaaaa。前12byte被轉換回來了

看完本章捷，接下來你就能了解一下[P2P Mode的運作](../p2p_mode/README_zh.md)
