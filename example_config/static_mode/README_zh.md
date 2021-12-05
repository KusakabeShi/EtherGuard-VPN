# Etherguard
[English](README.md)

Static Mode的[範例配置檔](./)的說明文件

## Static Mode

沒有自動選路，沒有握手伺服器  

十分類似原本的wireguard，一切都要提前配置好

但是除了peer以外，還要額外配置轉發表，所有人共用一份轉發表

設定檔裡面的`nexthoptable`部分，只有此模式會生效

這個模式下，不存在任何的Control Message，斷線偵測甚麼的也不會有  
請務必保持提前定義好的拓樸。不然如果存在中轉，中轉節點斷了，部分連線就會中斷

這份[範例配置檔](./)的網路拓樸如圖所示

!["Topology"](https://raw.githubusercontent.com/KusakabeSi/EtherGuard-VPN/master/example_config/static_mode/Example_static.png)

發出封包時，會設定起始ID=自己的Node ID，終點ID則是看Dst Mac Address。  
如果Dst MacAddr是廣播地址，或是不在自己的對應表裡面，就會設定終點=Boardcast

收到封包的時候，如果`dst==自己ID`，就會收下，不轉給任何人。  
同時還會看它的 Src Mac Address 和 Src NodeID ，並加入對應表  
這樣下次傳給他就可以直接傳給目標，而不用廣播給全節點了

所以設定檔中的轉發表如下表。格式是yaml的巢狀dictionary  
轉發/發送封包時，直接查詢 `NhTable[起點][終點]=下一跳`  
就知道下面一個封包要轉給誰了

```yaml
nexthoptable:
  1:
    2: 2
    3: 2
    4: 2
    5: 2
    6: 2
  2:
    1: 1
    3: 3
    4: 4
    5: 3
    6: 4
  3:
    1: 2
    2: 2
    4: 4
    5: 5
    6: 4
  4:
    1: 2
    2: 2
    3: 3
    5: 3
    6: 6
  5:
    1: 3
    2: 3
    3: 3
    4: 3
    6: 3
  6:
    1: 4
    2: 4
    3: 4
    4: 4
    5: 4
```

### Boardcast 
比較特別的是`終點ID=Boardcast`的情況。

假設今天的狀況:我是4號，我收到`起點ID = 1，終點ID=boardcast`的封包  
我應該只轉給6號就好，而不會轉給3號。  
因為3號會收到來自2號的封包，自己就不用重複遞送了

因此我有設計，如果`終點ID = Boardcast`，就會檢查Src到自己的所有鄰居，會不會經過自己  
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
1 0   0.5 Inf Inf Inf Inf
2 0.5 0   0.5 0.5 Inf Inf
3 Inf 0.5 0   0.5 0.5 Inf
4 Inf 0.5 0.5 0   Inf 0.5
5 Inf Inf 0.5 Inf 0   Inf
6 Inf Inf Inf 0.5 Inf 0
```

之後用這個指令就能輸出用Floyd Warshall算好的轉發表了，填入設定檔即可
```
./etherguard-go -config example_config/static_mode/path.txt -mode slove

NextHopTable:
  1:
    2: 2
    3: 2
    4: 2
    5: 2
    6: 2
  2:
    1: 1
    3: 3
    4: 4
    5: 3
    6: 4
  3:
    1: 2
    2: 2
    4: 4
    5: 5
    6: 4
  4:
    1: 2
    2: 2
    3: 3
    5: 3
    6: 6
  5:
    1: 3
    2: 3
    3: 3
    4: 3
    6: 3
  6:
    1: 4
    2: 4
    3: 4
    4: 4
    5: 4
```

程式還會額外輸出一些資訊，像是路徑表。  
會標示所有的起點終點組合的封包路徑，還有行經距離
```
Human readable:
src     dist            path
1 -> 2  0.500000        [1 2]
1 -> 3  1.000000        [1 2 3]
1 -> 4  1.000000        [1 2 4]
1 -> 5  1.500000        [1 2 3 5]
1 -> 6  1.500000        [1 2 4 6]
2 -> 1  0.500000        [2 1]
2 -> 3  0.500000        [2 3]
2 -> 4  0.500000        [2 4]
2 -> 5  1.000000        [2 3 5]
2 -> 6  1.000000        [2 4 6]
3 -> 1  1.000000        [3 2 1]
3 -> 2  0.500000        [3 2]
3 -> 4  0.500000        [3 4]
3 -> 5  0.500000        [3 5]
3 -> 6  1.000000        [3 4 6]
4 -> 1  1.000000        [4 2 1]
4 -> 2  0.500000        [4 2]
4 -> 3  0.500000        [4 3]
4 -> 5  1.000000        [4 3 5]
4 -> 6  0.500000        [4 6]
5 -> 1  1.500000        [5 3 2 1]
5 -> 2  1.000000        [5 3 2]
5 -> 3  0.500000        [5 3]
5 -> 4  1.000000        [5 3 4]
5 -> 6  1.500000        [5 3 4 6]
6 -> 1  1.500000        [6 4 2 1]
6 -> 2  1.000000        [6 4 2]
6 -> 3  1.000000        [6 4 3]
6 -> 4  0.500000        [6 4]
6 -> 5  1.500000        [6 4 3 5]
```

有些設定檔對應某些運作模式，這邊針對共同部分的設定做說明

### Edge config

1. `interface`
    1. `itype`: 裝置類型，意味著從VPN網路收到的封包要丟去哪個硬體
         1. `dummy`: 收到的封包直接丟棄，也不發出任何封包。作為中繼節點使用
         2. `stdio`: 收到的封包丟stdout，stdin進來的資料丟入vpn網路  
            需要參數: `macaddrprefix`,`l2headermode`
         3. `udpsock`: 把VPN網路收到的layer2封包讀寫去一個udp socket.  
            Paramaters: `recvaddr`,`sendaddr`
         3. `tcpsock`: 把VPN網路收到的layer2封包讀寫去一個tcp socket.  
            Paramaters: `recvaddr`,`sendaddr`
         3. `unixsock`: 把VPN網路收到的layer2封包讀寫去一個unix socket(SOCK_STREAM 模式).  
            Paramaters: `recvaddr`,`sendaddr`
         3. `unixgramsock`: 把VPN網路收到的layer2封包讀寫去一個unix socket(SOCK_DGRAM 模式).  
            Paramaters: `recvaddr`,`sendaddr`
         3. `unixpacketsock`: 把VPN網路收到的layer2封包讀寫去一個unix socket(SOCK_SEQPACKET 模式).  
            Paramaters: `recvaddr`,`sendaddr`
         3. `fd`: 把VPN網路收到的layer2封包讀寫去一個特定的file descriptor.  
            Paramaters: 無. 但是使用環境變數 `EG_FD_RX` 和 `EG_FD_TX` 來指定
         4. `vpp`: 使用libmemif使vpp加入VPN網路  
            需要參數: `name`,`vppifaceid`,`vppbridgeid`,`macaddrprefix`,`mtu`
         5. `tap`: Linux的tap設備。讓linux加入VPN網路  
            需要參數: `name`,`macaddrprefix`,`mtu`
    2. `name` : 裝置名稱
    3. `vppifaceid`: VPP 的 interface ID。一個VPP runtime內不能重複
    4. `vppbridgeid`: VPP 的網橋ID。不使用VPP網橋功能的話填0
    5. `macaddrprefix`: MAC地址前綴。真正的 MAC 地址=[前綴]:[NodeID]。  
                        如果這邊填了完整6格長度，就忽略`NodeID`
    6. `recvaddr`: 僅限`XXXsock`生效。listen地址，收到的東西丟去 VPN 網路
    7. `sendaddr`: 僅限`XXXsock`生效。連線地址，VPN網路收到的東西丟去這個地址
    8. `l2headermode`: 僅限 `stdio` 生效。debug用途，有三種模式:
        1. `nochg`: 從 VPN 網路收到什麼，就往tap裝置發送什麼。不對封包作任何更動
        2. `kbdbg`: 鍵盤bebug模式。搭配 `stdio` 模式，讓我 debug 用  
            因為前 12 byte 會用來做選路判斷，但是只是要debug，構造完整的封包就不是很方便  
            這個模式下，如果輸入b2content，就會幫你把b轉換成`FF:FF:FF:FF:FF:FF`， `2` 轉換成 `AA:BB:CC:DD:EE:02` 。封包內容變成 `b"0xffffffffffffaabbccddee02content"`。
            用鍵盤就能輕鬆產生L2 header，查看選路的行為
        3. `noL2`: 拔掉L2 Header的模式。  
           但是本VPN會查詢L2用作選路，所以會變成一律廣播
2. `nodeid`: 節點ID。節點之間辨識身分用的，同一網路內節點ID不能重複
3. `postscript`: etherguard初始化完畢之後要跑的腳本.
3. `nodename`: 節點名稱
4. `defaultttl`: 預設ttl(etherguard層使用，和乙太層不共通)
5. `l2fibtimeout`: MacAddr-> NodeID 查找表的 timeout(秒)
5. `privkey`: 私鑰，和wireguard規格一樣
5. `listenport`: 監聽的udp埠
6. `loglevel`: 紀錄log
    1. `loglevel`: wireguard原本的log紀錄器的loglevel。  
       有`debug`,`error`,`slient`三種程度
    2. `logtransit`: 轉送封包，也就是起點/終點都不是自己的封包的log
    3. `logcontrol`: Control Message的log
    4. `lognormal`: 收發普通封包，起點是自己or終點是自己的log
    5. `logntp`: NTP 同步時鐘相關的log
7. `dynamicroute`: 動態路由相關的設定。時間類設定單位都是秒
    1. `sendpinginterval`: 發送Ping訊息的間隔
    2. `dupchecktimeout`: 重複封包檢查的timeout。完全相同的封包收第二次會被丟棄
    1. `peeralivetimeout`: 每次收到封包就重置，超過時間沒收到就標記該peer離線
    3. `conntimeout`: 檢查peer離線的間格，如果標記離線，就切換下一個endpoint(supernode可能傳了多個endpoint過來)
    4. `savenewpeers`: 是否把下載來的鄰居資訊存到本地設定檔裡面
    5. `supernode`: 參見[Super模式](example_config/super_mode/README_zh.md)
    6. `p2p` 參見 [P2P模式](example_config/p2p_mode/README_zh.md)
    7. `ntpconfig`: NTP 相關的設定
        1. `usentp`: 是否使用ntp同步時鐘
        2. `maxserveruse`: 一次對多連線幾個NTP伺服器  
           第一次會全部連一遍測延遲，之後每次都取延遲前n低的來用
        3. `synctimeinterval`: 多久同步一次
        4. `ntptimeout`: 多久算是超時
        5. `servers`: NTP伺服器列表
8. `nexthoptable`: 轉發表。只有Static模式會用到，參見 [Static模式](example_config/super_mode/README_zh.md)
9. `resetconninterval`: 如果對方是動態ip就要用這個。每隔一段時間就會重新解析domain。
10. `peers`: 和wireguard一樣的peer資訊
    1. `nodeid`: 對方的節點ID
    2. `pubkey`: 對方的公鑰
    3. `pskey`: 對方的預共享金鑰。但是目前沒用(因為不能設定自己的)，之後會加
    4. `endpoint`: 對方的連線地址。如果roaming會覆寫設定檔
    5. `static`: 設定成true的話，每隔`resetconninterval`秒就會重新解析一次domain，與此同時也不會被roaming覆寫

### Super config

  參見 [example_config/super_mode/README_zh.md](example_config/super_mode/README_zh.md)

### Quick start

#### Run example config

在**不同terminal**分別執行以下命令

```
./etherguard-go -config example_config/super_mode/n1.yaml -mode edge
./etherguard-go -config example_config/super_mode/n2.yaml -mode edge
./etherguard-go -config example_config/super_mode/n3.yaml -mode edge
./etherguard-go -config example_config/super_mode/n4.yaml -mode edge
./etherguard-go -config example_config/super_mode/n5.yaml -mode edge
./etherguard-go -config example_config/super_mode/n6.yaml -mode edge
```

因為本範例配置是stdio的kbdbg模式，stdin會讀入VPN網路  
請在其中一個edge視窗中鍵入
```
b1message
```
因為`l2headermode`是`kbdbg`，所以b1會被轉換成 12byte 的layer 2 header，b是廣播地址`FF:FF:FF:FF:FF:FF`，1是普通地址`AA:BB:CC:DD:EE:01`，message是後面的payload，然後再丟入VPN  
此時應該要能夠在另一個視窗上看見字串b1message。前12byte被轉換回來了

#### Run your own etherguard

要正式使用，請將itype改成`tap`，並且修改各節點的公鑰私鑰和連線地址
再關閉不必要的log增加性能，最後部屬到不同節點即可

## 下一篇: [Super Mode的運作](../super_mode/README_zh.md)