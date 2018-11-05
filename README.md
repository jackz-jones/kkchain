# kkchain
**king kong chain**
## 1 build  
**cd ./cmd/kkchain**

**go build -o kkchain** 
## 2 run  
There are three ways to run a kkchain node. The order of kkchain initialization parameter is the system default parameter value, the configuration file parameter value, and the command line parameter value. The latter value will override the previous setting.
### 2.1 configuration file（recommended this way）
If you want use config file to run a kkchain node, run the following command:

**./kkchain –config config.file**

**NOTE：** config.file represent the kkchain configuration file，you can use config/kkchain.toml under this project's root directory.

**LIKE THAT：** ./kkchain –config ../config/kkchain.toml
### 2.2 command line
You can also enter specific commands to run the kkchain node, like that：

**kkchain -consensus.mine true -consensus.type pow -network.listen /ip4/127.0.0.1/tcp/9998 -network.nodekey node1.key -network.seed ["89b8bb2b66a41220a9b8ba8f019c291dc69c8d9b1ee023813f9db8f8bdcd1f76@/ip4/127.0.0.1/tcp/9998"] -datadir ./data -api.rpc true -api.rpcmodules ["kkc,personal,miner"] -api.rpcaddr /ip4/127.0.0.1/tcp/8545 -dht.bucketsize 2 -network.maxpeer 20**

description for the command parameters:

| command         | type     | description                                          |
| :---------------: | :--------: | :----------------------------------------------------: |
| consensus.mine  | bool     | start mine or not                                    |
| consensus.type  | string   | consensus mechanism (only supported pow now)         |
| network.listen  | string   | p2p network listen address                           |
| network.nodekey | string   | private key file (will auto create one if no)        |
| network.seed    | []string | bootstrap node list                                  |
| network.maxpeer | int      | allow the max count of connected peer                |
| dht.bucketsize  | int      | the size of dht bucket                               |
| datadir         | string   | blockchain data store path                           |
| api.rpc         | bool     | start rpc service or not                             |
| api.rpcmodules  | []string | modules need to be open (support kkc,personal,miner) |
| api.rpcaddr     | string   | rpc address for client request                       |

**NOTE：** If you run node without any command parameter, the node will start with followinf default config:
```
var (
        DefaultGeneralConfig = GeneralConfig{
    		DataDir: DefaultDataDir(),
    	}
    
    	DefaultNetworkConfig = NetworkConfig{
    		Listen:     "/ip4/127.0.0.1/tcp/9998",
    		PrivateKey: "node1.key",
    		NetworkId:  1,
    		MaxPeers:   20,
    		Seeds:      []string{"89b8bb2b66a41220a9b8ba8f019c291dc69c8d9b1ee023813f9db8f8bdcd1f76@/ip4/127.0.0.1/tcp/9998"},
    	}
    
    	DefaultDhtConfig = DhtConfig{
    		BucketSize: 16,
    	}
    
    	DefaultConsensusConfig = ConsensusConfig{
    		Mine: false,
    		Type: "pow",
    	}
    
    	DefaultAPIConfig = ApiConfig{
    		Rpc:        false,
    		RpcModules: []string{"kkc"},
    		RpcAddr:    "/ip4/127.0.0.1/tcp/8545",
    	}
)
```
**And if you want to run multiple nodes at local, should keep following parameters different:**
-datadir，-network.listen, -network.nodekey.
### 2.3 config file+command line
When you modify any parameter in config file, you can use this way to run node, and the command line parameter will overwrite the config.

**FOR EXAMPLE**

./kkchain –config ./config/kkchain.toml -network.listen /ip4/127.0.0.1/tcp/9999 -network.nodekey node2.key -datadir ./data/node2
## 3 examples for multiple node running at local
### 3.1 command line to run multiple nodes

**NODE 1（mining and opening rpc）:**<br>
./kkchain -consensus.mine -network.listen /ip4/127.0.0.1/tcp/9998 -network.nodekey node1.key -datadir ./data/node1 -api.rpc true -api.rpcmodules ["kkc,personal,miner"] -api.rpcaddr /ip4/127.0.0.1/tcp/8545<br><br>
**NODE 2（mining and opening rpc）:**<br>
./kkchain -consensus.mine -network.listen /ip4/127.0.0.1/tcp/9999 -network.nodekey node2.key -datadir ./data/node2 -api.rpc true -api.rpcmodules ["kkc,personal,miner"] -api.rpcaddr /ip4/127.0.0.1/tcp/8546<br><br>
**NODE 2（not mining and closing rpc）:**<br>
./kkchain -network.listen /ip4/127.0.0.1/tcp/10000 -network.nodekey node3.key -datadir ./data/node3<br><br> 

### 3.2 config file to run multiple nodes
**NODE 1:**<br>
./kkchain –config ./config/kkchain.toml<br><br>
**NODE 2:**<br>
./kkchain –config ./config/kkchain2.toml<br><br>
**NODE 3:**<br>
./kkchain –config ./config/kkchain3.toml<br><br> 

### 3.3 config file+command line to run multiple nodes
**NOTE:** The command line parameter datadir value will be the final.<br><br>
**NODE 1:**<br>
./kkchain –config ./config/kkchain.toml -datadir ./data/node1<br><br>
**NODE 2:**<br>
./kkchain –config ./config/kkchain2.toml -datadir ./data/node2<br><br> 
**NODE 3:**<br>
./kkchain –config ./config/kkchain3.toml -datadir ./data/node3<br><br>

