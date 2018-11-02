package main

import (
	"encoding/hex"
	"flag"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"time"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/config"
	"github.com/invin/kkchain/consensus"
	"github.com/invin/kkchain/consensus/pow"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/crypto/ed25519"
	"github.com/invin/kkchain/miner"
	"github.com/invin/kkchain/p2p"
	"github.com/invin/kkchain/p2p/impl"
	"github.com/invin/kkchain/params"

	"github.com/invin/kkchain/core/vm"
	log "github.com/sirupsen/logrus"
)

func main() {
	port := flag.String("p", "9998", "")
	keypath := flag.String("k", "", "")
	mineFlag := flag.String("m", "", "")
	fakeFlag := flag.String("f", "fakeMineFlag", "true is fake mine")
	dataStoreDir := flag.String("d", "", "A directory that stores block data")
	flag.Parse()

	//config := &core.Config{DataDir: ""}
	cfg := config.Config{
		GeneralConfig: config.DefaultGeneralConfig,
		Network:       &config.DefaultNetworkConfig,
		Dht:           &config.DefaultDhtConfig,
	}

	cfg.DataDir = *dataStoreDir
	if cfg.DataDir != "" {
		absdatadir, err := filepath.Abs(cfg.DataDir)
		if err != nil {
			return
		}
		cfg.DataDir = absdatadir

		if err := os.MkdirAll(cfg.DataDir, 0700); err != nil {
			return
		}
	}

	chainDb, _ := config.OpenDatabase(&cfg, "chaindata")

	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlock(chainDb, nil)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		log.Errorf("setup genesis failed： %s\n", genesisErr)
		return
	}
	log.WithFields(log.Fields{
		"config":  chainConfig,
		"genesis": genesisHash.String(),
	}).Info("Initialised chain configuration")

	engine := newEngine(fakeFlag)

	vmConfig := vm.Config{EnablePreimageRecording: false}
	chain, _ := core.NewBlockChain(chainConfig, vmConfig, chainDb, engine)

	go func() {
		ticker := time.NewTicker(8 * time.Second)
		for _ = range ticker.C {
			block := chain.CurrentBlock()
			fmt.Printf("!!!!!blockchain info: CurrentBlock:====> %s", block.String())
		}
	}()

	go doP2P(chain, *port, *keypath)

	if *mineFlag == "true" {
		doMiner(chainConfig, chain, engine)
	}

	select {}
}

func doP2P(bc *core.BlockChain, port, keypath string) {

	listen := "/ip4/127.0.0.1/tcp/" + port
	config.DefaultNetworkConfig.PrivateKey = keypath
	config.DefaultNetworkConfig.Listen = listen

	if port != "9998" {
		remoteKeyPath := "node1.key"
		pri, _ := p2p.LoadNodeKeyFromFile(remoteKeyPath)
		pub, _ := ed25519.New().PrivateToPublic(pri)
		node := "/ip4/127.0.0.1/tcp/9998"
		node = hex.EncodeToString(pub) + "@" + node
		log.Infof("remote peer: %s", node)
		config.DefaultNetworkConfig.Seeds = []string{node}
		config.DefaultDhtConfig.Seeds = []string{node}
	}

	net := impl.NewNetwork(&config.DefaultNetworkConfig, &config.DefaultDhtConfig, bc)

	err := net.Start()
	if err != nil {
		log.Errorf("failed to start server: %s\n", err)
	}

}

func newEngine(fakeFlag *string) consensus.Engine {
	home := os.Getenv("HOME")
	if home == "" {
		if user, err := user.Current(); err == nil {
			home = user.HomeDir
		}
	}
	var engine *pow.Ethash
	if *fakeFlag == "true" {
		engine = pow.NewFakeDelayer(15 * time.Second)
	} else {
		powconfig := pow.Config{
			CacheDir:       "ethash",
			CachesInMem:    2,
			CachesOnDisk:   3,
			DatasetDir:     filepath.Join(home, ".ethash"),
			DatasetsInMem:  1,
			DatasetsOnDisk: 2,
			PowMode:        pow.ModeNormal,
		}
		engine = pow.New(powconfig, nil)
	}
	return engine

}

func doMiner(chainConfig *params.ChainConfig, chain *core.BlockChain, engine consensus.Engine) {
	defer engine.Close()

	txpool := core.NewTxPool(core.DefaultTxPoolConfig, chainConfig, chain)

	miner := miner.New(chainConfig, chain, txpool, engine)
	defer miner.Close()

	miner.SetMiner(common.HexToAddress("0x67b1043995cf9fb7dd27f6f7521342498d473c05"))
	miner.Start()

	wait := make(chan interface{})
	select {
	case <-wait:

	}
}
