package chain

import (
	"context"

	"bytes"

	"encoding/json"

	"encoding/hex"

	"math/big"

	"time"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/p2p"
	sc "github.com/invin/kkchain/sync/common"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

var (
	errEmptyMsgContent = errors.New("empty msg content")
)

const (
	softResponseLimit = 2 * 1024 * 1024 // Target maximum size of returned blocks, headers or node data.
	estHeaderJSONSize = 500             // Approximate size of an RLP encoded block header
)

// chainHandler specifies the signature of functions that handle DHT messages.
type chainHandler func(context.Context, p2p.ID, *Message) (*Message, error)

func (c *Chain) handlerForMsgType(t Message_Type) chainHandler {
	switch t {
	case Message_CHAIN_STATUS:
		return c.handleChainStatus
	case Message_GET_BLOCK_BODIES:
		return c.handleGetBlockBodies
	case Message_BLOCKS_BODIES:
		return c.handleBlockBodies
	case Message_GET_BLOCK_HEADERS:
		return c.handleGetBlockHeaders
	case Message_BLOCK_HEADERS:
		return c.handleBlockHeaders
	case Message_TRANSACTIONS:
		return c.handleTransactions
	case Message_GET_RECEIPTS:
		return c.handleGetReceipts
	case Message_RECEIPTS:
		return c.handleReceipts
	case Message_NEW_BLOCK_HASHS:
		return c.handleNewBlockHashs
	case Message_NEW_BLOCK:
		return c.handleNewBlock
	case Message_GET_BLOCKS:
		return c.handleGetBlocks
	case Message_FETCH_BLOCKS:
		return c.handleFetchBlocks
	case Message_BLOCKS:
		return c.handleBlocks
	case Message_BLOCKS_FETCHED:
		return c.handleBlocksFetched
	default:
		return nil
	}
}

// only retrive chain status
func (c *Chain) handleChainStatus(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	peerID := hex.EncodeToString(p.PublicKey)

	localChainID := c.blockchain.ChainID()
	currentBlock := c.blockchain.CurrentBlock()

	localChainTD := currentBlock.Td
	if localChainTD == nil {
		localChainTD = new(big.Int)
	}

	localChainGenesisBlock := c.blockchain.GenesisBlock().Hash().Bytes()
	remoteChainStatus := pmes.ChainStatusMsg
	if remoteChainStatus == nil {
		c.peers.Unregister(peerID)
		return nil, errEmptyMsgContent
	}
	if localChainID != remoteChainStatus.ChainID ||
		!bytes.Equal(localChainGenesisBlock, remoteChainStatus.GenesisBlockHash) {
		c.peers.Unregister(peerID)
		return nil, errors.Errorf("remote node %s is in different chain", p.String())
	}

	// set head and td for this peer
	remoteHeadHash := common.BytesToHash(remoteChainStatus.CurrentBlockHash)
	remoteTD := new(big.Int).SetBytes(remoteChainStatus.Td)
	c.peers.Peer(peerID).SetHead(remoteHeadHash, remoteTD)

	return nil, nil
}

func (c *Chain) handleGetBlockBodies(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	var resp *Message
	msg := pmes.DataMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	// collect bodies
	bodies := []*types.Body{}
	hashes := []common.Hash{}
	err = json.Unmarshal(msg.Data, &hashes)
	if err != nil {
		log.Errorf("failed to unmarshal bytes to []common hash,error: %v", err)
		return nil, err
	}

	for _, hash := range hashes {
		block := c.blockchain.GetBlockByHash(hash)
		if block == nil {
			log.Errorf("failed to get block %s from local,error: %v", hash.String(), err)
			continue
		}
		body := block.Body()
		if body != nil {
			bodies = append(bodies, body)
		}
	}

	bbytes, err := json.Marshal(bodies)
	if err != nil {
		log.Errorf("failed to marshal bodies to bytes,error: %v", err)
		return nil, err
	}

	blockBodiesMsg := &DataMsg{
		Data: bbytes,
	}
	resp = NewMessage(Message_BLOCKS_BODIES, blockBodiesMsg)
	return resp, nil
}

func (c *Chain) handleGetBlockHeaders(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	var resp *Message
	msg := pmes.GetBlockHeadersMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	hashMode := len(msg.StartHash) == common.HashLength
	first := true
	maxNonCanonical := uint64(100)

	// Gather headers until the fetch or network limits is reached
	var (
		bytes   int
		headers []*types.Header
		unknown bool
	)

	originNum := msg.StartNum
	originHash := common.BytesToHash(msg.StartHash)

	for !unknown && len(headers) < int(msg.Amount) && bytes < softResponseLimit && len(headers) < sc.MaxHeaderFetch {
		// Retrieve the next header satisfying the query
		var origin *types.Header
		if hashMode {
			if first {
				first = false
				origin = c.blockchain.GetHeaderByHash(originHash)
				if origin != nil {
					originNum = origin.Number.Uint64()
				}
			} else {
				origin = c.blockchain.GetHeader(originHash, originNum)
			}
		} else {
			origin = c.blockchain.GetHeaderByNumber(originNum)
		}
		if origin == nil {
			break
		}
		headers = append(headers, origin)
		bytes += estHeaderJSONSize

		// Advance to the next header of the query
		switch {
		case hashMode && msg.Reverse:
			// Hash based traversal towards the genesis block
			ancestor := msg.Skip + 1
			if ancestor == 0 {
				unknown = true
			} else {
				originHash, originNum = c.blockchain.GetAncestor(originHash, originNum, ancestor, &maxNonCanonical)
				unknown = (originHash == common.Hash{})
			}
		case hashMode && !msg.Reverse:
			// Hash based traversal towards the leaf block
			var (
				current = origin.Number.Uint64()
				next    = current + msg.Skip + 1
			)
			if next <= current {
				log.WithFields(logrus.Fields{
					"current":  current,
					"skip":     msg.Skip,
					"next":     next,
					"attacker": p.String(),
				}).Warning("GetBlockHeaders skip overflow attack")
				unknown = true
			} else {
				if header := c.blockchain.GetHeaderByNumber(next); header != nil {
					nextHash := header.Hash()
					expOldHash, _ := c.blockchain.GetAncestor(nextHash, next, msg.Skip+1, &maxNonCanonical)
					if expOldHash == originHash {
						originHash, originNum = nextHash, next
					} else {
						unknown = true
					}
				} else {
					unknown = true
				}
			}
		case msg.Reverse:
			// Number based traversal towards the genesis block
			if originNum >= msg.Skip+1 {
				originNum -= msg.Skip + 1
			} else {
				unknown = true
			}

		case !msg.Reverse:
			// Number based traversal towards the leaf block
			originNum += msg.Skip + 1
		}
	}

	// append headers from local blockchain
	hbytes, err := json.Marshal(headers)
	if err != nil {
		log.Errorf("failed to marshal %d block header to bytes", len(headers))
		return nil, err // FIXME: pass through?
	}

	// response block headers msg
	blockHeadersMsg := &DataMsg{
		Data: hbytes,
	}
	resp = NewMessage(Message_BLOCK_HEADERS, blockHeadersMsg)
	return resp, nil
}

func (c *Chain) handleBlockBodies(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	msg := pmes.DataMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	bodies := []*types.Body{}
	err = json.Unmarshal(msg.Data, &bodies)
	if err != nil {
		log.Errorf("failed to unmarshal bytes to block body,error: %v", err)
		return nil, err
	}

	// TODO: execute received body tx to local chain

	// no resp for block bodies
	return nil, nil
}

func (c *Chain) handleBlockHeaders(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	msg := pmes.DataMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	pid := hex.EncodeToString(p.PublicKey)
	headers := []*types.Header{}
	err = json.Unmarshal(msg.Data, &headers)
	if err != nil {
		log.Errorf("failed to unmarshal bytes to block header,error: %v", err)
		return nil, err
	}

	// FIXME: is ID right?
	if len(headers) > 0 {
		c.syncer.DeliverHeaders(pid, headers)
	}
	// no response for block headers msg
	return nil, nil
}

func (c *Chain) handleTransactions(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	msg := pmes.DataMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	txs := []*types.Transaction{}
	err = json.Unmarshal(msg.Data, &txs)
	if err != nil {
		log.Errorf("failed to unmarshal bytes to transaction,error: %v", err)
		return nil, err
	}

	// TODO: execute received tx ..

	// no resp
	return nil, nil
}

func (c *Chain) handleGetReceipts(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	var resp *Message
	msg := pmes.DataMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	receiptHashes := []common.Hash{}
	err = json.Unmarshal(msg.Data, &receiptHashes)
	if err != nil {
		log.Errorf("failed to unmarshal bytes to transaction,error: %v", err)
		return nil, err
	}

	receipts := []*types.Receipt{}
	for _, receiptHash := range receiptHashes {
		receipt := c.blockchain.GetReceiptByHash(receiptHash)
		if receipt == nil {
			continue
		}
		receipts = append(receipts, receipt)
	}

	rbytes, err := json.Marshal(receipts)
	if err != nil {
		log.Errorf("failed to marshal receipt to bytes,error: %v", err)
		return nil, err
	}

	receiptsMsg := &DataMsg{
		Data: rbytes,
	}
	resp = NewMessage(Message_RECEIPTS, receiptsMsg)
	return resp, nil
}

func (c *Chain) handleReceipts(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	msg := pmes.DataMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	receipts := []*types.Receipt{}
	err = json.Unmarshal(msg.Data, &receipts)
	if err != nil {
		log.Errorf("failed to unmarshal bytes to receipt,error: %v", err)
		return nil, err
	}

	// TODO: update local state ..

	// no resp
	return nil, nil
}

func (c *Chain) handleNewBlockHashs(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	msg := pmes.DataMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	id := hex.EncodeToString(p.PublicKey)
	peer := c.peers.Peer(id)

	// collect block headers
	unknown := make(newBlockHashesData, 0)
	newBlockHash := newBlockHashesData{}
	err = json.Unmarshal(msg.Data, &newBlockHash)
	if err != nil {
		log.Errorf("failed to unmarshal bytes to common hash,error: %v", err)
		return nil, err
	}

	for i, _ := range newBlockHash {
		// mark remote peer known the block
		peer.MarkBlock(newBlockHash[i].Hash)

		if !c.blockchain.HasBlock(newBlockHash[i].Hash, newBlockHash[i].Number) {
			unknown = append(unknown, newBlockHash[i])
		}
	}

	// schedule all unknown hashes for retrival
	for _, block := range unknown {
		c.syncer.Notify(id, block.Hash, block.Number, time.Now(), peer.requestBlocksByHash)
	}

	return nil, nil
}

func (c *Chain) handleNewBlock(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	msg := pmes.DataMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	pid := hex.EncodeToString(p.PublicKey)
	peer := c.peers.Peer(pid)

	blocks := []*types.Block{}
	err = json.Unmarshal(msg.Data, &blocks)
	if err != nil {
		log.Errorf("failed to unmarshal bytes to block,error: %v", err)
		return nil, err
	}

	for _, receiveBlock := range blocks {
		log.WithFields(logrus.Fields{
			"number":      receiveBlock.Header().Number,
			"hash":        receiveBlock.Hash().String(),
			"parent_hash": receiveBlock.ParentHash().String(),
			"difficulty":  receiveBlock.Difficulty(),
		}).Info("receive a new block")

		// fill up receive time and origin peer
		receiveBlock.ReceivedAt = time.Now()
		receiveBlock.ReceivedFrom = p

		// mark remote peer hash known this block
		peer.MarkBlock(receiveBlock.Hash())

		// schedule import new block
		c.syncer.Enqueue(pid, receiveBlock)

		var (
			trueHead = receiveBlock.ParentHash()
			trueTD   = new(big.Int).Sub(receiveBlock.Td, receiveBlock.Header().Difficulty)
		)

		// Update the peers total difficulty if better than the previous
		if _, td := peer.Head(); trueTD.Cmp(td) > 0 {
			peer.SetHead(trueHead, trueTD)

			// Schedule a sync if above ours. Note, this will not fire a sync for a gap of
			// a singe block (as the true TD is below the propagated block), however this
			// scenario should easily be covered by the fetcher.
			currentBlock := c.blockchain.CurrentBlock()
			if trueTD.Cmp(c.blockchain.GetTd(currentBlock.Hash(), currentBlock.NumberU64())) > 0 {
				go c.syncer.Synchronise(NewDPeer(peer))
			}
		}
	}

	if len(blocks) == 1 {
		c.syncer.NewBlock(pid, blocks[0])
	}

	// no resp
	return nil, nil
}

func (c *Chain) handleGetBlocks(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	msg := pmes.GetBlocksMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	blocks := []*types.Block{}
	
	for i := msg.StartNum; i <= msg.StartNum+msg.Amount; i++ {
		block := c.blockchain.GetBlockByNumber(i)
		if block == nil {
			log.Warnf("Failed to get block %d", i)
			break
		}
		blocks = append(blocks, block)
	}
	
	return c.buildBlocksMsg(Message_BLOCKS, blocks)	
}

func (c *Chain) handleFetchBlocks(ctx context.Context, p p2p.ID, pmes *Message) (_ *Message, err error) {
	msg := pmes.FetchBlocksMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	blocks := []*types.Block{}
	
	for _, hash := range msg.Hashes {
		temp := common.BytesToHash(hash)
		block := c.blockchain.GetBlockByHash(temp)
		if block == nil {
			log.Debug("Failed to get block ", temp.String())
			break
		}

		blocks = append(blocks, block)
	}
	
	return c.buildBlocksMsg(Message_BLOCKS_FETCHED, blocks)
	
}

func (c *Chain) buildBlocksMsg(msgid Message_Type, blocks []*types.Block) (*Message, error) {
	bbytes, err := json.Marshal(blocks)
	if err != nil {
		log.Errorf("failed to marshal %d blocks", len(blocks))
		return nil, err
	}

	dataMsg := &DataMsg{
		Data: bbytes,
	}
	return NewMessage(msgid, dataMsg), nil
}

func (c *Chain) handleBlocks(ctx context.Context, p p2p.ID, pmes *Message) (*Message, error) {
	return c.deliverBlocks(ctx, p, pmes, false)
}

func (c *Chain) handleBlocksFetched(ctx context.Context, p p2p.ID, pmes *Message) (*Message, error) {
	return c.deliverBlocks(ctx, p, pmes, true)
}

func (c *Chain) deliverBlocks(ctx context.Context, p p2p.ID, pmes *Message, fetched bool) (_ *Message, err error) {
	msg := pmes.DataMsg
	if msg == nil {
		return nil, errEmptyMsgContent
	}

	pid := hex.EncodeToString(p.PublicKey)
	blocks := []*types.Block{}
	err = json.Unmarshal(msg.Data, &blocks)
	if err != nil {
		log.Errorf("failed to unmarshal bytes to block,error: %v", err)
		return nil, err
	}

	//mark remote peer hash known this block
	for _, block := range blocks {
		c.peers.Peer(pid).MarkBlock(block.Hash())
		log.WithFields(logrus.Fields{
			"number":      block.Header().Number,
			"hash":        block.Hash().String(),
			"parent_hash": block.ParentHash().String(),
			"difficulty":  block.Difficulty(),
		}).Info("receive history block")
	}

	c.syncer.DeliverBlocks(pid, blocks, fetched)

	// no resp
	return nil, nil
}
