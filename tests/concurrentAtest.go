package main

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/consensus/pow"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/crypto"
	"github.com/invin/kkchain/params"
	"github.com/invin/kkchain/storage/memdb"
)

/*
  测试过程1（黑盒）：
   1.启动kkchain
   2.执行TestTxsSequenceExecute方法或者TestTxszConcurrentExecute方法
   3.方法体内会向rpc服务发起n次的交易请求，最终将会输出执行完需要的总耗时
   测试过程2（白盒）：
   1.启动kkchain
   2.执行TestTxsSequenceExecute方法或者TestTxszConcurrentExecute方法
   3.方法体内会向rpc服务发起n次的交易请求，最终将会输出执行完需要的总耗时

   我准备写个测试用例测试下在项目里顺序执行和并发执行的耗时的区别，看是否并发真的会比顺序执行更快。
   先不考虑tx之间依赖的情况。大家帮忙看下下面的分析逻辑是否有什么问题。
   ****************************************************************
   原理：
   现在项目里tx执行的开始是从commitTask方法开始的，都是在worker.go里的mineLoop方法触发的。
   以下三种情况会触发：
	1.刚启动程序的时候，
	   case <-w.startCh:
		w.commitTask()
	2.本地挖矿成功后,会调用WriteBlockWithState方法写入区块（这个过程会再次执行tx），并发送chainHead事件，从而触发
	    case <-w.chainHeadCh:
			if w.isRunning() {
				w.commitTask()
			}
	3.获取其他节点的最新区块放入fetch的队列，遍历队列里的区块，如果有符合插入条件的，将会调用blockchain.InsertChain方法插入区块，
	  插入完后，会发送chainHead事件，从而触发
	    case <-w.chainHeadCh:
			if w.isRunning() {
				w.commitTask()
			}

	因此tx的执行过程如下（以调用committask作为起点，以挖矿成功写入区块作为终点）：
	挖矿的情况：
	 挖矿成功=>WriteBlockWithState方法（会再次执行tx）写入区块=>调用发送chainHead事件
	起点：1.执行commitTask方法
		 2.取出txpoll中pending队列的tx
		 3.预执行这些tx，看是否够gas    (第一次执行tx)
		 4.全部tx执行成功并且够gas费用打包成区块
		 5.调用挖矿程序开始挖矿
	终点： 挖矿成功=>WriteBlockWithState方法（会再次执行tx，第二次执行tx）写入区块

	通过fetcher插入区块的情况：（以调用committask作为起点，以挖矿成功写入区块作为终点）
	fetcher的loop方法找到有符合插入条件的区块=>WriteBlockWithState方法（会再次执行tx）写入区块=>调用发送chainHead事件
	起点：1.执行commitTask方法
		 2.取出txpoll中pending队列的tx
		 3.预执行这些tx，看是否够gas    (第一次执行tx)
		 4.全部tx执行成功并且够gas费用打包成区块
		 5.调用挖矿程序开始挖矿
	终点： 挖矿成功=>WriteBlockWithState方法（会再次执行tx，第二次执行tx）写入区块

	因此统计的实际执行tx的总耗时需要由两次执行tx的时间加起来。
	第一次统计执行tx的代码实际为：worker.go里的commitTask方法里的以下代码：
		if len(txs) > 0 {
		//apply txs and get block
		if w.commitTransactions(txs, w.miner) == false {
			return
		}
	}
	第二次统计执行tx的代码实际为：blockchain.go里的insertChain方法里的以下代码：
	// Process block using the parent state as reference point.
	receipts, logs, usedGas, err := bc.processor.Process(block, state, bc.vmConfig)

   这两次执行tx，最终都会调用core.ApplyTransaction(）方法来执行单条tx。因此下面的测试主要将会针对上述两个方法进行测试。
   测试case1: 执行相同的tx集合，顺序执行tx的总耗时和并发执行tx的总耗时的区别。
   测试case2: 执行相同的tx集合，测试commitTask方法和InsertChain方法这两个方法在顺序执行tx的总耗时和并发执行tx的总耗时的区别。

   *********************************************************************************
	测试数据构造
	 1.3条tx，1条tx为普通转账，1条tx为创建合约，1条为tx为合约调用 （3条tx的调用者都不相同）
	 2.3条tx，全部为普通转账的tx（3条tx的调用者地址都不相同）
	 3.3条tx，全部为创建合约的tx （3条tx的调用者地址都不相同）
	 4.3条tx，全部为合约调用的tx （3条tx的调用者地址都不相同）

*/

//测试的合约和输入参数数据
/*contract source code  complie version：0.4.25
pragma solidity ^0.4.0;
contract Test {
    mapping(uint256 => uint256) public data;
    event Fun(uint256 c);
    function fun(uint256 a,uint256 b) returns(uint256 c){
        emit Fun(a+b);
        data[a+b]=a+b;
        return a+b;
    }
}
*/
var (
	code            = "608060405234801561001057600080fd5b50610119806100206000396000f30060806040526004361060485763ffffffff7c0100000000000000000000000000000000000000000000000000000000600035041663e9a58c408114604d578063f0ba8440146077575b600080fd5b348015605857600080fd5b506065600435602435608c565b60408051918252519081900360200190f35b348015608257600080fd5b50606560043560db565b60408051838301815290516000917f23f54f87f4b5ab78f25d15d4f83e12dc9c218476be07d632a5715acb97dcc736919081900360200190a15001600081815260208190526040902081905590565b600060208190529081526040902054815600a165627a7a723058209b5c72cf00fb85d92c11776d1e24bd7bf9657fd9d6d1300f356e1ac1afbcda280029"
	codeByteData    = common.Hex2Bytes(code)
	invokeInputCode = "e9a58c4000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002"
	inputByteData   = common.Hex2Bytes(invokeInputCode)
	invokeFunc      = "e9a58c4"

	key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	key3, _ = crypto.HexToECDSA("9a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7b")
	key4, _ = crypto.HexToECDSA("6a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7c")
	addr1   = crypto.PubkeyToAddress(key1.PublicKey)
	addr2   = crypto.PubkeyToAddress(key2.PublicKey)
	addr3   = crypto.PubkeyToAddress(key3.PublicKey)
	addr4   = crypto.PubkeyToAddress(key4.PublicKey)
	db      = memdb.New()
	//db, _ = rocksdb.New("./tmp", nil)

	initcreateContractGas, _ = core.IntrinsicGas(codeByteData, true)            //53000
	storageContranctDataGas  = uint64(len(codeByteData)) * params.CreateDataGas //187*200=37400

	// Ensure that key1 has some funds in the genesis block.
	gspec = &core.Genesis{
		Config: &params.ChainConfig{ChainID: new(big.Int).SetInt64(1)},
		Alloc: core.GenesisAlloc{addr1: {Balance: big.NewInt(10000000000)},
			addr2: {Balance: big.NewInt(10000000000)},
			addr3: {Balance: big.NewInt(10000000000)},
			addr4: {Balance: big.NewInt(10000000000)}},
	}
	genesis = gspec.MustCommit(db)

	vmConfig      = vm.Config{}
	blockchain, _ = core.NewBlockChain(gspec.Config, vmConfig, db, pow.NewFaker())
	processor     = core.NewStateProcessor(gspec.Config, blockchain)

	signer = types.NewInitialSigner(new(big.Int).SetInt64(1))

	number = 1000
)

//1.测试顺序执行交易需要的总耗时，分别执行4种情况的数据耗时
func TestTxsSequenceExecute(t *testing.T) {
	//1.测试4种情况数据耗时：
	// 情况1:3*number条tx，number条tx为普通转账，number条tx为创建合约，number条为tx为合约调用
	// 情况2:3*number条tx，3*number条tx为普通转账
	// 情况3:3*number条tx，3*number条tx为创建合约
	// 情况4:3*number条tx，3*number条tx为合约调用
	//产生的是1,2,3,4号区块对应上述4种情况
	//var contractAddr common.Address
	chain, _ := core.GenerateChain(gspec.Config, genesis, pow.NewFaker(), db, 1, func(i int, gen *core.BlockGen) {
		//var createContractTx *types.Transaction
		switch i {
		case 0:
			// number条tx : addr1 sends addr2 some ether.
			//t1 := time.Now()
			tx1, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(50), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			gen.AddTx(tx1)
			// number条tx : addr1 sends addr2 some ether.
			//t1 := time.Now()
			tx2, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr3), addr4, big.NewInt(50), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key3)
			gen.AddTx(tx2)

		}
	})

	fmt.Println("chain length:", len(chain))
	fmt.Println("********************test processor.Process function Time consuming *******")
	// state, err := state.New(chain[0].StateRoot(), state.NewDatabase(db))
	// if err != nil {
	// 	fmt.Println("Error:New state error.", err)
	// }
	t1 := time.Now()
	if i, err := blockchain.InsertChain(types.Blocks{chain[0]}); err != nil {
		fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
		return
	}
	t2 := time.Now()
	fmt.Println("InsertChain block[0] spend time.", t2.Sub(t1))

	// t1 = time.Now()
	// if i, err := blockchain.InsertChain(types.Blocks{chain[1]}); err != nil {
	// 	fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
	// 	return
	// }
	// t2 = time.Now()
	// fmt.Println("InsertChain block[1]  spend time.", t2.Sub(t1))

	// t1 = time.Now()
	// if i, err := blockchain.InsertChain(types.Blocks{chain[2]}); err != nil {
	// 	fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
	// 	return
	// }
	// t2 = time.Now()
	// fmt.Println("InsertChain block[2]  spend time.", t2.Sub(t1))

	// t1 = time.Now()
	// if i, err := blockchain.InsertChain(types.Blocks{chain[3]}); err != nil {
	// 	fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
	// 	return
	// }
	// t2 = time.Now()
	// fmt.Println("InsertChain block[3]  spend time.", t2.Sub(t1))
	// //测试情况1耗时
	// t1 := time.Now()
	// receipts, logs, usedGas, err := processor.Process(chain[0], state, vmConfig)
	// t2 := time.Now()
	// fmt.Println("test case1(", number, "tx:transfer,", number, "tx:create contract,", number, "tx:invoke contract) spend time:", t2.Sub(t1))
	// //fmt.Printf("receipts:%#v \n ", receipts)
	// fmt.Printf("logs:%#v \n ", logs)
	// fmt.Printf("usedGas:%#v \n ", usedGas)
	// fmt.Printf("err:%#v \n ", err)

	// //测试情况2耗时
	// t1 = time.Now()
	// receipts, logs, usedGas, err = processor.Process(chain[1], state, vmConfig)
	// t2 = time.Now()
	// fmt.Println("test case2(", 3*number, "tx:transfer) spend time:", t2.Sub(t1))
	// //fmt.Printf("receipts:%#v \n ", receipts)
	// fmt.Printf("logs:%#v \n ", logs)
	// fmt.Printf("usedGas:%#v \n ", usedGas)
	// fmt.Printf("err:%#v \n ", err)

	// //测试情况3耗时
	// t1 = time.Now()
	// receipts, logs, usedGas, err = processor.Process(chain[2], state, vmConfig)
	// t2 = time.Now()
	// fmt.Println("test case3(", 3*number, "tx:create contract) spend time:", t2.Sub(t1))
	// //fmt.Printf("receipts:%#v \n ", receipts)
	// fmt.Printf("logs:%#v \n ", logs)
	// fmt.Printf("usedGas:%#v \n ", usedGas)
	// fmt.Printf("err:%#v \n ", err)

	// //测试情况4耗时
	// t1 = time.Now()
	// receipts, logs, usedGas, err = processor.Process(chain[3], state, vmConfig)
	// t2 = time.Now()
	// fmt.Println("test case4(", 3*number, "tx:invoke contract) spend time:", t2.Sub(t1))
	// //fmt.Printf("receipts:%#v \n ", receipts)
	// fmt.Printf("logs:%#v \n ", logs)
	// fmt.Printf("usedGas:%#v \n ", usedGas)
	// fmt.Printf("err:%#v \n ", err)

	// receipts = receipts

}

//2.测试并发执行交易需要的总耗时，分别执行4种情况的数据耗时
func TestTxszConcurrentExecute(t *testing.T) {
}

func main() {
	// wg := sync.WaitGroup{}
	// wg.Add(3)
	// exec := func() {
	// 	wg.Done()
	// }
	// t1 := time.Now()
	// go exec()
	// go exec()
	// go exec()
	// wg.Wait()
	// t2 := time.Now()
	// fmt.Println("WaitGroup  spend time.", t2.Sub(t1)) // 2个协程:15~24us;3个协程：20～45us

	//swap test
	// a, b := 1, 2
	// a, b = b, a
	// fmt.Println("a:", a, ";b:", b)
	var t *testing.T
	TestTxsSequenceExecute(t)
}
