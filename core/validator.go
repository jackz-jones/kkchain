package core

import (
	"fmt"

	"github.com/invin/kkchain/consensus"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
)

type BlockValidator struct {
	bc *BlockChain // Canonical block chain
}

// NewBlockValidator returns a new block validator which is safe for re-use
func NewBlockValidator(blockchain *BlockChain) *BlockValidator {
	validator := &BlockValidator{
		bc: blockchain, // Canonical block chain
	}
	return validator
}

func (v *BlockValidator) ValidateBody(block *types.Block) error {
	// Check whether the block's known, and if not, that it's linkable
	if v.bc.HasBlockAndState(block.Hash(), block.NumberU64()) {
		return ErrKnownBlock
	}
	if !v.bc.HasBlockAndState(block.ParentHash(), block.NumberU64()-1) {
		if !v.bc.HasBlock(block.ParentHash(), block.NumberU64()-1) {
			return consensus.ErrUnknownAncestor
		}
		return consensus.ErrPrunedAncestor
	}
	// Header validity is known at this point, check the uncles and transactions
	header := block.Header()
	if hash := types.DeriveSha(block.Transactions()); hash != header.TxRoot {
		return fmt.Errorf("transaction root hash mismatch: have %x, want %x", hash, header.TxRoot)
	}
	return nil
}

// ValidateState validates the various changes that happen after a state
// transition, such as amount of used gas, the receipt roots and the state root
// itself. ValidateState returns a database batch if the validation was a success
// otherwise nil and an error is returned.
func (v *BlockValidator) ValidateState(block, parent *types.Block, statedb *state.StateDB, receipts types.Receipts, usedGas uint64) error {
	header := block.Header()
	if block.GasUsed() != usedGas {
		return fmt.Errorf("invalid gas used (remote: %d local: %d)", block.GasUsed(), usedGas)
	}
	// Validate the received block's bloom with the one derived from the generated receipts.
	// For valid blocks this should always validate to true.
	rbloom := types.CreateBloom(receipts)
	if rbloom != header.Bloom {
		return fmt.Errorf("invalid bloom (remote: %x  local: %x)", header.Bloom, rbloom)
	}
	// Tre receipt Trie's root (R = (Tr [[H1, R1], ... [Hn, R1]]))
	receiptSha := types.DeriveSha(receipts)
	if receiptSha != header.ReceiptRoot {
		return fmt.Errorf("invalid receipt root hash (remote: %x local: %x)", header.ReceiptRoot, receiptSha)
	}
	// Validate the state root against the received state root and throw
	// an error if they don't match.
	if root := statedb.IntermediateRoot(true); header.StateRoot != root {
		return fmt.Errorf("invalid merkle root (remote: %x local: %x)", header.StateRoot, root)
	}
	return nil
}
