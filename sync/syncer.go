package sync

import (
	"errors"
	"sync/atomic"
	"time"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/sync/downloader"
	"github.com/invin/kkchain/sync/fetcher"
	"github.com/invin/kkchain/sync/peer"
	"github.com/jbenet/goprocess"
	log "github.com/sirupsen/logrus"
)

const (
	forceSyncCycle      = 3 * time.Second // Time interval to force syncs, even if few peers are available
	minDesiredPeerCount = 5               // Amount of peers desired to start syncing
)

const (
	Stopped = iota
	Started
)

var (
	errBusy = errors.New("busy")
)

// Syncer represents syncer for blockchain.
// It includes both fetching and downloading
type Syncer struct {
	status int32
	proc   goprocess.Process
	ctrl   *core.Controller
	// chain      *Chain
	buddy      SyncBuddy
	blockchain *core.BlockChain
	downloader *downloader.Downloader
	fetcher    *fetcher.Fetcher
}

// SyncBuddy defines interface for cooperating with synchronization
type SyncBuddy interface {
	Peers() peer.PeerSet
	// InsertBlocks(blocks types.Blocks) (int, error)
	BroadcastBlock(block *types.Block, propagate bool)
	AcceptTxs()
	RemovePeer(id string)
}

// New creates a new syncer object
func New(buddy SyncBuddy, bc *core.BlockChain) *Syncer {
	validator := func(header *types.Header) error {
		return bc.Engine().VerifyHeader(bc, header)
	}
	heighter := func() uint64 {
		return bc.CurrentBlock().NumberU64()
	}
	inserter := func(blocks types.Blocks) (int, error) {
		buddy.AcceptTxs()
		return bc.InsertChain(blocks)
	}
	return &Syncer{
		status:     Stopped,
		buddy:      buddy,
		blockchain: bc,
		downloader: downloader.New(bc, buddy.Peers()),
		fetcher:    fetcher.New(bc.GetBlockByHash, validator, buddy.BroadcastBlock, heighter, inserter, buddy.RemovePeer),
	}
}

// Start starts sync operation with remote peers
func (s *Syncer) Start() error {
	if !atomic.CompareAndSwapInt32(&s.status, Stopped, Started) {
		log.Warning("Already started")
		return errBusy
	}
	// Create goprocess with tear down
	s.proc = goprocess.WithTeardown(func() error {
		return nil
	})

	loop := func(p goprocess.Process) {
		s.fetcher.Start()
		defer s.fetcher.Stop()
		defer s.downloader.Terminate()
		// Wait for different events to fire synchronization operations
		forceSync := time.NewTicker(forceSyncCycle)
		defer forceSync.Stop()

		for {
			select {
			case <-p.Closing():
				return
			case <-forceSync.C:
				// Force a sync even if not enough peers are present
				peers := s.buddy.Peers()
				go s.Synchronise(peers.BestPeer())
			}
		}
	}

	s.proc.Go(loop)

	return nil
}

// synchronise tries to synchronise with best peer
func (s *Syncer) Synchronise(p peer.Peer) {
	// Short circuit if no peers are available
	if p == nil {
		return
	}

	// Make sure the peer's TD is higher than our own
	currentBlock := s.blockchain.CurrentBlock()
	td := s.blockchain.GetTd(currentBlock.Hash(), currentBlock.NumberU64())

	pHead, pTd := p.Head()
	if pTd.Cmp(td) <= 0 {
		return
	}

	// Run the sync cycle, and disable fast sync if we've went past the pivot block
	if err := s.downloader.Synchronise(p.ID(), pHead, pTd, downloader.FullSync); err != nil {
		log.Errorf("Failed to sync,error: %v", err)
		return
	}

	// Otherwise try to sync with downloader
	s.buddy.AcceptTxs()
	if head := s.blockchain.CurrentBlock(); head.NumberU64() > 0 {
		go s.buddy.BroadcastBlock(head, false)
	}

}

// Stop stops sync operation
func (s *Syncer) Stop() {
	if s.proc != nil {
		s.proc.Close()
		s.proc = nil
	}

	atomic.StoreInt32(&s.status, Stopped)
}

// NewBlock injects a new received block from remote peer
func (s *Syncer) NewBlock(id string, block *types.Block) error {
	// s.fetcher.Enqueue(id, block)
	// TODO: compare TD and triggle synchronize if necessary
	return nil
}

// Enqueue tries to fill gaps the the fetcher's future import queue.
func (s *Syncer) Enqueue(peer string, block *types.Block) error {
	return s.fetcher.Enqueue(peer, block)
}

// DeliverHeaders injects a new batch of block headers received from a remote
// node into the local schedule.
func (s *Syncer) DeliverHeaders(id string, headers []*types.Header) (err error) {
	// TODO: filter from fetchers
	filter := len(headers) == 1
	if filter {
		// TODO: send the header to the fetcher just in case
		// headers = s.fetcher.FilterHeaders(p.id, headers, time.Now())
	}

	if len(headers) > 0 || !filter {
		return s.downloader.DeliverHeaders(id, headers)
	}

	return nil
}

// DeliverBlocks injects a new batch of blocks  received from a remote
// node into the local schedule.
func (s *Syncer) DeliverBlocks(id string, blocks []*types.Block) (err error) {
	// TODO: filter from fetchers
	return s.downloader.DeliverBlocks(id, blocks)
}

// Notify notifies the hash of headers
// TODO: move common definitions to a separate package
func (s *Syncer) Notify(peer string, hash common.Hash, number uint64, time time.Time, blockFetcher func([]common.Hash) error) error {
	return s.fetcher.Notify(peer, hash, number, time, blockFetcher)
}
