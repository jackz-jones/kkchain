package miner

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/consensus/pow"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/params"

	"time"

	"github.com/invin/kkchain/config"
	"github.com/invin/kkchain/core/vm"
	logger "github.com/sirupsen/logrus"
)

func TestMiner_Start(t *testing.T) {

}

func TestMiner_Stop(t *testing.T) {

}

func TestMine(t *testing.T) {

	home := os.Getenv("HOME")
	if home == "" {
		if user, err := user.Current(); err == nil {
			home = user.HomeDir
		}
	}

	cfg := &config.Config{
		GeneralConfig: config.DefaultGeneralConfig,
		Network:       &config.DefaultNetworkConfig,
		Dht:           &config.DefaultDhtConfig,
	}

	chainDb, err := config.OpenDatabase(cfg, "chaindata")
	if err != nil {
		return
	}

	chainConfig, genesisHash, genesisErr := core.SetupGenesisBlock(chainDb, nil)
	if _, ok := genesisErr.(*params.ConfigCompatError); genesisErr != nil && !ok {
		t.Error("setup genesis failed", genesisErr)
		return
	}
	t.Log("Initialised chain configuration", "config", chainConfig, "genesis", genesisHash.String())

	powConfig := pow.Config{
		CacheDir:       "ethash",
		CachesInMem:    2,
		CachesOnDisk:   3,
		DatasetDir:     filepath.Join(home, ".ethash"),
		DatasetsInMem:  1,
		DatasetsOnDisk: 2,
		PowMode:        pow.ModeNormal,
	}

	logger.Info(powConfig.DatasetDir)
	engine := pow.New(powConfig, nil)
	defer engine.Close()

	vmConfig := vm.Config{EnablePreimageRecording: false}
	chain, _ := core.NewBlockChain(chainConfig, vmConfig, chainDb, engine)

	txpool := core.NewTxPool(core.DefaultTxPoolConfig, chainConfig, chain)

	miner := New(chainConfig, chain, txpool, engine)
	defer miner.Close()

	miner.SetMiner(common.HexToAddress("0x67b1043995cf9fb7dd27f6f7521342498d473c05"))
	miner.Start()

	time.Sleep(time.Duration(8 * time.Second))

	logger.Info("PostSyncStartEvent")
	chain.PostSyncStartEvent(core.StartEvent{})

	time.Sleep(time.Duration(10 * time.Second))
	logger.Info("PostSyncDoneEvent")
	chain.PostSyncDoneEvent(core.DoneEvent{})

	wait := make(chan interface{})
	select {
	case <-wait:

	}
}
