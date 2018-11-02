package consensus

import (
	"math/big"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
)

type Context struct {
	//parent block

	//chain reader

	//state

	//now time

	//validators

	//self

	//proposer

}

// ChainReader defines a small collection of methods needed to access the local
// blockchain during header verification.
type ChainReader interface {

	// CurrentHeader retrieves the current header from the local chain.
	CurrentHeader() *types.Header

	// GetHeader retrieves a block header from the database by hash and number.
	GetHeader(hash common.Hash, number uint64) *types.Header

	// GetHeaderByNumber retrieves a block header from the database by number.
	GetHeaderByNumber(number uint64) *types.Header

	// GetHeaderByHash retrieves a block header from the database by its hash.
	GetHeaderByHash(hash common.Hash) *types.Header

	// GetBlock retrieves a block from the database by hash and number.
	GetBlock(hash common.Hash, number uint64) *types.Block
}

// Engine is an algorithm agnostic consensus engine.
type Engine interface {
	Initialize(chain ChainReader, header *types.Header) error
	Finalize(chain ChainReader, state *state.StateDB, block *types.Block) error

	Execute(chain ChainReader, block *types.Block, stop <-chan struct{}) (*types.Block, error)
	PostExecute(chain ChainReader, block *types.Block) error

	VerifyHeader(chain ChainReader, header *types.Header) error
	Verify(block *types.Block) error
	Close() error

	CalcDifficulty(chain ChainReader, time uint64, parent *types.Header) *big.Int

	// Author retrieves the address of the account that minted the given
	// block, which may be different from the header's coinbase if a consensus
	// engine is based on signatures.
	Author(header *types.Header) (common.Address, error)
}

type Network interface {
	Broadcast()
}
