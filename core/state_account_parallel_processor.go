package core

import (
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

// StateAccountParallelProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateAccountParallelProcessor implements Processor.
type StateAccountParallelProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateAccountParallelProcessor(config *params.ChainConfig, bc *BlockChain) *StateAccountParallelProcessor {
	return &StateAccountParallelProcessor{
		config: config,
		bc:     bc,
	}
}

// Process  processes the state changes parallel according to the rules by running
// the transaction messages using the statedb and applying any rewards to
// the processor (coinbase).
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateAccountParallelProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receipts     types.Receipts
		header       = block.Header()
		allLogs      []*types.Log
		totalGasUsed = new(uint64)
		gp           = new(GasPool).AddGas(block.GasLimit())
	)
	//fmt.Printf("############3333333 header.GasUsed:%d", header.GasUsed)
	//1.按账户去划分，先并发执行能执行的tx
	//根据dag构造并发执行的txmap
	totalTx := map[int]*types.Transaction{}
	for i, tx := range block.Txs {
		totalTx[i] = tx
	}
	vertices := header.ExecutionDag.GetNodes()

	parallelTxMaps, sequenceTxMaps := GetParrlelAndSequenceTxMaps(vertices, block)

	_, receiptsArray, totalGasUsed, err := p.ParallelExecuteApplyTransactions(parallelTxMaps, 0, header, statedb)
	if err != nil {
		return nil, nil, 0, err
	}
	receipts = append(receipts, receiptsArray...)

	//2.串行执行剩下的tx
	//根据dag构造并发执行的txmap
	if len(sequenceTxMaps) > 0 {
		// Iterate over and process the individual transactions
		for i, tx := range block.Transactions() {
			//fmt.Println("执行串行队列")
			statedb.Prepare(tx.Hash(), block.Hash(), i)
			receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, statedb, header, tx, totalGasUsed, cfg)
			if err != nil {
				return nil, nil, 0, err
			}

			receipts = append(receipts, receipt)
			allLogs = append(allLogs, receipt.Logs...)
		}
	}

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.bc.engine.Finalize(p.bc, statedb, block)

	// for i, receipt := range receipts {
	// 	fmt.Printf("!!!!!22222------receipt[%d]: %#v,%#v,%#v, \n", i, receipt.TxHash.String(), receipt.GasUsed, receipt.CumulativeGasUsed)
	// }
	// fmt.Printf("!!!!!22222------receipts %#v \n", receipts)
	//合并allLogs
	for i := 0; i < block.Txs.Len(); i++ {
		allLogs = append(allLogs, receipts[i].Logs...)
	}

	//fmt.Printf("############444444 totalGasUsed:%d \n", totalGasUsed)

	return receipts, allLogs, *totalGasUsed, nil
}

//merge per account
func (p *StateAccountParallelProcessor) ApplyTransactions(txMaps map[common.Address]types.Transactions, count int, header *types.Header, statedb *state.StateDB) (types.Transactions, types.Receipts, error) {

	var executedTx types.Transactions
	var receipts types.Receipts

	gasPool := new(GasPool).AddGas(header.GasLimit)
	lock := &sync.Mutex{}
	var pend sync.WaitGroup

	pending := make(map[common.Address]types.Transactions)
	pendingCount := 0

	//log.Info("gas pool:", gasPool)
	dag := dag.NewDag()
	lastTxids := make(map[common.Address]common.Hash)

	for acc, txs := range txMaps {
		pend.Add(1)

		go func(acc common.Address, accTxs types.Transactions) {

			defer pend.Done()

			txState := statedb.Copy()
			usedGas := new(uint64)

			lock.Lock()
			gp := new(GasPool).AddGas(gasPool.Gas())
			lock.Unlock()

			var accExecutedTx types.Transactions
			var accReceipts types.Receipts

			excepts := make(map[common.Address]*big.Int)
			exceptMiner := true
			base := txState.GetBalance(header.Miner)

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
				if (from == header.Miner) || (msg.To() != nil && *msg.To() == header.Miner) {
					exceptMiner = false
				}
			}

			if exceptMiner {
				excepts[header.Miner] = base
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

//ParallelExecuteApplyTransactions 这个方法仅仅是并发执行并行队列，并且不会修改任何原有区块的信息，并且因为是验证阶段，无需再生成dag信息
func (p *StateAccountParallelProcessor) ParallelExecuteApplyTransactions(txMaps map[common.Address]types.Transactions, count int, header *types.Header, statedb *state.StateDB) (types.Transactions, types.Receipts, *uint64, error) {

	var executedTx types.Transactions
	var receipts types.Receipts
	totalUsedGas := new(uint64)

	// parallel := exexuteParallel
	// if exexuteParallel > len(txMaps) {
	// 	parallel = len(txMaps)
	// }

	// parallelCh := make(chan bool, parallel)
	gasPool := new(GasPool).AddGas(header.GasLimit)
	lock := &sync.Mutex{}
	var pend sync.WaitGroup

	//log.Info("gas pool:", gasPool)
	//log.Info("****parallel:", parallel)
	for acc, txs := range txMaps {
		//parallelCh <- true
		pend.Add(1)

		go func(acc common.Address, accTxs types.Transactions) {
			// defer func() {
			// 	<-parallelCh
			// }()

			defer pend.Done()

			txState := statedb.Copy()
			usedGas := new(uint64)

			lock.Lock()
			gp := new(GasPool).AddGas(gasPool.Gas())
			lock.Unlock()

			var accExecutedTx types.Transactions
			var accReceipts types.Receipts

			excepts := make(map[common.Address]*big.Int)
			exceptMiner := true
			base := txState.GetBalance(header.Miner)
			//fmt.Println("@@@@@@@@@@@@@11111111execute tx!!!")
			//fmt.Printf("!!!!txMaps.accTxs:%#v", accTxs)
			// Iterate over and process the individual transactions
			for i, tx := range accTxs {
				//fmt.Println("@@@@@@@@@@@@@execute tx!!!")
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
				// if (from != header.Miner) && (msg.To() == nil || *msg.To() != header.Miner) {
				// 	excepts[header.Miner] = txState.GetBalance(header.Miner)
				// }
				if (from == header.Miner) || (msg.To() != nil && *msg.To() == header.Miner) {
					exceptMiner = false
				}

			}

			if exceptMiner {
				excepts[header.Miner] = base
			}

			lock.Lock()
			//other goroutine has execute over
			if err := gasPool.SubGas(*usedGas); err != nil {
				log.WithFields(log.Fields{"acc": acc.String(), "gasPool": gasPool, "used gas": *usedGas, "err": err}).Info("execute error")
				lock.Unlock()
				return
			}

			//log.WithFields(log.Fields{"acc": acc.String(), "gasPool": gasPool, "used gas": *usedGas}).Info("gas pool")

			//merge to stateDb, bypass collect conflicts and dependencies
			err := statedb.Merge(txState, excepts)
			if err != nil {
				log.WithFields(log.Fields{"acc": acc.String(), "nonce": statedb.GetNonce(acc)}).Info("merge failed")
				lock.Unlock()
				return
			}
			//log.WithFields(log.Fields{"acc": acc.String(), "nonce": statedb.GetNonce(acc)}).Info("merge success")

			//if don't conflict, execute successful, then append to returns
			executedTx = append(executedTx, accExecutedTx...)
			receipts = append(receipts, accReceipts...)
			*totalUsedGas += *usedGas
			//fmt.Printf("**********header.GasUsed:%d\n", header.GasUsed)
			lock.Unlock()

		}(acc, txs)
	}

	pend.Wait()
	//close(parallelCh)

	//log.WithFields(log.Fields{"executed_tx_count": executedTx.Len(), "gas pool": gasPool}).Info("ApplyTransactions over")
	return executedTx, receipts, totalUsedGas, nil
}

func GetParrlelAndSequenceTxMaps(vertices []*dag.Node, block *types.Block) (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	parallelTxMaps := map[common.Address]types.Transactions{}
	sequenceTxMaps := map[common.Address]types.Transactions{}
	for _, node := range vertices {
		tx := block.Txs[node.Index]
		fromAddress, err := types.Sender(types.NewInitialSigner(tx.ChainId()), tx)
		if err != nil {
			log.Errorf("get fromAddress from tx failed!err:", err)
			return nil, nil
		}
		if node.ParentCounter == 0 {
			parallelTxMaps[fromAddress] = append(parallelTxMaps[fromAddress], tx)
			tmpNode := node
			for {
				if len(tmpNode.Children) == 1 {
					parallelTxMaps[fromAddress] = append(parallelTxMaps[fromAddress], block.Txs[tmpNode.Children[0].Index])
					tmpNode = tmpNode.Children[0]
				} else {
					break
				}
			}
		}
		//找出多条并发队列的汇集点，构造顺序执行的txmap
		if node.ParentCounter > 1 {
			sequenceTxMaps[fromAddress] = append(sequenceTxMaps[fromAddress], block.Txs[node.Index])
			tmpNode := node
			for {
				if len(tmpNode.Children) == 1 {
					tmptx := block.Txs[tmpNode.Index]
					tmpFromAddress, err := types.Sender(types.NewInitialSigner(tmptx.ChainId()), tmptx)
					if err != nil {
						log.Errorf("get fromAddress from tx failed!err:", err)
						return nil, nil
					}
					sequenceTxMaps[tmpFromAddress] = append(sequenceTxMaps[tmpFromAddress], block.Txs[tmpNode.Children[0].Index])
					tmpNode = tmpNode.Children[0]
				} else {
					break
				}
			}
		}
	}
	// for key, value := range parallelTxMaps {
	// 	fmt.Printf("&&&&&&parallelTxMaps key:%#v ,value:\n", key.String())
	// 	for i, v := range value {
	// 		fmt.Printf("&&&&&&parallelTxMaps  txs[%d]:txhash:%#v\n", i, v.Hash().String())
	// 	}
	// }
	// for key, value := range sequenceTxMaps {
	// 	fmt.Printf("&&&&&&sequenceTxMaps key:%#v ,value:\n", key.String())
	// 	for i, v := range value {
	// 		fmt.Printf("&&&&&&sequenceTxMaps  txs[%d]:txhash:%#v\n", i, v.Hash().String())
	// 	}
	// }
	return parallelTxMaps, sequenceTxMaps
}
