package node

import (
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"os"

	"github.com/invin/kkchain/accounts"
	"github.com/invin/kkchain/accounts/keystore"
	"github.com/invin/kkchain/api"
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/config"
	"github.com/invin/kkchain/consensus"
	"github.com/invin/kkchain/consensus/pow"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/miner"
	"github.com/invin/kkchain/p2p"
	"github.com/invin/kkchain/p2p/dht"
	"github.com/invin/kkchain/p2p/impl"
	"github.com/invin/kkchain/params"
	"github.com/invin/kkchain/rpc"
	"github.com/invin/kkchain/storage"
	log "github.com/sirupsen/logrus"
)

type Node struct {
	stop chan struct{} // Channel to wait for termination notifications
	lock *sync.RWMutex

	accman            *accounts.Manager
	ephemeralKeystore string // if non-empty, the key directory that will be removed by Stop
	config            *config.Config
	chainConfig       *params.ChainConfig

	chainDb    storage.Database // Block chain database
	engine     consensus.Engine
	blockchain *core.BlockChain
	txPool     *core.TxPool
	miner      *miner.Miner
	coinbase   common.Address
	network    p2p.Network

	httpEndpoint  string
	httpListener  net.Listener
	httpHandler   *rpc.Server
	inprocHandler *rpc.Server
}

func New(cfg *config.Config) (*Node, error) {

	chainDb, err := config.OpenDatabase(cfg, "chaindata")
	if err != nil {
		return nil, err
	}

	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlock(chainDb, nil)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		log.Errorf("setup genesis failed: %s", genesisErr)
		return nil, genesisErr
	}

	log.WithFields(log.Fields{
		"config":  chainConfig,
		"genesis": genesisHash.String(),
	}).Info("Initialised chain configuration")

	node := &Node{
		stop:        make(chan struct{}),
		lock:        &sync.RWMutex{},
		config:      cfg,
		chainConfig: chainConfig,
		chainDb:     chainDb,
		engine:      createConsensusEngine(cfg),
	}

	node.coinbase = common.HexToAddress(cfg.Consensus.Coinbase)

	vmConfig := vm.Config{EnablePreimageRecording: false}

	node.blockchain, err = core.NewBlockChain(chainConfig, vmConfig, chainDb, node.engine)
	if err != nil {
		return nil, err
	}
	txConfig := core.DefaultTxPoolConfig
	if txConfig.Journal != "" {
		txConfig.Journal = cfg.ResolvePath(txConfig.Journal)
	}

	node.txPool = core.NewTxPool(txConfig, chainConfig, node.blockchain)
	node.miner = miner.New(chainConfig, node.blockchain, node.txPool, node.engine)
	node.network = impl.NewNetwork(cfg.Network, cfg.Dht, node.blockchain, node.txPool)

	node.accman, node.ephemeralKeystore, err = config.MakeAccountManager(cfg)
	if err != nil {
		return nil, err
	}

	return node, nil
}

// AccountManager retrieves the account manager used by the protocol stack.
func (n *Node) AccountManager() *accounts.Manager {
	return n.accman
}

func (n *Node) ChainDb() storage.Database {
	return n.chainDb
}

func (n *Node) Miner() *miner.Miner {
	return n.miner
}

func (n *Node) BlockChain() *core.BlockChain {
	return n.blockchain
}

func (n *Node) TxPool() *core.TxPool {
	return n.txPool
}

func (n *Node) ChainConfig() *params.ChainConfig {
	return n.chainConfig
}

func (n *Node) Coinbase() (eb common.Address, err error) {
	n.lock.RLock()
	coinbase := n.coinbase
	n.lock.RUnlock()

	if coinbase != (common.Address{}) {
		return coinbase, nil
	}

	if wallets := n.AccountManager().Wallets(); len(wallets) > 0 {
		if accounts := wallets[0].Accounts(); len(accounts) > 0 {
			coinbase := accounts[0].Address

			n.lock.Lock()
			n.coinbase = coinbase
			n.lock.Unlock()

			log.Infof("Coinbase automatically configured,address: %s", coinbase.String())
			return coinbase, nil
		}

		// if you want to mine and there are no accounts, should make a faucet account for test
	} else {
		ks := n.AccountManager().Backends(keystore.KeyStoreType)[0].(*keystore.KeyStore)
		acc, err := ks.NewAccount("123")
		if err == nil {
			log.Infof("new a faucet account %s ", acc.Address.String())
			return acc.Address, nil
		} else {
			log.Errorf("failed to new a initial account: %v", err)
		}
	}
	return common.Address{}, fmt.Errorf("coinbase must be explicitly specified")
}

func (n *Node) SetCoinbase(coinbase common.Address) {
	n.lock.Lock()
	n.coinbase = coinbase
	n.lock.Unlock()

	n.miner.SetMiner(coinbase)
}

func (n *Node) SartMining(local bool) error {
	cb, err := n.Coinbase()
	if err != nil {
		log.Errorf("Cannot start mining without etherbase,err: %v", err)
		return fmt.Errorf("coinbase missing: %v", err)
	}

	n.SetCoinbase(cb)

	if local {
		// If local (CPU) mining is started, we can disable the transaction rejection
		// mechanism introduced to speed sync times. CPU mining on mainnet is ludicrous
		// so none will ever hit this path, whereas marking sync done on CPU mining
		// will ensure that private networks work in single miner mode too.
		n.network.Chain().AcceptTxs()
	}
	go n.miner.Start()
	return nil
}

func (n *Node) Start() {

	go func() {
		ticker := time.NewTicker(8 * time.Second)
		for _ = range ticker.C {
			block := n.blockchain.CurrentBlock()
			fmt.Printf("!!!!!blockchain info: CurrentBlock:====> %s", block.String())
		}
	}()

	go func() {
		err := n.network.Start()
		if err != nil {
			log.Fatalf("failed to start server: %s\n", err)
		}
	}()

	if n.config.Consensus.Mine {
		err := n.SartMining(true)
		if err != nil {
			log.Errorf("failed to start mining,err: %v", err)
		}
	}

	if n.config.Api.Rpc {
		err := n.startRPC()
		if err != nil {
			log.Errorf("failed to start rpc service,err: %v", err)
		}
	}
}

func (n *Node) Stop() {
	n.network.Stop()

	n.engine.Close()

	if n.config.Consensus.Mine {
		n.miner.Close()
	}

	n.chainDb.Close()
	n.stopHTTP()
	n.stopInProc()

	// Remove the keystore if it was created ephemerally.
	if n.ephemeralKeystore != "" {
		os.RemoveAll(n.ephemeralKeystore)
	}
}

func createConsensusEngine(cfg *config.Config) consensus.Engine {

	powconfig := pow.DefaultConfig
	powconfig.PowMode = pow.ModeNormal

	return pow.New(powconfig, nil)
}

// startRPC is a helper method to start all the various RPC endpoint during node
// startup. It's not meant to be called at any time afterwards as it makes certain
// assumptions about the state of the node.
func (n *Node) startRPC() error {
	apiBackend := &APIBackend{
		n,
		nil,
	}
	gpoParams := api.DefaultGasOracleConfig
	apiBackend.SetGasOracle(api.NewOracle(apiBackend, gpoParams))
	apis := APIs(apiBackend, n)

	err := n.startInProc(apis)
	if err != nil {
		return err
	}

	// if you want to use personal_newAccount、personal_unlockAccount ...,
	// you should add personal inferface into modules when process startHTTP.
	modules := n.config.Api.RpcModules
	cors := []string{""}
	vhosts := []string{"localhost"}

	endpoint := ""
	netaddr, err := dht.ToNetAddr(n.config.Api.RpcAddr)
	if err != nil {
		log.WithFields(log.Fields{
			"rpcAddr": n.config.Api.RpcAddr,
			"error":   err,
		}).Error("failed to convert multiaddr to net addr")
		endpoint = "127.0.0.1:8545"
	} else {
		endpoint = netaddr.String()
	}

	err = n.startHTTP(endpoint, apis, modules, cors, vhosts, rpc.DefaultHTTPTimeouts)
	if err != nil {
		return err
	}

	return nil
}

// startInProc initializes an in-process RPC endpoint.
func (n *Node) startInProc(apis []rpc.API) error {
	// Register all the APIs exposed by the services
	handler := rpc.NewServer()
	for _, api := range apis {
		if err := handler.RegisterName(api.Namespace, api.Service); err != nil {
			return err
		}
		log.WithFields(log.Fields{
			"service":   api.Service,
			"namespace": api.Namespace,
		}).Debug("InProc registered")
	}
	n.inprocHandler = handler
	return nil
}

// startHTTP initializes and starts the HTTP RPC endpoint.
func (n *Node) startHTTP(endpoint string, apis []rpc.API, modules []string, cors []string, vhosts []string, timeouts rpc.HTTPTimeouts) error {

	// Short circuit if the HTTP endpoint isn't being exposed
	if endpoint == "" {
		return nil
	}
	listener, handler, err := rpc.StartHTTPEndpoint(endpoint, apis, modules, cors, vhosts, timeouts)
	if err != nil {
		return err
	}
	log.WithFields(log.Fields{
		"url":     fmt.Sprintf("http://%s", endpoint),
		"modules": modules,
		"cors":    strings.Join(cors, ","),
		"vhosts":  strings.Join(vhosts, ","),
	}).Info("HTTP endpoint opened")

	// All listeners booted successfully
	n.httpEndpoint = endpoint
	n.httpListener = listener
	n.httpHandler = handler

	return nil
}

// stopInProc terminates the in-process RPC endpoint.
func (n *Node) stopInProc() {
	if n.inprocHandler != nil {
		n.inprocHandler.Stop()
		n.inprocHandler = nil
	}
}

// stopHTTP terminates the HTTP RPC endpoint.
func (n *Node) stopHTTP() {
	if n.httpListener != nil {
		n.httpListener.Close()
		n.httpListener = nil
		log.Infof("HTTP endpoint closed,url: %s", fmt.Sprintf("http://%s", n.httpEndpoint))
	}
	if n.httpHandler != nil {
		n.httpHandler.Stop()
		n.httpHandler = nil
	}
}
