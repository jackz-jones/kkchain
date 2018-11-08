package chain

import (
	"context"
	"math"
	"sync/atomic"

	"math/big"
	"math/rand"

	"encoding/hex"

	"github.com/gogo/protobuf/proto"
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/event"
	"github.com/invin/kkchain/p2p"
	"github.com/invin/kkchain/sync"
	syncPeer "github.com/invin/kkchain/sync/peer"
	logrus "github.com/sirupsen/logrus"
)

// Create a new instance of logger
var log = logrus.New()

const (
	protocolChain = "/kkchain/p2p/chain/1.0.0"

	// txChanSize is the size of channel listening to NewTxsEvent.
	// The number is referenced from the size of tx pool.
	txChanSize = 4096

	// This is the target size for the packs of transactions sent by txsyncLoop.
	// A pack can get larger than this if a single transactions exceeds this size.
	txsyncPackSize = 100 * 1024
)

type txsync struct {
	p   *peer
	txs []*types.Transaction
}

// Chain implements protocol for chain related messages
type Chain struct {
	// self
	host p2p.Host

	// use to manager broadcasting for remote
	peers    *PeerSet
	maxPeers int

	blockchain *core.BlockChain

	txPool *core.TxPool

	mineBlockSub event.Subscription

	newMinedBlockCh chan core.NewMinedBlockEvent

	acceptTxs uint32 // Flag whether we're considered synchronised (enables transaction processing)

	syncer *sync.Syncer

	quitCh chan struct{}

	// tx sync
	txsCh    chan core.NewTxsEvent
	txsSub   event.Subscription
	txsyncCh chan *txsync
	quitSync chan struct{}
}

func init() {
	log.SetLevel(logrus.DebugLevel)
}

// New creates a new Chain object
func New(host p2p.Host, bc *core.BlockChain, txpool *core.TxPool) *Chain {
	c := &Chain{
		host:       host,
		blockchain: bc,
		txPool:     txpool,
		peers:      NewPeerSet(),
		quitCh:     make(chan struct{}),
		txsyncCh:   make(chan *txsync),
		quitSync:   make(chan struct{}),
	}
	c.newMinedBlockCh = make(chan core.NewMinedBlockEvent, 256)

	if err := host.SetMessageHandler(protocolChain, c.handleMessage); err != nil {
		panic(err)
	}

	host.Register(c)
	c.syncer = sync.New(c, bc)

	return c
}

func (c *Chain) GetBlockChain() *core.BlockChain {
	return c.blockchain
}

// handleMessage handles messages within the stream
func (c *Chain) handleMessage(conn p2p.Conn, msg proto.Message) {
	// check message type
	switch message := msg.(type) {
	case *Message:
		c.doHandleMessage(conn, message)
	default:
		conn.Close()
		log.Errorf("unexpected message: %v", msg)
	}
}

// doHandleMessage handles messsage
func (c *Chain) doHandleMessage(conn p2p.Conn, msg *Message) {
	// get handler
	handler := c.handlerForMsgType(msg.GetType())
	if handler == nil {
		conn.Close()
		log.Errorf("unknown message type: %v", msg.GetType())
		return
	}

	// dispatch handler
	ctx := context.Background()
	pid := conn.RemotePeer()

	rpmes, err := handler(ctx, pid, msg)

	// if nil response, return it before serializing
	if rpmes == nil {
		//log.Warning("got back nil response from request")
		if err != nil {
			log.Error("failed to make response for request,error: %v", err)
		}
		return
	}

	// send out response msg
	if err = conn.WriteMessage(rpmes, protocolChain); err != nil {
		conn.Close()
		log.Errorf("send response error: %s", err)
		return
	}
}

func (c *Chain) Connected(conn p2p.Conn) {

	// create a peer with this conn, and register
	peer := NewPeer(conn)
	c.peers.Register(peer)

	// Propagate existing transactions when a new peer connect in
	c.AcceptTxs()
	c.syncTransactions(peer)

	log.WithFields(logrus.Fields{
		"remote_address": conn.RemotePeer().Address,
		"remote_id":      hex.EncodeToString(conn.RemotePeer().PublicKey),
	}).Info("a conn is notified")
	currentBlock := c.blockchain.CurrentBlock()
	if currentBlock == nil {
		log.Warning("local chain current block is nil")
		return
	}
	chainID := c.blockchain.ChainID()

	// td is stored in db, can not get it by currentBlock.Td
	td := c.blockchain.GetTd(currentBlock.Hash(), currentBlock.NumberU64())
	if td == nil {
		td = new(big.Int).SetInt64(2)
	}
	currentBlockHash := currentBlock.Hash().Bytes()
	currentBlockNum := currentBlock.NumberU64()
	genesisBlockHash := c.blockchain.GenesisBlock().Hash().Bytes()

	chainMsg := &ChainStatusMsg{
		ChainID:          chainID,
		Td:               td.Bytes(),
		CurrentBlockHash: currentBlockHash,
		CurrentBlockNum:  currentBlockNum,
		GenesisBlockHash: genesisBlockHash,
	}
	chainStatueMsg := NewMessage(Message_CHAIN_STATUS, chainMsg)
	err := conn.WriteMessage(chainStatueMsg, protocolChain)
	if err != nil {
		log.Errorf("failed to send chain status msg to %s", conn.RemotePeer())
	}
}

func (c *Chain) Disconnected(conn p2p.Conn) {
	log.Infof("a disconn is notified,remote ID: %s", conn.RemotePeer())
	id := hex.EncodeToString(conn.RemotePeer().PublicKey)
	c.peers.Unregister(id)
}

func (c *Chain) RemovePeer(id string) {
	// Short circuit if the peer was already removed
	peer := c.peers.Peer(id)
	if peer == nil {
		return
	}
	log.Debugf("Removing KKChain peer,id: %v", id)

	// Unregister the peer from the downloader and peer set
	// pm.downloader.UnregisterPeer(id)
	if err := c.peers.Unregister(id); err != nil {
		log.WithFields(logrus.Fields{
			"peer": id,
			"err":  err,
		}).Error("Peer removal failed")
	}
	// Hard disconnect at the networking layer
	// if peer != nil {
	// 	peer.Peer.Disconnect(p2p.DiscUselessPeer)
	// }
}

func (c *Chain) Start(maxPeers int) {
	c.maxPeers = maxPeers

	// broadcast transactions
	c.txsCh = make(chan core.NewTxsEvent, txChanSize)
	c.txsSub = c.txPool.SubscribeNewTxsEvent(c.txsCh)
	go c.txBroadcastLoop()

	// broadcast mined blocks
	c.mineBlockSub = c.blockchain.SubscribeNewMinedBlockEvent(c.newMinedBlockCh)
	go c.minedBroadcastLoop()

	// start sync handlers
	go c.syncer.Start()
	go c.txsyncLoop()
}

func (c *Chain) txBroadcastLoop() {
	for {
		select {
		case event := <-c.txsCh:
			c.BroadcastTxs(event.Txs)

			// Err() channel will be closed when unsubscribing.
		case <-c.txsSub.Err():
			return
		}
	}
}

// BroadcastTxs will propagate a batch of transactions to all peers which are not known to
// already have the given transaction.
func (c *Chain) BroadcastTxs(txs types.Transactions) {
	var txset = make(map[*peer]types.Transactions)

	// Broadcast transactions to a batch of peers not knowing about it
	for _, tx := range txs {
		peers := c.peers.PeersWithoutTx(tx.Hash())
		for _, peer := range peers {
			txset[peer] = append(txset[peer], tx)
		}
		log.WithFields(logrus.Fields{
			"hash":       tx.Hash().String(),
			"recipients": len(peers),
		}).Info("Broadcast transaction")
	}
	// FIXME include this again: peers = peers[:int(math.Sqrt(float64(len(peers))))]
	for peer, txs := range txset {
		peer.AsyncSendTransactions(txs)
	}
}

// syncTransactions starts sending all currently pending transactions to the given peer.
func (c *Chain) syncTransactions(p *peer) {
	var txs types.Transactions
	pending, _, _ := c.txPool.Pending()
	for _, batch := range pending {
		txs = append(txs, batch...)
	}
	if len(txs) == 0 {
		return
	}
	select {
	case c.txsyncCh <- &txsync{p, txs}:
	case <-c.quitSync:
	}
}

// txsyncLoop takes care of the initial transaction sync for each new
// connection. When a new peer appears, we relay all currently pending
// transactions. In order to minimise egress bandwidth usage, we send
// the transactions in small packs to one peer at a time.
func (c *Chain) txsyncLoop() {
	var (
		pending = make(map[string]*txsync)
		sending = false               // whether a send is active
		pack    = new(txsync)         // the pack that is being sent
		done    = make(chan error, 1) // result of the send
	)

	// send starts a sending a pack of transactions from the sync.
	send := func(s *txsync) {
		// Fill pack with transactions up to the target size.
		size := common.StorageSize(0)
		pack.p = s.p
		pack.txs = pack.txs[:0]
		for i := 0; i < len(s.txs) && size < txsyncPackSize; i++ {
			pack.txs = append(pack.txs, s.txs[i])
			size += s.txs[i].Size()
		}
		// Remove the transactions that will be sent.
		s.txs = s.txs[:copy(s.txs, s.txs[len(pack.txs):])]
		if len(s.txs) == 0 {
			delete(pending, s.p.ID)
		}
		// Send the pack in the background.
		log.WithFields(logrus.Fields{
			"count": len(pack.txs),
			"bytes": size,
		}).Info("Sending batch of transactions")
		sending = true
		go func() { done <- pack.p.SendTransactions(pack.txs) }()
	}

	// pick chooses the next pending sync.
	pick := func() *txsync {
		if len(pending) == 0 {
			return nil
		}
		n := rand.Intn(len(pending)) + 1
		for _, s := range pending {
			if n--; n == 0 {
				return s
			}
		}
		return nil
	}

	for {
		select {
		case s := <-c.txsyncCh:
			pending[s.p.ID] = s
			if !sending {
				send(s)
			}
		case err := <-done:
			sending = false
			// Stop tracking peers that cause send failures.
			if err != nil {
				log.Debugf("Transaction send failed,err: %v", err)
				delete(pending, pack.p.ID)
			}
			// Schedule the next send.
			if s := pick(); s != nil {
				send(s)
			}
		case <-c.quitSync:
			return
		}
	}
}

func (c *Chain) Stop() {
	close(c.quitCh)
	c.syncer.Stop()
	c.txsSub.Unsubscribe() // quits txBroadcastLoop
	close(c.quitSync)
	c.mineBlockSub.Unsubscribe() // quits blockBroadcastLoop
	c.peers.Close()
}

// Mined broadcast loop
func (c *Chain) minedBroadcastLoop() {
	for {
		select {
		case newMinedBlockCh := <-c.newMinedBlockCh:

			// fill up td
			block := newMinedBlockCh.Block
			if block.Td == nil {
				parentTD := c.blockchain.GetTd(block.ParentHash(), block.NumberU64()-1)
				block.Td = new(big.Int).Add(parentTD, block.Difficulty())
			}

			c.BroadcastBlock(block, true)  // First propagate block to peers
			c.BroadcastBlock(block, false) // Only then announce to the rest

		case <-c.quitCh:
			return
		}
	}
}

// will only announce it's availability (depending what's requested).
func (c *Chain) BroadcastBlock(block *types.Block, propagate bool) {
	hash := block.Hash()
	peers := c.peers.PeersWithoutBlock(hash)

	// If propagation is requested, send to a subset of the peer
	if propagate {
		// Calculate the TD of the block (it's not imported yet, so block.Td is not valid)
		// var td *big.Int
		// if parent := c.blockchain.GetBlock(block.ParentHash(), block.NumberU64()-1); parent != nil {
		// 	td = new(big.Int).Add(block.Difficulty(), c.blockchain.GetTd(block.ParentHash(), block.NumberU64()-1))
		// } else {
		// 	log.Error("Propagating dangling block", "number", block.Number(), "hash", hash)
		// 	return
		// }
		// Send the block to a subset of our peers
		transfer := peers[:int(math.Sqrt(float64(len(peers))))]
		for _, peer := range transfer {
			peer.SendNewBlock(block)
		}
		return
	}
	// Otherwise if the block is indeed in out own chain, announce it
	if c.blockchain.HasBlock(hash, block.NumberU64()) {
		for _, peer := range peers {
			peer.SendNewBlockHashes([]common.Hash{block.Hash()}, []uint64{block.NumberU64()})
		}
	}
}

// // Blockchain returns the target blockchain
// func (c *Chain) Blockchain() {
// 	return c.blockchain
// }

// // Peers returns the active peers
// func (c *Chain) Peers() {
// 	return c.peers
// }

// AcceptTxs sets flag on for accepts transactions
func (c *Chain) AcceptTxs() {
	atomic.StoreUint32(&c.acceptTxs, 1) // Mark initial sync done on any fetcher import
}

func (c *Chain) Peers() syncPeer.PeerSet {
	return NewDPeerSet(c.peers)
}

// DPeerSet is a thin wrapper for original peerset, through which we can do testing easily
type DPeerSet struct {
	ps *PeerSet
}

// NewDPeerSet creates a download peerset
func NewDPeerSet(ps *PeerSet) *DPeerSet {
	return &DPeerSet{
		ps: ps,
	}
}

// Register registers peer
func (s *DPeerSet) Register(p syncPeer.Peer) error {
	panic("not supported yet")
	return nil
}

// UnRegister unregisters peer specified by id
func (s *DPeerSet) UnRegister(id string) error {
	panic("not supported yet")
	return nil
}

// Peer returns the peer with specified id
func (s *DPeerSet) Peer(id string) syncPeer.Peer {
	p := s.ps.Peer(id)
	return NewDPeer(p)
}

// BestPeer returns the best peer
func (s *DPeerSet) BestPeer() syncPeer.Peer {
	if p := s.ps.BestPeer(); p != nil {
		return NewDPeer(p)
	}

	log.Warning("found no best peer, possible no peers")
	return nil
}

// DPeer represent a peer for downloading. currently, It is a wrapper for peer
type DPeer struct {
	p *peer
}

// NewDPeer represents a peer for downloading
func NewDPeer(p *peer) *DPeer {
	return &DPeer{
		p: p,
	}
}

// ID returns the identification of the peer
func (dp *DPeer) ID() string {
	return dp.p.ID
}

// Head returns the current head of the peer
func (dp *DPeer) Head() (hash common.Hash, td *big.Int) {
	return dp.p.Head()
}

// RequestHeadersByHash fetches a batch of blocks' headers corresponding to the
// specified header query, based on the hash of an origin block.
func (dp *DPeer) RequestHeadersByHash(origin common.Hash, amount int, skip int, reverse bool) error {
	return dp.p.requestHeadersByHash(origin, amount, skip, reverse)
}

// RequestHeadersByNumber fetches a batch of blocks' headers corresponding to the
// specified header query, based on the number of an origin block.
func (dp *DPeer) RequestHeadersByNumber(origin uint64, amount int, skip int, reverse bool) error {
	return dp.p.requestHeadersByNumber(origin, amount, skip, reverse)
}

// RequestBlocksByNumber fetches a batch of blocks corresponding to the
// specified range
func (dp *DPeer) RequestBlocksByNumber(origin uint64, amount int) error {
	return dp.p.requestBlocksByNumber(origin, amount)
}
