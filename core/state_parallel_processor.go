package core

import (
	"errors"
	"fmt"
	"sync"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/dag"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/params"

	log "github.com/sirupsen/logrus"
)

//思考：为了发挥并发执行交易的优势，同时减少statedb的复制，应该就可能把tx集合分成多条很长的队列集合，
//这样每个队列集合可以复用同一个statedb，减少statedb复制的次数
var (
	// ParallelNum num
	//problem:setup different concurrent coroutine will generate different results,
	//        so we must the num of  coroutine by the number of transactions executed concurrently for the first time
	// setup ParallelNum 0 means ParallelNum will be config by dispathcer
	ParallelNum = 0
	// VerifyExecutionTimeout 0 means unlimited
	VerifyExecutionTimeout = 0
	ErrInvalidDagBlock     = errors.New("block's dag is incorrect")
)

// StateProcessor is a basic Processor, which takes care of transitioning
// state from one point to another.
//
// StateProcessor implements Processor.
type StateParallelProcessor struct {
	config *params.ChainConfig // Chain configuration options
	bc     *BlockChain         // Canonical block chain
}

// NewStateProcessor initialises a new StateProcessor.
func NewStateParallelProcessor(config *params.ChainConfig, bc *BlockChain) *StateParallelProcessor {
	return &StateParallelProcessor{
		config: config,
		bc:     bc,
	}
}

type verifyCtx struct {
	mergeCh   chan bool
	block     *types.Block
	lastState *state.StateDB
}

// Process  processes the state changes parallel according to the rules by running
// the transaction messages using the statedb and applying any rewards to
// the processor (coinbase).
//
// Process returns the receipts and logs accumulated during the process and
// returns the amount of gas that was used in the process. If any of the
// transactions failed to execute due to insufficient gas it will return an error.
func (p *StateParallelProcessor) Process(block *types.Block, statedb *state.StateDB, cfg vm.Config) (types.Receipts, []*types.Log, uint64, error) {
	var (
		receiptsArray = make([]*types.Receipt, block.Txs.Len())
		usedGasArray  = make([]*(uint64), block.Txs.Len())
		receipts      types.Receipts
		usedGas       = new(uint64)
		header        = block.Header()
		allLogs       []*types.Log
		gp            = new(GasPool).AddGas(block.GasLimit())
	)

	//Parallel execution tx by dag
	context := &verifyCtx{
		mergeCh: make(chan bool, 1),
		block:   block,
	}
	count := 0
	//totalExecuteStateDb := statedb.Copy()
	// var (
	// 	key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	// 	key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	// 	key3, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7b")
	// 	key4, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7c")
	// 	addr1   = crypto.PubkeyToAddress(key1.PublicKey)
	// 	addr2   = crypto.PubkeyToAddress(key2.PublicKey)
	// 	addr3   = crypto.PubkeyToAddress(key3.PublicKey)
	// 	addr4   = crypto.PubkeyToAddress(key4.PublicKey)
	// )
	// fmt.Printf("!!!!!!!!Process currentBlock num %d,hash  : %#v,stateroot: %#v \n ", p.bc.CurrentBlock().Header().Number,
	// 	p.bc.CurrentBlock().Hash().String(), p.bc.CurrentBlock().StateRoot().String())
	// fmt.Printf("7777!!!!Process statedb.trie.Hash() %v\n", statedb.GetTrie().Hash().String())
	//statedb.Prepare(common.Hash{}, block.Hash(), 0)
	dispatcher := dag.NewDispatcher(&block.Header().ExecutionDag, ParallelNum, int64(VerifyExecutionTimeout), context, func(node *dag.Node, context interface{}) (interface{}, error) {
		// TODO: if system occurs, the block won't be retried any more
		ctx := context.(*verifyCtx)
		block := ctx.block
		//mergeCh := ctx.mergeCh
		lastStateDb := node.LastState()

		idx := node.Index()
		if idx < 0 || idx > block.Txs.Len()-1 {
			return nil, ErrInvalidDagBlock
		}
		tx := block.Txs[idx] //通过这样的方式来取到相应的tx来执行

		usedGasArray[idx] = new(uint64)

		//log.Info("execute tx " + string(idx) + ",hash:" + tx.Hash().String())

		//这一段copy消耗了太多时间，没有这段，3000条tx用时274ms，有了这段，需要3.6s还不算合并的
		//mergeCh <- true
		//execute tx
		// for addr, stateObject := range statedb.GetStateObjects() {
		// 	fmt.Printf("!!!!----4444遍历stateDb.stateObjects addr %#v,version %d \n", addr.String(), stateObject.GetVersion())
		// }
		var txStateDb *state.StateDB
		if lastStateDb != nil {
			count++
			//fmt.Printf("!!!引用上次的stateDB :%v \n", lastStateDb)
			txStateDb = lastStateDb.(*state.StateDB)
		} else {
			txStateDb = statedb.CopyWithStateObjects()
		}

		//txStateDb := statedb.Copy()
		//add version info
		txStateDb.SetSnapshotVersion(statedb.GetSnapshotVersion())
		// fmt.Printf("***********55555***************txStateDb version :%d,statedb verson:%d \n", txStateDb.GetSnapshotVersion(), statedb.GetSnapshotVersion())
		// for addr, stateObject := range txStateDb.GetStateObjects() {
		// 	fmt.Printf("!!!!----55555遍历txStateDb.stateObjects addr %#v,version %d \n", addr.String(), stateObject.GetVersion())
		// }
		txStateDb.Prepare(tx.Hash(), block.Hash(), idx)
		//statedb.Prepare(tx.Hash(), block.Hash(), idx)
		//<-mergeCh

		//fmt.Printf("^^^^^^^---Begin exectute tx:%d \n", idx)
		//statedb.Prepare(tx.Hash(), block.Hash(), idx)
		receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, txStateDb, header, tx, usedGasArray[idx], cfg)
		// for addr := range txStateDb.GetStateObjectsDirty() {
		// 	fmt.Printf("^^^^^^^---After ApplyTransaction tx:%d,txStateDb dirtyStateObject==>addr: %#v\n", idx, addr.String())
		// }
		// fmt.Printf("#######txStateDb add1 balance  : %#v \n ", txStateDb.GetBalance(addr1))
		// fmt.Printf("####### txStateDb add2 balance  : %#v \n ", txStateDb.GetBalance(addr2))
		// fmt.Printf("####### txStateDb add3 balance  : %#v \n ", txStateDb.GetBalance(addr3))
		// fmt.Printf("####### txStateDb add4 balance  : %#v \n ", txStateDb.GetBalance(addr4))
		//fmt.Printf("mmmmmm!!!!Process txstatedb.trie.Hash() %v\n", txStateDb.GetTrie().Hash().String())
		//receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, stateDb, header, tx, usedGas, cfg)
		if err != nil {
			fmt.Printf("ApplyTransaction err:%#v\n", err)
			return nil, err
		}
		receiptsArray[idx] = receipt
		//fmt.Printf("#####赋值 receiptsArray[%d] txHash : %v \n", idx, receiptsArray[idx].TxHash.String())

		//mergeCh <- true
		//merge statedb
		//fmt.Printf("!!!begin merge txstatedb.txidx: %d \n", idx)
		//fmt.Printf("***********6666***************txStateDb version :%d\n", txStateDb.GetSnapshotVersion())
		/*
			利用snapshot实现版本控制
			 第一次merge txdb的初始version为0，statedb的version为0，执行完tx后，txdb的version加1=1，遇到冲突时可以更新，更新完statedb的version为1
			 如果有并发merge时，txdb的初始version为0，statedb的version为1，执行完tx后，txdb的version加1=1，
			 因为txdb的version并没有大于statedb的version，所以遇到冲突时报错
		*/
		//merge 这段运行3000次，大约需要消耗200ms左右
		errMerge := statedb.MergeStateDB(txStateDb)
		if errMerge != nil {
			fmt.Printf("MergeTxStateDB failed!err:%v\n", errMerge)
			return nil, errMerge
		}
		// fmt.Printf("!!#######tx:%d,add1 balance  : %#v \n ", idx, statedb.GetBalance(addr1))
		// fmt.Printf("!!#######tx:%d,add2 balance  : %#v \n ", idx, statedb.GetBalance(addr2))
		// fmt.Printf("!!#######tx:%d,add3 balance  : %#v \n ", idx, statedb.GetBalance(addr3))
		// fmt.Printf("!!#######tx:%d,add4 balance  : %#v \n ", idx, statedb.GetBalance(addr4))
		//fmt.Printf("-------------------------------!!!Merge txstatedb success.txidx: %d \n", idx)
		//fmt.Printf("QQQQQQQ!!!!Process statedb.trie.Hash() %v\n", statedb.GetTrie().Hash().String())
		//<-mergeCh
		return txStateDb, nil
	})

	if err := dispatcher.Run(); err != nil {
		log.Info("Failed to verify txs in block.\n" +
			"ExecutionDag:" + block.Header().ExecutionDag.String() + "\n" +
			"err:" + err.Error() + "\n")
		return nil, nil, 0, err
	}
	fmt.Printf("@@@@@@@@@@@@@@@!!!总共引用上次的stateDB的次数为 %d\n", count)
	// fmt.Printf("555555!!!!Process writeBlock num %d,hash  : %#v ,stateroot: %#v\n ", block.Header().Number, block.Hash().String(),
	// 	block.StateRoot().String())
	// fmt.Printf("888888!!!!Process statedb.trie.Hash() %v\n", statedb.GetTrie().Hash().String())
	// errMerge := statedb.MergeStateDB(statedb)
	// if errMerge != nil {
	// 	fmt.Printf("Merge totalExcuteStateDB failed!err:%v\n", errMerge)
	// 	return nil, nil, 0, errMerge
	// }
	// fmt.Printf("@@#######add1 balance  : %#v \n ", statedb.GetBalance(addr1))
	// fmt.Printf("@@#######add2 balance  : %#v \n ", statedb.GetBalance(addr2))
	// fmt.Printf("@@#######add3 balance  : %#v \n ", statedb.GetBalance(addr3))
	// fmt.Printf("@@#######add4 balance  : %#v \n ", statedb.GetBalance(addr4))

	// for addr := range statedb.GetJournal().GetDirtys() {
	// 	fmt.Printf("@@#######before finalize : %#v \n ", addr.String())
	// }

	// Finalize the block, applying any consensus engine specific extras (e.g. block rewards)
	p.bc.engine.Finalize(p.bc, statedb, block)

	// fmt.Printf("6666!!!!Process writeBlock num %d,hash  : %#v ,stateroot: %#v\n ", block.Header().Number, block.Hash().String(),
	// 	block.StateRoot().String())

	// fmt.Printf("------len(receiptsArray) :%d\n", len(receiptsArray))
	// for i, receipts := range receiptsArray {
	// 	fmt.Printf("!!!receiptsArray[%d] txHash : %#v \n", i, receipts)
	// }
	//merge receipts,allLogs
	for i := 0; i < block.Txs.Len(); i++ {
		//fmt.Printf("遍历receiptsArray[%d] txHash : %#v \n", i, receiptsArray[i].TxHash.String())
		receipts = append(receipts, receiptsArray[i])
		allLogs = append(allLogs, receiptsArray[i].Logs...)
		*usedGas += *usedGasArray[i]
	}

	//设置usedGas费用
	//fmt.Println("-----------total GasUsed %d", *usedGas)
	block.Header().GasUsed = *usedGas

	//检验receipts
	// receiptSha := types.DeriveSha(receipts)
	// if receiptSha != header.ReceiptRoot {
	// 	fmt.Printf("******StateParallelProcessor ：invalid receipt root hash (remote: %x local: %x)", header.ReceiptRoot, receiptSha)
	// }

	return receipts, allLogs, *usedGas, nil
}

func (p *StateParallelProcessor) ApplyTransactions(txMaps map[common.Address]types.Transactions, count int, header *types.Header, statedb *state.StateDB) (types.Transactions, types.Receipts, error) {

	var executedTx types.Transactions
	var receipts types.Receipts

	parallel := ParallelNum
	if ParallelNum > len(txMaps) {
		parallel = len(txMaps)
	}

	parallelCh := make(chan bool, parallel)
	gasPool := new(GasPool).AddGas(header.GasLimit)
	lock := &sync.RWMutex{}
	var pend sync.WaitGroup

	for _, txs := range txMaps {
		parallelCh <- true

		lock.Lock()
		gp := new(GasPool).AddGas(gasPool.Gas())
		lock.Unlock()

		pend.Add(1)
		go func(accTxs types.Transactions) {
			defer func() {
				<-parallelCh
			}()

			defer pend.Done()

			txState := statedb.Copy()
			usedGas := new(uint64)

			// Iterate over and process the individual transactions
			for i, tx := range accTxs {

				log.WithFields(log.Fields{"gasPool": gasPool, "tx": tx.Hash().String()}).Info("new tx prepare to execute")
				// If we don't have enough gas for any further transactions then we're done
				if gasPool.Gas() < params.TxGas {
					log.WithFields(log.Fields{"have": gasPool, "want": params.TxGas}).Debug("Not enough gas for further transactions")
					break
				}

				// Start executing the transaction
				txState.Prepare(tx.Hash(), common.Hash{}, i)
				snap := txState.Snapshot()
				receipt, gas, err := ApplyTransaction(p.config, p.bc, &header.Miner, gp, txState, header, tx, usedGas, vm.Config{})
				if err != nil {
					log.WithFields(log.Fields{"tx": tx.Hash().String(), "err": err}).Info("execute failed")
					txState.RevertToSnapshot(snap)
					break
				}
				log.WithFields(log.Fields{"tx": tx.Hash().String()}).Info("execute success")

				//TODO: merge to stateDb, bypass collect conflicts and dependencies

				lock.Lock()
				//TODO: if conflict, the remain txs save to pending txmap

				//if don't conflict, execute successful, then append to returns
				executedTx = append(executedTx, tx)
				receipts = append(receipts, receipt)
				header.GasUsed += gas
				gasPool.SubGas(gas)

				//TODO：add dag

				lock.Unlock()

			}

		}(txs)
	}

	pend.Wait()

	close(parallelCh)

	//TODO：serially execute txs in pending txmap

	log.WithFields(log.Fields{"executed_tx_count": executedTx.Len()}).Info("ApplyTransactions over")
	return executedTx, receipts, nil
}
