package core

import (
	"fmt"
	"sync"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/crypto"
	"github.com/invin/kkchain/params"
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
		//TODO: apply tx
		receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, statedb, header, tx, usedGas, cfg)
		if err != nil {
			return nil, nil, 0, err
		}
		//receipt := &types.Receipt{}

		receipts = append(receipts, receipt)
		allLogs = append(allLogs, receipt.Logs...)
	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.bc.engine.Finalize(p.bc, statedb, block)

	return receipts, allLogs, *usedGas, nil
}

// Process processes the state changes according to the rules by running
// the transaction messages using the statedb and applying any rewards to
// the processor (coinbase).
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateProcessor) ProcessConcurrent(routineNum int, block *types.Block, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receiptsArray = make([]types.Receipts, routineNum)
		receipts      types.Receipts
		usedGasArray  = make([]*uint64, routineNum)
		totalusedGas  = new(uint64)
		header        = block.Header()
		allLogsArray  = make([][]*types.Log, routineNum)
		allLogs       []*types.Log
		gp            = new(GasPool).AddGas(block.GasLimit())
		retrunError   error
		txs           = block.Transactions()
	)

	// Iterate over and process the individual transactions
	wg := sync.WaitGroup{}
	wg.Add(routineNum)
	executeTxs := func(txs types.Transactions, receipts types.Receipts, allLogs []*types.Log, usedGasArray []*uint64, index int) {
		for i, tx := range txs {
			statedb.Prepare(tx.Hash(), block.Hash(), i)
			fmt.Printf("tx.nonce : %d \n", tx.Nonce())
			//TODO: apply tx
			receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, statedb, header, tx, usedGasArray[index], cfg)
			if err != nil {
				receipts = nil
				allLogs = nil
				*totalusedGas = 0
				retrunError = err
				fmt.Printf("ApplyTransaction error : %s \n", err)
				return
			}
			//receipt := &types.Receipt{}

			receipts = append(receipts, receipt)
			allLogs = append(allLogs, receipt.Logs...)
		}
		wg.Done()
	}
	for j := 0; j < routineNum; j++ {
		if j == routineNum-1 {
			go executeTxs(txs[(j+1)*routineNum:], receiptsArray[j], allLogsArray[j], usedGasArray, j)
		} else {
			go executeTxs(txs[j*routineNum:(j+1)*routineNum], receiptsArray[j], allLogsArray[j], usedGasArray, j)
		}

	}
	wg.Wait()
	if retrunError != nil {
		fmt.Printf("executeTxs error : %s \n", retrunError)
	}
	//combine receipts,allLogs and totalGas
	for j := 0; j < routineNum; j++ {
		receipts = append(receipts, receiptsArray[j]...)
		allLogs = append(allLogs, allLogsArray[j]...)
		*totalusedGas = *totalusedGas + *usedGasArray[j]
	}
	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.bc.engine.Finalize(p.bc, statedb, block)

	return receipts, allLogs, *totalusedGas, nil
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
	// statedb.Finalise(true)   //cancel Finalise for concurrent test .add by lmh 20181101

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
