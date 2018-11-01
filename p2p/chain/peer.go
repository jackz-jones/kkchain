package chain

//go:generate moq -out peer_moq_test.go . Peer

import (
	"errors"
	"math/big"
	"sync"

	"time"

	"encoding/json"

	"encoding/hex"

	mapset "github.com/deckarep/golang-set"
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/p2p"
	"github.com/sirupsen/logrus"
)

var (
	errClosed            = errors.New("peer set is closed")
	errAlreadyRegistered = errors.New("peer is already registered")
	errNotRegistered     = errors.New("peer is not registered")
)

const (
	maxKnownTxs    = 32768 // Maximum transactions hashes to keep in the known list (prevent DOS)
	maxKnownBlocks = 1024  // Maximum block hashes to keep in the known list (prevent DOS)

	// maxQueuedTxs is the maximum number of transaction lists to queue up before
	// dropping broadcasts. This is a sensitive number as a transaction list might
	// contain a single transaction, or thousands.
	maxQueuedTxs = 128

	// maxQueuedProps is the maximum number of block propagations to queue up before
	// dropping broadcasts. There's not much point in queueing stale blocks, so a few
	// that might cover uncles should be enough.
	maxQueuedProps = 4

	// maxQueuedAnns is the maximum number of block announcements to queue up before
	// dropping broadcasts. Similarly to block propagations, there's no point to queue
	// above some healthy uncle limit, so use that.
	maxQueuedAnns = 4

	handshakeTimeout = 5 * time.Second
)

type hashOrNumber struct {
	Hash   common.Hash // Block hash from which to retrieve headers (excludes Number)
	Number uint64      // Block hash from which to retrieve headers (excludes Hash)
}

// propEvent is a block propagation, waiting for its turn in the broadcast queue.
type propEvent struct {
	block *types.Block
	td    *big.Int
}

// newBlockHashesData is the network packet for the block announcements.
type newBlockHashesData []struct {
	Hash   common.Hash // Hash of one particular block being announced
	Number uint64      // Number of one particular block being announced
}

type peer struct {
	ID          string
	conn        p2p.Conn
	head        common.Hash
	td          *big.Int
	lock        sync.RWMutex
	knownTxs    mapset.Set                // Set of transaction hashes known to be known by this peer
	knownBlocks mapset.Set                // Set of block hashes known to be known by this peer
	queuedTxs   chan []*types.Transaction // Queue of transactions to broadcast to the peer
	queuedProps chan *propEvent           // Queue of blocks to broadcast to the peer
	queuedAnns  chan *types.Block         // Queue of blocks to announce to the peer
	term        chan struct{}             // Termination channel to stop the broadcaster
}

func NewPeer(conn p2p.Conn) *peer {
	return &peer{
		conn:        conn,
		head:        common.Hash{},
		td:          new(big.Int).SetInt64(1),
		ID:          hex.EncodeToString(conn.RemotePeer().PublicKey),
		knownTxs:    mapset.NewSet(),
		knownBlocks: mapset.NewSet(),
		queuedTxs:   make(chan []*types.Transaction, maxQueuedTxs),
		queuedProps: make(chan *propEvent, maxQueuedProps),
		queuedAnns:  make(chan *types.Block, maxQueuedAnns),
		term:        make(chan struct{}),
	}
}

// push all broadcast msg to remote peer
func (p *peer) broadcast() {
	for {
		select {
		case txs := <-p.queuedTxs:
			if err := p.SendTransactions(txs); err != nil {
				log.WithFields(logrus.Fields{
					"tx_count": len(txs),
					"error":    err,
				}).Error("failed to broadcast txs")
				return
			}
		case block := <-p.queuedAnns:
			if err := p.SendNewBlockHashes([]common.Hash{block.Hash()}, []uint64{block.NumberU64()}); err != nil {
				log.WithFields(logrus.Fields{
					"hash":   block.Hash(),
					"number": block.NumberU64(),
					"error":  err,
				}).Error("failed to broadcast block hashes")
				return
			}
		case prop := <-p.queuedProps:
			if err := p.SendNewBlock(prop.block); err != nil {
				log.WithFields(logrus.Fields{
					"block_num": prop.block.NumberU64(),
					"error":     err,
				}).Error("failed to broadcast new block")
				return
			}
		case <-p.term:
			return
		}

	}
}

func (p *peer) close() {
	close(p.term)
}

func (p *peer) Head() (hash common.Hash, td *big.Int) {
	p.lock.Lock()
	defer p.lock.Unlock()
	copy(hash[:], p.head[:])
	return hash, new(big.Int).Set(p.td)
}

func (p *peer) SetHead(hash common.Hash, td *big.Int) {
	p.lock.Lock()
	defer p.lock.Unlock()
	copy(p.head[:], hash[:])
	p.td.Set(td)
}

func (p *peer) MarkBlock(hash common.Hash) {
	for p.knownBlocks.Cardinality() >= maxKnownBlocks {
		p.knownBlocks.Pop()
	}
	p.knownBlocks.Add(hash)
}

func (p *peer) MarkTransactions(hash common.Hash) {
	for p.knownTxs.Cardinality() >= maxKnownTxs {
		p.knownTxs.Pop()
	}
	p.knownTxs.Add(hash)
}

func (p *peer) SendTransactions(txs []*types.Transaction) error {
	for _, tx := range txs {
		p.knownTxs.Add(tx.Hash())
	}

	txbytes, err := json.Marshal(txs)
	if err != nil {
		log.Errorf("failed to marshal transaction to bytes,error: %v", err)
		return err
	}
	return p.conn.SendChainMsg(int32(Message_TRANSACTIONS), txbytes)
}

func (p *peer) SendNewBlockHashes(hashes []common.Hash, num []uint64) error {
	request := make(newBlockHashesData, len(hashes))
	for i, hash := range hashes {
		p.knownBlocks.Add(hash)
		request[i].Hash = hash
		request[i].Number = num[i]
	}
	hbytes, err := json.Marshal(request)
	if err != nil {
		log.Errorf("failed to marshal hash to bytes,error: %v", err)
		return err
	}
	return p.conn.SendChainMsg(int32(Message_NEW_BLOCK_HASHS), hbytes)
}

func (p *peer) SendNewBlock(block *types.Block) error {
	p.knownBlocks.Add(block.Hash())
	blocks := []*types.Block{block}
	bbytes, err := json.Marshal(blocks)
	if err != nil {
		log.WithFields(logrus.Fields{
			"block_num":  block.NumberU64(),
			"block_hash": block.Hash().String(),
			"error":      err,
		}).Errorf("failed to marshal block to bytes")
		return err
	}
	return p.conn.SendChainMsg(int32(Message_NEW_BLOCK), bbytes)
}

func (p *peer) AsyncSendNewBlock(block *types.Block) {
	prop := &propEvent{
		block: block,
		td:    block.Td,
	}
	select {
	case p.queuedProps <- prop:
		p.knownBlocks.Add(block.Hash())
	default:
		log.WithFields(logrus.Fields{
			"number": block.NumberU64(),
			"hash":   block.Hash().String(),
		}).Debug("Dropping block propagation")
	}
}

func (p *peer) AsyncSendNewBlockHash(block *types.Block) {
	select {
	case p.queuedAnns <- block:
		p.knownBlocks.Add(block.Hash())
	default:
		log.Debugf("Dropping block announcement,hash: %v", block.Hash().String())
	}
}

func (p *peer) AsyncSendTransactions(txs []*types.Transaction) {
	select {
	case p.queuedTxs <- txs:
		for _, tx := range txs {
			p.knownTxs.Add(tx.Hash())
		}
	default:
		log.Debugf("Dropping transaction propagation,count: %d", len(txs))
	}
}

func (p *peer) requestHeader(origin []common.Hash) error {
	if len(origin) <= 0 {
		return errors.New("nil hash for request")
	}
	return p.requestHeadersByHash(origin[0], 1, 0, false)
}

// requestHeadersByHash fetches a batch of blocks' headers corresponding to the
// specified header query, based on the hash of an origin block.
func (p *peer) requestHeadersByHash(origin common.Hash, amount int, skip int, reverse bool) error {
	log.WithFields(logrus.Fields{
		"count":    amount,
		"fromhash": origin.String(),
		"skip":     skip,
		"reverse":  reverse,
	}).Debug("Fetching batch of headers")
	msg := &GetBlockHeadersMsg{
		StartHash: origin.Bytes(),
		Amount:    uint64(amount),
		Skip:      uint64(skip),
		Reverse:   reverse,
	}
	return p.conn.SendChainMsg(int32(Message_GET_BLOCK_HEADERS), msg)
}

// requestBlocksByHash fetchs blocks with hashes 
func (p *peer) requestBlocksByHash(hashes []common.Hash) error {
	log.Debugf("Fetching %d blocks", len(hashes))
	temp := [][]byte{}
	for _, hash := range hashes {
		temp = append(temp, hash.Bytes())
	}
	msg := &FetchBlocksMsg{
		Hashes: temp,
	}

	return p.conn.SendChainMsg(int32(Message_FETCH_BLOCKS), msg)
}

// requestHeadersByNumber fetches a batch of blocks' headers corresponding to the
// specified header query, based on the number of an origin block.
func (p *peer) requestHeadersByNumber(origin uint64, amount int, skip int, reverse bool) error {
	log.WithFields(logrus.Fields{
		"count":   amount,
		"fromnum": origin,
		"skip":    skip,
		"reverse": reverse,
	}).Debug("Fetching batch of headers")
	msg := &GetBlockHeadersMsg{
		StartNum: origin,
		Amount:   uint64(amount),
		Skip:     uint64(skip),
		Reverse:  reverse,
	}
	return p.conn.SendChainMsg(int32(Message_GET_BLOCK_HEADERS), msg)
}

// requestBlocksByNumber fetches a batch of blocks corresponding to the
// specified range
func (p *peer) requestBlocksByNumber(origin uint64, amount int) error {
	log.WithFields(logrus.Fields{
		"count":   amount,
		"fromnum": origin,
	}).Debug("Fetching batch of blocks")
	msg := &GetBlocksMsg{
		StartNum: origin,
		Amount:   uint64(amount),
	}
	return p.conn.SendChainMsg(int32(Message_GET_BLOCKS), msg)
}

type PeerSet struct {
	peers  map[string]*peer
	lock   sync.RWMutex
	closed bool
}

func NewPeerSet() *PeerSet {
	return &PeerSet{peers: make(map[string]*peer)}
}

func (ps *PeerSet) Register(p *peer) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	if ps.closed {
		return errClosed
	}
	if _, ok := ps.peers[p.ID]; ok {
		return errAlreadyRegistered
	}
	ps.peers[p.ID] = p
	go p.broadcast()
	return nil
}

func (ps *PeerSet) Unregister(id string) error {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	if ps.closed {
		return errClosed
	}

	p, ok := ps.peers[id]
	if !ok {
		return errNotRegistered
	}
	delete(ps.peers, id)
	p.close()
	return nil
}

func (ps *PeerSet) Peer(id string) *peer {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	return ps.peers[id]
}

func (ps *PeerSet) Len() int {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	return len(ps.peers)
}

func (ps *PeerSet) PeersWithoutBlock(hash common.Hash) []*peer {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	result := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.knownBlocks.Contains(hash) {
			result = append(result, p)
		}
	}
	return result
}

func (ps *PeerSet) PeersWithoutTx(hash common.Hash) []*peer {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	result := make([]*peer, 0, len(ps.peers))
	for _, p := range ps.peers {
		if !p.knownTxs.Contains(hash) {
			result = append(result, p)
		}
	}
	return result
}

// find the best difficult peer
func (ps *PeerSet) BestPeer() *peer {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	var (
		bestPeer *peer
		bestTD   *big.Int
	)
	for _, p := range ps.peers {
		if _, td := p.Head(); bestPeer == nil || td.Cmp(bestTD) > 0 {
			bestPeer, bestTD = p, td
		}
	}
	return bestPeer
}

func (ps *PeerSet) Close() {
	ps.lock.Lock()
	defer ps.lock.Unlock()
	for _, p := range ps.peers {
		p.close()
	}
	ps.closed = true
}
