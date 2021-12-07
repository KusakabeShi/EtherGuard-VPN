
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
  -cfgmode string
        cfgmode 快速生成設定檔的模式，目前只實作了super模式 [none|super|p2p]
  -config string
        設定檔路徑
  -example
        印一個範例設定檔
  -help
        Show this help
  -mode string
        運作模式，有兩種運作模式 super/edge
        solve是用來解 Floyd Warshall的，Static模式會用到
        gencfg則是快速生成設定檔
  -no-uapi
        不使用UAPI。使用UAPI，你可以用wg命令看到一些連線資訊(畢竟是從wireguard-go改的)
  -version
        顯示版本
```

## Mode

1. Static 模式: 類似於原本的wireguard的模式。 [詳細介紹](example_config/static_mode/README_zh.md)
2. Super 模式: 受到[n2n](https://github.com/ntop/n2n)的啟發寫的模式。 [詳細介紹](example_config/super_mode/README_zh.md)
3. P2P 模式: 受到[tinc](https://github.com/gsliepen/tinc)的啟發寫的模式。 [詳細介紹](example_config/p2p_mode/README_zh.md)

## Quick start

內建小工具可以快速生成設定檔，搞定私鑰公鑰等等，暫時只支援Super模式

首先按需求修改`example_config/super_mode/gensuper.yaml`

```
$ ./etherguard-go -mode gencfg -cfgmode super -config example_config/super_mode/gensuper.yaml
```

順帶一提，最後三個欄位，IP的部分可以直接省略沒關係  
這個欄位唯一的目的只是在啟動以後，調用ip命令，幫tap接口加個ip  
和VPN本身運作完全無關  
VPN起來以後，自己手動加ip也行  

把一個super，2個edge分別搬去三台機器  
或是2台機器，super和edge可以是同一台

然後在Supernode執行  
```
./etherguard-go -config [設定檔位置] -mode super
```
然後在EdgeNode執行  
```
./etherguard-go -config [設定檔位置] -mode edge
```

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
