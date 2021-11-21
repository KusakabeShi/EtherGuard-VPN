# Etherguard
[English](README.md)

P2P Mode的[範例配置檔](./)的說明文件
在了解Super Mode的運作之前，建議您先閱讀[Super Mode的運作](../super_mode/README_zh.md)方法，再閱讀本篇會比較好

## P2P Mode
受到[tinc](https://github.com/gsliepen/tinc)的啟發

和[Super模式運作](../super_mode/README_zh.md)有點相似，不過也有點修改  

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

## Config Paramaters

P2P模式也有幾個參數
1. usep2p: 是否啟用P2P模式
1. sendpeerinterval: 廣播BoardcastPeer的間格
1. graphrecalculatesetting: 一些和[Floyd-Warshall演算法](https://zh.wikipedia.org/zh-tw/Floyd-Warshall算法)相關的參數
    1. staticmode: 關閉Floyd-Warshall演算法，只使用一開始載入的nexthoptable。P2P單純用來打洞
    1. jittertolerance: 抖動容許誤差，收到Pong以後，一個37ms，一個39ms，不會觸發重新計算
    1. jittertolerancemultiplier: 一樣是抖動容許誤差，但是高ping的話允許更多誤差  
        https://www.desmos.com/calculator/raoti16r5n
    1. nodereporttimeout: 收到的`Pong`封包的有效期限。太久沒收到就變回Infinity
    1. recalculatecooldown: Floyd-Warshal是O(n^3)時間複雜度，不能太常算。設個冷卻時間

## Note
P2P模式下，PSK是禁用的。因為n個節點有n(n-1)/2的連線，每個連線都要使用不同PSK  
又不像static mode提前設好，peer數固定不再變動  
也不像super mode，有中心伺服器統一分發
每對peer要協商出一個PSK有難度，因此我設定禁用PSK了，只用wireguard原本的加密系統

**最後，P2P模式我還沒有大規模測試過，穩定性不知如何。PR is welcome**