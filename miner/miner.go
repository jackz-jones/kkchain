package miner

import (
	"sync/atomic"

	"github.com/invin/kkchain/consensus"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/event"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/params"
	logger "github.com/sirupsen/logrus"
)

type Miner struct {
	quitCh chan struct{}

	worker *worker

	chain *core.BlockChain

	syncDone      int32
	isLocalMining int32

	// sync start event
	syncStartCh  chan core.StartEvent
	syncStartSub event.Subscription
	// sync over event
	syncDoneCh  chan core.DoneEvent
	syncDoneSub event.Subscription
}

func New(config *params.ChainConfig, bc *core.BlockChain, txpool *core.TxPool, engine consensus.Engine) *Miner {
	miner := &Miner{
		quitCh:      make(chan struct{}),
		syncStartCh: make(chan core.StartEvent),
		syncDoneCh:  make(chan core.DoneEvent),
		worker:      newWorker(config, bc, txpool, engine),
		chain:       bc,
		syncDone:    1,
	}

	go miner.onEvent()
	return miner
}

func (m *Miner) onEvent() {
	// Subscribe events
	m.syncStartSub = m.chain.SubscribeSyncStartEvent(m.syncStartCh)
	defer m.syncStartSub.Unsubscribe()

	m.syncDoneSub = m.chain.SubscribeSyncDoneEvent(m.syncDoneCh)
	defer m.syncDoneSub.Unsubscribe()

	for {
		select {
		case <-m.syncStartCh:
			atomic.StoreInt32(&m.syncDone, 0)
			if m.Mining() {
				m.Stop()
				atomic.StoreInt32(&m.isLocalMining, 1)
			}
		case <-m.syncDoneCh:
			atomic.StoreInt32(&m.syncDone, 1)
			if atomic.LoadInt32(&m.isLocalMining) == 1 {
				m.Start()
			}
			//
		case err := <-m.syncStartSub.Err():
			logger.Error(err)
			return
		case err := <-m.syncDoneSub.Err():
			logger.Error(err)
			return
		case <-m.quitCh:
			return
		}
	}
}

// Start start mine service.
func (m *Miner) Start() {
	atomic.StoreInt32(&m.isLocalMining, 1)

	if atomic.LoadInt32(&m.syncDone) == 0 {
		return
	}

	if m.worker.isRunning() {
		return
	}

	m.worker.start()
}

// Stop stop mine service.
func (m *Miner) Stop() {
	m.worker.stop()
	atomic.StoreInt32(&m.isLocalMining, 0)
}

func (m *Miner) Close() {
	m.worker.close()
	close(m.quitCh)
}

func (m *Miner) Mining() bool {
	return m.worker.isRunning()
}

func (m *Miner) SetMiner(addr common.Address) {
	m.worker.setMiner(addr)
}

func (m *Miner) GetMiner() common.Address {
	return m.worker.miner
}

// PendingBlock returns the currently pending block.
//
// Note, to access both the pending block and the pending state
// simultaneously, please use Pending(), as the pending state can
// change between multiple method calls
func (self *Miner) PendingBlock() *types.Block {
	return self.worker.pendingBlock()
}

// Pending returns the currently pending block and associated state.
func (self *Miner) Pending() (*types.Block, *state.StateDB) {
	return self.worker.pending()
}
