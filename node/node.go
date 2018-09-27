package node

import (
	"fmt"
	"sync"
	"time"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/config"
	"github.com/invin/kkchain/consensus"
	"github.com/invin/kkchain/consensus/pow"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/miner"
	"github.com/invin/kkchain/p2p"
	"github.com/invin/kkchain/p2p/impl"
	"github.com/invin/kkchain/params"
	"github.com/invin/kkchain/storage"

	log "github.com/sirupsen/logrus"
)

type Node struct {
	stop chan struct{} // Channel to wait for termination notifications
	lock *sync.RWMutex

	config      *config.Config
	chainConfig *params.ChainConfig

	chainDb    storage.Database // Block chain database
	engine     consensus.Engine
	blockchain *core.BlockChain
	txPool     *core.TxPool
	miner      *miner.Miner
	network    p2p.Network
}

func New(cfg *config.Config) (*Node, error) {

	chainDb, err := config.OpenDatabase(cfg, "chaindata")
	if err != nil {
		return nil, err
	}

	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlock(chainDb, nil)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		log.Errorf("setup genesis failed： %s\n", genesisErr)
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

	node.blockchain, err = core.NewBlockChain(chainDb, node.engine)
	if err != nil {
		return nil, err
	}

	node.txPool = core.NewTxPool()
	node.miner = miner.New(node.blockchain, node.txPool, node.engine)

	node.network = impl.NewNetwork(cfg.Network, cfg.Dht, node.blockchain)

	return node, nil
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
			log.Errorf("failed to start server: %s\n", err)
		}
	}()

	n.miner.SetMiner(common.HexToAddress("0x67b1043995cf9fb7dd27f6f7521342498d473c05"))
	if n.config.Consensus.Mine {
		n.miner.Start()
	}

}

func (n *Node) Stop() {
	n.network.Stop()

	n.engine.Close()

	n.miner.Close()

	n.chainDb.Close()
}

func createConsensusEngine(cfg *config.Config) consensus.Engine {

	powconfig := pow.DefaultConfig
	powconfig.PowMode = pow.ModeNormal

	return pow.New(powconfig, nil)
}
