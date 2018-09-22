package chain

import (
	"fmt"
	"testing"
)

func TestNewMessage(t *testing.T) {
	chainMsg := &ChainStatusMsg{
		ChainID:          123,
		Td:               []byte("433223"),
		CurrentBlockHash: []byte("433223"),
		CurrentBlockNum:  uint64(250),
		GenesisBlockHash: []byte("433223"),
	}
	m1 := NewMessage(Message_CHAIN_STATUS, chainMsg)
	fmt.Printf("\nchain msg: %v\n", m1)

	dataMsg := &DataMsg{
		Data: []byte{0x34},
	}
	m2 := NewMessage(Message_GET_BLOCK_BODIES, dataMsg)
	fmt.Printf("\nget block bodies msg: %v\n", m2)

	getBlockHeadersMsg := &GetBlockHeadersMsg{
		StartNum:  uint64(1),
		StartHash: []byte{0x34, 0x56},
		Amount:    23,
		Skip:      23,
		Reverse:   false,
	}
	m3 := NewMessage(Message_GET_BLOCK_HEADERS, getBlockHeadersMsg)
	fmt.Printf("\nget block headers msg: %v\n", m3)
}
