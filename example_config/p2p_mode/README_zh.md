# Etherguard
[English](README.md) | [中文](#)

## P2P Mode
此模式是受到[tinc](https://github.com/gsliepen/tinc)的啟發，只有EdgeNode，EdgeNode會彼交換資訊  
EdgeNodes會嘗試互相連線，並且通報其他EdgeNoses連線成功與否  
每個Edge各自執行[Floyd-Warshall演算法](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)，若不能直達則使用最短路徑  
**此模式尚未經過長時間測試，尚不建議生產環境使用**

## Quick Start
首先，按照需求修改`gensp2p.yaml`

```yaml
Config output dir: /tmp/eg_gen_p2p      # 設定檔輸出位置
ConfigTemplate for edge node: ""        # 設定檔Template
Network name: "EgNet"
Edge Node:
  MacAddress prefix: ""                 # 留空隨機產生
  IPv4 range: 192.168.76.0/24           # 順帶一提，IP的部分可以直接省略沒關係  
  IPv6 range: fd95:71cb:a3df:e586::/64  # 這個欄位唯一的目的只是在啟動以後，調用ip命令，幫tap接口加個ip  
  IPv6 LL range: fe80::a3df:0/112       # 和VPN本身運作完全無關  
Edge Nodes:                             # 所有的節點相關設定
  1:
    Endpoint(optional): ""
  2:
    Endpoint(optional): ""
  3:
    Endpoint(optional): 127.0.0.1:3003
  4:
    Endpoint(optional): 127.0.0.1:3004
  5:
    Endpoint(optional): ""
  6:
    Endpoint(optional): ""
```
接著執行這個，就會生成所需設定檔了。
```
./etherguard-go -mode gencfg -cfgmode p2p -config example_config/p2p_mode/gensp2p.yaml
```

把這些設定檔不捨去對應節點，然後再執行  
```
./etherguard-go -config [設定檔位置] -mode edge
```
就可以了

確認運作以後，可以關閉不必要的log增加性能

## Documentation

P2P Mode的[範例配置檔](./)的說明文件
在了解Super Mode的運作之前，建議您先閱讀[Super Mode的運作](../super_mode/README_zh.md)方法，再閱讀本篇會比較好

### ControlMsg

P2P模式又引入一種新的 `終點ID` 叫做 `ControlMsg`  
和 Static 模式下的Boardcast非常相似。只不過 `Boardcast` 會盡量避免重複發送  
`ControlMsg` 才不管，只要收到一律轉發給剩餘的全部節點  
你可以當成廣播有2種，一種是**普通廣播**，會查看轉發表，不會重複發送  
另一種是**flood廣播**，不查看轉發表，盡量發給全部的節點

所以P2P模式的 `ControlMsg` 會額外引入一個**Dup檢查**。  
所有進來的 `ControlMsg` 都會算一遍CRC32，並儲存在一個有timeout的dictionary裡面  
只要有一模一樣CRC32就會被丟棄。計算時只考慮封包內容，不考慮src/dst/TTL之類的標頭  
所以一樣的內容收2遍，第二個一定會被丟棄

### Ping
首先和Super模式一樣，會定期向所有節點廣播`Ping`，TTL=0 所以不會被轉發  
只會抵達可以直連的節點  
但是收到Ping以後產生的`Pong`不會回給Super，而是傳給其他所有的節點

### Pong
Pong封包是一種`ControlMsg`，使用**flood廣播**盡量讓每個節點都收到  
因為是P2P模式，每人都維護自己的 Distance Matrix  
收到Pong封包的時候，就更新自己的 Distance Matrix  
更新完以後，就跑[Floyd-Warshall演算法](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)更新自己的轉發表

### QueryPeer
是一種`ControlMsg`，使用**flood廣播**盡量讓每個節點都收到  
剛加入網路的節點會廣播這個封包，要求其他節點提供他們的peer訊息  
如果收到了`QureyPeer` 封包，就會開始發送 `BoardcastPeer` 封包  
每個BoardcastPeer只能攜帶一個peer的訊息，所以自己有幾個peer就會發送幾遍

### BoardcastPeer
是一種`ControlMsg`，使用**flood廣播**來發送，盡量讓每個節點都收到  
裡面包含了 NodeID，PubKey，PSKey，Endpoint，queryID，這五種資料  
每個節點都**定期**把自己全部的peer廣播一遍，其中queryID填入0  
但是共同擁有的節點，因為內容都長一樣(NodeID/PubKey等等)，會觸發`ControlMsg`的**Dup檢查**  
所以流量不會爆炸

還有一種情況  
節點只要收到`QueryPeer`，也會把自己全部的peer廣播發送一遍，而且queryID填入請求者的NodeID  
因為NodeID不是0了，就不會和前面的定期廣播長一樣，就不會觸發Dup檢查  
保證新加入的節點能立刻拿到其他所有節點的資訊

收到`BoardcastPeer`時，會先檢查自己有沒有這個Peer，若沒有就新增Peer  
如果已經有了，再檢查Peer是不是離線。  
如果已經離線，就用收到的Endpoint覆蓋掉自己原本的Endpoint

### EdgeNode Config Parameter

<a name="P2P"></a>P2P      | Description
------------------------|:-----
UseP2P                  | 是否啟用P2P模式
SendPeerInterval        | 廣播BoardcastPeer的間格
[GraphRecalculateSetting](../super_mode/README_zh.md#GraphRecalculateSetting) | 一些和[Floyd-Warshall演算法](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)相關的參數

#### Run example config

在**不同terminal**分別執行以下命令

```
./etherguard-go -config example_config/p2p_mode/EgNet_edge1.yaml -mode edge
./etherguard-go -config example_config/p2p_mode/EgNet_edge2.yaml -mode edge
./etherguard-go -config example_config/p2p_mode/EgNet_edge3.yaml -mode edge
./etherguard-go -config example_config/p2p_mode/EgNet_edge4.yaml -mode edge
./etherguard-go -config example_config/p2p_mode/EgNet_edge5.yaml -mode edge
./etherguard-go -config example_config/p2p_mode/EgNet_edge6.yaml -mode edge
```

因為本範例配置是stdio的kbdbg模式，stdin會讀入VPN網路  
請在其中一個edge視窗中鍵入
```
b1message
```
因為`L2HeaderMode`是`kbdbg`，所以b1會被轉換成 12byte 的layer 2 header，b是廣播地址`FF:FF:FF:FF:FF:FF`，1是普通地址`AA:BB:CC:DD:EE:01`，message是後面的payload，然後再丟入VPN  
此時應該要能夠在另一個視窗上看見字串b1message。前12byte被轉換回來了

## Note
P2P模式下，PSK是禁用的。因為n個節點有n(n-1)/2的連線，每個連線都要使用不同PSK  
又不像static mode提前設好，peer數固定不再變動  
也不像super mode，有中心伺服器統一分發
每對peer要協商出一個PSK有難度，因此我設定禁用PSK了，只用wireguard原本的加密系統

**最後，P2P模式我還沒有大規模測試過，穩定性不知如何。PR is welcome**