package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/dag"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/crypto"
	"github.com/invin/kkchain/params"

	log "github.com/sirupsen/logrus"
)

var (
	// ParallelNum num
	ParallelNum = 2
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
	mergeCh chan bool
	block   *types.Block
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
		receipts types.Receipts
		usedGas  = new(uint64)
		header   = block.Header()
		allLogs  []*types.Log
		gp       = new(GasPool).AddGas(block.GasLimit())
	)

	//Parallel execution tx by dag
	context := &verifyCtx{
		mergeCh: make(chan bool, 1),
		block:   block,
	}
	dispatcher := dag.NewDispatcher(block.ExecutionDag, ParallelNum, int64(VerifyExecutionTimeout), context, func(node *dag.Node, context interface{}) error {
		// TODO: if system occurs, the block won't be retried any more
		ctx := context.(*verifyCtx)
		block := ctx.block
		mergeCh := ctx.mergeCh

		idx := node.Index()
		if idx < 0 || idx > block.Txs.Len()-1 {
			return ErrInvalidDagBlock
		}
		tx := block.Txs[idx]

		log.Debug("execute tx." + tx.Hash().String())

		mergeCh <- true
		//execute tx
		txStateDb := statedb.Copy()
		txStateDb.Prepare(tx.Hash(), block.Hash(), idx)
		<-mergeCh

		receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, txStateDb, header, tx, usedGas, cfg)
		if err != nil {
			return err
		}

		mergeCh <- true
		//merge statedb,receipts,allLogs,totalusedGas
		statedb.MergeStateObjects(txStateDb.GetStateObjects())
		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
		<-mergeCh

		return nil
	})

	if err := dispatcher.Run(); err != nil {
		log.Info("Failed to verify txs in block.\n" +
			"ExecutionDag:" + block.ExecutionDag.String() + "\n" +
			"err:" + err.Error() + "\n")
		return nil, nil, 0, err
	}

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.bc.engine.Finalize(p.bc, statedb, block)
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		key3, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7b")
		key4, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7c")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		addr3   = crypto.PubkeyToAddress(key3.PublicKey)
		addr4   = crypto.PubkeyToAddress(key4.PublicKey)
	)
	fmt.Printf("Process***** add1 balance  : %#v \n ", statedb.GetBalance(addr1))
	fmt.Printf("Process***** add2 balance  : %#v \n ", statedb.GetBalance(addr2))
	fmt.Printf("Process***** add3 balance  : %#v \n ", statedb.GetBalance(addr3))
	fmt.Printf("Process***** add4 balance  : %#v \n ", statedb.GetBalance(addr4))

	return receipts, allLogs, *usedGas, nil
}

func (p *StateParallelProcessor) ApplyTransactions(txMaps map[common.Address]types.Transactions, count int, header *types.Header, statedb *state.StateDB) (types.Transactions, types.Receipts, error) {

	var executedTx types.Transactions
	var receipts types.Receipts

	parallel := ParallelNum
	if ParallelNum > len(txMaps) {
		parallel = len(txMaps)
	}

	parallelCh := make(chan bool, parallel)
	gasPool := new(GasPool).AddGas(header.GasLimit)
	lock := &sync.RWMutex{}
	var pend sync.WaitGroup

	for _, txs := range txMaps {
		parallelCh <- true

		lock.Lock()
		gp := new(GasPool).AddGas(gasPool.Gas())
		lock.Unlock()

		pend.Add(1)
		go func(accTxs types.Transactions) {
			defer func() {
				<-parallelCh
			}()

			defer pend.Done()

			txState := statedb.Copy()
			usedGas := new(uint64)

			// Iterate over and process the individual transactions
			for i, tx := range accTxs {

				log.WithFields(log.Fields{"gasPool": gasPool, "tx": tx.Hash().String()}).Info("new tx prepare to execute")
				// If we don't have enough gas for any further transactions then we're done
				if gasPool.Gas() < params.TxGas {
					log.WithFields(log.Fields{"have": gasPool, "want": params.TxGas}).Debug("Not enough gas for further transactions")
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
				log.WithFields(log.Fields{"tx": tx.Hash().String()}).Info("execute success")

				//TODO: merge to stateDb, bypass collect conflicts and dependencies

				lock.Lock()
				//TODO: if conflict, the remain txs save to pending txmap

				//if don't conflict, execute successful, then append to returns
				executedTx = append(executedTx, tx)
				receipts = append(receipts, receipt)
				header.GasUsed += gas
				gasPool.SubGas(gas)

				//TODO：add dag

				lock.Unlock()

			}

		}(txs)
	}

	pend.Wait()

	close(parallelCh)

	//TODO：serially execute txs in pending txmap

	log.WithFields(log.Fields{"executed_tx_count": executedTx.Len()}).Info("ApplyTransactions over")
	return executedTx, receipts, nil
}
