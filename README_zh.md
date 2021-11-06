
# Etherguard

[English](README.md)

[![Contributor Covenant](https://img.shields.io/badge/Contributor%20Covenant-2.1-4baaaa.svg)](code_of_conduct.md)

一個從wireguard-go改來的Full Mesh Layer2 VPN.  

OSPF能夠根據cost自動選路  
但是實際上，我們偶爾會遇到去程/回程不對等的問題  
之前我就在想，能不能根據單向延遲選路呢?  
例如我有2條線路，一條去程快，一條回程快。就自動過去回來各自走快的?  

所以我就想弄一個這種的VPN了，任兩節點會測量單向延遲，並且使用[Floyd-Warshall演算法](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)演算法找出任兩節點間的最佳路徑  
來回都會是最佳的。有2條線路，一條去程快，一條回程快，就會自動各走各的

擔心時鐘不同步，單向延遲測量不正確?  
沒問題的，證明可以看這邊: [https://www.kskb.eu.org/2021/08/rootless-routerpart-3-etherguard.html](https://www.kskb.eu.org/2021/08/rootless-routerpart-3-etherguard.html)

## Usage

```bash
Usage of ./etherguard-go-vpp:
  -bind string
        UDP socket bind mode. [linux|std]
        You may need this if tou want to run Etherguard under WSL. (default "linux")
  -config string
        設定檔路徑
  -example
        印一個範例設定檔
  -help
        Show this help
  -mode string
        運作模式，有兩種模式 super/edge
        solve是用來解 Floyd Warshall的，Static模式會用到
  -no-uapi
        不使用UAPI。使用UAPI，你可以用wg命令看到一些連線資訊(畢竟是從wireguard-go改的)
  -version
        顯示版本
```

## Mode

1. Static 模式: 類似於原本的wireguard的模式。 [詳細介紹](example_config/static_mode/README_zh.md)
2. Super 模式: 受到[n2n](https://github.com/ntop/n2n)的啟發寫的模式。 [詳細介紹](example_config/super_mode/README_zh.md)
3. P2P 模式: 受到[tinc](https://github.com/gsliepen/tinc)的啟發寫的模式。 [詳細介紹](example_config/p2p_mode/README_zh.md)

## Common Config Paramater

有些設定檔對應某些運作模式，這邊針對共同部分的設定做說明

### Edge config

邊緣節點是實際執行VPN的節點

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
    3. `conntimeout`: 鄰居應該要發Ping過來，超過就視為鄰居掛了
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

## Build

### No-vpp version

編譯沒有VPP libmemif的版本。可以在一般linux電腦上使用

安裝 Go 1.16

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

編譯有VPP libmemif的版本。

用這個版本的話你的電腦要有libmemif.so才能run起來

安裝 VPP 和 libemif

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
