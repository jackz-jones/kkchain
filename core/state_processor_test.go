package core

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/crypto"
	"github.com/invin/kkchain/params"
	"github.com/invin/kkchain/storage/memdb"
	"github.com/sirupsen/logrus"
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

func TestStateProcessor_ApplyTransactions2(t *testing.T) {
	t.Parallel()

	db := memdb.New()
	statedb, err := state.New(common.Hash{}, state.NewDatabase(db))
	if err != nil {
		t.Errorf("new state db failed. error: %v", err)
		return
	}

	chainConfig := params.TestChainConfig

	//processor := NewStateParallelProcessor(chainConfig, nil)
	processor := NewStateProcessor(chainConfig, nil)

	key, _ := crypto.GenerateKey()

	addr := crypto.PubkeyToAddress(key.PublicKey)
	fmt.Printf("address: %s\n", addr.String())

	//init balance of sender
	statedb.SetBalance(addr, new(big.Int).SetUint64(2e18))

	miner := common.HexToAddress("0x1e3d31aa1a7d72ac206bec98666a75968281655f")
	to := common.HexToAddress("0xd67487d6b9aec47bb15bcefd0f606d14c642af3e")
	to2 := common.HexToAddress("0x28426B47577f75ECb43a7C6076d55a513184CC54")

	txcount := 100
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
		txs[i] = transaction(uint64(i), to2, 200000, chainConfig.ChainID, key)
		fmt.Printf("tx hash: %s\n", txs[i].Hash().String())
	}

	txMaps[addr] = txs

	//txMaps, txcount := genTestTxsMap(chainConfig.ChainID)

	statedb.Commit(false)

	statedb2 := statedb.Copy()

	header := &types.Header{
		Miner:       miner,
		Number:      big.NewInt(int64(0)),
		ParentHash:  common.Hash{},
		Difficulty:  big.NewInt(1),
		TxRoot:      types.EmptyRootHash,
		ReceiptRoot: types.EmptyRootHash,
		//GasLimit:    GenesisGasLimit,
		GasLimit: 47123880000,
		Time:     big.NewInt(time.Now().Unix()),
	}

	start := time.Now()
	executedTxs, _, err := processor.ApplyTransactions(txMaps, txcount, header, statedb2)
	if err != nil {
		return
	}
	end := time.Now()
	fmt.Printf("#####ApplyTransactions[%d/%d] cost %v\n", executedTxs.Len(), txcount, end.Sub(start))

	//for i, tx := range executedTxs {
	//	fmt.Printf("#####tx[%d]: %s, cost: %d, GasUsed: %d, CumulativeGasUsed: %d\n", i, tx.Hash().String(), receipts[i].GasUsed, receipts[i].GasUsed, receipts[i].CumulativeGasUsed)
	//}

	obj := statedb2.GetOrNewStateObject(addr)
	fmt.Printf("%s:{nonce: %d, balance: %d}\n", obj.Address().String(), obj.Nonce(), obj.Balance())

	fmt.Printf("to: %d\nminer: %d\n", statedb2.GetBalance(to), statedb2.GetBalance(miner))

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

	initBalance := 2e18

	//change this setting
	sender := 3
	to := 2
	perCount := 5000

	senders, _, txMaps, count := genTxsMap_Transfer(sender, to, perCount, chainConfig.ChainID)
	for _, sender := range senders {
		statedb.SetBalance(sender, new(big.Int).SetUint64(uint64(initBalance)))
	}

	senders2, _, txMaps2, count2 := genTxsMap_Transfer(sender, to, perCount, chainConfig.ChainID)
	for _, sender := range senders2 {
		statedb.SetBalance(sender, new(big.Int).SetUint64(uint64(initBalance)))
	}
	statedb.Commit(false)

	statedb2 := statedb.Copy()

	miner := common.HexToAddress("0x1e3d31aa1a7d72ac206bec98666a75968281655f")

	processor := NewStateProcessor(chainConfig, nil)
	fastProcessor := NewStateParallelProcessor(chainConfig, nil)

	fmt.Println("--------------------fast processor--------------")
	executedTxs, receipts, gas, err := applyTxs(fastProcessor, txMaps, count, statedb2, miner)
	if err != nil {
		t.Errorf("fast process failed, err: %v\n", err)
	}
	fmt.Println("--------------------end fast processor--------------")

	fmt.Println("--------------------processor-------------------")
	executedTxs2, receipts2, gas2, err := applyTxs(processor, txMaps2, count2, statedb2, miner)
	if err != nil {
		t.Errorf("process failed, err: %v\n", err)
	}
	fmt.Println("--------------------end processor--------------")

	//verify
	gasUsed := uint64(0)
	fmt.Println("--------------------verify fast processor--------------")
	result, gasUsed1 := verifyResult(executedTxs, receipts, statedb2, chainConfig.ChainID, uint64(initBalance))
	if !result {
		t.Error("verify fast procssor failed")
	}
	fmt.Printf("gas used: %d\n", gasUsed1)
	if gas != gasUsed1 {
		t.Errorf("gas used error. header: %d, total gas: %d\n", gas, gasUsed1)
	}
	fmt.Println("--------------------end verify fast processor--------------")

	gasUsed += gasUsed1

	fmt.Println("--------------------verify processor--------------")
	result2, gasUsed2 := verifyResult(executedTxs2, receipts2, statedb2, chainConfig.ChainID, uint64(initBalance))
	if !result2 {
		t.Error("verify procssor failed")
	}
	fmt.Printf("gas used: %d\n", gasUsed2)
	if gas2 != gasUsed2 {
		t.Errorf("gas used error. header: %d, total gas: %d\n", gas2, gasUsed2)
	}
	fmt.Println("--------------------end verify processor--------------")
	gasUsed += gasUsed2

	minerBalance := statedb2.GetBalance(miner).Uint64()
	if gasUsed != minerBalance {
		t.Errorf("miner: %s, balacne: %d, require: %d\n", miner.String(), minerBalance, gasUsed)
	}

}

func genTxsMap_Transfer(sender, to, perCount int, ChainID *big.Int) ([]common.Address, []common.Address, map[common.Address]types.Transactions, int) {

	txMaps := map[common.Address]types.Transactions{}
	count := 0

	senders := make([]common.Address, sender)
	tos := make([]common.Address, to)

	for i := 0; i < to; i++ {
		key, _ := crypto.GenerateKey()
		tos[i] = crypto.PubkeyToAddress(key.PublicKey)
		fmt.Printf("to[%d]: %s\n", i, tos[i].String())
	}

	var retTos []common.Address
	for i := 0; i < sender; i++ {
		key, _ := crypto.GenerateKey()
		senders[i] = crypto.PubkeyToAddress(key.PublicKey)
		fmt.Printf("sender[%d]: %s\n", i, senders[i].String())

		receiver := common.Address{}
		if i < to {
			receiver = tos[i]

		} else {
			r := rand.Int()
			receiver = tos[r%to]
		}
		retTos = append(retTos, receiver)
		txs := make(types.Transactions, perCount)
		for j := 0; j < perCount; j++ {
			txs[j] = transaction(uint64(j), receiver, 200000, ChainID, key)
			//fmt.Printf("tx hash: %s\n", txs[j].Hash().String())
		}
		count += perCount
		txMaps[senders[i]] = txs
	}

	return senders, retTos, txMaps, count
}

func applyTxs(processor Processor, txMaps map[common.Address]types.Transactions, count int, statedb *state.StateDB, miner common.Address) (types.Transactions, types.Receipts, uint64, error) {

	header := &types.Header{
		Miner:       miner,
		Number:      big.NewInt(int64(0)),
		ParentHash:  common.Hash{},
		Difficulty:  big.NewInt(1),
		TxRoot:      types.EmptyRootHash,
		ReceiptRoot: types.EmptyRootHash,
		//GasLimit:    GenesisGasLimit,
		GasLimit: 47123880000,
		Time:     big.NewInt(time.Now().Unix()),
	}

	start := time.Now()
	executedTxs, receipts, err := processor.ApplyTransactions(txMaps, count, header, statedb)
	if err != nil {
		return nil, nil, 0, err
	}
	end := time.Now()
	fmt.Printf("#####ApplyTransactions[%d/%d] cost %v\n", executedTxs.Len(), count, end.Sub(start))

	//for i, tx := range executedTxs {
	//	fmt.Printf("#####tx[%d]: %s, cost: %d, GasUsed: %d, CumulativeGasUsed: %d\n", i, tx.Hash().String(), receipts[i].GasUsed, receipts[i].GasUsed, receipts[i].CumulativeGasUsed)
	//}

	receiptMap := make(map[common.Hash]types.Receipt)
	for i, tx := range executedTxs {
		receiptMap[tx.Hash()] = *receipts[i]
	}

	return executedTxs, receipts, header.GasUsed, err
}

func verifyResult(executedTxs types.Transactions, receipts types.Receipts, statedb *state.StateDB, ChainID *big.Int, initBalance uint64) (bool, uint64) {
	result := true

	senderUsedBalance := make(map[common.Address]uint64)
	toBalance := make(map[common.Address]uint64)
	gasUsed := uint64(0)
	for i, tx := range executedTxs {
		msg, _ := tx.AsMessage(types.NewInitialSigner(ChainID))
		from := msg.From()
		to := msg.To()

		receipt := receipts[i]

		if used, exist := senderUsedBalance[from]; exist {
			senderUsedBalance[from] = used + receipt.GasUsed + tx.Value().Uint64()
		} else {
			senderUsedBalance[from] = receipt.GasUsed + tx.Value().Uint64()
		}

		if balance, exist := toBalance[*to]; exist {
			toBalance[*to] = balance + tx.Value().Uint64()
		} else {
			toBalance[*to] = tx.Value().Uint64()
		}

		gasUsed += receipt.GasUsed
	}

	for sender, used := range senderUsedBalance {
		balance := statedb.GetBalance(sender).Uint64()
		require := initBalance - used
		if require != balance {
			logrus.Errorf("sender: %s, balacne: %d, require: %d\n", sender.String(), balance, require)
			result = false
		}
	}

	for to, require := range toBalance {
		balance := statedb.GetBalance(to).Uint64()
		if require != balance {
			logrus.Errorf("to: %s, balacne: %d, require: %d\n", to.String(), balance, require)
			result = false
		}
	}

	return result, gasUsed
}
