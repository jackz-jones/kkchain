package types

import (
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"sort"
	"sync/atomic"
	"time"
	"unsafe"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/crypto/sha3"
	"github.com/invin/kkchain/rlp"
	"github.com/pkg/errors"
)

var (
	EmptyRootHash = DeriveSha(Transactions{})
)

// nonce for POW computation
type BlockNonce [8]byte

// from uint64 to BlockNonce
func EncodeNonce(i uint64) BlockNonce {
	var n BlockNonce
	binary.BigEndian.PutUint64(n[:], i)
	return n
}

// from BlockNonce to uint64
func (n BlockNonce) Uint64() uint64 {
	return binary.BigEndian.Uint64(n[:])
}

type Header struct {
	ParentHash  common.Hash    `json:"parentHash"       gencodec:"required"`
	Miner       common.Address `json:"miner"            gencodec:"required"`
	StateRoot   common.Hash    `json:"stateRoot"        gencodec:"required"`
	TxRoot      common.Hash    `json:"transactionsRoot" gencodec:"required"`
	ReceiptRoot common.Hash    `json:"receiptsRoot"     gencodec:"required"`
	Bloom       Bloom          `json:"logsBloom"        gencodec:"required"`
	Difficulty  *big.Int       `json:"difficulty"       gencodec:"required"`
	Number      *big.Int       `json:"number"           gencodec:"required"`
	GasLimit    uint64         `json:"gasLimit"         gencodec:"required"`
	GasUsed     uint64         `json:"gasUsed"          gencodec:"required"`
	Time        *big.Int       `json:"timestamp"        gencodec:"required"`
	Extra       []byte         `json:"extraData"        gencodec:"required"`
	MixDigest   common.Hash    `json:"mixHash"          gencodec:"required"`
	Nonce       BlockNonce     `json:"nonce"            gencodec:"required"`
}

// Hash returns the block hash of the header
func (h *Header) Hash() common.Hash {
	return rlpHash(h)
}

// HashNoNonce returns the hash which is used as input for the proof-of-work search.
func (h *Header) HashNoNonce() common.Hash {
	return rlpHash([]interface{}{
		h.ParentHash,
		h.Miner,
		h.StateRoot,
		h.TxRoot,
		h.ReceiptRoot,
		h.Bloom,
		h.Difficulty,
		h.Number,
		h.GasLimit,
		h.GasUsed,
		h.Time,
		h.Extra,
	})
}

// Size returns the approximate memory used by all internal contents. It is used
// to approximate and limit the memory consumption of various caches.
func (h *Header) Size() common.StorageSize {
	return common.StorageSize(unsafe.Sizeof(*h)) + common.StorageSize(len(h.Extra)+(h.Difficulty.BitLen()+h.Number.BitLen()+h.Time.BitLen())/8)
}

func rlpHash(x interface{}) (h common.Hash) {
	hw := sha3.NewKeccak256()
	rlp.Encode(hw, x)
	hw.Sum(h[:0])
	return h
}

// Body is a simple (mutable, non-safe) data container for storing and moving
// a block's data contents (transactions) together.
type Body struct {
	Transactions []*Transaction
}

type Block struct {
	Head *Header
	Txs  Transactions

	// caches
	hash atomic.Value
	size atomic.Value

	// Td is used by package core to store the total difficulty
	// of the chain up to and including the block.
	Td *big.Int

	// These fields are used by package eth to track
	// inter-peer block relay.
	ReceivedAt   time.Time
	ReceivedFrom interface{}
}

type StorageBlock Block

// "external" block encoding. used for protocol, etc.
type extblock struct {
	Head *Header
	Txs  []*Transaction
}

// "storage" block encoding. used for database.
type storageblock struct {
	Head *Header
	Txs  []*Transaction
	TD   *big.Int
}

func NewBlock(header *Header, txs []*Transaction, receipts []*Receipt) *Block {
	b := &Block{Head: CopyHeader(header), Td: new(big.Int)}
	if len(txs) != len(receipts) {
		panic(errors.New(fmt.Sprintf("failed to new block(%d) for the different count between txs and receipts", header.Number.Uint64())))
	}
	if len(txs) == 0 {
		b.Head.TxRoot = EmptyRootHash
	} else {
		b.Head.TxRoot = DeriveSha(Transactions(txs))
		b.Txs = make(Transactions, len(txs))
		copy(b.Txs, txs)
	}

	if len(receipts) == 0 {
		b.Head.ReceiptRoot = EmptyRootHash
	} else {
		b.Head.ReceiptRoot = DeriveSha(Receipts(receipts))
		b.Head.Bloom = CreateBloom(receipts)
	}

	return b
}

func NewBlockWithHeader(header *Header) *Block {
	return &Block{Head: CopyHeader(header)}
}

func CopyHeader(h *Header) *Header {
	cpy := *h
	if cpy.Time = new(big.Int); h.Time != nil {
		cpy.Time.Set(h.Time)
	}
	if cpy.Difficulty = new(big.Int); h.Difficulty != nil {
		cpy.Difficulty.Set(h.Difficulty)
	}
	if cpy.Number = new(big.Int); h.Number != nil {
		cpy.Number.Set(h.Number)
	}
	if len(h.Extra) > 0 {
		cpy.Extra = make([]byte, len(h.Extra))
		copy(cpy.Extra, h.Extra)
	}
	return &cpy
}

// DeprecatedTd is an old relic for extracting the TD of a block. It is in the
// code solely to facilitate upgrading the database from the old format to the
// new, after which it should be deleted. Do not use!
func (b *Block) DeprecatedTd() *big.Int {
	return b.Td
}

func (b *Block) DecodeRLP(s *rlp.Stream) error {
	var eb extblock
	_, size, _ := s.Kind()
	if err := s.Decode(&eb); err != nil {
		return err
	}
	b.Head, b.Txs = eb.Head, eb.Txs
	b.size.Store(common.StorageSize(rlp.ListSize(size)))
	return nil
}

// EncodeRLP serializes b into RLP block format.
func (b *Block) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, extblock{
		Head: b.Head,
		Txs:  b.Txs,
	})
}

func (b *StorageBlock) DecodeRLP(s *rlp.Stream) error {
	var sb storageblock
	if err := s.Decode(&sb); err != nil {
		return err
	}
	b.Head, b.Txs, b.Td = sb.Head, sb.Txs, sb.TD
	return nil
}

func (b *Block) Transactions() Transactions { return b.Txs }

func (b *Block) Transaction(hash common.Hash) *Transaction {
	for _, transaction := range b.Txs {
		if transaction.Hash() == hash {
			return transaction
		}
	}
	return nil
}

func (b *Block) Number() *big.Int     { return new(big.Int).Set(b.Head.Number) }
func (b *Block) GasLimit() uint64     { return b.Head.GasLimit }
func (b *Block) GasUsed() uint64      { return b.Head.GasUsed }
func (b *Block) Difficulty() *big.Int { return new(big.Int).Set(b.Head.Difficulty) }
func (b *Block) Time() *big.Int       { return new(big.Int).Set(b.Head.Time) }

func (b *Block) NumberU64() uint64        { return b.Head.Number.Uint64() }
func (b *Block) MixDigest() common.Hash   { return b.Head.MixDigest }
func (b *Block) Nonce() uint64            { return binary.BigEndian.Uint64(b.Head.Nonce[:]) }
func (b *Block) Bloom() Bloom             { return b.Head.Bloom }
func (b *Block) Miner() common.Address    { return b.Head.Miner }
func (b *Block) StateRoot() common.Hash   { return b.Head.StateRoot }
func (b *Block) ParentHash() common.Hash  { return b.Head.ParentHash }
func (b *Block) TxRoot() common.Hash      { return b.Head.TxRoot }
func (b *Block) ReceiptRoot() common.Hash { return b.Head.ReceiptRoot }
func (b *Block) Extra() []byte            { return common.CopyBytes(b.Head.Extra) }

func (b *Block) Header() *Header            { return CopyHeader(b.Head) }
func (b *Block) HeaderWithoutCopy() *Header { return b.Head }

// Body returns the non-header content of the block.
func (b *Block) Body() *Body { return &Body{b.Txs} }

func (b *Block) HashNoNonce() common.Hash {
	return b.Head.HashNoNonce()
}

// Size returns the true RLP encoded storage size of the block, either by encoding
// and returning it, or returning a previsouly cached value.
func (b *Block) Size() common.StorageSize {
	if size := b.size.Load(); size != nil {
		return size.(common.StorageSize)
	}
	c := writeCounter(0)
	rlp.Encode(&c, b)
	b.size.Store(common.StorageSize(c))
	return common.StorageSize(c)
}

func (b *Block) String() string {
	str := fmt.Sprintf(`
	number: %d
	{
		hash: %s
		parent: %s
		state: %s
		diff: 0x%x
		gaslimit: %d
		gasused: %d
		nonce: 0x%x
		tx: %d
        miner: %s
	}`+"\n", b.Number(), b.Hash().String(), b.ParentHash().String(), b.StateRoot().String(), b.Difficulty(), b.GasLimit(), b.GasUsed(), b.Nonce(), b.Txs.Len(), b.Miner().String())
	return str
}

type writeCounter common.StorageSize

func (c *writeCounter) Write(b []byte) (int, error) {
	*c += writeCounter(len(b))
	return len(b), nil
}

func (b *Block) WithSeal(header *Header) *Block {
	cpy := *header
	return &Block{
		Head: &cpy,
		Txs:  b.Txs,
	}
}

// WithBody returns a new block with the given transaction.
func (b *Block) WithBody(transactions []*Transaction) *Block {
	block := &Block{
		Head: CopyHeader(b.Head),
		Txs:  make([]*Transaction, len(transactions)),
	}
	copy(block.Txs, transactions)
	return block
}

// returns the hash of block header.
func (b *Block) Hash() common.Hash {
	if hash := b.hash.Load(); hash != nil {
		return hash.(common.Hash)
	}
	v := b.Head.Hash()
	b.hash.Store(v)
	return v
}

type Blocks []*Block

type BlockBy func(b1, b2 *Block) bool

func (self BlockBy) Sort(blocks Blocks) {
	bs := blockSorter{
		blocks: blocks,
		by:     self,
	}
	sort.Sort(bs)
}

type blockSorter struct {
	blocks Blocks
	by     func(b1, b2 *Block) bool
}

func (self blockSorter) Len() int { return len(self.blocks) }
func (self blockSorter) Swap(i, j int) {
	self.blocks[i], self.blocks[j] = self.blocks[j], self.blocks[i]
}
func (self blockSorter) Less(i, j int) bool { return self.by(self.blocks[i], self.blocks[j]) }

func Number(b1, b2 *Block) bool { return b1.Head.Number.Cmp(b2.Head.Number) < 0 }
