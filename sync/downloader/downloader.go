package downloader

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"math/big"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/core/types"
	sc "github.com/invin/kkchain/sync/common"
	"github.com/invin/kkchain/sync/peer"
	log "github.com/sirupsen/logrus"
)

// var (
// 	MaxHashFetch    = 512      // Amount of hashes to be fetched per retrieval request
// 	MaxBlockFetch   = 128      // Amount of blocks to be fetched per retrieval request
// 	MaxBlockPerSync = 128 * 20 // FIXME: Amount of blocks to be fetched per seesion
// 	MaxHeaderFetch  = 192      // Amount of block headers to be fetched per retrieval request
// 	MaxSkeletonSize = 128      // Number of header fetches to need for a skeleton assembly
// 	MaxBodyFetch    = 128      // Amount of block bodies to be fetched per retrieval request

// 	rttMinEstimate   = 2 * time.Second  // Minimum round-trip time to target for download requests
// 	rttMaxEstimate   = 20 * time.Second // Maximum round-trip time to target for download requests
// 	rttMinConfidence = 0.1              // Worse confidence factor in our estimated RTT value
// 	ttlScaling       = 3                // Constant scaling factor for RTT -> TTL conversion
// 	ttlLimit         = time.Minute      // Maximum TTL allowance to prevent reaching crazy timeouts

// 	fsMinFullBlocks  = 64              // Number of blocks to retrieve fully even in fast sync
// )

var (
	errBusy                    = errors.New("busy")
	errUnknownPeer             = errors.New("peer is unknown or unhealthy")
	errBadPeer                 = errors.New("action from bad peer ignored")
	errStallingPeer            = errors.New("peer is stalling")
	errNoPeers                 = errors.New("no peers to keep download active")
	errTimeout                 = errors.New("timeout")
	errEmptyHeaderSet          = errors.New("empty header set by peer")
	errPeersUnavailable        = errors.New("no peers available or all tried for download")
	errInvalidAncestor         = errors.New("retrieved ancestor is invalid")
	errInvalidChain            = errors.New("retrieved hash chain is invalid")
	errInvalidBlock            = errors.New("retrieved block is invalid")
	errInvalidBody             = errors.New("retrieved block body is invalid")
	errInvalidReceipt          = errors.New("retrieved receipt is invalid")
	errCancelBlockFetch        = errors.New("block download canceled (requested)")
	errCancelHeaderFetch       = errors.New("block header download canceled (requested)")
	errCancelBodyFetch         = errors.New("block body download canceled (requested)")
	errCancelReceiptFetch      = errors.New("receipt download canceled (requested)")
	errCancelStateFetch        = errors.New("state data download canceled (requested)")
	errCancelHeaderProcessing  = errors.New("header processing canceled (requested)")
	errCancelContentProcessing = errors.New("content processing canceled (requested)")
	errNoSyncActive            = errors.New("no sync active")
	errTooOld                  = errors.New("peer doesn't speak recent enough protocol version (need version >= 62)")
)

type SyncMode int

// SyncMode represents the synchronization mode of downloader
const (
	FullSync = iota
)

const (
	MaxIterations      = 192
	BlocksPerIteration = 128
)

type dataPack interface {
	PeerID() string
	Items() int
	Stats() string
}

// headerPack is a batch of block headers returned by a peer.
type headerPack struct {
	peerID  string
	headers []*types.Header
}

func (p *headerPack) PeerID() string { return p.peerID }
func (p *headerPack) Items() int     { return len(p.headers) }
func (p *headerPack) Stats() string  { return fmt.Sprintf("%d", len(p.headers)) }

// blockPack is a batch of blocks returned by a peer
type blockPack struct {
	peerID string
	blocks []*types.Block
}

func (b *blockPack) PeerID() string { return b.peerID }
func (b *blockPack) Items() int     { return len(b.blocks) }
func (b *blockPack) Stats() string  { return fmt.Sprintf("%d", len(b.blocks)) }

// LightChain encapsulates functions required to synchronise a light chain.
type LightChain interface {
	// HasHeader verifies a header's presence in the local chain.
	// HasHeader(common.Hash, uint64) bool

	// GetHeaderByHash retrieves a header from the local chain.
	GetHeaderByHash(common.Hash) *types.Header

	// CurrentHeader retrieves the head header from the local chain.
	CurrentHeader() *types.Header

	// GetTd returns the total difficulty of a local block.
	GetTd(common.Hash, uint64) *big.Int

	// InsertHeaderChain inserts a batch of headers into the local chain.
	// InsertHeaderChain([]*types.Header, int) (int, error)

	// Rollback removes a few recently added elements from the local chain.
	// Rollback([]common.Hash)
}

// BlockChain encapsulates functions required to sync a (full or fast) blockchain.
type BlockChain interface {
	LightChain

	// HasBlock verifies a block's presence in the local chain.
	HasBlock(common.Hash, uint64) bool

	// GetBlockByHash retrieves a block from the local chain.
	GetBlockByHash(common.Hash) *types.Block

	// CurrentBlock retrieves the head block from the local chain.
	CurrentBlock() *types.Block

	// CurrentFastBlock retrieves the head fast block from the local chain.
	// CurrentFastBlock() *types.Block

	// FastSyncCommitHead directly commits the head block to a certain entity.
	// FastSyncCommitHead(common.Hash) error

	// InsertChain inserts a batch of blocks into the local chain.
	InsertChain(types.Blocks) (int, error)

	// InsertReceiptChain inserts a batch of receipts into the local chain.
	// InsertReceiptChain(types.Blocks, []types.Receipts) (int, error)

	// PostSyncStartEvent posts start event at beginning
	PostSyncStartEvent(event core.StartEvent)

	// PostSyncDoneEvent posts stop event after finished
	PostSyncDoneEvent(event core.DoneEvent)
}

// Downloader is the package for downloading blocks from remote peers
type Downloader struct {
	blockchain BlockChain
	ps         peer.PeerSet
	mode       SyncMode // Synchronization mode defining the strategy used (per sync cycle)

	pending  []*types.Block  // Blocks waiting for inserting to blockchain
	headerCh chan dataPack   // Channel receiving inbound block headers
	blockCh  chan dataPack   // Channel receiving inbound blocks
	dropPeer peer.PeerDropFn // Drops a peer for misbehaving

	synchronising int32         // Flag indicating if we're synchronizing or not
	cancelPeer    string        // Identifier of the peer currently being used as the master (cancel on drop)
	cancelCh      chan struct{} // Channel to cancel mid-flight syncs
	cancelLock    sync.RWMutex  // Lock to protect the cancel channel and peer in delivers

	// Statistics
	syncStatsChainOrigin uint64 // Origin block number where syncing started at
	syncStatsChainHeight uint64 // Highest block number known when syncing started
	// syncStatsState       stateSyncStats
	syncStatsLock sync.RWMutex // Lock protecting the sync stats fields

	quitCh   chan struct{} // Quit channel to signal termination
	quitLock sync.RWMutex  // Lock to prevent double closes
}

// New creates a downloader object
func New(blockchain BlockChain, ps peer.PeerSet) *Downloader {
	return &Downloader{
		blockchain: blockchain,
		ps:         ps,
		headerCh:   make(chan dataPack, 1),
		blockCh:    make(chan dataPack, 1),
		quitCh:     make(chan struct{}),
	}
}

// Synchronise tries to sync up our local block chain with a remote peer, both
// adding various sanity checks as well as wrapping it with various log entries.
func (d *Downloader) Synchronise(id string, head common.Hash, td *big.Int, mode SyncMode) error {
	err := d.synchronise(id, head, td, mode)

	switch err {
	case nil:
	case errBusy:

	case errTimeout, errBadPeer, errStallingPeer,
		errEmptyHeaderSet, errPeersUnavailable, errTooOld,
		errInvalidAncestor, errInvalidChain:
		log.WithFields(log.Fields{
			"peer": id,
			"err":  err,
		}).Error("Synchronisation failed, dropping peer")
		if d.dropPeer == nil {
			// The dropPeer method is nil when `--copydb` is used for a local copy.
			// Timeouts can occur if e.g. compaction hits at the wrong time, and can be ignored
			log.Warningf("Downloader wants to drop peer, but peerdrop-function is not set,peer: %s", id)
		} else {
			d.dropPeer(id)
		}
	default:
		log.Errorf("Synchronisation failed, retrying,err: %v", err)
	}
	return err
}

// synchronise will select the peer and use it for synchronising. If an empty string is given
// it will use the best peer possible and synchronize if its TD is higher than our own. If any of the
// checks fail an error will be returned. This method is synchronous
func (d *Downloader) synchronise(id string, hash common.Hash, td *big.Int, mode SyncMode) error {
	if !atomic.CompareAndSwapInt32(&d.synchronising, 0, 1) {
		return errBusy
	}

	defer atomic.StoreInt32(&d.synchronising, 0)

	// reset innel stats
	d.pending = make([]*types.Block, 0, MaxIterations*BlocksPerIteration)

	// drain in-flight channels
	for empty := false; !empty; {
		select {
		case <-d.blockCh:
		default:
			empty = true
		}
	}

	// Create cancel channel for aborting mid-flight and mark the master peer
	d.cancelLock.Lock()
	d.cancelCh = make(chan struct{}) // Always create a new channel
	d.cancelPeer = id
	d.cancelLock.Unlock()

	defer d.Cancel() // No matter what, we can't leave the cancel channel open

	// Set the requested sync mode, unless it's forbidden
	d.mode = mode

	// Retrieve the origin peer and initiate the downloading process
	p := d.ps.Peer(id)
	if p == nil {
		return errUnknownPeer
	}

	return d.syncWithPeer(p, hash, td)
}

// syncWithPeer starts a block synchronization based on the hash chain from the
// specified peer and head hash.
func (d *Downloader) syncWithPeer(p peer.Peer, hash common.Hash, td *big.Int) (err error) {
	// TODO: tomorrow
	//d.startFeed.Send(StartEvent{})
	d.blockchain.PostSyncStartEvent(core.StartEvent{})

	defer func() {
		// reset on error
		//d.doneFeed.Send(DoneEvent{err})
		d.blockchain.PostSyncDoneEvent(core.DoneEvent{err})
	}()

	defer func(start time.Time) {
		log.Debugf("Synchronisation terminated,elapsed: %v", time.Since(start))
	}(time.Now())

	// Look up the sync boundaries: the common ancestor and the target block
	latest, err := d.fetchHeight(p)
	if err != nil {
		return err
	}

	height := latest.Number.Uint64()
	origin, err := d.findAncestor(p, height)
	if err != nil {
		return err
	}

	// Set statistic
	d.syncStatsLock.Lock()
	if d.syncStatsChainHeight <= origin || d.syncStatsChainOrigin > origin {
		d.syncStatsChainOrigin = origin
	}
	d.syncStatsChainHeight = height
	d.syncStatsLock.Unlock()

	// TODO: concurrent
	if err = d.fetchBlocks(p, origin+1); err == nil {
		err = d.importBlocks()
	}

	return err
}

// cancel aborts all of the operations and resets the queue. However, cancel does
// not wait for the running download goroutines to finish. This method should be
// used when cancelling the downloads from inside the downloader.
func (d *Downloader) cancel() {
	// Close the current cancel channel
	d.cancelLock.Lock()
	if d.cancelCh != nil {
		select {
		case <-d.cancelCh:
			// Channel was already closed
		default:
			close(d.cancelCh)
		}
	}
	d.cancelLock.Unlock()
}

// Cancel aborts all of the operations and waits for all download goroutines to
// finish before returning.
func (d *Downloader) Cancel() {
	d.cancel()
	// d.cancelWg.Wait()
}

// Terminate interrupts the downloader, canceling all pending operations.
// The downloader cannot be reused after calling Terminate.
func (d *Downloader) Terminate() {
	// Close the termination channel (make sure double close is allowed)
	d.quitLock.Lock()
	select {
	case <-d.quitCh:
	default:
		close(d.quitCh)
	}
	d.quitLock.Unlock()

	// Cancel any pending download requests
	d.Cancel()
}

// fetchHeight retrieves the head header of the remote peer to aid in estimating
// the total time a pending synchronisation would take.
func (d *Downloader) fetchHeight(p peer.Peer) (*types.Header, error) {

	// Request the advertised remote head block and wait for the response
	head, _ := p.Head()
	go p.RequestHeadersByHash(head, 1, 0, false)

	ttl := d.requestTTL()
	timeout := time.After(ttl)
	for {
		select {
		case <-d.cancelCh:
			return nil, errCancelBlockFetch

		case packet := <-d.headerCh:
			// Discard anything not from the origin peer
			if packet.PeerID() != p.ID() {
				log.Debugf("Received headers from incorrect peer,id: %s", packet.PeerID())
				break
			}
			// Make sure the peer actually gave something valid
			headers := packet.(*headerPack).headers
			if len(headers) != 1 {
				log.Debugf("Multiple headers for single request,header count: %d", len(headers))
				return nil, errBadPeer
			}
			head := headers[0]
			return head, nil

		case <-timeout:
			log.Warnf("Waiting for head header timed out,elapsed: %v", ttl)
			return nil, errTimeout

		case <-d.blockCh:
			// Out of bounds delivery, ignore
		}
	}
}

// findAncestor tries to locate the common ancestor link of the local chain and
// a remote peers blockchain. In the general case when our node was in sync and
// on the correct chain, checking the top N links should already get us a match.
// In the rare scenario when we ended up on a long reorganisation (i.e. none of
// the head links match), we do a binary search to find the common ancestor.
func (d *Downloader) findAncestor(p peer.Peer, height uint64) (uint64, error) {
	// Figure out the valid ancestor range to prevent rewrite attacks
	floor, ceil := int64(-1), d.blockchain.CurrentBlock().NumberU64()

	// Request the topmost blocks to short circuit binary ancestor lookup
	head := ceil
	if head > height {
		head = height
	}
	from := int64(head) - int64(sc.MaxHeaderFetch)
	if from < 0 {
		from = 0
	}
	// Span out with 15 block gaps into the future to catch bad head reports
	limit := 2 * sc.MaxHeaderFetch / 16
	count := 1 + int((int64(ceil)-from)/16)
	if count > limit {
		count = limit
	}
	go p.RequestHeadersByNumber(uint64(from), int(count), 15, false)

	// Wait for the remote response to the head fetch
	number, hash := uint64(0), common.Hash{}

	ttl := d.requestTTL()
	timeout := time.After(ttl)

	for finished := false; !finished; {
		select {
		case <-d.cancelCh:
			return 0, errCancelHeaderFetch

		case packet := <-d.headerCh:
			// Discard anything not from the origin peer
			if packet.PeerID() != p.ID() {
				log.Warnf("Received headers from incorrect peer,id: %s", packet.PeerID())
				break
			}
			// Make sure the peer actually gave something valid
			headers := packet.(*headerPack).headers
			if len(headers) == 0 {
				log.Warning("Empty head header set")
				return 0, errEmptyHeaderSet
			}
			// Make sure the peer's reply conforms to the request
			for i := 0; i < len(headers); i++ {
				if number := headers[i].Number.Int64(); number != from+int64(i)*16 {
					log.WithFields(log.Fields{
						"index":     i,
						"requested": from + int64(i)*16,
						"received":  number,
					}).Warning("Head headers broke chain ordering")
					return 0, errInvalidChain
				}
			}
			// Check if a common ancestor was found
			finished = true
			for i := len(headers) - 1; i >= 0; i-- {
				// Skip any headers that underflow/overflow our requested set
				if headers[i].Number.Int64() < from || headers[i].Number.Uint64() > ceil {
					continue
				}
				// Otherwise check if we already know the header or not
				if d.mode == FullSync && d.blockchain.HasBlock(headers[i].Hash(), headers[i].Number.Uint64()) {
					number, hash = headers[i].Number.Uint64(), headers[i].Hash()

					// If every header is known, even future ones, the peer straight out lied about its head
					if number > height && i == limit-1 {
						log.WithFields(log.Fields{
							"reported": height,
							"found":    number,
						}).Warning("Lied about chain head")
						return 0, errStallingPeer
					}
					break
				}
			}

		case <-timeout:
			log.Debugf("Waiting for head header timed out,elapsed: %v", ttl)
			return 0, errTimeout

		case <-d.blockCh:
			// Out of bounds delivery, ignore
		}
	}
	// If the head fetch already found an ancestor, return
	if hash != (common.Hash{}) {
		if int64(number) <= floor {
			log.WithFields(log.Fields{
				"number":    number,
				"hash":      hash.String(),
				"allowance": floor,
			}).Warning("Ancestor below allowance")
			return 0, errInvalidAncestor
		}
		log.WithFields(log.Fields{
			"number": number,
			"hash":   hash.String(),
		}).Debug("Found common ancestor")
		return number, nil
	}
	// Ancestor not found, we need to binary search over our chain
	start, end := uint64(0), head
	if floor > 0 {
		start = uint64(floor)
	}
	for start+1 < end {
		// Split our chain interval in two, and request the hash to cross check
		check := (start + end) / 2

		ttl := d.requestTTL()
		timeout := time.After(ttl)

		go p.RequestHeadersByNumber(check, 1, 0, false)

		// Wait until a reply arrives to this request
		for arrived := false; !arrived; {
			select {
			case <-d.cancelCh:
				return 0, errCancelHeaderFetch

			case packer := <-d.headerCh:
				// Discard anything not from the origin peer
				if packer.PeerID() != p.ID() {
					log.Debugf("Received headers from incorrect peer,id: %s", packer.PeerID())
					break
				}
				// Make sure the peer actually gave something valid
				headers := packer.(*headerPack).headers
				if len(headers) != 1 {
					log.Debugf("Multiple headers for single request,header count: %d", len(headers))
					return 0, errBadPeer
				}
				arrived = true

				// Modify the search interval based on the response
				if d.mode == FullSync && !d.blockchain.HasBlock(headers[0].Hash(), headers[0].Number.Uint64()) {
					end = check
					break
				}
				header := d.blockchain.GetHeaderByHash(headers[0].Hash()) // Independent of sync mode, header surely exists
				if header.Number.Uint64() != check {
					log.WithFields(log.Fields{
						"number":  header.Number,
						"hash":    header.Hash().String(),
						"request": check,
					}).Debug("Received non requested header")
					return 0, errBadPeer
				}
				start = check

			case <-timeout:
				log.Debugf("Waiting for search header timed out,elapsed: %v", ttl)
				return 0, errTimeout

			case <-d.blockCh:
				// Out of bounds delivery, ignore
			}
		}
	}
	// Ensure valid ancestry and return
	if int64(start) <= floor {
		log.WithFields(log.Fields{
			"number":    start,
			"hash":      hash.String(),
			"allowance": floor,
		}).Warning("Ancestor below allowance")
		return 0, errInvalidAncestor
	}
	log.WithFields(log.Fields{
		"number": number,
		"hash":   hash.String(),
	}).Debug("Found common ancestor")
	return start, nil
}

// fetchBlocks retrieves the blocks from remote peer
func (d *Downloader) fetchBlocks(p peer.Peer, from uint64) error {
	log.Debugf("Directing block downloads,origin: %d", from)
	defer log.Debug("Block download terminated")

	request := time.Now()

	timeout := time.NewTimer(0) // timer to dump a non-responsive active peer
	<-timeout.C                 // timeout channel should be initially empty
	defer timeout.Stop()

	var ttl time.Duration
	getBlocks := func(from uint64) {
		request = time.Now()
		ttl = d.requestTTL()
		timeout.Reset(ttl)

		log.WithFields(log.Fields{
			"count":       sc.MaxBlockFetch,
			"from":        from,
			"requestTime": request,
		}).Debug("Fetching full blocks")
		go p.RequestBlocksByNumber(uint64(from), int(sc.MaxBlockFetch))
	}

	// Start pulling the blocks until all is done
	getBlocks(from)

	for {
		select {
		case <-d.cancelCh:
			return errCancelBlockFetch

		case packet := <-d.blockCh:
			// Make sure the active peer is giving us the skeleton headers
			if packet.PeerID() != p.ID() {
				log.Debugf("Received block from incorrect peer,id: %s", packet.PeerID())
				break
			}

			timeout.Stop()

			// If no more blocks are inbould, notify the block fetchers and return
			if packet.Items() == 0 {
				log.Debug("No more blocks avvailable")
				return nil
			}

			blocks := packet.(*blockPack).blocks
			if len(blocks) > 0 {
				log.Debugf("Received blocks,count: %d", len(blocks))

				d.pending = append(d.pending, blocks...)
				from += uint64(len(blocks))
			}

			if len(d.pending) < sc.MaxBlockPerSync {
				getBlocks(from)
			} else {
				log.Debugf("Total downloaded blocks,count: %d", len(d.pending))
				return nil
			}

		case <-timeout.C:
			if d.dropPeer == nil {
				// The dropPeer method is nil when `--copydb` is used for a local copy.
				// Timeouts can occur if e.g. compaction hits at the wrong time, and can be ignored
				log.Warningf("Downloader wants to drop peer, but peerdrop-function is not set,peer id: %s", p.ID())
				break
			}
			// Header retrieval timed out, consider the peer bad and drop
			log.Debugf("Header request timed out,elapsed: %v", ttl)
			d.dropPeer(p.ID())

			return errBadPeer
		}
	}

}

// importBlocks takes fetch results from the queue and imports them into the chain.
func (d *Downloader) importBlocks() error {
	// Check pending results
	if len(d.pending) == 0 {
		log.Debug("Empty blocks")
		return nil
	}

	// Exit if we're requested
	select {
	case <-d.quitCh:
		return errCancelContentProcessing
	default:
	}

	// Insert to local blockchain
	if index, err := d.blockchain.InsertChain(d.pending); err != nil {
		log.WithFields(log.Fields{
			"number": d.pending[index].NumberU64,
			"hash":   d.pending[index].Hash().String(),
			"err":    err,
		}).Debug("Downloaded item processing failed")
		return errInvalidChain
	}

	// Everything is ok
	return nil
}

func (d *Downloader) requestRTT() time.Duration {
	// FIXME: is time unit ok?
	return time.Duration(5 * time.Second)
}

// requestTTL returns the current timeout allowance for a single download request
// to finish under.
func (d *Downloader) requestTTL() time.Duration {
	// FIXME: it it enough?
	return time.Duration(10 * time.Second)
}

// DeliverHeaders injects a new batch of block headers received from a remote
// node into the download schedule.
func (d *Downloader) DeliverHeaders(id string, headers []*types.Header) (err error) {
	return d.deliver(id, d.headerCh, &headerPack{id, headers})
}

// DeliverBlocks injects a new batch of block bodies received from a remote node.
func (d *Downloader) DeliverBlocks(id string, blocks []*types.Block) (err error) {
	return d.deliver(id, d.blockCh, &blockPack{id, blocks})
}

// deliver injects a new batch of data received from a remote node.
func (d *Downloader) deliver(id string, destCh chan dataPack, packet dataPack) (err error) {
	// Deliver or abort if the sync is canceled while queuing
	d.cancelLock.RLock()
	cancel := d.cancelCh
	d.cancelLock.RUnlock()
	if cancel == nil {
		return errNoSyncActive
	}
	select {
	case destCh <- packet:
		return nil
	case <-cancel:
		return errNoSyncActive
	}
}
