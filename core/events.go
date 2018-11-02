package core

import (
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/types"
)

// NewTxsEvent is posted when a batch of transactions enter the transaction pool.
type NewTxsEvent struct{ Txs types.Transactions }

// PendingLogsEvent is posted pre mining and notifies of pending logs.
type PendingLogsEvent struct {
	Logs []*types.Log
}

// NewMinedBlockEvent is posted when a block has been imported.
type NewMinedBlockEvent struct{ Block *types.Block }

// RemovedLogsEvent is posted when a reorg happens
type RemovedLogsEvent struct{ Logs []*types.Log }

// ChainEvent represents the chain event
type ChainEvent struct {
	Block *types.Block
	Hash  common.Hash
	Logs  []*types.Log
}

// ChainSideEvent represents the side events
type ChainSideEvent struct {
	Block *types.Block
}

// ChainHeadEvent represents the head events
type ChainHeadEvent struct{ Block *types.Block }

// StartEvent is triggered when downloading is started
type StartEvent struct{}

// DoneEvent is triggered when downloading is done
type DoneEvent struct{ Err error }
