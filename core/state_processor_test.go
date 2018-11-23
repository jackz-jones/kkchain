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

func genTestTxsMap(chainId *big.Int) (map[common.Address]types.Transactions, int) {

	signer := types.NewInitialSigner(chainId)

	// Generate a batch of accounts to start with
	accoutNum := 1
	keys := make([]*ecdsa.PrivateKey, accoutNum)
	for i := 0; i < len(keys); i++ {
		keys[i], _ = crypto.GenerateKey()
	}

	// Generate a batch of transactions with overlapping values, but shifted nonces
	groups := map[common.Address]types.Transactions{}
	count := 0
	for start, key := range keys {
		addr := crypto.PubkeyToAddress(key.PublicKey)
		countPerCount := 2
		for i := 0; i < countPerCount; i++ {
			tx, _ := types.SignTx(types.NewTransaction(uint64(start+i), common.Address{}, big.NewInt(100), 100, big.NewInt(int64(start+i)), nil), signer, key)
			groups[addr] = append(groups[addr], tx)
		}
		count += countPerCount
	}

	return groups, count
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

	addr := crypto.PubkeyToAddress(key.PublicKey)
	fmt.Printf("address: %s\n", addr.String())

	//init balance of sender
	statedb.SetBalance(addr, new(big.Int).SetUint64(2e18))

	miner := common.HexToAddress("0x1e3d31aa1a7d72ac206bec98666a75968281655f")
	to := common.HexToAddress("0xd67487d6b9aec47bb15bcefd0f606d14c642af3e")

	txcount := 2
	txMaps := map[common.Address]types.Transactions{}

	txs := make(types.Transactions, txcount)
	for i := 0; i < len(txs); i++ {
		txs[i] = transaction(uint64(i), to, 200000, chainConfig.ChainID, key)
		fmt.Printf("tx hash: %s\n", txs[i].Hash().String())
	}

	txMaps[addr] = txs

	key, _ = crypto.GenerateKey()
	addr = crypto.PubkeyToAddress(key.PublicKey)
	fmt.Printf("address: %s\n", addr.String())
	statedb.SetBalance(addr, new(big.Int).SetUint64(2e18))
	txs = make(types.Transactions, txcount)
	for i := 0; i < len(txs); i++ {
		txs[i] = transaction(uint64(i), to, 200000, chainConfig.ChainID, key)
		fmt.Printf("tx hash: %s\n", txs[i].Hash().String())
	}

	txMaps[addr] = txs

	//txMaps, txcount := genTestTxsMap(chainConfig.ChainID)

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
	executedTxs, receipts, err := processor.ApplyTransactions(txMaps, txcount, header, statedb)
	if err != nil {
		return
	}
	end := time.Now()
	fmt.Printf("#####ApplyTransactions[%d/%d] cost %v\n", executedTxs.Len(), txcount, end.Sub(start))

	for i, tx := range executedTxs {
		fmt.Printf("#####tx[%d]: %s, cost: %d\n", i, tx.Hash().String(), receipts[i].GasUsed)
	}

	obj := statedb.GetOrNewStateObject(addr)
	fmt.Printf("%s:{nonce: %d, balance: %d}\n", obj.Address().String(), obj.Nonce(), obj.Balance())

	fmt.Printf("to: %d\nminer: %d\n", statedb.GetBalance(to), statedb.GetBalance(miner))

}

func TestStateProcessor_test(t *testing.T) {
	data := make([]int, 10, 20)
	data[0] = 1
	data[1] = 2
	dataappend := make([]int, 10, 20) //len <=10 则     result[0] = 99 会 影响源Slice
	dataappend[0] = 1
	dataappend[1] = 2
	result := append(data, dataappend...)
	fmt.Println("result length:", len(result), "cap:", cap(result), ":", result)
	fmt.Printf("result %p\n", result)
	result = append(result, 9)
	result[0] = 99
	result[11] = 98
	data[2] = 3
	fmt.Println("data length:", len(data), "cap:", cap(data), ":", data)
	fmt.Printf("data %p\n", data)
	fmt.Println("result length:", len(result), "cap:", cap(result), ":", result)
	fmt.Printf("result %p\n", result)
	fmt.Println("dataappend length:", len(dataappend), "cap:", cap(dataappend), ":", dataappend)
	fmt.Printf("dataappend %p\n", dataappend)
}
