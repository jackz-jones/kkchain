# 公链RPC接口使用说明
## 1 config配置项说明
### 1.1 ApiConfig：rpcmodules

在原来的ApiConfig中增加了rpcmodules配置项，表明节点启动开启的一些rpc服务模块，如eth、personal、miner（目前代码中只支持这三种）。

另外，默认情况下，rpcmodules只配置了eth，即外部配置文件中如果不配其他的服务模块，只会开启eth相关接口。
### 1.2 配置文件示例

```
...

##### api configuration options #####
[api]
# Enable rpc
rpc = true

# RPC service modules
rpcmodules = ["eth","personal","miner"]

# Address to listen for incoming connections
rpcaddr = "/ip4/127.0.0.1/tcp/8545"

...
```

## 2 具体的rpc接口

目前，各rpc接口基本沿用以太坊的，所以各接口的参数和返回结果可以参考以太坊官方说明：

[https://github.com/ethereum/wiki/wiki/JSON-RPC](https://github.com/ethereum/wiki/wiki/JSON-RPC)

[https://github.com/ethereum/go-ethereum/wiki/Management-APIs](https://github.com/ethereum/go-ethereum/wiki/Management-APIs)

以下只简单描述接口作用，部分常用的接口给出调用示例。

### 2.1 eth模块

#### （1）查询给定区块中交易数

eth\_getBlockTransactionCountByNumber

eth\_getBlockTransactionCountByHash

eth\_getTransactionCount

#### （2）查询交易

eth\_getTransactionByBlockNumberAndIndex

eth\_getTransactionByBlockHashAndIndex

eth\_getTransactionByHash

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_getTransactionByHash&quot;,&quot;params&quot;:[&quot;0xb469cd8e924f31dac8dfa8e73f8b211cc4fc0e7654585097b5c119d8ba9a3525&quot;],&quot;id&quot;:1}&#39; localhost:8547

#### （3）发送交易

eth\_sendTransaction

eth\_sendRawTransaction

**调用示例：**

a、发送普通交易

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_sendTransaction&quot;,&quot;params&quot;:[{&quot;from&quot;:&quot;0x182a0009ce80b57e0b24756f38894c782794cdae&quot;,&quot;to&quot;:&quot;0xe084ac0a7b047871a2fd44e94e6375b80a7f2460&quot;,&quot;gas&quot;:&quot;0x5abc&quot;,&quot;gasPrice&quot;:&quot;0x9&quot;,&quot;value&quot;:&quot;0x100&quot;}],&quot;id&quot;:1}&#39; localhost:8547

b、发送合约部署交易

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_sendTransaction&quot;,&quot;params&quot;:[{&quot;from&quot;:&quot;0x182a0009ce80b57e0b24756f38894c782794cdae&quot;,&quot;gas&quot;:&quot;0x5abce&quot;,&quot;gasPrice&quot;:&quot;0x9&quot;,&quot;data&quot;:&quot;0x6060604052341561000f57600080fd5b60b18061001d6000396000f300606060405260043610603f576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff1680631003e2d2146044575b600080fd5b3415604e57600080fd5b606260048080359060200190919050506078565b6040518082815260200191505060405180910390f35b60006002820190509190505600a165627a7a7230582021c0f29d392030c5703e324aa8ecebe863260bfbd28e8beb4d7a617be0ea95b70029&quot;}],&quot;id&quot;:1}&#39; localhost:8547

#### （4）签名交易

eth\_sign

#### （5）查询交易收据

eth\_getTransactionReceipt

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_getTransactionReceipt&quot;,&quot;params&quot;:[&quot;0x021b79ce2a6acda1d1c5a895dc092738ced60cd6760a1b1d6a919c46e412f0d0&quot;],&quot;id&quot;:1}&#39; localhost:8547

#### （6）查询最新块号

eth\_blockNumber

#### （7）查询余额

eth\_getBalance

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_getBalance&quot;,&quot;params&quot;:[&quot;0xe084ac0a7b047871a2fd44e94e6375b80a7f2460&quot;,&quot;latest&quot;],&quot;id&quot;:1}&#39; localhost:8547

#### （8）查询区块

eth\_getBlockByNumber

eth\_getBlockByHash

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_getBlockByHash&quot;,&quot;params&quot;:[&quot;0xe34598d9d7ae19dc8a1f4252a9ae3c8e95c33c9573bc1d21878675f82c44ac88&quot;, true],&quot;id&quot;:1}&#39; localhost:8547

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_getBlockByNumber&quot;,&quot;params&quot;:[&quot;0x261&quot;, true],&quot;id&quot;:1}&#39; localhost:8547

#### （9）查询合约的字节码

eth\_getCode

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_getCode&quot;,&quot;params&quot;:[&quot;0x519ea2272e44316a7f5bff560e3ac20e0db6422f&quot;,&quot;latest&quot;],&quot;id&quot;:1}&#39; localhost:8547

#### （10）查询给定地址存储位置的值

eth\_getStorageAt

#### （11）立即执行合约调用，而不创建交易

eth\_call

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_call&quot;,&quot;params&quot;:[{&quot;from&quot;:&quot;0x182a0009ce80b57e0b24756f38894c782794cdae&quot;,&quot;to&quot;:&quot;0x519ea2272e44316a7f5bff560e3ac20e0db6422f&quot;,&quot;gas&quot;:&quot;0x5abce&quot;,&quot;gasPrice&quot;:&quot;0x9&quot;,&quot;data&quot;:&quot;0x1003e2d20000000000000000000000000000000000000000000000000000000000000006&quot;},&quot;latest&quot;],&quot;id&quot;:1}&#39; localhost:8547

#### （12）估算给定交易完成所需的gas值

eth\_estimateGas

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_estimateGas&quot;,&quot;params&quot;:[{&quot;from&quot;:&quot;0x182a0009ce80b57e0b24756f38894c782794cdae&quot;,&quot;to&quot;:&quot;0x911b8a75391f5147346c670f9eec03242687bfec&quot;,&quot;gas&quot;:&quot;0x5abce&quot;,&quot;gasPrice&quot;:&quot;0x9&quot;,&quot;data&quot;:&quot;0xa0acb9dd000000000000000000000000000000000000000000000000000000000000002000000000000000000000000000000000000000000000000000000000000000033131310000000000000000000000000000000000000000000000000000000000&quot;}],&quot;id&quot;:1}&#39; localhost:8547

#### （13）查询节点的地址列表

eth\_accounts

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_accounts&quot;,&quot;params&quot;:[],&quot;id&quot;:1}&#39; localhost:8547

#### （14）查询节点是否在挖矿

eth\_mining

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;eth\_mining&quot;,&quot;params&quot;:[],&quot;id&quot;:1}&#39; localhost:8547

### 2.2 personal模块

personal模块原来在以太坊中是不对外开放访问的，只允许登陆cli之后，通过对应的指令调用。目前在公链中是开放的，也有发送交易、签名交易等多个接口，这里只列出常用的接口：

#### （1）查询节点的地址列表

personal\_listAccounts

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;personal\_listAccounts&quot;,&quot;params&quot;:[],&quot;id&quot;:1}&#39; localhost:8547

#### （2）创建新账户

personal\_newAccount

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;personal\_newAccount&quot;,&quot;params&quot;:[&quot;123&quot;],&quot;id&quot;:1}&#39; localhost:8547

#### （3）解锁账户

personal\_unlockAccount

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;personal\_unlockAccount&quot;,&quot;params&quot;:[&quot;0xFdc3D56Da692F3A26B9A053D900d7055F35735d4&quot;,&quot;123&quot;],&quot;id&quot;:1}&#39; localhost:8547

#### （4）锁定账户

personal\_lockAccount

### 2.3 miner模块

miner模块原来在以太坊中也是不对外开放访问的，只允许登陆cli之后，通过对应的指令调用。目前在公链中是开放的，常用的接口有：

#### （1）开启挖矿

miner\_start

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;miner\_start&quot;,&quot;params&quot;:[1],&quot;id&quot;:1}&#39; localhost:8547

#### （2）停止挖矿

miner\_stop

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;miner\_stop&quot;,&quot;params&quot;:[],&quot;id&quot;:1}&#39; localhost:8547

#### （3）设置新的miner

miner\_setMiner

**调用示例：**

curl -X POST -H &quot;Content-Type:application/json&quot; --data &#39;{&quot;jsonrpc&quot;:&quot;2.0&quot;,&quot;method&quot;:&quot;miner\_setMiner&quot;,&quot;params&quot;:[&quot;0x4A3D3d67c706D4C74E03f61Befe793C489064A0C&quot;],&quot;id&quot;:1}&#39; localhost:8547