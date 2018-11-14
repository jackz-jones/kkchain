package core

import (
	"crypto/ecdsa"
	"math/big"
	"testing"

	"fmt"
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/crypto"
	"github.com/invin/kkchain/params"
	"github.com/invin/kkchain/storage/memdb"
	"time"
)

func transaction(nonce uint64, to common.Address, gaslimit uint64, chainId *big.Int, key *ecdsa.PrivateKey) *types.Transaction {
	return pricedTransaction(nonce, to, gaslimit, big.NewInt(1), chainId, key)
}

func pricedTransaction(nonce uint64, to common.Address, gaslimit uint64, gasprice, chainId *big.Int, key *ecdsa.PrivateKey) *types.Transaction {
	tx, _ := types.SignTx(types.NewTransaction(nonce, to, big.NewInt(100), gaslimit, gasprice, nil), types.NewInitialSigner(chainId), key)
	return tx
}

func TestStateProcessor_ApplyTransactions(t *testing.T) {
	t.Parallel()

	db := memdb.New()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db))
	if err != nil {
		t.Errorf("new state db failed. error: %v", err)
		return
	}

	chainConfig := params.TestChainConfig

	processor := NewStateParallelProcessor(chainConfig, nil)

	key, _ := crypto.GenerateKey()
	key.Public()

	addr := crypto.PubkeyToAddress(key.PublicKey)
	t.Logf("address: %s\n", addr.String())
	fmt.Printf("address: %s\n", addr.String())

	//init balance of sender
	statedb.SetBalance(addr, new(big.Int).SetUint64(2e18))

	miner := common.HexToAddress("0x1e3d31aa1a7d72ac206bec98666a75968281655f")
	to := common.HexToAddress("0xd67487d6b9aec47bb15bcefd0f606d14c642af3e")

	txcount := 10
	txs := make(types.Transactions, txcount)
	for i := 0; i < len(txs); i++ {
		txs[i] = transaction(uint64(i), to, 200000, chainConfig.ChainID, key)
	}

	header := &types.Header{
		Miner:       miner,
		Number:      big.NewInt(int64(0)),
		ParentHash:  common.Hash{},
		Difficulty:  big.NewInt(1),
		TxRoot:      types.EmptyRootHash,
		ReceiptRoot: types.EmptyRootHash,
		GasLimit:    GenesisGasLimit,
		Time:        big.NewInt(time.Now().Unix()),
	}

	start := time.Now()
	_, _, err = processor.ApplyTransactions(txs, header, statedb)
	if err != nil {
		return
	}
	end := time.Now()
	fmt.Printf("#####ApplyTransactions[%d] cost %v\n", txcount, end.Sub(start))

	obj := statedb.GetOrNewStateObject(addr)
	fmt.Printf("%s:{nonce: %d, balance: %d}\n", obj.Address().String(), obj.Nonce(), obj.Balance())

	fmt.Printf("to: %d\nminer: %d\n", statedb.GetBalance(to), statedb.GetBalance(miner))

}
