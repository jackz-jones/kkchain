package core

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math/big"
	"math/rand"
	"sync"
	"sync/atomic"

	"github.com/hashicorp/golang-lru"
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/consensus"
	"github.com/invin/kkchain/core/rawdb"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/event"
	"github.com/invin/kkchain/storage"

	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/params"
	log "github.com/sirupsen/logrus"
)

const (
	tdCacheLimit        = 1024
	numberCacheLimit    = 2048
	headerCacheLimit    = 512
	bodyCacheLimit      = 256
	blockCacheLimit     = 256
	maxFutureBlocks     = 256
	maxTimeFutureBlocks = 30
	badBlockLimit       = 10
	triesInMemory       = 128

	// BlockChainVersion ensures that an incompatible database forces a resync from scratch.
	BlockChainVersion = 3
)

var (
	ErrNoGenesis = errors.New("Genesis not found in chain")
)

type Config struct {
	DataDir string
}

type BlockChain struct {
	mu          *sync.RWMutex
	wg          sync.WaitGroup // chain processing wait group for shutting down
	procmu      *sync.RWMutex  // block processor lock
	chainmu     *sync.RWMutex  // blockchain insertion lock
	config      Config
	chainConfig *params.ChainConfig // Chain & network configuration
	vmConfig    vm.Config

	engine    consensus.Engine
	validator Validator // block and state validator interface
	processor Processor // block processor interface

	db           storage.Database
	blockCache   *lru.Cache // Cache for the most recent entire blocks
	futureBlocks *lru.Cache // future blocks are blocks added for later processing

	logsFeed          event.Feed
	chainHeadFeed     event.Feed
	newMinedBlockFeed event.Feed
	scope             event.SubscriptionScope

	stateCache state.Database // State database to reuse between imports (contains state cache)

	genesisBlock *types.Block
	currentBlock atomic.Value

	headerCache *lru.Cache // Cache for the most recent block headers
	tdCache     *lru.Cache // Cache for the most recent block total difficulties
	numberCache *lru.Cache // Cache for the most recent block numbers

	syncStartFeed event.Feed
	syncDoneFeed  event.Feed

	chainID uint64

	txsFeed      event.Feed
	newBlockFeed event.Feed
}

func NewBlockChain(chainConfig *params.ChainConfig, vmConfig vm.Config, db storage.Database, engine consensus.Engine) (*BlockChain, error) {

	headerCache, _ := lru.New(headerCacheLimit)
	blockCache, _ := lru.New(blockCacheLimit)
	futureBlocks, _ := lru.New(maxFutureBlocks)
	tdCache, _ := lru.New(tdCacheLimit)
	numberCache, _ := lru.New(numberCacheLimit)

	bc := &BlockChain{
		chainConfig:  chainConfig,
		vmConfig:     vmConfig,
		mu:           &sync.RWMutex{},
		procmu:       &sync.RWMutex{},
		chainmu:      &sync.RWMutex{},
		db:           db,
		stateCache:   state.NewDatabase(db),
		headerCache:  headerCache,
		tdCache:      tdCache,
		numberCache:  numberCache,
		blockCache:   blockCache,
		futureBlocks: futureBlocks,
		engine:       engine,
	}

	bc.SetValidator(NewBlockValidator(bc))
	bc.SetProcessor(NewStateProcessor(chainConfig, bc))
	//init genesis block
	bc.genesisBlock = bc.GetBlockByNumber(0)
	if bc.genesisBlock == nil {
		return nil, ErrNoGenesis
	}

	//load latest state
	if err := bc.loadLastState(); err != nil {
		return nil, err
	}

	return bc, nil
}

func (bc *BlockChain) ChainID() uint64 {
	return bc.chainID
}

func (bc *BlockChain) GenesisBlock() *types.Block {
	return bc.genesisBlock
}

func (bc *BlockChain) Engine() consensus.Engine {
	return bc.engine
}

// loadLastState loads the last known chain state from the database. This method
// assumes that the chain manager mutex is held.
func (bc *BlockChain) loadLastState() error {
	// Restore the last known head block
	head := rawdb.ReadHeadBlockHash(bc.db)
	if head == (common.Hash{}) {
		// Corrupt or empty database, init from scratch
		log.Warn("Empty database, resetting chain")
		return bc.Reset()
	}
	// Make sure the entire head block is available
	currentBlock := bc.GetBlockByHash(head)
	if currentBlock == nil {
		// Corrupt or empty database, init from scratch
		log.Warnf("Head block missing, resetting chain,hash: %v", head.String())
		return bc.Reset()
	}
	// Make sure the state associated with the block is available
	if _, err := state.New(currentBlock.StateRoot(), bc.stateCache); err != nil {
		// Dangling block without a state associated, init from scratch
		log.WithFields(log.Fields{
			"number": currentBlock.Number(),
			"hash":   currentBlock.Hash().String(),
		}).Warn("Head state missing, repairing chain")
		if err := bc.repair(&currentBlock); err != nil {
			return err
		}
	}
	// Everything seems to be fine, set as the head block
	bc.currentBlock.Store(currentBlock)

	// Restore the last known head header
	currentHeader := currentBlock.Header()
	if head := rawdb.ReadHeadHeaderHash(bc.db); head != (common.Hash{}) {
		if header := bc.GetHeaderByHash(head); header != nil {
			currentHeader = header
		}
	}

	headerTd := bc.GetTd(currentHeader.Hash(), currentHeader.Number.Uint64())
	blockTd := bc.GetTd(currentBlock.Hash(), currentBlock.NumberU64())

	log.WithFields(log.Fields{
		"number": currentHeader.Number,
		"hash":   currentHeader.Hash().String(),
		"td":     headerTd,
	}).Info("Loaded most recent local header")
	log.WithFields(log.Fields{
		"number": currentBlock.NumberU64(),
		"hash":   currentBlock.Hash().String(),
		"td":     blockTd,
	}).Info("Loaded most recent local full block")

	return nil
}

// repair tries to repair the current blockchain by rolling back the current block
// until one with associated state is found. This is needed to fix incomplete db
// writes caused either by crashes/power outages, or simply non-committed tries.
//
// This method only rolls back the current block. The current header and current
// fast block are left intact.
func (bc *BlockChain) repair(head **types.Block) error {
	for {
		// Abort if we've rewound to a head block that does have associated state
		if _, err := state.New((*head).StateRoot(), bc.stateCache); err == nil {
			log.WithFields(log.Fields{
				"number": (*head).NumberU64(),
				"hash":   (*head).Hash(),
			}).Info("Rewound blockchain to past state")
			return nil
		}
		// Otherwise rewind one block and recheck state availability there
		(*head) = bc.GetBlock((*head).ParentHash(), (*head).NumberU64()-1)
	}
}

// Reset purges the entire blockchain, restoring it to its genesis state.
func (bc *BlockChain) Reset() error {
	return bc.ResetWithGenesisBlock(bc.genesisBlock)
}

// ResetWithGenesisBlock purges the entire blockchain, restoring it to the
// specified genesis state.
func (bc *BlockChain) ResetWithGenesisBlock(genesis *types.Block) error {
	// Dump the entire block chain and purge the caches
	if err := bc.SetHead(0); err != nil {
		return err
	}
	bc.mu.Lock()
	defer bc.mu.Unlock()

	// Prepare the genesis block and reinitialise the chain
	if err := bc.WriteTd(genesis.Hash(), genesis.NumberU64(), genesis.Difficulty()); err != nil {
		log.Errorf("Failed to write genesis block TD,err: %v", err)
	}
	rawdb.WriteBlock(bc.db, genesis)

	bc.genesisBlock = genesis
	bc.insert(bc.genesisBlock)
	bc.currentBlock.Store(bc.genesisBlock)

	return nil
}

// SetHead rewinds the local chain to a new head. In the case of headers, everything
// above the new head will be deleted and the new one set. In the case of blocks
// though, the head may be further rewound if block bodies are missing (non-archive
// nodes after a fast sync).
func (bc *BlockChain) SetHead(head uint64) error {
	log.Warnf("Rewinding blockchain,target: %v", head)

	bc.mu.Lock()
	defer bc.mu.Unlock()

	height := uint64(0)
	if hdr := bc.CurrentHeader(); hdr != nil {
		height = hdr.Number.Uint64()
	}
	batch := bc.db.NewBatch()
	for hdr := bc.CurrentHeader(); hdr != nil && hdr.Number.Uint64() > head; hdr = bc.CurrentHeader() {
		hash := hdr.Hash()
		num := hdr.Number.Uint64()
		rawdb.DeleteBody(batch, hash, num)
		rawdb.DeleteHeader(batch, hash, num)
		rawdb.DeleteTd(batch, hash, num)

		bc.currentBlock.Store(bc.GetBlock(hdr.ParentHash, hdr.Number.Uint64()-1))
	}
	// Roll back the canonical chain numbering
	for i := height; i > head; i-- {
		rawdb.DeleteCanonicalHash(batch, i)
	}
	batch.Write()

	// Clear out any stale content from the caches
	bc.headerCache.Purge()
	bc.tdCache.Purge()
	bc.numberCache.Purge()

	if bc.CurrentHeader() == nil {
		bc.currentBlock.Store(bc.genesisBlock)
	}
	currentHeaderHash := bc.CurrentHeader().Hash()

	rawdb.WriteHeadHeaderHash(bc.db, currentHeaderHash)

	// Clear out any stale content from the caches
	bc.blockCache.Purge()

	if currentBlock := bc.CurrentBlock(); currentBlock != nil {
		if _, err := state.New(currentBlock.StateRoot(), bc.stateCache); err != nil {
			// Rewound state missing, rolled back to before pivot, reset to genesis
			bc.currentBlock.Store(bc.genesisBlock)
		}
	}

	// If either blocks reached nil, reset to the genesis state
	if currentBlock := bc.CurrentBlock(); currentBlock == nil {
		bc.currentBlock.Store(bc.genesisBlock)
	}

	currentBlock := bc.CurrentBlock()

	rawdb.WriteHeadBlockHash(bc.db, currentBlock.Hash())

	return bc.loadLastState()
}

// WriteBlockWithState writes the block and all associated state to the database.
func (bc *BlockChain) WriteBlockWithState(block *types.Block, receipts []*types.Receipt, state *state.StateDB) error {
	bc.wg.Add(1)
	defer bc.wg.Done()

	// Calculate the total difficulty of the block
	ptd := bc.GetTd(block.ParentHash(), block.NumberU64()-1)
	if ptd == nil {
		return consensus.ErrUnknownAncestor
	}
	// Make sure no inconsistent state is leaked during insertion
	bc.mu.Lock()
	defer bc.mu.Unlock()

	currentBlock := bc.CurrentBlock()
	localTd := bc.GetTd(currentBlock.Hash(), currentBlock.NumberU64())
	externTd := new(big.Int).Add(block.Difficulty(), ptd)

	// Irrelevant of the canonical status, write the block itself to the database
	if err := bc.WriteTd(block.Hash(), block.NumberU64(), externTd); err != nil {
		return err
	}
	rawdb.WriteBlock(bc.db, block)

	root, err := state.Commit(false)
	if err != nil {
		return err
	}
	triedb := bc.stateCache.TrieDB()

	// If we're running an archive node, always flush
	if err := triedb.Commit(root, false); err != nil {
		return err
	}

	// Write other block data using a batch.
	batch := bc.db.NewBatch()
	rawdb.WriteReceipts(batch, block.Hash(), block.NumberU64(), receipts)

	// If the total difficulty is higher than our known, add it to the canonical chain
	// Second clause in the if statement reduces the vulnerability to selfish mining.
	// Please refer to http://www.cs.cornell.edu/~ie53/publications/btcProcFC.pdf
	reorg := externTd.Cmp(localTd) > 0
	currentBlock = bc.CurrentBlock()
	if !reorg && externTd.Cmp(localTd) == 0 {
		// Split same-difficulty blocks by number, then at random
		reorg = block.NumberU64() < currentBlock.NumberU64() || (block.NumberU64() == currentBlock.NumberU64() && rand.Float64() < 0.5)
	}
	if reorg {
		// Reorganise the chain if the parent is not the head block
		if block.ParentHash() != currentBlock.Hash() {
			if err := bc.reorg(currentBlock, block); err != nil {
				return err
			}
		}
		// Write the positional metadata for transaction/receipt lookups and preimages
		rawdb.WriteTxLookupEntries(batch, block)
		rawdb.WritePreimages(batch, block.NumberU64(), state.Preimages())

	}

	if err := batch.Write(); err != nil {
		return err
	}

	// Set new head.
	bc.insert(block)
	return nil
}

// reorgs takes two blocks, an old chain and a new chain and will reconstruct the blocks and inserts them
// to be part of the new canonical chain and accumulates potential missing transactions and post an
// event about them
func (bc *BlockChain) reorg(oldBlock, newBlock *types.Block) error {
	var (
		newChain    types.Blocks
		oldChain    types.Blocks
		commonBlock *types.Block
		deletedTxs  types.Transactions
	)

	// first reduce whoever is higher bound
	if oldBlock.NumberU64() > newBlock.NumberU64() {
		// reduce old chain
		for ; oldBlock != nil && oldBlock.NumberU64() != newBlock.NumberU64(); oldBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1) {
			oldChain = append(oldChain, oldBlock)
			deletedTxs = append(deletedTxs, oldBlock.Transactions()...)
		}
	} else {
		// reduce new chain and append new chain blocks for inserting later on
		for ; newBlock != nil && newBlock.NumberU64() != oldBlock.NumberU64(); newBlock = bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1) {
			newChain = append(newChain, newBlock)
		}
	}
	if oldBlock == nil {
		return fmt.Errorf("Invalid old chain")
	}
	if newBlock == nil {
		return fmt.Errorf("Invalid new chain")
	}

	for {
		if oldBlock.Hash() == newBlock.Hash() {
			commonBlock = oldBlock
			break
		}

		oldChain = append(oldChain, oldBlock)
		newChain = append(newChain, newBlock)
		deletedTxs = append(deletedTxs, oldBlock.Transactions()...)

		oldBlock, newBlock = bc.GetBlock(oldBlock.ParentHash(), oldBlock.NumberU64()-1), bc.GetBlock(newBlock.ParentHash(), newBlock.NumberU64()-1)
		if oldBlock == nil {
			return fmt.Errorf("Invalid old chain")
		}
		if newBlock == nil {
			return fmt.Errorf("Invalid new chain")
		}
	}
	// Ensure the user sees large reorgs
	if len(oldChain) > 0 && len(newChain) > 0 {
		//logFn := log.Debug
		//if len(oldChain) > 63 {
		//	logFn = log.Warn
		//}
		log.WithFields(log.Fields{
			"number":   commonBlock.NumberU64(),
			"hash":     commonBlock.Hash().String(),
			"drop":     len(oldChain),
			"dropfrom": oldChain[0].Hash().String(),
			"add":      len(newChain),
			"addfrom":  newChain[0].Hash().String(),
		}).Debug("Chain split detected")
	} else {
		log.WithFields(log.Fields{
			"oldnum":  oldBlock.NumberU64(),
			"oldhash": oldBlock.Hash().String(),
			"newnum":  newBlock.NumberU64(),
			"newhash": newBlock.Hash().String(),
		}).Error("Impossible reorg, please file an issue")
	}
	// Insert the new chain, taking care of the proper incremental order
	var addedTxs types.Transactions
	for i := len(newChain) - 1; i >= 0; i-- {
		// insert the block in the canonical way, re-writing history
		bc.insert(newChain[i])
		// write lookup entries for hash based transaction/receipt searches
		rawdb.WriteTxLookupEntries(bc.db, newChain[i])
		addedTxs = append(addedTxs, newChain[i].Transactions()...)
	}
	// calculate the difference between deleted and added transactions
	diff := types.TxDifference(deletedTxs, addedTxs)
	// When transactions get deleted from the database that means the
	// receipts that were created in the fork must also be deleted
	batch := bc.db.NewBatch()
	for _, tx := range diff {
		rawdb.DeleteTxLookupEntry(batch, tx.Hash())
	}
	batch.Write()

	//if len(oldChain) > 0 {
	//	go func() {
	//		for _, block := range oldChain {
	//			bc.chainSideFeed.Send(ChainSideEvent{Block: block})
	//		}
	//	}()
	//}

	return nil
}

// insert injects a new head block into the current block chain. This method
// assumes that the block is indeed a true head. It will also reset the head
// header and the head fast sync block to this very same block if they are older
// or if they are on a different side chain.
//
// Note, this function assumes that the `mu` mutex is held!
func (bc *BlockChain) insert(block *types.Block) {

	// Add the block to the canonical chain number scheme and mark as the head
	rawdb.WriteCanonicalHash(bc.db, block.Hash(), block.NumberU64())
	rawdb.WriteHeadBlockHash(bc.db, block.Hash())

	bc.currentBlock.Store(block)

}

// State returns a new mutable state based on the current HEAD block.
func (bc *BlockChain) State() (*state.StateDB, error) {
	return bc.StateAt(bc.CurrentBlock().StateRoot())
}

// StateAt returns a new mutable state based on a particular point in time.
func (bc *BlockChain) StateAt(root common.Hash) (*state.StateDB, error) {
	return state.New(root, bc.stateCache)
}

// CurrentBlock retrieves the current head block of the canonical chain. The
// block is retrieved from the blockchain's internal cache.
func (bc *BlockChain) CurrentBlock() *types.Block {
	return bc.currentBlock.Load().(*types.Block)
}

// CurrentHeader retrieves the current header from the local chain.
func (bc *BlockChain) CurrentHeader() *types.Header {
	block := bc.currentBlock.Load()
	if block == nil {
		return nil
	}
	return block.(*types.Block).Header()
}

// GetHeader retrieves a block header from the database by hash and number.
func (bc *BlockChain) GetHeader(hash common.Hash, number uint64) *types.Header {
	// Short circuit if the header's already in the cache, retrieve otherwise
	if header, ok := bc.headerCache.Get(hash); ok {
		return header.(*types.Header)
	}
	header := rawdb.ReadHeader(bc.db, hash, number)
	if header == nil {
		return nil
	}
	// Cache the found header for next time and return
	bc.headerCache.Add(hash, header)
	return header
}

// GetHeaderByNumber retrieves a block header from the database by number.
func (bc *BlockChain) GetHeaderByNumber(number uint64) *types.Header {
	hash := rawdb.ReadCanonicalHash(bc.db, number)
	if hash == (common.Hash{}) {
		return nil
	}
	return bc.GetHeader(hash, number)
}

// GetHeaderByHash retrieves a block header from the database by its hash.
func (bc *BlockChain) GetHeaderByHash(hash common.Hash) *types.Header {
	number := bc.GetBlockNumber(hash)
	if number == nil {
		return nil
	}
	return bc.GetHeader(hash, *number)
}

// HasBlock checks if a block is fully present in the database or not.
func (bc *BlockChain) HasBlock(hash common.Hash, number uint64) bool {
	if bc.blockCache.Contains(hash) {
		return true
	}
	return rawdb.HasBody(bc.db, hash, number)
}

// GetBlock retrieves a block from the database by hash and number.
func (bc *BlockChain) GetBlock(hash common.Hash, number uint64) *types.Block {
	// Short circuit if the block's already in the cache, retrieve otherwise
	if block, ok := bc.blockCache.Get(hash); ok {
		return block.(*types.Block)
	}
	block := rawdb.ReadBlock(bc.db, hash, number)
	if block == nil {
		return nil
	}
	// Cache the found block for next time and return
	bc.blockCache.Add(block.Hash(), block)
	return block
}

// GetBlockByNumber retrieves a block from the database by number, caching it
// (associated with its hash) if found.
func (bc *BlockChain) GetBlockByNumber(number uint64) *types.Block {
	hash := rawdb.ReadCanonicalHash(bc.db, number)
	if hash == (common.Hash{}) {
		return nil
	}
	return bc.GetBlock(hash, number)
}

// GetTd retrieves a block's total difficulty in the canonical chain from the
// database by hash and number, caching it if found.
func (bc *BlockChain) GetTd(hash common.Hash, number uint64) *big.Int {
	if cached, ok := bc.tdCache.Get(hash); ok {
		return cached.(*big.Int)
	}
	td := rawdb.ReadTd(bc.db, hash, number)
	if td == nil {
		return nil
	}
	// Cache the found body for next time and return
	bc.tdCache.Add(hash, td)
	return td
}

// GetTdByHash retrieves a block's total difficulty in the canonical chain from the
// database by hash, caching it if found.
func (bc *BlockChain) GetTdByHash(hash common.Hash) *big.Int {
	number := bc.GetBlockNumber(hash)
	if number == nil {
		return nil
	}
	return bc.GetTd(hash, *number)
}

// WriteTd stores a block's total difficulty into the database, also caching it
// along the way.
func (bc *BlockChain) WriteTd(hash common.Hash, number uint64, td *big.Int) error {
	rawdb.WriteTd(bc.db, hash, number, td)
	bc.tdCache.Add(hash, new(big.Int).Set(td))
	return nil
}

// GetBlockByHash retrieves a block from the database by hash, caching it if found.
func (bc *BlockChain) GetBlockByHash(hash common.Hash) *types.Block {
	number := bc.GetBlockNumber(hash)
	if number == nil {
		return nil
	}
	return bc.GetBlock(hash, *number)
}

// GetBlockNumber retrieves the block number belonging to the given hash
// from the cache or database
func (bc *BlockChain) GetBlockNumber(hash common.Hash) *uint64 {
	if cached, ok := bc.numberCache.Get(hash); ok {
		number := cached.(uint64)
		return &number
	}
	number := rawdb.ReadHeaderNumber(bc.db, hash)
	if number != nil {
		bc.numberCache.Add(hash, *number)
	}
	return number
}

func (bc *BlockChain) GetReceiptByHash(hash common.Hash) *types.Receipt {
	return nil
}

func (bc *BlockChain) SubscribeSyncStartEvent(ch chan<- StartEvent) event.Subscription {
	return bc.scope.Track(bc.syncStartFeed.Subscribe(ch))
}

func (bc *BlockChain) PostSyncStartEvent(event StartEvent) {
	bc.syncStartFeed.Send(event)
}

func (bc *BlockChain) SubscribeSyncDoneEvent(ch chan<- DoneEvent) event.Subscription {
	return bc.scope.Track(bc.syncDoneFeed.Subscribe(ch))
}

func (bc *BlockChain) PostSyncDoneEvent(event DoneEvent) {
	bc.syncDoneFeed.Send(event)
}

// PostChainEvents iterates over the events generated by a chain insertion and
// posts them into the event feed.
// TODO: Should not expose PostChainEvents. The chain events should be posted in WriteBlock.
func (bc *BlockChain) PostChainEvents(events []interface{}, logs []*types.Log) {
	// post event logs for further processing
	if logs != nil {
		bc.logsFeed.Send(logs)
	}
	for _, event := range events {
		switch ev := event.(type) {
		case ChainHeadEvent:
			bc.chainHeadFeed.Send(ev)

		case NewMinedBlockEvent:
			bc.newMinedBlockFeed.Send(ev)

		}
	}
}

// SubscribeLogsEvent registers a subscription of []*types.Log.
func (bc *BlockChain) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return bc.scope.Track(bc.logsFeed.Subscribe(ch))
}

// SubscribeChainHeadEvent registers a subscription of ChainHeadEvent.
func (bc *BlockChain) SubscribeChainHeadEvent(ch chan<- ChainHeadEvent) event.Subscription {
	return bc.scope.Track(bc.chainHeadFeed.Subscribe(ch))
}

func (bc *BlockChain) SubscribeNewMinedBlockEvent(ch chan<- NewMinedBlockEvent) event.Subscription {
	return bc.scope.Track(bc.newMinedBlockFeed.Subscribe(ch))
}

// HasState checks if state trie is fully present in the database or not.
func (bc *BlockChain) HasState(hash common.Hash) bool {
	_, err := bc.stateCache.OpenTrie(hash)
	return err == nil
}

// HasBlockAndState checks if a block and associated state trie is fully present
// in the database or not, caching it if present.
func (bc *BlockChain) HasBlockAndState(hash common.Hash, number uint64) bool {
	// Check first that the block itself is known
	block := bc.GetBlock(hash, number)
	if block == nil {
		return false
	}
	return bc.HasState(block.StateRoot())
}

// encodeBlockNumber encodes a block number as big endian uint64
func encodeBlockNumber(number uint64) []byte {
	enc := make([]byte, 8)
	binary.BigEndian.PutUint64(enc, number)
	return enc
}

//GetDb return  db of blockchain using
func (bc *BlockChain) GetDb() storage.Database {
	return bc.db
}

func (bc *BlockChain) SubscribeTxsEvent(ch chan<- []*types.Transaction) event.Subscription {
	return bc.scope.Track(bc.txsFeed.Subscribe(ch))
}

func (bc *BlockChain) PostTxsEvent(txs []*types.Transaction) {
	bc.txsFeed.Send(txs)
}

func (bc *BlockChain) SubscribeNewBlockEvent(ch chan<- *types.Block) event.Subscription {
	return bc.scope.Track(bc.newBlockFeed.Subscribe(ch))
}

func (bc *BlockChain) PostNewBlockEvent(block *types.Block) {
	bc.currentBlock.Store(block)
	bc.newBlockFeed.Send(block)
}

func (bc *BlockChain) InsertChain(chain types.Blocks) (int, error) {
	n, events, logs, err := bc.insertChain(chain)
	bc.PostChainEvents(events, logs)
	return n, err
}

func (bc *BlockChain) insertChain(chain types.Blocks) (int, []interface{}, []*types.Log, error) {
	if len(chain) == 0 {
		return 0, nil, nil, nil
	}

	// Do a sanity check that the provided chain is actually ordered and linked
	for i := 1; i < len(chain); i++ {
		if chain[i].NumberU64() != chain[i-1].NumberU64()+1 || chain[i].ParentHash() != chain[i-1].Hash() {
			// Chain broke ancestry, log a message (programming error) and skip insertion
			log.WithFields(log.Fields{
				"number":     chain[i].NumberU64(),
				"hash":       chain[i].Hash().String(),
				"parent":     chain[i].ParentHash().String(),
				"prevnumber": chain[i-1].NumberU64(),
				"prevhash":   chain[i-1].Hash().String(),
			}).Error("Non contiguous block insert")

			return 0, nil, nil, fmt.Errorf("non contiguous insert: item %d is #%d [%x…], item %d is #%d [%x…] (parent [%x…])", i-1, chain[i-1].NumberU64(),
				chain[i-1].Hash().Bytes()[:4], i, chain[i].NumberU64(), chain[i].Hash().Bytes()[:4], chain[i].ParentHash().Bytes()[:4])
		}
	}

	// Do a sanity check that the provided chain is actually ordered and linked
	for i := 1; i < len(chain); i++ {
		if chain[i].NumberU64() != chain[i-1].NumberU64()+1 || chain[i].ParentHash() != chain[i-1].Hash() {
			// Chain broke ancestry, log a message (programming error) and skip insertion
			log.WithFields(log.Fields{
				"number":     chain[i].NumberU64(),
				"hash":       chain[i].Hash().String(),
				"parent":     chain[i].ParentHash().String(),
				"prevnumber": chain[i-1].NumberU64(),
				"prevhash":   chain[i-1].Hash().String(),
			}).Error("Non contiguous block insert")

			return 0, nil, nil, fmt.Errorf("non contiguous insert: item %d is #%d [%x…], item %d is #%d [%x…] (parent [%x…])", i-1, chain[i-1].NumberU64(),
				chain[i-1].Hash().Bytes()[:4], i, chain[i].NumberU64(), chain[i].Hash().Bytes()[:4], chain[i].ParentHash().Bytes()[:4])
		}
	}

	bc.wg.Add(1)
	defer bc.wg.Done()

	bc.chainmu.Lock()
	defer bc.chainmu.Unlock()

	// A queued approach to delivering events. This is generally
	// faster than direct delivery and requires much less mutex
	// acquiring.
	var (
		//stats         = insertStats{startTime: mclock.Now()}
		events        = make([]interface{}, 0, len(chain))
		lastCanon     *types.Block
		coalescedLogs []*types.Log
	)

	for i, block := range chain {

		//TODO: optimization parallel verify header???
		err := bc.engine.VerifyHeader(bc, block.Header())
		if err == nil {
			err = bc.Validator().ValidateBody(block)
		}

		switch {
		case err == ErrKnownBlock:
			// Block and state both already known. However if the current block is below
			// this number we did a rollback and we should reimport it nonetheless.
			if bc.CurrentBlock().NumberU64() >= block.NumberU64() {
				continue
			}
		case err == consensus.ErrPrunedAncestor:
			// Block competing with the canonical chain, store in the db, but don't process
			// until the competitor TD goes above the canonical TD
			currentBlock := bc.CurrentBlock()
			localTd := bc.GetTd(currentBlock.Hash(), currentBlock.NumberU64())
			externTd := new(big.Int).Add(bc.GetTd(block.ParentHash(), block.NumberU64()-1), block.Difficulty())
			if localTd.Cmp(externTd) > 0 {
				if err = bc.WriteBlockWithoutState(block, externTd); err != nil {
					return i, events, coalescedLogs, err
				}
				continue
			}
			// Competitor chain beat canonical, gather all blocks from the common ancestor
			var winner []*types.Block

			parent := bc.GetBlock(block.ParentHash(), block.NumberU64()-1)
			for !bc.HasState(parent.StateRoot()) {
				winner = append(winner, parent)
				parent = bc.GetBlock(parent.ParentHash(), parent.NumberU64()-1)
			}
			for j := 0; j < len(winner)/2; j++ {
				winner[j], winner[len(winner)-1-j] = winner[len(winner)-1-j], winner[j]
			}
			// Import all the pruned blocks to make the state available
			bc.chainmu.Unlock()
			_, evs, logs, err := bc.insertChain(winner)
			bc.chainmu.Lock()
			events, coalescedLogs = evs, logs

			if err != nil {
				return i, events, coalescedLogs, err
			}
		case err != nil:
			return i, events, coalescedLogs, err
		}

		// Create a new statedb using the parent block and report an
		// error if it fails.
		var parent *types.Block
		if i == 0 {
			parent = bc.GetBlock(block.ParentHash(), block.NumberU64()-1)
		} else {
			parent = chain[i-1]
		}
		state, err := state.New(parent.StateRoot(), bc.stateCache)
		if err != nil {
			return i, events, coalescedLogs, err
		}

		// Process block using the parent state as reference point.
		receipts, logs, usedGas, err := bc.processor.Process(block, state, bc.vmConfig)
		if err != nil {
			return i, events, coalescedLogs, err
		}

		//Validate the state using the default validator
		err = bc.Validator().ValidateState(block, parent, state, receipts, usedGas)
		if err != nil {
			return i, events, coalescedLogs, err
		}

		// Write the block to the chain and get the status.
		err = bc.WriteBlockWithState(block, receipts, state)
		if err != nil {
			return i, events, coalescedLogs, err
		}

		coalescedLogs = append(coalescedLogs, logs...)
		events = append(events, ChainEvent{block, block.Hash(), logs})
		lastCanon = block
	}

	// Append a single chain head event if we've progressed the chain
	if lastCanon != nil && bc.CurrentBlock().Hash() == lastCanon.Hash() {
		events = append(events, ChainHeadEvent{lastCanon})
	}
	return 0, events, coalescedLogs, nil
}

func (bc *BlockChain) WriteBlockWithoutState(block *types.Block, td *big.Int) (err error) {
	bc.wg.Add(1)
	defer bc.wg.Done()

	if err := bc.WriteTd(block.Hash(), block.NumberU64(), td); err != nil {
		return err
	}

	rawdb.WriteBlock(bc.db, block)
	return nil
}

//SetValidator sets the validator which is used to validate incoming blocks.
func (bc *BlockChain) SetValidator(validator Validator) {
	bc.procmu.Lock()
	defer bc.procmu.Unlock()
	bc.validator = validator
}

// Validator returns the current validator.
func (bc *BlockChain) Validator() Validator {
	bc.procmu.RLock()
	defer bc.procmu.RUnlock()
	return bc.validator
}

// SetProcessor sets the processor required for making state modifications.
func (bc *BlockChain) SetProcessor(processor Processor) {
	bc.procmu.Lock()
	defer bc.procmu.Unlock()
	bc.processor = processor
}

// Processor returns the current processor.
func (bc *BlockChain) Processor() Processor {
	bc.procmu.RLock()
	defer bc.procmu.RUnlock()
	return bc.processor
}

// GetAncestor retrieves the Nth ancestor of a given block. It assumes that either the given block or
// a close ancestor of it is canonical. maxNonCanonical points to a downwards counter limiting the
// number of blocks to be individually checked before we reach the canonical chain.
//
// Note: ancestor == 0 returns the same block, 1 returns its parent and so on.
func (bc *BlockChain) GetAncestor(hash common.Hash, number, ancestor uint64, maxNonCanonical *uint64) (common.Hash, uint64) {
	if ancestor > number {
		return common.Hash{}, 0
	}
	if ancestor == 1 {
		// in this case it is cheaper to just read the header
		if header := bc.GetHeader(hash, number); header != nil {
			return header.ParentHash, number - 1
		} else {
			return common.Hash{}, 0
		}
	}
	for ancestor != 0 {
		if rawdb.ReadCanonicalHash(bc.db, number) == hash {
			number -= ancestor
			return rawdb.ReadCanonicalHash(bc.db, number), number
		}
		if *maxNonCanonical == 0 {
			return common.Hash{}, 0
		}
		*maxNonCanonical--
		ancestor--
		header := bc.GetHeader(hash, number)
		if header == nil {
			return common.Hash{}, 0
		}
		hash = header.ParentHash
		number--
	}
	return hash, number
}
