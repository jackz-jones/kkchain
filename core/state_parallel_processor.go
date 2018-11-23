package core

import (
	"errors"
	"fmt"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/core/dag"
	"github.com/invin/kkchain/core/state"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/params"

	"sync"
	"time"

	log "github.com/sirupsen/logrus"
)

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
	mergeCh chan bool
	block   *types.Block
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
	dispatcher := dag.NewDispatcher(&block.Header().ExecutionDag, ParallelNum, int64(VerifyExecutionTimeout), context, func(node *dag.Node, context interface{}) error {
		// TODO: if system occurs, the block won't be retried any more
		ctx := context.(*verifyCtx)
		block := ctx.block
		mergeCh := ctx.mergeCh

		idx := node.Index()
		if idx < 0 || idx > block.Txs.Len()-1 {
			return ErrInvalidDagBlock
		}
		tx := block.Txs[idx] //通过这样的方式来取到相应的tx来执行

		log.Debug("execute tx." + tx.Hash().String())

		//这一段copy消耗了太多时间，没有这段，3000条tx用时274ms，有了这段，需要3.6s还不算合并的
		mergeCh <- true
		//execute tx
		// for addr, stateObject := range statedb.GetStateObjects() {
		// 	fmt.Printf("!!!!----4444遍历stateDb.stateObjects addr %#v,version %d \n", addr.String(), stateObject.GetVersion())
		// }
		txStateDb := statedb.CopyWithStateObjects()
		//add version info
		txStateDb.SetSnapshotVersion(statedb.GetSnapshotVersion())
		// fmt.Printf("***********55555***************txStateDb version :%d,statedb verson:%d \n", txStateDb.GetSnapshotVersion(), statedb.GetSnapshotVersion())
		// for addr, stateObject := range txStateDb.GetStateObjects() {
		// 	fmt.Printf("!!!!----55555遍历txStateDb.stateObjects addr %#v,version %d \n", addr.String(), stateObject.GetVersion())
		// }
		txStateDb.Prepare(tx.Hash(), block.Hash(), idx)
		//statedb.Prepare(tx.Hash(), block.Hash(), idx)
		<-mergeCh

		//fmt.Printf("^^^^^^^---Begin exectute tx:%d \n", idx)
		//statedb.Prepare(tx.Hash(), block.Hash(), idx)
		receipt, _, err := ApplyTransaction(p.config, p.bc, nil, gp, txStateDb, header, tx, usedGas, cfg)
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
			return err
		}
		receiptsArray[idx] = receipt
		//fmt.Printf("#####receiptsArray[%d] txHash : %v \n", idx, receiptsArray[idx].TxHash.String())

		mergeCh <- true
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
			return errMerge
		}
		//fmt.Printf("-------------------------------!!!Merge txstatedb success.txidx: %d \n", idx)
		//fmt.Printf("QQQQQQQ!!!!Process statedb.trie.Hash() %v\n", statedb.GetTrie().Hash().String())
		<-mergeCh
		return nil
	})

	if err := dispatcher.Run(); err != nil {
		log.Info("Failed to verify txs in block.\n" +
			"ExecutionDag:" + block.Header().ExecutionDag.String() + "\n" +
			"err:" + err.Error() + "\n")
		return nil, nil, 0, err
	}
	// fmt.Printf("!!#######add1 balance  : %#v \n ", totalExecuteStateDb.GetBalance(addr1))
	// fmt.Printf("!!#######add2 balance  : %#v \n ", totalExecuteStateDb.GetBalance(addr2))
	// fmt.Printf("!!#######add3 balance  : %#v \n ", totalExecuteStateDb.GetBalance(addr3))
	// fmt.Printf("!!#######add4 balance  : %#v \n ", totalExecuteStateDb.GetBalance(addr4))
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

	//merge receipts,allLogs
	for i := 0; i < block.Txs.Len(); i++ {
		//fmt.Printf("!!!receiptsArray[%d] txHash : %v \n", i, receiptsArray[i].TxHash.String())
		receipts = append(receipts, receiptsArray[i])
		allLogs = append(allLogs, receiptsArray[i].Logs...)
	}

	//检验receipts
	// receiptSha := types.DeriveSha(receipts)
	// if receiptSha != header.ReceiptRoot {
	// 	fmt.Printf("******StateParallelProcessor ：invalid receipt root hash (remote: %x local: %x)", header.ReceiptRoot, receiptSha)
	// }

	return receipts, allLogs, *usedGas, nil
}

func (p *StateParallelProcessor) ApplyTransactions(txs types.Transactions, header *types.Header, statedb *state.StateDB) (types.Transactions, types.Receipts, error) {

	var executedTx types.Transactions
	var receipts types.Receipts

	gasPool := new(GasPool).AddGas(header.GasLimit)

	resultCh := make(chan *ApplyResult, len(txs))

	//TODO：limit parallel number if needed
	var pend sync.WaitGroup
	for _, tx := range txs {
		pend.Add(1)

		go func() {
			defer pend.Done()
			p.applyTx(gasPool, statedb, header, tx, resultCh)
		}()
	}
	pend.Wait()
	close(resultCh)

	for result := range resultCh {
		if result.tx != nil {
			executedTx = append(executedTx, result.tx)
			receipts = append(receipts, result.receipt)
			header.GasUsed += result.gas
		}
	}

	return executedTx, receipts, nil
}

type ApplyResult struct {
	tx      *types.Transaction
	receipt *types.Receipt
	gas     uint64
	err     error
}

func (p *StateParallelProcessor) applyTx(gp *GasPool, stateDb *state.StateDB, header *types.Header, tx *types.Transaction, resultCh chan *ApplyResult) {

	result := make(chan *ApplyResult)
	go func() {
		txState := stateDb.Copy()
		usedGas := new(uint64)

		// If we don't have enough gas for any further transactions then we're done
		if gp.Gas() < params.TxGas {
			log.WithFields(log.Fields{"have": gp, "want": params.TxGas}).Debug("Not enough gas for further transactions")
			result <- &ApplyResult{
				gas: 0,
				err: ErrAvailGasTooLow,
			}
			return
		}

		// Start executing the transaction
		txState.Prepare(tx.Hash(), common.Hash{}, 0)

		snap := txState.Snapshot()
		receipt, gas, err := ApplyTransaction(p.config, p.bc, &header.Miner, gp, txState, header, tx, usedGas, vm.Config{})
		if err != nil {
			txState.RevertToSnapshot(snap)
			result <- &ApplyResult{
				gas: 0,
				err: err,
			}
			return
		}

		result <- &ApplyResult{
			tx:      tx,
			receipt: receipt,
			gas:     gas,
		}
	}()

	select {
	case <-time.After(10 * time.Second):
		log.Error(ErrExecuteTimeout)
		resultCh <- &ApplyResult{}
		return
	case res := <-result:
		if res.err != nil {
			log.Error(res.err)
			resultCh <- &ApplyResult{}
			return
		}

		//TODO: merge to stateDb, bypass collect conflicts and dependencies

		//if don't conflict, set to channel
		resultCh <- res
	}

}
