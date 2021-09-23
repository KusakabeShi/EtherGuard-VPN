# Etherguard
[English](README.md)

Super Mode的[範例配置檔](./)的說明文件
在了解Super Mode的運作之前，建議您先閱讀[Static Mode的運作](../static_mode/README_zh.md)方法，再閱讀本篇會比較好

## Super Mode

Super Mode是受到[n2n](https://github.com/ntop/n2n)的啟發  
分為super node和edge node兩種節點  

全部節點都會和supernode建立連線  
藉由supernode交換其他節點的資訊，以及udp打洞  
由supernode執行[Floyd-Warshall演算法](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)，並把計算結果分發給全部edge node

在super mode模式下，設定檔裡面的`nexthoptable`以及`peers`是無效的。  
這些資訊都是從super node上面下載  
同時，supernode會幫每個連線生成Preshared Key，分發給edge使用。  
```golang
psk = shs256("PubkeyPeerA" + "PubkeyPeerB" + "主廚特調當季精選海鹽")[:32]
```

### SuperMsg

但是比起Static mode，Super mode引入了一種新的 `終點ID` 叫做 `SuperMsg`。  
所有送往Super node的封包都會是這種類型。  
這種封包不會在edge node之間傳播，收到也會不會轉給任何人，如同`終點ID == 自己`一般

### Register

具體運作方式類似這張圖  
![EGS01](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS01.png)  
首先edge node發送regiater給super node  
super node收到以後就知道這個edge的endpoint IP和埠號。  
更新進資料庫以後發布`UpdatePeerMsg`。  
其他edge node收到以後就用HTTP API去下載完整的peer list。並且把自己沒有的peer通通加到本地

### Ping/Pong
有了peer list以後，接下來的運作方式類似這張圖  
![EGS02](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS02.png)  
Edge node 會嘗試向其他所有peer發送`Ping`，裡面會攜帶節點自己的時間  
`Ping` 封包的TTL=0 所以不會被轉發，只會抵達可以直連的節點  
收到`Ping`，就會產生一個`Pong`，並攜帶時間差。這個時間就是單向延遲  
但是他不會把`Pong`送回給原節點，而是送給Super node

### 轉發表  
Super node收到節點們傳來的Pong以後，就知道他們的單向延遲了。接下來的運作方式類似這張圖  
![image](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS03.png)  
Super node收到Pong以後，就會更新它裡面的`Distance matrix`，並且重新計算轉發表  
如果有變動，就發布`UpdateNhTableMsg`  
其他edge node收到以後就用HTTP API去下載完整的轉發表

### HTTP API  
為什麼要用HTTP額外下載呢?直接`UpdateXXX`夾帶資訊不好嗎?  
因為udp是不可靠協議，能攜帶的內容量也有上限。  
但是peer list包含了全部的peer資訊，長度不是固定的，可能超過  
所以這樣設計，`UpdateXXX`單純只是告訴edge node有資訊更新，請速速用HTTP下載

而且`UpdateXXX`本身不可靠，說不定根本就沒抵達edge node。  
所以`UpdateXXX`這類資訊都帶了`state hash`。用HTTP API的時候要帶上  
這樣super node收到HTTP API看到`state hash`就知道這個edge node確實有收到`UpdateXXX`了。  
不然每隔一段時間就會重新發送`UpdateXXX`給該節點

### peerstate  
HTTP還有一個API  
```
http://127.0.0.1:3000/api/peerstate?Password=passwd
```  
可以給前端看的，用來顯示現在各節點之間的單向延遲狀況  
之後可以用來畫力導向圖。

這個json下載下來有一個叫做`infinity`的欄位，值應該永遠是99999  
因為json沒辦法表達無限大。所以大於這個數值的就是無限大，不可達的意思  
這個數值是編譯時決定的，一般不會動。但說不定你想改code，改成999呢?  
所以有這個欄位，前端顯示時看到數值大於這個，就視為不可達，不用畫線了

### peeradd
新增peer

範例:  
```
curl -X POST "http://127.0.0.1:3000/api/peer/add?Password=passwd_addpeer" \
  -H "Content-Type: application/x-www-form-urlencoded" \
  -d "nodeid=1&name=Node_01&pubkey=ZqzLVSbXzjppERslwbf2QziWruW3V/UIx9oqwU8Fn3I=&pskey=iPM8FXfnHVzwjguZHRW9bLNY%2bh7%2bB1O2oTJtktptQkI="
```
參數:
1. URL query: Password: 新增peer用的密碼
1. Post body:
    1. nodeid: Node ID
    1. pubkey: Public Key
    1. pskey: Preshared Key

### peerdel  
刪除peer

範例:   
```
curl "http://127.0.0.1:3000/api/peer/del?Password=passwd_delpeer&nodeid=1"
```

## Config Paramaters

### Super mode的edge node有幾個參數
1. `usesupernode`: 是否啟用Super mode
1. `pskey`: 和supernode建立連線用的Preshared Key
1. `connurlv4`: Super node的IPv4連線地址
1. `pubkeyv4`: Super node的IPv4工鑰
1. `connurlv6`: Super node的IPv6連線地址
1. `pubkeyv6`: Super node的IPv6工鑰
1. `apiurl`: Super node的HTTP(S) API連線地址
1. `supernodeinfotimeout`: Supernode Timeout

### Super node本身的設定檔

1. nodename: 節點名稱
1. privkeyv4: ipv4用的私鑰
1. privkeyv6: ipv6用的私鑰
1. listenport: 監聽udp埠號
1. statepassword: Guest API 的密碼
1. loglevel: 參考 [README_zh.md](../README_zh.md)
1. repushconfiginterval: 重新push`UpdateXXX`的間格
1. passwords: HTTP API 密碼
    1. showstate: 節點資訊
    1. addpeer: 新增peer
    1. delpeer: 刪除peer
1. graphrecalculatesetting:
    1.   jittertolerance: 抖動容許誤差，收到Pong以後，一個37ms，一個39ms，不會觸發重新計算
    1.   jittertolerancemultiplier: 一樣是抖動容許誤差，但是高ping的話允許更多誤差  
                                    https://www.desmos.com/calculator/raoti16r5n
    1.   nodereporttimeout: 收到的`Pong`封包的有效期限。太久沒收到就變回Infinity
    1.   recalculatecooldown: Floyd-Warshal是O(n^3)時間複雜度，不能太頻繁計算。設個冷卻時間
1. edgetemplate: 給`addpeer`API用的。參考這個設定檔，顯示一個範例設定檔給edge
1. peers: Peer列表，參考 [README_zh.md](../README_zh.md)
    1.   nodeid: Peer的節點ID
    1.   name: Peer名稱(顯示在前端)
    1.   pubkey: peer 公鑰
    1.   pskey: preshared key 該peer和本Supernode連線的PSK

##
執行此範例設定檔(請開三個terminal):
```bash
./etherguard-go -config example_config/super_mode/s1.yaml -mode super
./etherguard-go -config example_config/super_mode/n1.yaml -mode edge
./etherguard-go -config example_config/super_mode/n2.yaml -mode edge
```
因為是stdio模式，stdin會讀入VPN網路  
請在其中一個edge視窗中鍵入
```
b1aaaaaaaaaa
```
b1會被轉換成 12byte 的layer 2 header，b是廣播地址`FF:FF:FF:FF:FF:FF`，1是普通地址`AA:BB:CC:DD:EE:01`，aaaaaaaaaa是後面的payload，然後再丟入VPN  
此時應該要能夠在另一個視窗上看見字串b1aaaaaaaaaa。前12byte被轉換回來了

## V4 V6 兩個公鑰
為什麼要分開IPv4和IPv6呢?  
因為有這種情況:

![OneChannel](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS04.png)

這樣的話SuperNode就不知道Node02的ipv4地址，就不能幫助Node1和Node2打洞了

![TwoChannel](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS05.png)

所以要像這樣，V4和V6都建立一條通道，才能讓V4和V6同時都被處理到

## 打洞可行性
對於不同的NAT type，打洞的可行性可以參考這張圖([出處](https://dh2i.com/kbs/kbs-2961448-understanding-different-nat-types-and-hole-punching/))

![EGS06](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS06.png)  

還有，就算雙方都是ConeNAT，也不保證100%成功。  
還得看NAT設備的支援情況，詳見[此文](https://bford.info/pub/net/p2pnat/)，裡面3.5章節描述的情況，也無法打洞成功

## Relay node
因為Etherguard的Supernode單純只負責幫忙打洞+計算[Floyd-Warshall](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)，並分發運算結果  
而他本身並不參與資料轉發。因此如上章節描述打洞失敗，且沒有任何可達路徑的話，就需要搭建relay node  
基本上任意一個節點有公網ip，就不用擔心沒有路徑可達了。但是還是說明一下

Relay node其實也是一個edge node，只不過被設定成為interface=dummy，不串接任何真實接口  
![EGS07](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/super_mode/EGS07.png)  
只是在設定時要注意，Supernode地只要設定成Supernode的**外網ip**。  
因為如果用127.0.0.1連接supernode，supernode看到封包的src IP就是127.0.0.1，就會把127.0.0.1分發給`Node_1`和`Node_2`  
`Node_1`和`Node_2`看到`Node_R`的連線地址是`127.0.0.1`，就連不上了



看完本章捷，接下來你就能了解一下[P2P Mode的運作](../p2p_mode/README_zh.md)
