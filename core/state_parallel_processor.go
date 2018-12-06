package core

import (
	"errors"
	"fmt"
	"math/big"
	"sync"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/dag"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/params"

	log "github.com/sirupsen/logrus"
)

//思考：为了发挥并发执行交易的优势，同时减少statedb的复制，应该就可能把tx集合分成多条很长的队列集合，
//这样每个队列集合可以复用同一个statedb，减少statedb复制的次数
var (
	exexuteParallel = 10000
	// ParallelNum num
	//problem:setup different concurrent coroutine will generate different results,
	//        so we must the num of  coroutine by the number of transactions executed concurrently for the first time
	// setup ParallelNum 0 means ParallelNum will be config by dispathcer
	ParallelNum = 0
	// VerifyExecutionTimeout 0 means unlimited
	VerifyExecutionTimeout = 0
	ErrInvalidDagBlock     = errors.New("block's dag is incorrect")
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateParallelProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateParallelProcessor(config *params.ChainConfig, bc *BlockChain) *StateParallelProcessor {
	return &StateParallelProcessor{
		config: config,
		bc:     bc,
	}
}

type verifyCtx struct {
	mergeCh   chan bool
	block     *types.Block
	lastState *state.StateDB
}

// Process  processes the state changes parallel according to the rules by running
// the transaction messages using the statedb and applying any rewards to
// the processor (coinbase).
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateParallelProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receiptsArray = make([]*types.Receipt, block.Txs.Len())
		usedGasArray  = make([]*(uint64), block.Txs.Len())
		receipts      types.Receipts
		usedGas       = new(uint64)
		header        = block.Header()
		allLogs       []*types.Log
		gp            = new(GasPool).AddGas(block.GasLimit())
	)

	//Parallel execution tx by dag
	context := &verifyCtx{
		mergeCh: make(chan bool, 1),
		block:   block,
	}
	count := 0

	dispatcher := dag.NewDispatcher(&block.Header().ExecutionDag, uint64(ParallelNum), int64(VerifyExecutionTimeout), context, func(node *dag.Node, context interface{}) (interface{}, error) {
		// TODO: if system occurs, the block won't be retried any more
		ctx := context.(*verifyCtx)
		block := ctx.block
		lastStateDb := node.LastState()

		idx := node.GetIndex()
		if idx < 0 || idx > uint64(block.Txs.Len()-1) {
			return nil, ErrInvalidDagBlock
		}
		tx := block.Txs[idx] //通过这样的方式来取到相应的tx来执行

		usedGasArray[idx] = new(uint64)

		var txStateDb *state.StateDB
		if lastStateDb != nil {
			count++
			txStateDb = lastStateDb.(*state.StateDB)
		} else {
			//txStateDb = statedb.CopyWithStateObjects()
			txStateDb = statedb.Copy()
		}

		//add version info
		txStateDb.SetSnapshotVersion(statedb.GetSnapshotVersion())
		txStateDb.Prepare(tx.Hash(), block.Hash(), int(idx))

		receipt, _, err := ApplyTransaction(p.config, p.bc, &header.Miner, gp, txStateDb, header, tx, usedGasArray[idx], cfg)

		if err != nil {
			fmt.Printf("ApplyTransaction err:%#v\n", err)
			return nil, err
		}
		receiptsArray[idx] = receipt

		/*
			利用snapshot实现版本控制
			 第一次merge txdb的初始version为0，statedb的version为0，执行完tx后，txdb的version加1=1，遇到冲突时可以更新，更新完statedb的version为1
			 如果有并发merge时，txdb的初始version为0，statedb的version为1，执行完tx后，txdb的version加1=1，
			 因为txdb的version并没有大于statedb的version，所以遇到冲突时报错
		*/
		//errMerge := statedb.MergeStateDB(txStateDb)
		excepts := make(map[common.Address]*big.Int)
		from, _ := types.Sender(types.NewInitialSigner(p.config.ChainID), tx)
		msg, _ := tx.AsMessage(types.NewInitialSigner(p.config.ChainID))
		if (from != header.Miner) && (msg.To() == nil || *msg.To() != header.Miner) {
			excepts[header.Miner] = txStateDb.GetBalance(header.Miner)
		}
		errMerge := statedb.Merge(txStateDb, excepts)
		if errMerge != nil {
			fmt.Printf("MergeTxStateDB failed!err:%v\n", errMerge)
			return nil, errMerge
		}

		return txStateDb, nil
	})

	if err := dispatcher.Run(); err != nil {
		log.Info("Failed to verify txs in block.\n" +
			"ExecutionDag:" + block.Header().ExecutionDag.String() + "\n" +
			"err:" + err.Error() + "\n")
		return nil, nil, 0, err
	}
	fmt.Printf("@@@@@@@@@@@@@@@!!!总共引用上次的stateDB的次数为 %d\n", count)

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.bc.engine.Finalize(p.bc, statedb, block)

	//merge receipts,allLogs
	for i := 0; i < block.Txs.Len(); i++ {
		//fmt.Printf("22222222------append receipt[%d]: %#v,%#v,%#v\n", i, receiptsArray[i].TxHash.String(), receiptsArray[i].GasUsed, receiptsArray[i].CumulativeGasUsed)
		receipts = append(receipts, receiptsArray[i])
		allLogs = append(allLogs, receiptsArray[i].Logs...)
		*usedGas += *usedGasArray[i]
	}
	//fmt.Printf("22222222------sProcess receipts %#v \n", receipts)

	//设置usedGas费用
	block.Header().GasUsed = *usedGas

	return receipts, allLogs, *usedGas, nil
}

//merge per account
func (p *StateParallelProcessor) ApplyTransactions(txMaps map[common.Address]types.Transactions, count int, header *types.Header, statedb *state.StateDB) (types.Transactions, types.Receipts, error) {

	var executedTx types.Transactions
	var receipts types.Receipts

	parallel := exexuteParallel
	if exexuteParallel > len(txMaps) {
		parallel = len(txMaps)
	}

	parallelCh := make(chan bool, parallel)
	gasPool := new(GasPool).AddGas(header.GasLimit)
	lock := &sync.Mutex{}
	var pend sync.WaitGroup

	pending := make(map[common.Address]types.Transactions)
	pendingCount := 0

	//log.Info("gas pool:", gasPool)
	dag := dag.NewDag()
	lastTxids := make(map[common.Address]common.Hash)

	for acc, txs := range txMaps {
		parallelCh <- true
		pend.Add(1)

		go func(acc common.Address, accTxs types.Transactions) {
			defer func() {
				<-parallelCh
			}()

			defer pend.Done()

			txState := statedb.Copy()
			usedGas := new(uint64)

			lock.Lock()
			gp := new(GasPool).AddGas(gasPool.Gas())
			lock.Unlock()

			var accExecutedTx types.Transactions
			var accReceipts types.Receipts

			excepts := make(map[common.Address]*big.Int)

			// Iterate over and process the individual transactions
			for i, tx := range accTxs {
				from, _ := types.Sender(types.NewInitialSigner(p.config.ChainID), tx)
				//log.WithFields(log.Fields{"gasPool": gasPool, "tx": tx.Hash().String(), "from": from.String()}).Info("new tx prepare to execute")
				// If we don't have enough gas for any further transactions then we're done
				if gp.Gas() < params.TxGas {
					log.WithFields(log.Fields{"have": gp, "want": params.TxGas}).Debug("Not enough gas for further transactions")
					break
				}

				// Start executing the transaction
				txState.Prepare(tx.Hash(), common.Hash{}, i)
				snap := txState.Snapshot()
				receipt, gas, err := ApplyTransaction(p.config, p.bc, &header.Miner, gp, txState, header, tx, usedGas, vm.Config{})
				if err != nil {
					log.WithFields(log.Fields{"tx": tx.Hash().String(), "err": err}).Info("execute failed")
					txState.RevertToSnapshot(snap)
					break
				}
				//log.WithFields(log.Fields{"tx": tx.Hash().String()}).Info("execute success")
				receipt.CumulativeGasUsed = gas
				accExecutedTx = append(accExecutedTx, tx)
				accReceipts = append(accReceipts, receipt)

				msg, _ := tx.AsMessage(types.NewInitialSigner(p.config.ChainID))
				if (from != header.Miner) && (msg.To() == nil || *msg.To() != header.Miner) {
					excepts[header.Miner] = txState.GetBalance(header.Miner)
				}

			}

			lock.Lock()
			//other goroutine has execute over
			if err := gasPool.SubGas(*usedGas); err != nil {
				//log.WithFields(log.Fields{"acc": acc.String(), "gasPool": gasPool, "used gas": *usedGas, "err": err}).Info("execute error")
				lock.Unlock()
				return
			}

			//log.WithFields(log.Fields{"acc": acc.String(), "gasPool": gasPool, "used gas": *usedGas}).Info("gas pool")

			//merge to stateDb, bypass collect conflicts and dependencies
			err := statedb.Merge(txState, excepts)
			if err != nil {
				log.WithFields(log.Fields{"acc": acc.String(), "nonce": statedb.GetNonce(acc)}).Info("merge failed")
				pending[acc] = accTxs
				pendingCount += accTxs.Len()
				lock.Unlock()
				return
			}
			//log.WithFields(log.Fields{"acc": acc.String(), "nonce": statedb.GetNonce(acc)}).Info("merge success")

			//if don't conflict, execute successful, then append to returns
			executedTx = append(executedTx, accExecutedTx...)
			receipts = append(receipts, accReceipts...)
			header.GasUsed += *usedGas
			//fmt.Printf("**********header.GasUsed:%d\n", header.GasUsed)

			//add dag
			var last common.Hash
			for i, tx := range accExecutedTx {
				txid := tx.Hash()
				dag.AddNode(txid)
				if i != 0 {
					dag.AddEdge(last, txid)
				}
				last = txid
			}
			lastTxids[acc] = last

			lock.Unlock()

		}(acc, txs)
	}

	pend.Wait()

	close(parallelCh)

	//serially execute txs in pending tx map
	log.WithFields(log.Fields{"pending": pendingCount}).Info("serially execute")
	//for a, acctxs := range pending {
	//	for _, tx := range acctxs {
	//		log.WithFields(log.Fields{"from": a.String(), "tx": tx.Hash().String(), "nonce": statedb.GetNonce(a)}).Info("serially execute")
	//	}
	//}

	if pendingCount > 0 {
		processor := NewStateProcessor(p.config, p.bc)
		pendingTxs, pendingReceipts, err := processor.ApplyTransactions(pending, pendingCount, header, statedb)
		if err == nil {
			executedTx = append(executedTx, pendingTxs...)
			receipts = append(receipts, pendingReceipts...)
			//add dag
			var last common.Hash
			for i, tx := range pendingTxs {
				txid := tx.Hash()
				dag.AddNode(txid)
				if i == 0 {
					for _, node := range lastTxids {
						dag.AddEdge(node, txid)
					}
				} else {
					dag.AddEdge(last, txid)
				}
				last = txid
			}
		}
	}

	header.ExecutionDag = *dag
	//log.WithFields(log.Fields{"executed_tx_count": executedTx.Len(), "gas pool": gasPool}).Info("ApplyTransactions over")
	return executedTx, receipts, nil
}

//merge per transaction
func (p *StateParallelProcessor) ApplyTransactions2(txMaps map[common.Address]types.Transactions, count int, header *types.Header, statedb *state.StateDB) (types.Transactions, types.Receipts, error) {

	var executedTx types.Transactions
	var receipts types.Receipts

	parallel := exexuteParallel
	if exexuteParallel > len(txMaps) {
		parallel = len(txMaps)
	}

	parallelCh := make(chan bool, parallel)
	gasPool := new(GasPool).AddGas(header.GasLimit)
	lock := &sync.Mutex{}
	var pend sync.WaitGroup

	pending := make(map[common.Address]types.Transactions)
	pendingCount := 0

	//log.Info("gas pool:", gasPool)
	dag := dag.NewDag()
	lastTxids := make(map[common.Address]common.Hash)

	for acc, txs := range txMaps {
		parallelCh <- true
		pend.Add(1)

		go func(acc common.Address, accTxs types.Transactions) {
			defer func() {
				<-parallelCh
			}()

			defer pend.Done()

			//start := time.Now()
			txState := statedb.Copy()
			//end := time.Now()
			//log.Infof("CopyAndReset cost: %v, executedTx: %d\n", end.Sub(start), executedTx.Len())
			usedGas := new(uint64)

			excepts := make(map[common.Address]*big.Int)

			// Iterate over and process the individual transactions
			for i, tx := range accTxs {
				from, _ := types.Sender(types.NewInitialSigner(p.config.ChainID), tx)
				lock.Lock()
				gp := new(GasPool).AddGas(gasPool.Gas())
				lock.Unlock()

				//log.WithFields(log.Fields{"gasPool": gasPool, "tx": tx.Hash().String(), "from": from.String()}).Info("new tx prepare to execute")
				// If we don't have enough gas for any further transactions then we're done
				if gp.Gas() < params.TxGas {
					log.WithFields(log.Fields{"have": gasPool, "want": params.TxGas}).Debug("Not enough gas for further transactions")
					break
				}

				msg, _ := tx.AsMessage(types.NewInitialSigner(p.config.ChainID))
				if (from != header.Miner) && (msg.To() == nil || *msg.To() != header.Miner) {
					excepts[header.Miner] = txState.GetBalance(header.Miner)
				}

				// Start executing the transaction
				txState.Prepare(tx.Hash(), common.Hash{}, i)
				snap := txState.Snapshot()
				receipt, gas, err := ApplyTransaction(p.config, p.bc, &header.Miner, gp, txState, header, tx, usedGas, vm.Config{})
				if err != nil {
					log.WithFields(log.Fields{"tx": tx.Hash().String(), "err": err}).Info("execute failed")
					txState.RevertToSnapshot(snap)
					break
				}
				//log.WithFields(log.Fields{"tx": tx.Hash().String()}).Info("execute success")

				lock.Lock()
				//other goroutine has execute over
				if err := gasPool.SubGas(gas); err != nil {
					log.WithFields(log.Fields{"acc": acc.String(), "gasPool": gasPool, "used gas": *usedGas, "err": err}).Info("execute error")
					lock.Unlock()
					return
				}

				//log.WithFields(log.Fields{"acc": acc.String(), "gasPool": gasPool, "used gas": *usedGas}).Info("gas pool")

				//start1 := time.Now()
				//merge to stateDb, bypass collect conflicts and dependencies
				err = statedb.Merge(txState, excepts)
				//end1 := time.Now()
				//log.Infof("Merge cost: %v\n", end1.Sub(start1))
				if err != nil {
					//log.WithFields(log.Fields{"acc": acc.String(), "nonce": statedb.GetNonce(acc), "tx": tx.Hash().String()}).Info("merge failed")
					pending[acc] = accTxs[i:]
					pendingCount += accTxs[i:].Len()
					lock.Unlock()
					return
				}
				//log.WithFields(log.Fields{"acc": acc.String(), "nonce": statedb.GetNonce(acc), "tx": tx.Hash().String()}).Info("merge success")

				//if don't conflict, execute successful, then append to returns
				executedTx = append(executedTx, tx)
				receipts = append(receipts, receipt)
				header.GasUsed += gas
				fmt.Printf("header.GasUsed:%d\n", header.GasUsed)

				//add dag
				txid := tx.Hash().String()
				dag.AddNode(txid)
				if last, exist := lastTxids[acc]; exist {
					dag.AddEdge(last.String(), txid)
				}
				lastTxids[acc] = tx.Hash()

				lock.Unlock()

			}

		}(acc, txs)
	}

	pend.Wait()

	close(parallelCh)

	//serially execute txs in pending tx map
	log.WithFields(log.Fields{"pending": pendingCount}).Info("serially execute")
	//for a, acctxs := range pending {
	//	for _, tx := range acctxs {
	//		log.WithFields(log.Fields{"from": a.String(), "tx": tx.Hash().String(), "nonce": statedb.GetNonce(a)}).Info("serially execute")
	//	}
	//}

	if pendingCount > 0 {
		processor := NewStateProcessor(p.config, p.bc)
		pendingTxs, pendingReceipts, err := processor.ApplyTransactions(pending, pendingCount, header, statedb)
		if err == nil {
			executedTx = append(executedTx, pendingTxs...)
			receipts = append(receipts, pendingReceipts...)
			//add dag
			var last string
			for i, tx := range pendingTxs {
				txid := tx.Hash().String()
				dag.AddNode(txid)
				if i == 0 {
					for _, node := range lastTxids {
						dag.AddEdge(node.String(), txid)
					}
				} else {
					dag.AddEdge(last, txid)
				}
				last = txid
			}
		}
	}

	header.ExecutionDag = *dag
	//log.WithFields(log.Fields{"executed_tx_count": executedTx.Len(), "gas pool": gasPool}).Info("ApplyTransactions over")
	return executedTx, receipts, nil
}
