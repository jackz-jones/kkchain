package core

import (
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/params"

	log "github.com/sirupsen/logrus"
	"sync"
	"time"
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

// Process processes the state changes according to the rules by running
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

	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		statedb.Prepare(tx.Hash(), block.Hash(), i)
		receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, statedb, header, tx, usedGas, cfg)
		if err != nil {
			return nil, nil, 0, err
		}

		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.bc.engine.Finalize(p.bc, statedb, block)

	return receipts, allLogs, *usedGas, nil
}

func (p *StateParallelProcessor) ApplyTransactions(txs types.Transactions, header *types.Header, statedb *state.StateDB) (types.Transactions, types.Receipts, error) {

	var executedTx types.Transactions
	var receipts types.Receipts

	gasPool := new(GasPool).AddGas(header.GasLimit)

	resultCh := make(chan *ApplyResult, len(txs))

	//TODO：limit parallel number if needed
	var pend sync.WaitGroup
	for _, tx := range txs {
		pend.Add(1)

		go func() {
			defer pend.Done()
			p.applyTx(gasPool, statedb, header, tx, resultCh)
		}()
	}
	pend.Wait()
	close(resultCh)

	for result := range resultCh {
		if result.tx != nil {
			executedTx = append(executedTx, result.tx)
			receipts = append(receipts, result.receipt)
			header.GasUsed += result.gas
		}
	}

	return executedTx, receipts, nil
}

type ApplyResult struct {
	tx      *types.Transaction
	receipt *types.Receipt
	gas     uint64
	err     error
}

func (p *StateParallelProcessor) applyTx(gp *GasPool, stateDb *state.StateDB, header *types.Header, tx *types.Transaction, resultCh chan *ApplyResult) {

	result := make(chan *ApplyResult)
	go func() {
		txState := stateDb.Copy()
		usedGas := new(uint64)

		// If we don't have enough gas for any further transactions then we're done
		if gp.Gas() < params.TxGas {
			log.WithFields(log.Fields{"have": gp, "want": params.TxGas}).Debug("Not enough gas for further transactions")
			result <- &ApplyResult{
				gas: 0,
				err: ErrAvailGasTooLow,
			}
			return
		}

		// Start executing the transaction
		txState.Prepare(tx.Hash(), common.Hash{}, 0)

		snap := txState.Snapshot()
		receipt, gas, err := ApplyTransaction(p.config, p.bc, &header.Miner, gp, txState, header, tx, usedGas, vm.Config{})
		if err != nil {
			txState.RevertToSnapshot(snap)
			result <- &ApplyResult{
				gas: 0,
				err: err,
			}
			return
		}

		result <- &ApplyResult{
			tx:      tx,
			receipt: receipt,
			gas:     gas,
		}
	}()

	select {
	case <-time.After(10 * time.Second):
		log.Error(ErrExecuteTimeout)
		resultCh <- &ApplyResult{}
		return
	case res := <-result:
		if res.err != nil {
			log.Error(res.err)
			resultCh <- &ApplyResult{}
			return
		}

		//TODO: merge to stateDb, bypass collect conflicts and dependencies

		//if don't conflict, set to channel
		resultCh <- res
	}

}
