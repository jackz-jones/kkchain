package main

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/consensus/pow"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/core/dag"
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
	测试数据构造：4种情况
	 1.3000条tx，1000条tx为普通转账，1000条tx为创建合约，1000条为tx为合约调用
	 2.3000条tx，全部为普通转账的tx
	 3.3000条tx，全部为创建合约的tx
	 4.3000条tx，全部为合约调用的tx
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
	code         = "608060405234801561001057600080fd5b506101f4806100206000396000f30060806040526004361061004c576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff168063e9a58c4014610051578063f0ba84401461009c575b600080fd5b34801561005d57600080fd5b5061008660048036038101908080359060200190929190803590602001909291905050506100dd565b6040518082815260200191505060405180910390f35b3480156100a857600080fd5b506100c7600480360381019080803590602001909291905050506101b0565b6040518082815260200191505060405180910390f35b600080600090505b60648112156101015760018301925080806001019150506100e5565b600a8385011015610128578284016000808587018152602001908152602001600020819055505b600b838501141561013c57600b6001819055505b600c838501141561015057600c6002819055505b600d838501141561016457600d6003819055505b836001819055507f23f54f87f4b5ab78f25d15d4f83e12dc9c218476be07d632a5715acb97dcc7368385016040518082815260200191505060405180910390a182840191505092915050565b600060205280600052604060002060009150905054815600a165627a7a723058205bce95d0f653b5bcb1e137f3f358d0be86b6cc2f0715519a2da996dd6eb0e9ac0029"
	codeByteData = common.Hex2Bytes(code)
	/*
			合约代码：
				pragma solidity ^0.4.0;
		contract Test {
		    mapping(uint256 => uint256) public data;
		    uint256 a1;
		    uint256 a2;
		    uint256 a3;
		    event Fun(uint256 c);
		    function fun(uint256 a,uint256 b) returns(uint256 c){
		          for(int256 i=0;i<100;i++){
		              a=a+1; //另一个合约的代码写成是b=b+1
		          }

		         if(a+b<10){
		              data[a+b]=a+b;
		         }
		         if(a+b==11){
		             a1=11;
		         }
		          if(a+b==12){
		             a2=12;
		         }
		           if(a+b==13){
		             a3=13;
		         }
		         a1=a;
		         emit Fun(a+b);
		         return a+b;

		    }
		}

	*/

	codeNew         = "608060405234801561001057600080fd5b506101f4806100206000396000f30060806040526004361061004c576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff168063e9a58c4014610051578063f0ba84401461009c575b600080fd5b34801561005d57600080fd5b5061008660048036038101908080359060200190929190803590602001909291905050506100dd565b6040518082815260200191505060405180910390f35b3480156100a857600080fd5b506100c7600480360381019080803590602001909291905050506101b0565b6040518082815260200191505060405180910390f35b600080600090505b60648112156101015760018401935080806001019150506100e5565b600a8385011015610128578284016000808587018152602001908152602001600020819055505b600b838501141561013c57600b6001819055505b600c838501141561015057600c6002819055505b600d838501141561016457600d6003819055505b836001819055507f23f54f87f4b5ab78f25d15d4f83e12dc9c218476be07d632a5715acb97dcc7368385016040518082815260200191505060405180910390a182840191505092915050565b600060205280600052604060002060009150905054815600a165627a7a72305820a3aa64c38b2791fd773a9d55c66c244bbd0096b76331b83a03fcd34b911c9ce10029"
	codeByteDataNew = common.Hex2Bytes(codeNew)
	//result=11，操作的是类型为unit256的全局变量a1
	invokeInputCode1 = "e9a58c4000000000000000000000000000000000000000000000000000000000000000050000000000000000000000000000000000000000000000000000000000000006"
	inputByteData1   = common.Hex2Bytes(invokeInputCode1)
	//result=12，操作的是类型为unit256的全局变量a2
	invokeInputCode2 = "e9a58c4000000000000000000000000000000000000000000000000000000000000000060000000000000000000000000000000000000000000000000000000000000006"
	inputByteData2   = common.Hex2Bytes(invokeInputCode2)
	//result=13，操作的是类型为unit256的全局变量a3
	invokeInputCode3 = "e9a58c4000000000000000000000000000000000000000000000000000000000000000070000000000000000000000000000000000000000000000000000000000000006"
	inputByteData3   = common.Hex2Bytes(invokeInputCode3)

	//相加结果小于10时，操作的是合约里同一个类型为map的全局变量data
	invokeInputCode4 = "e9a58c4000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000001"
	inputByteData4   = common.Hex2Bytes(invokeInputCode3)
	invokeInputCode5 = "e9a58c4000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000002"
	inputByteData5   = common.Hex2Bytes(invokeInputCode3)
	invokeInputCode6 = "e9a58c4000000000000000000000000000000000000000000000000000000000000000010000000000000000000000000000000000000000000000000000000000000003"
	inputByteData6   = common.Hex2Bytes(invokeInputCode3)
	invokeFunc       = "e9a58c4"

	key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
	key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
	key3, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7b")
	key4, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7c")
	addr1   = crypto.PubkeyToAddress(key1.PublicKey)
	addr2   = crypto.PubkeyToAddress(key2.PublicKey)
	addr3   = crypto.PubkeyToAddress(key3.PublicKey)
	addr4   = crypto.PubkeyToAddress(key4.PublicKey)
	db      = memdb.New()
	//db, _ = rocksdb.New("./tmp", nil)

	initcreateContractGas, _   = core.IntrinsicGas(codeByteData, true)                     //53000
	storageContranctDataGas    = uint64(len(codeByteData)) * params.CreateDataGas          //187*200=37400
	storageContranctDataGasNew = uint64(len(codeByteDataNew))*params.CreateDataGas + 10000 //需要91600

	// Ensure that key1 has some funds in the genesis block.
	gspec = &core.Genesis{
		Config: &params.ChainConfig{ChainID: new(big.Int).SetInt64(1)},
		Alloc: core.GenesisAlloc{addr1: {Balance: big.NewInt(100000000000)},
			addr2: {Balance: big.NewInt(100000000000)},
			addr3: {Balance: big.NewInt(100000000000)},
			addr4: {Balance: big.NewInt(100000000000)}},
	}
	genesis = gspec.MustCommit(db)

	vmConfig      = vm.Config{}
	blockchain, _ = core.NewBlockChain(gspec.Config, vmConfig, db, pow.NewFaker())
	processor     = core.NewStateProcessor(gspec.Config, blockchain)

	signer = types.NewInitialSigner(new(big.Int).SetInt64(1))

	number = 2000
	//3000*3=9000千条交易，并发执行采用copy的方式，需要占用5GB左右内存。必须缩减这个占用的内存才行。
	//考虑以什么方式可以减少copy的次数
)

//测试结果：
/*
 当tx数=400时，也就是一共800。顺序执行和开两个协程执行耗时接近，都是140ms左右，超过500时，并发执行比顺序执行耗时更小。
 当tx数=1000时，也就是一共2000。顺序执行比并发执行平均多耗时50-60ms左右
 当tx数=2000时，也就是一共2000。顺序执行比并发执行平均多耗时150-170ms左右
 当tx数=3000时，也就是一共6000。顺序执行比并发执行平均多耗时320-340ms左右
*/

//1.测试顺序执行交易需要的总耗时，分别执行4种情况的数据耗时
func TestTxsSequenceExecutePerformance(t *testing.T) {
	//1.测试4种情况数据耗时：
	// 情况1:3*number条tx，number条tx为普通转账，number条tx为创建合约，number条为tx为合约调用
	// 情况2:3*number条tx，3*number条tx为普通转账
	// 情况3:3*number条tx，3*number条tx为创建合约
	// 情况4:3*number条tx，3*number条tx为合约调用
	//产生的是1,2,3,4号区块对应上述4种情况
	//var contractAddr common.Address
	chain, _ := core.GenerateChain(gspec.Config, genesis, pow.NewFaker(), db, 2, func(i int, gen *core.BlockGen) {
		//var createContractTx *types.Transaction
		switch i {
		case 0:
			// number条tx : addr1 sends addr2 some ether.
			t1 := time.Now()
			// for k := 0; k < number; k++ {
			// 	tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(50), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			// 	gen.AddTx(tx)
			// }
			// number条tx : addr1 create contract
			// for k := 0; k < number; k++ {
			// 	//注意：如果设置amount大于0，evm执行则会返回reverted错误
			// 	createContractTx, _ = types.SignTx(types.NewContractCreation(gen.TxNonce(addr3), big.NewInt(0), initcreateContractGas+storageContranctDataGas, new(big.Int).SetInt64(1), codeByteData), signer, key3)
			// 	gen.AddTx(createContractTx)
			// }
			// number+1条tx : 1条：add4创建一个新合约  numnber条：addr4 调用新合约的方法
			//先创建一个另外的新的合约，再开始调用
			createContractTxNew, _ := types.SignTx(types.NewContractCreation(gen.TxNonce(addr4), big.NewInt(0), initcreateContractGas+storageContranctDataGasNew, new(big.Int).SetInt64(1), codeByteDataNew), signer, key4)
			gen.AddTx(createContractTxNew)
			msg, _ := createContractTxNew.AsMessage(types.NewInitialSigner(gspec.Config.ChainID))
			contractAddrNew := crypto.CreateAddress(msg.From(), createContractTxNew.Nonce())
			for k := 0; k < number; k++ {
				invokeContractFuncTx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr4), contractAddrNew, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData1), signer, key4)
				gen.AddTx(invokeContractFuncTx)
			}

			createContractTxNew, _ = types.SignTx(types.NewContractCreation(gen.TxNonce(addr3), big.NewInt(0), initcreateContractGas+storageContranctDataGasNew, new(big.Int).SetInt64(1), codeByteData), signer, key3)
			gen.AddTx(createContractTxNew)
			msg, _ = createContractTxNew.AsMessage(types.NewInitialSigner(gspec.Config.ChainID))
			contractAddrNew = crypto.CreateAddress(msg.From(), createContractTxNew.Nonce())
			for k := 0; k < number; k++ {
				invokeContractFuncTx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr3), contractAddrNew, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData1), signer, key3)
				gen.AddTx(invokeContractFuncTx)
			}
			t2 := time.Now()
			fmt.Println("test case1 sequence(", number, "tx:transfer,", number, "tx:create contract,", number, "tx:invoke contract) spend time:", t2.Sub(t1))
		case 1:
			// number条tx : addr1 sends addr2 some ether.
			t1 := time.Now()

			dag := dag.NewDag()
			//number = 500
			//txArray1 := make([]*types.Transaction, number)
			// for k := 0; k < number; k++ {
			// 	txArray1[k], _ = types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(50), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			// 	gen.AddTx(txArray1[k])
			// 	dag.AddNode(txArray1[k].Hash().String())
			// 	if k > 0 {
			// 		dag.AddEdge(txArray1[k-1].Hash().String(), txArray1[k].Hash().String())
			// 	}
			// }
			// // number条tx : addr3 create contract
			// txArray2 := make([]*types.Transaction, number)
			// for k := 0; k < number; k++ {
			// 	//注意：如果设置amount大于0，evm执行则会返回reverted错误
			// 	txArray2[k], _ = types.SignTx(types.NewContractCreation(gen.TxNonce(addr3), big.NewInt(0), initcreateContractGas+storageContranctDataGas, new(big.Int).SetInt64(1), codeByteData), signer, key3)
			// 	gen.AddTx(txArray2[k])
			// 	dag.AddNode(txArray2[k].Hash().String())
			// 	if k > 0 {
			// 		dag.AddEdge(txArray2[k-1].Hash().String(), txArray2[k].Hash().String())
			// 	}
			// }
			// // number+1条tx : 1条：add4创建一个新合约  numnber条：addr4 调用新合约的方法
			// //先创建一个另外的新的合约，再开始调用
			createContractTxNew, _ := types.SignTx(types.NewContractCreation(gen.TxNonce(addr4), big.NewInt(0), initcreateContractGas+storageContranctDataGasNew, new(big.Int).SetInt64(1), codeByteDataNew), signer, key4)
			gen.AddTx(createContractTxNew)
			dag.AddNode(createContractTxNew.Hash().String())
			msg, _ := createContractTxNew.AsMessage(types.NewInitialSigner(gspec.Config.ChainID))
			contractAddrNew := crypto.CreateAddress(msg.From(), createContractTxNew.Nonce())
			txArray3 := make([]*types.Transaction, number)
			for k := 0; k < number; k++ {
				txArray3[k], _ = types.SignTx(types.NewTransaction(gen.TxNonce(addr4), contractAddrNew, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData1), signer, key4)
				gen.AddTx(txArray3[k])
				dag.AddNode(txArray3[k].Hash().String())
				if k == 0 {
					dag.AddEdge(createContractTxNew.Hash().String(), txArray3[k].Hash().String())
				}
				if k > 0 {
					dag.AddEdge(txArray3[k-1].Hash().String(), txArray3[k].Hash().String())
				}
			}

			createContractTxNew, _ = types.SignTx(types.NewContractCreation(gen.TxNonce(addr3), big.NewInt(0), initcreateContractGas+storageContranctDataGasNew, new(big.Int).SetInt64(1), codeByteData), signer, key3)
			gen.AddTx(createContractTxNew)
			dag.AddNode(createContractTxNew.Hash().String())
			msg, _ = createContractTxNew.AsMessage(types.NewInitialSigner(gspec.Config.ChainID))
			contractAddrNew = crypto.CreateAddress(msg.From(), createContractTxNew.Nonce())
			txArray3 = make([]*types.Transaction, number)
			for k := 0; k < number; k++ {
				txArray3[k], _ = types.SignTx(types.NewTransaction(gen.TxNonce(addr3), contractAddrNew, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData1), signer, key3)
				gen.AddTx(txArray3[k])
				dag.AddNode(txArray3[k].Hash().String())
				if k == 0 {
					dag.AddEdge(createContractTxNew.Hash().String(), txArray3[k].Hash().String())
				}
				if k > 0 {
					dag.AddEdge(txArray3[k-1].Hash().String(), txArray3[k].Hash().String())
				}
			}
			t2 := time.Now()
			fmt.Println("test case1 concurrent exexcute(", number, "tx:transfer,", number, "tx:create contract,", number, "tx:invoke contract) spend time:", t2.Sub(t1))
			//fmt.Println("!!!!!ExecutionDag:" + dag.String() + "\n")
			gen.AddExecutionDag(*dag)
			// case 2:
			// 	// 3*number条tx : addr1 sends addr2 some ether.
			// 	t1 := time.Now()
			// 	for k := 0; k < 3*number; k++ {
			// 		tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(50), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			// 		gen.AddTx(tx)
			// 	}
			// 	t2 := time.Now()
			// 	fmt.Println("test case2(", 3*number, "tx:transfer) spend time:", t2.Sub(t1))
			// case 3:
			// 	// 3*number条tx : addr1 create contract
			// 	t1 := time.Now()
			// 	for k := 0; k < 3*number; k++ {
			// 		createContractTx, _ = types.SignTx(types.NewContractCreation(gen.TxNonce(addr1), big.NewInt(0), initcreateContractGas+storageContranctDataGas, new(big.Int).SetInt64(1), codeByteData), signer, key1)
			// 		gen.AddTx(createContractTx)
			// 	}
			// 	t2 := time.Now()
			// 	fmt.Println("test case3(", 3*number, "tx:create contract) spend time:", t2.Sub(t1))
			// 	msg, _ := createContractTx.AsMessage(types.NewInitialSigner(gspec.Config.ChainID))
			// 	contractAddr = crypto.CreateAddress(msg.From(), createContractTx.Nonce())
			// case 4:
			// 	// 3*number条tx : addr1 invoke  contract function
			// 	t1 := time.Now()
			// 	for k := 0; k < 3*number; k++ {
			// 		invokeContractFuncTx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), contractAddr, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData1), signer, key1)
			// 		gen.AddTx(invokeContractFuncTx)
			// 	}
			// 	t2 := time.Now()
			// 	fmt.Println("test case4(", 3*number, "tx:invoke contract) spend time:", t2.Sub(t1))
			//
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
	fmt.Println("-----------InsertChain block[0] by sequence Process spend time.", t2.Sub(t1))

	state, _ := blockchain.State()
	fmt.Printf("current block num : %#v \n ", blockchain.CurrentBlock().Header().Number)
	fmt.Printf("add1 balance  : %#v \n ", state.GetBalance(addr1))
	fmt.Printf("add2 balance  : %#v \n ", state.GetBalance(addr2))
	fmt.Printf("add3 balance  : %#v \n ", state.GetBalance(addr3))
	fmt.Printf("add4 balance  : %#v \n ", state.GetBalance(addr4))

	/*****************************/
	parallelBlockchain, _ := core.NewBlockChain(gspec.Config, vmConfig, db, pow.NewFaker())
	parallelProcessor := core.NewStateParallelProcessor(gspec.Config, blockchain)
	parallelBlockchain.SetProcessor(parallelProcessor)
	t1 = time.Now()
	if i, err := parallelBlockchain.InsertChain(types.Blocks{chain[1]}); err != nil {
		fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
		return
	}
	t2 = time.Now()
	fmt.Println("-----------InsertChain block[1] by concurrent Process spend time.", t2.Sub(t1))

	state, _ = parallelBlockchain.State()
	fmt.Printf("current block num : %#v \n ", parallelBlockchain.CurrentBlock().Header().Number)
	fmt.Printf("add1 balance  : %#v \n ", state.GetBalance(addr1))
	fmt.Printf("add2 balance  : %#v \n ", state.GetBalance(addr2))
	fmt.Printf("add3 balance  : %#v \n ", state.GetBalance(addr3))
	fmt.Printf("add4 balance  : %#v \n ", state.GetBalance(addr4))

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

func TestTxsConcurrentExecuteWithConflict(t *testing.T) {
	chain, _ := core.GenerateChain(gspec.Config, genesis, pow.NewFaker(), db, 2, func(i int, gen *core.BlockGen) {
		//var createContractTx *types.Transaction
		switch i {
		case 0:
			tx1, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(100), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			gen.AddTx(tx1)
			tx2, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(200), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			gen.AddTx(tx2)
			dag := dag.NewDag()
			dag.AddNode(tx1.Hash().String())
			dag.AddNode(tx2.Hash().String())
			//dag.AddEdge(tx1.Hash().String(), tx2.Hash().String())
			fmt.Println("!!!!!ExecutionDag:" + dag.String() + "\n")
			gen.AddExecutionDag(*dag)
		}
	})

	parallelBlockchain, _ := core.NewBlockChain(gspec.Config, vmConfig, db, pow.NewFaker())
	parallelProcessor := core.NewStateParallelProcessor(gspec.Config, blockchain)
	parallelBlockchain.SetProcessor(parallelProcessor)

	fmt.Println("chain length:", len(chain))
	fmt.Println("********************test parallelProcessor.Process function Time consuming *******")
	// state, err := state.New(chain[0].StateRoot(), state.NewDatabase(db))
	// if err != nil {
	// 	fmt.Println("Error:New state error.", err)
	// }
	t1 := time.Now()
	fmt.Printf("11** current block num : %#v，hash %v \n ", parallelBlockchain.CurrentBlock().Header().Number,
		parallelBlockchain.CurrentBlock().Hash().String())
	fmt.Printf("11** chain[0].hash %v \n ", chain[0].Hash().String())
	fmt.Printf("11** chain[1].parentHash %v \n ", chain[1].Header().ParentHash.String())
	if i, err := parallelBlockchain.InsertChain(types.Blocks{chain[0]}); err != nil {
		fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
		return
	}
	fmt.Printf("22** current block num : %#v，hash %v \n ", parallelBlockchain.CurrentBlock().Header().Number,
		parallelBlockchain.CurrentBlock().Hash().String())

	parent := parallelBlockchain.GetHeader(chain[1].Header().ParentHash, 1)
	if parent != nil {
		fmt.Printf("*****parent: %v \n", parent.Hash().String())
	}
	// if i, err := parallelBlockchain.InsertChain(types.Blocks{chain[1]}); err != nil {
	// 	fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
	// 	return
	// }
	t2 := time.Now()
	fmt.Println("InsertChain   spend time.", t2.Sub(t1))

	fmt.Println("Print relate infomation==>")
	fmt.Printf("block[1].dag : %#v \n ", chain[0].Header().ExecutionDag)
	state, _ := parallelBlockchain.State()
	fmt.Printf("current block num : %#v \n ", parallelBlockchain.CurrentBlock().Header().Number)
	fmt.Printf("add1 balance  : %#v \n ", state.GetBalance(addr1))
	fmt.Printf("add2 balance  : %#v \n ", state.GetBalance(addr2))
	fmt.Printf("add3 balance  : %#v \n ", state.GetBalance(addr3))
	fmt.Printf("add4 balance  : %#v \n ", state.GetBalance(addr4))
}

//2.测试并发执行交易需要的总耗时，
func TestTxsConcurrentExecute(t *testing.T) {
	//构造两个区块进行执行：
	//区块1包含了一个3个tx，分别是add1创建合约A，add1给add2转账100token，add3给add4转账50token
	//区块2包含了4个tx，分别是add1给add2转账50token，add1调用合约A，add3调用合约A,add4调用合约A,传入的参数都不同，最终影响的map的值也是不同的，看是否能并发执行
	var contractAddr common.Address
	chain, _ := core.GenerateChain(gspec.Config, genesis, pow.NewFaker(), db, 2, func(i int, gen *core.BlockGen) {
		//var createContractTx *types.Transaction
		switch i {
		case 0:
			//add1转账add2 100token
			tx1, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(100), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			gen.AddTx(tx1)
			//add3转账add4 50token
			tx2, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr3), addr4, big.NewInt(50), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key3)
			gen.AddTx(tx2)
			//addr1创建合约
			//tx3, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(150), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			tx3, _ := types.SignTx(types.NewContractCreation(gen.TxNonce(addr1), big.NewInt(0), initcreateContractGas+storageContranctDataGas, new(big.Int).SetInt64(1), codeByteData), signer, key1)
			gen.AddTx(tx3)
			msg, _ := tx3.AsMessageWithoutCheckNonce(types.NewInitialSigner(gspec.Config.ChainID))
			contractAddr = crypto.CreateAddress(msg.From(), tx3.Nonce())
			//add1调用合约
			//tx4, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(200), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			tx4, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), contractAddr, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData1), signer, key1)
			gen.AddTx(tx4)
			//构造区块的由tx执行关系组成的dag，dag图为
			//   tx1  =>  tx3=》tx4
			//   tx2
			dag := dag.NewDag()
			dag.AddNode(tx1.Hash().String())
			dag.AddNode(tx2.Hash().String())
			dag.AddNode(tx3.Hash().String())
			dag.AddNode(tx4.Hash().String())
			//如果不加依赖全部并发执行会报冲突错误
			dag.AddEdge(tx1.Hash().String(), tx3.Hash().String())
			//dag.AddEdge(tx2.Hash().String(), tx3.Hash().String())
			dag.AddEdge(tx3.Hash().String(), tx4.Hash().String())
			gen.AddExecutionDag(*dag)
		case 1:
			tx1, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(100), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			gen.AddTx(tx1)
			tx2, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), contractAddr, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData1), signer, key1)
			gen.AddTx(tx2)
			tx3, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr3), contractAddr, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData2), signer, key3)
			gen.AddTx(tx3)
			tx4, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr4), contractAddr, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData3), signer, key4)
			gen.AddTx(tx4)
			//构造区块的由tx执行关系组成的dag,dag图为
			//   tx1  =>  tx2
			//   tx3
			//   tx4
			dag := dag.NewDag()
			dag.AddNode(tx1.Hash().String())
			dag.AddNode(tx2.Hash().String())
			dag.AddNode(tx3.Hash().String())
			dag.AddNode(tx4.Hash().String())
			dag.AddEdge(tx1.Hash().String(), tx2.Hash().String())
			gen.AddExecutionDag(*dag)

		}
	})

	parallelBlockchain, _ := core.NewBlockChain(gspec.Config, vmConfig, db, pow.NewFaker())
	parallelProcessor := core.NewStateParallelProcessor(gspec.Config, blockchain)
	parallelBlockchain.SetProcessor(parallelProcessor)

	fmt.Println("chain length:", len(chain))
	fmt.Println("********************test parallelProcessor.Process function Time consuming *******")
	// state, err := state.New(chain[0].StateRoot(), state.NewDatabase(db))
	// if err != nil {
	// 	fmt.Println("Error:New state error.", err)
	// }

	// fmt.Printf("11** current block num : %#v，hash %v \n ", parallelBlockchain.CurrentBlock().Header().Number,
	// 	parallelBlockchain.CurrentBlock().Hash().String())
	// fmt.Printf("11** chain[0].hash %v \n ", chain[0].Hash().String())
	// fmt.Printf("11** chain[1].parentHash %v \n ", chain[1].Header().ParentHash.String())
	t1 := time.Now()
	if i, err := parallelBlockchain.InsertChain(types.Blocks{chain[0]}); err != nil {
		fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
		return
	}
	t2 := time.Now()
	fmt.Println("InsertChain   spend time.", t2.Sub(t1))
	// fmt.Printf("22** current block num : %#v，hash %v \n ", parallelBlockchain.CurrentBlock().Header().Number,
	// 	parallelBlockchain.CurrentBlock().Hash().String())

	parent := parallelBlockchain.GetHeader(chain[1].Header().ParentHash, 1)
	if parent != nil {
		fmt.Printf("*****parent: %v \n", parent.Hash().String())
	}
	// if i, err := parallelBlockchain.InsertChain(types.Blocks{chain[1]}); err != nil {
	// 	fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
	// 	return
	// }

	fmt.Println("Print relate infomation==>")
	fmt.Printf("block[1].dag : %#v \n ", chain[0].Header().ExecutionDag)
	state, _ := parallelBlockchain.State()
	fmt.Printf("current block num : %#v \n ", parallelBlockchain.CurrentBlock().Header().Number)
	fmt.Printf("add1 balance  : %#v \n ", state.GetBalance(addr1))
	fmt.Printf("add2 balance  : %#v \n ", state.GetBalance(addr2))
	fmt.Printf("add3 balance  : %#v \n ", state.GetBalance(addr3))
	fmt.Printf("add4 balance  : %#v \n ", state.GetBalance(addr4))

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
	//TestTxsSequenceExecute(t)
	//TestTxsConcurrentExecute(t)
	//TestTxsConcurrentExecuteWithConflict(t)
	TestTxsSequenceExecutePerformance(t)
}
