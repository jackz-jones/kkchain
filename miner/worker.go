package miner

import (
	"math/big"
	"sync"
	"sync/atomic"
	"time"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/consensus"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/event"

	"fmt"

	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/params"
	log "github.com/sirupsen/logrus"
)

type context struct {
	state    *state.StateDB // apply state changes here
	header   *types.Header
	txs      []*types.Transaction
	receipts []*types.Receipt
	tcount   int           // tx count in cycle
	gasPool  *core.GasPool // available gas used to pack transactions
}

// task contains all information for consensus engine sealing and result submitting.
type task struct {
	block     *types.Block
	receipts  []*types.Receipt
	state     *state.StateDB
	createdAt time.Time
}

type worker struct {
	config  *params.ChainConfig
	startCh chan struct{}
	quitCh  chan struct{}
	miner   common.Address

	running int32

	mu *sync.RWMutex // The lock used to protect the coinbase and extra fields

	chain  *core.BlockChain
	txpool *core.TxPool
	engine consensus.Engine

	//tx pool add new txs
	txsCh  chan core.NewTxsEvent
	txsSub event.Subscription

	//new block inserted to chain
	chainHeadCh  chan core.ChainHeadEvent
	chainHeadSub event.Subscription

	currentCtx *context

	taskCh   chan *task
	resultCh chan *task

	snapshotMu    *sync.RWMutex // The lock used to protect the block snapshot and state snapshot
	snapshotBlock *types.Block
	snapshotState *state.StateDB
}

func newWorker(config *params.ChainConfig, bc *core.BlockChain, txpool *core.TxPool, engine consensus.Engine) *worker {
	w := &worker{
		config:      config,
		startCh:     make(chan struct{}, 1),
		quitCh:      make(chan struct{}),
		mu:          &sync.RWMutex{},
		chain:       bc,
		txpool:      txpool,
		engine:      engine,
		txsCh:       make(chan core.NewTxsEvent),
		chainHeadCh: make(chan core.ChainHeadEvent),
		taskCh:      make(chan *task),
		resultCh:    make(chan *task),
		snapshotMu:  &sync.RWMutex{},
	}

	// Subscribe events from tx pool
	w.txsSub = txpool.SubscribeNewTxsEvent(w.txsCh)

	// Subscribe events from inbound handler
	w.chainHeadSub = bc.SubscribeChainHeadEvent(w.chainHeadCh)

	go w.mineLoop()
	go w.taskLoop()
	go w.waitResult()

	// Submit first work to initialize pending state.
	w.startCh <- struct{}{}

	return w
}

// setMiner sets the miner used to initialize the block miner field.
func (w *worker) setMiner(addr common.Address) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.miner = addr
}

func (w *worker) mineLoop() {
	defer w.txsSub.Unsubscribe()
	defer w.chainHeadSub.Unsubscribe()

	for {
		select {
		case txs := <-w.txsCh:
			fmt.Printf("\n\nreceive new txs: \n")
			for _, tx := range txs.Txs {
				fmt.Println(tx.String())
			}
		case <-w.chainHeadCh:
			if w.isRunning() {
				w.commitTask()
			}
		case <-w.startCh:
			w.commitTask()
		case <-w.quitCh:
			//Stopped Mining
			return
		}
	}

}

// start sets the running status as 1 and triggers new work submitting.
func (w *worker) start() {
	atomic.StoreInt32(&w.running, 1)
	//start first mining
	w.startCh <- struct{}{}
}

// stop sets the running status as 0.
func (w *worker) stop() {
	atomic.StoreInt32(&w.running, 0)
}

// isRunning returns an indicator whether worker is running or not.
func (w *worker) isRunning() bool {
	return atomic.LoadInt32(&w.running) == 1
}

// pending returns the pending state and corresponding block.
func (w *worker) pending() (*types.Block, *state.StateDB) {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	if w.snapshotState == nil {
		return nil, nil
	}
	return w.snapshotBlock, w.snapshotState.Copy()
}

// pendingBlock returns pending block.
func (w *worker) pendingBlock() *types.Block {
	// return a snapshot to avoid contention on currentMu mutex
	w.snapshotMu.RLock()
	defer w.snapshotMu.RUnlock()
	return w.snapshotBlock
}

// close terminates all background threads maintained by the worker and cleans up buffered channels.
// Note the worker does not support being closed multiple times.
func (w *worker) close() {
	close(w.quitCh)
	// Clean up buffered channels
	for empty := false; !empty; {
		select {
		case <-w.resultCh:
		default:
			empty = true
		}
	}
}

func (w *worker) waitResult() {
	for {
		select {
		case result := <-w.resultCh:
			// Short circuit when receiving empty result.
			if result == nil {
				continue
			}
			block := result.block

			w.blockinfo("new block mined!!! =====>", block)

			// Short circuit when receiving duplicate result caused by resubmitting.
			if w.chain.HasBlock(block.Hash(), block.NumberU64()) {
				continue
			}
			// Update the block hash in all logs since it is now available and not when the
			// receipt/log of individual transactions were created.

			// Commit block and state to database.
			err := w.chain.WriteBlockWithState(block, result.receipts, result.state)
			if err != nil {
				log.Errorf("Failed writing block to chain,err: %v", err)
				continue
			}
			// Broadcast the block and announce chain insertion event
			var (
				events []interface{}
				logs   []*types.Log
			)

			events = append(events, core.ChainHeadEvent{Block: block})
			events = append(events, core.NewMinedBlockEvent{Block: block})
			w.chain.PostChainEvents(events, logs)
			w.engine.PostExecute(w.chain, block)

		case <-w.quitCh:
			return

		}
	}
}

func (w *worker) commitTask() {
	w.mu.RLock()
	defer w.mu.RUnlock()

	if w.currentCtx != nil {
	}
	tstart := time.Now()
	parent := w.chain.CurrentBlock()

	tstamp := tstart.Unix()
	if parent.Time().Cmp(new(big.Int).SetInt64(tstamp)) >= 0 {
		tstamp = parent.Time().Int64() + 1
	}
	// this will ensure we're not going off too far in the future
	if now := time.Now().Unix(); tstamp > now+1 {
		wait := time.Duration(tstamp-now) * time.Second
		log.Infof("Mining too far in the future,wait: %v", wait)
		time.Sleep(wait)
	}

	num := parent.Number()
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     num.Add(num, common.Big1),
		Time:       big.NewInt(tstamp),
		Miner:      w.miner,
		GasLimit:   CalcGasLimit(parent),
	}

	// Could potentially happen if starting to mine in an odd state.
	err := w.currentContext(parent, header)
	if err != nil {
		log.Errorf("Failed to create mining context,err: %v", err)
		return
	}

	// should initial snapshotState whatever worker is running
	w.updateSnapshot()

	// if worker is not running, just return
	if !w.isRunning() {
		return
	}

	//Initialize
	err = w.engine.Initialize(w.chain, header)
	if err == consensus.ErrNotProposer {
		//not proposer, just wait block coming
		w.taskCh <- &task{block: types.NewBlock(header, nil, nil), createdAt: time.Now()}
		return
	}

	//get txs from pending pool
	pending, count, _ := w.txpool.Pending()
	txs := make(types.Transactions, 0, count)
	for _, acctxs := range pending {
		for _, tx := range acctxs {
			txs = append(txs, tx)
		}
	}

	if len(txs) > 0 {
		//apply txs and get block
		if w.commitTransactions(txs, w.miner) == false {
			return
		}
	}

	// Deep copy receipts here to avoid interaction between different tasks.
	receipts := make([]*types.Receipt, len(w.currentCtx.receipts))
	for i, l := range w.currentCtx.receipts {
		receipts[i] = new(types.Receipt)
		*receipts[i] = *l
	}
	block := types.NewBlock(header, w.currentCtx.txs, receipts)

	//finalize block before consensus
	s := w.currentCtx.state.Copy()
	err = w.engine.Finalize(w.chain, s, block)
	if err != nil {
		return
	}

	w.taskCh <- &task{block: block, state: s, receipts: receipts, createdAt: time.Now()}
}

// taskLoop is a standalone goroutine to fetch sealing task from the generator and
// push them to consensus engine.
func (w *worker) taskLoop() {
	var (
		stopCh chan struct{}
	)

	// interrupt aborts the in-flight sealing task.
	interrupt := func() {
		if stopCh != nil {
			close(stopCh)
			stopCh = nil
		}
	}
	for {
		select {
		case task := <-w.taskCh:

			// Reject duplicate sealing work due to resubmitting.

			interrupt()
			stopCh = make(chan struct{})
			go w.seal(task, stopCh)
		case <-w.quitCh:
			interrupt()
			return
		}
	}
}

// seal pushes a sealing task to consensus engine and submits the result.
func (w *worker) seal(t *task, stop <-chan struct{}) {
	var (
		err error
		res *task
	)

	if t.block, err = w.engine.Execute(w.chain, t.block, stop); t.block != nil {
		//log.Info("Successfully sealed new block", "number", t.block.Number(), "hash", t.block.Hash(),
		//	"elapsed", time.Since(t.createdAt))
		res = t
	} else {
		if err != nil {
			log.Errorf("Block sealing failed,err: %v", err)
		}
		res = nil
	}

	select {
	case w.resultCh <- res:
	case <-w.quitCh:
	}
}

func (w *worker) currentContext(parent *types.Block, header *types.Header) error {
	state, err := w.chain.StateAt(parent.StateRoot())
	if err != nil {
		return err
	}
	ctx := &context{
		state:  state,
		header: header,
		tcount: 0,
	}

	// Keep track of transactions which return errors so they can be removed
	w.currentCtx = ctx
	return nil
}

// updateSnapshot updates pending snapshot block and state.
// Note this function assumes the current variable is thread safe.
func (w *worker) updateSnapshot() {
	w.snapshotMu.Lock()
	defer w.snapshotMu.Unlock()

	w.snapshotBlock = types.NewBlock(
		w.currentCtx.header,
		w.currentCtx.txs,
		w.currentCtx.receipts,
	)

	w.snapshotState = w.currentCtx.state.Copy()
}

func (w *worker) commitTransactions(txs types.Transactions, coinbase common.Address) bool {
	if w.currentCtx == nil {
		return false
	}

	if w.currentCtx.gasPool == nil {
		w.currentCtx.gasPool = new(core.GasPool).AddGas(w.currentCtx.header.GasLimit)
	}

	for _, tx := range txs {
		// If we don't have enough gas for any further transactions then we're done
		if w.currentCtx.gasPool.Gas() < params.TxGas {
			log.WithFields(log.Fields{"have": w.currentCtx.gasPool, "want": params.TxGas}).Debug("Not enough gas for further transactions")
			break
		}

		// Start executing the transaction
		w.currentCtx.state.Prepare(tx.Hash(), common.Hash{}, w.currentCtx.tcount)
		_, err := w.commitTransaction(tx, coinbase)
		if err == nil {
			w.currentCtx.tcount++
		}
	}

	return true
}

func (w *worker) commitTransaction(tx *types.Transaction, coinbase common.Address) ([]*types.Log, error) {
	snap := w.currentCtx.state.Snapshot()

	receipt, _, err := core.ApplyTransaction(w.config, w.chain, &coinbase, w.currentCtx.gasPool, w.currentCtx.state, w.currentCtx.header, tx, &w.currentCtx.header.GasUsed, vm.Config{})
	if err != nil {
		w.currentCtx.state.RevertToSnapshot(snap)
		return nil, err
	}
	w.currentCtx.txs = append(w.currentCtx.txs, tx)
	w.currentCtx.receipts = append(w.currentCtx.receipts, receipt)

	return receipt.Logs, nil
}

func (w *worker) blockinfo(desc string, block *types.Block) {
	log.WithFields(log.Fields{
		"number":     block.NumberU64(),
		"hash":       block.Hash().String(),
		"parentHash": block.ParentHash().String(),
		"stateRoot":  block.StateRoot().String(),
		"difficulty": block.Difficulty(),
		"gasLimit":   block.GasLimit(),
		"gasUsed":    block.GasUsed(),
		"nonce":      block.Nonce(),
		"tx":         block.Txs.Len(),
	}).Info(desc)
}

var (
	GasLimitBoundDivisor uint64 = 1024                 // The bound divisor of the gas limit, used in update calculations.
	MinGasLimit          uint64 = 5000                 // Minimum the gas limit may ever be.
	TargetGasLimit              = core.GenesisGasLimit // The artificial target
)

// CalcGasLimit computes the gas limit of the next block after parent.
// This is miner strategy, not consensus protocol.
func CalcGasLimit(parent *types.Block) uint64 {
	// contrib = (parentGasUsed * 3 / 2) / 1024
	contrib := (parent.GasUsed() + parent.GasUsed()/2) / GasLimitBoundDivisor

	// decay = parentGasLimit / 1024 -1
	decay := parent.GasLimit()/GasLimitBoundDivisor - 1

	/*
		strategy: gasLimit of block-to-mine is set based on parent's
		gasUsed value.  if parentGasUsed > parentGasLimit * (2/3) then we
		increase it, otherwise lower it (or leave it unchanged if it's right
		at that usage) the amount increased/decreased depends on how far away
		from parentGasLimit * (2/3) parentGasUsed is.
	*/
	limit := parent.GasLimit() - decay + contrib
	if limit < MinGasLimit {
		limit = MinGasLimit
	}
	// however, if we're now below the target (TargetGasLimit) we increase the
	// limit as much as we can (parentGasLimit / 1024 -1)
	if limit < TargetGasLimit {
		limit = parent.GasLimit() + decay
		if limit > TargetGasLimit {
			limit = TargetGasLimit
		}
	}
	return limit
}
