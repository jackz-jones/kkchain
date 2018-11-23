package core

import (
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/crypto"
	"github.com/invin/kkchain/params"

	log "github.com/sirupsen/logrus"
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateProcessor(config *params.ChainConfig, bc *BlockChain) *StateProcessor {
	return &StateProcessor{
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
func (p *StateProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error) {
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

func (p *StateProcessor) ApplyTransactions(txMaps map[common.Address]types.Transactions, count int, header *types.Header, statedb *state.StateDB) (types.Transactions, types.Receipts, error) {

	var executedTx types.Transactions
	var receipts types.Receipts
	gasPool := new(GasPool).AddGas(header.GasLimit)
	tcount := 0

	txs := make(types.Transactions, 0, count)
	for _, acctxs := range txMaps {
		for _, tx := range acctxs {
			txs = append(txs, tx)
		}
	}

	for _, tx := range txs {
		// If we don't have enough gas for any further transactions then we're done
		if gasPool.Gas() < params.TxGas {
			log.WithFields(log.Fields{"have": gasPool, "want": params.TxGas}).Debug("Not enough gas for further transactions")
			break
		}

		from, _ := types.Sender(types.NewInitialSigner(p.config.ChainID), tx)

		// Start executing the transaction
		statedb.Prepare(tx.Hash(), common.Hash{}, tcount)

		snap := statedb.Snapshot()

		receipt, _, err := ApplyTransaction(p.config, p.bc, &header.Miner, gasPool, statedb, header, tx, &header.GasUsed, vm.Config{})
		switch err {
		case ErrGasLimitReached:
			// Pop the current out-of-gas transaction without shifting in the next from the account
			log.Warningf("Gas limit exceeded for current block", "sender", from)
			statedb.RevertToSnapshot(snap)
		case ErrNonceTooLow:
			// New head notification data race between the transaction pool and miner, shift
			log.Warningf("Skipping transaction with low nonce", "sender", from, "nonce", tx.Nonce())
			statedb.RevertToSnapshot(snap)
		case ErrNonceTooHigh:
			// Reorg notification data race between the transaction pool and miner, skip account =
			log.Warningf("Skipping account with hight nonce", "sender", from, "nonce", tx.Nonce())
			statedb.RevertToSnapshot(snap)

		case nil:
			// Everything ok, collect the logs and shift in the next transaction from the same account
			executedTx = append(executedTx, tx)
			receipts = append(receipts, receipt)

			tcount++

		default:
			// Strange error, discard the transaction and get the next in line (note, the
			// nonce-too-high clause will prevent us from executing in vain).
			log.Debug("Transaction failed, account skipped", "hash", tx.Hash(), "err", err)
			statedb.RevertToSnapshot(snap)
		}

	}

	return executedTx, receipts, nil
}

// ApplyTransaction attempts to apply a transaction to the given state database
// and uses the input parameters for its environment. It returns the receipt
// for the transaction, gas used and an error if the transaction failed,
// indicating the block was invalid.
func ApplyTransaction(config *params.ChainConfig, bc ChainContext, author *common.Address, gp *GasPool, statedb *state.StateDB, header *types.Header, tx *types.Transaction, usedGas *uint64, cfg vm.Config) (*types.Receipt, uint64, error) {
	msg, err := tx.AsMessage(types.NewInitialSigner(config.ChainID))
	if err != nil {
		return nil, 0, err
	}
	// Create a new context to be used in the EVM environment
	context := NewEVMContext(msg, header, bc, author)
	// Create a new environment which holds all relevant information
	// about the transaction and calling mechanisms.
	vmenv := vm.NewEVM(context, statedb, config, cfg)
	// Apply the transaction to the current state (included in the env)
	_, gas, failed, err := ApplyMessage(vmenv, msg, gp)
	if err != nil {
		return nil, 0, err
	}
	// Update the state with pending changes
	var root []byte
	statedb.Finalise(true)

	*usedGas += gas

	// Create a new receipt for the transaction, storing the intermediate root and gas used by the tx
	// based on the eip phase, we're passing wether the root touch-delete accounts.
	receipt := types.NewReceipt(root, failed, *usedGas)
	receipt.TxHash = tx.Hash()
	receipt.GasUsed = gas
	// if the transaction created a contract, store the creation address in the receipt.
	if msg.To() == nil {
		receipt.ContractAddress = crypto.CreateAddress(vmenv.Context.Origin, tx.Nonce())
	}
	// Set the receipt logs and create a bloom for filtering
	receipt.Logs = statedb.GetLogs(tx.Hash())
	receipt.Bloom = types.CreateBloom(types.Receipts{receipt})

	return receipt, gas, err
}
