package core

import (
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/event"
	"sync"
)

type TxPool struct {
	txFeed event.Feed
	scope  event.SubscriptionScope

	mu *sync.RWMutex

	pending map[common.Address]*txList // All currently processable transactions
}

func NewTxPool() *TxPool {
	return &TxPool{
		mu:      &sync.RWMutex{},
		pending: make(map[common.Address]*txList),
	}
}

// Pending retrieves all currently processable transactions, groupped by origin
// account and sorted by nonce. The returned transaction set is a copy and can be
// freely modified by calling code.
func (pool *TxPool) Pending() (map[common.Address]types.Transactions, int, error) {
	pool.mu.Lock()
	defer pool.mu.Unlock()

	count := 0
	pending := make(map[common.Address]types.Transactions)
	for addr, list := range pool.pending {
		txs := list.Flatten()
		count += txs.Len()
		pending[addr] = txs
	}
	return pending, count, nil
}

// SubscribeNewTxsEvent registers a subscription of NewTxsEvent and
// starts sending event to the given channel.
func (pool *TxPool) SubscribeTxsEvent(ch chan<- types.Transactions) event.Subscription {
	return pool.scope.Track(pool.txFeed.Subscribe(ch))
}
