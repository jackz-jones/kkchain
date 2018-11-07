package types

import (
	"math/big"
	"testing"

	"encoding/json"

	"github.com/influxdata/influxdb/pkg/testing/assert"
	"github.com/invin/kkchain/common"
)

func mockTxs() Transactions {
	txs := make(Transactions, 3)
	for i, _ := range txs {
		txs[i] = &Transaction{
			TxData: txdata{
				Nonce:    uint64(i),
				GasPrice: new(big.Int).SetInt64(10000),
				GasLimit: 999999,
				Receiver: &common.Address{},
				Amount:   new(big.Int).SetInt64(1000),
				Payload:  []byte{0x12, 0x34, 0x56},
				Hash:     &common.Hash{},
			},
		}
	}
	return txs
}

func TestDeriveSha(t *testing.T) {
	txs := mockTxs()
	block := NewBlock(&Header{}, txs, nil)
	validateTxRoot := DeriveSha(block.Txs)
	assert.Equal(t, validateTxRoot, block.TxRoot())
}

func TestJsonMarshal(t *testing.T) {
	txs := mockTxs()
	preTxRoot := DeriveSha(txs)
	bytes, _ := json.Marshal(txs)
	marshalTxs := make(Transactions, len(txs))
	json.Unmarshal(bytes, &marshalTxs)
	afterTxRoot := DeriveSha(marshalTxs)
	assert.Equal(t, preTxRoot, afterTxRoot)
}
