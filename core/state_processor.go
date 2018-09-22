package core

import (
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	bc *BlockChain // Canonical block chain
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(bc *BlockChain) *StateProcessor {
	return &StateProcessor{
		bc: bc,
	}
}

// Process processes the state changes according to the rules by running
// the transaction messages using the statedb and applying any rewards to both
// the processor (coinbase) and any included uncles.
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receipts types.Receipts
		usedGas  = new(uint64)
		allLogs  []*types.Log
	)

	// Iterate over and process the individual transactions
	for i, tx := range block.Transactions() {
		statedb.Prepare(tx.Hash(), block.Hash(), i)
		//TODO: apply tx
		//receipt, _, err := ApplyTransaction(p.bc, nil, gp, statedb, header, tx, usedGas, cfg)
		//if err != nil {
		//	return nil, nil, 0, err
		//}
		receipt := &types.Receipt{}

		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.bc.engine.Finalize(p.bc, statedb, block)

	return receipts, allLogs, *usedGas, nil
}
