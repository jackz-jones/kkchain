package peer 

import (
	"math/big"

	"github.com/invin/kkchain/common"
)

// Peer defines interface for requesting blocks and headers from remote peer
type Peer interface {
	// Returns the ID of the target peer
	ID() string

	// Returns the head block hash and total difficulty of the peer
	Head() (hash common.Hash, td *big.Int)

	// Request headers by hash
	RequestHeadersByHash(origin common.Hash, amount int, skip int, reverse bool) error

	// Request headers by number
	RequestHeadersByNumber(origin uint64, amount int, skip int, reverse bool) error

	// Request blocks from the peer
	RequestBlocksByNumber(origin uint64, amount int) error 
}

// PeerSet is the interface for managing peers
type PeerSet interface {
	// Register the peer
	Register(p Peer) error

	// Unregister peer
	UnRegister(id string) error

	// Returns the peer
	Peer(id string) Peer

	// Returns the best peer for synchronizaton
	BestPeer() Peer
}

// PeerDropFn is a callback type for dropping a peer detected as malicious.
type PeerDropFn func(id string)