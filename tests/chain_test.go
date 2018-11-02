package main

import (
	"fmt"
	"math/big"
	"testing"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/consensus/pow"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/core/types"
	"github.com/invin/kkchain/core/vm"
	"github.com/invin/kkchain/crypto"
	"github.com/invin/kkchain/params"
	"github.com/invin/kkchain/storage/memdb"
)

//1.测试外部账户之间转账
//func TestExternalAccountTransfer() {
func TestExternalAccountTransfer(t *testing.T) {
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		key2, _ = crypto.HexToECDSA("8a1f9a8f95be41cd7ccb6168179afb4504aefe388d1e14474d32c45c72ce7b7a")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		addr2   = crypto.PubkeyToAddress(key2.PublicKey)
		db      = memdb.New()
	)

	// Ensure that key1 has some funds in the genesis block.
	gspec := &core.Genesis{
		Config: &params.ChainConfig{ChainID: new(big.Int).SetInt64(1)},
		Alloc:  core.GenesisAlloc{addr1: {Balance: big.NewInt(1000000)}},
	}
	genesis := gspec.MustCommit(db)

	signer := types.NewInitialSigner(new(big.Int).SetInt64(1))
	chain, receipts := core.GenerateChain(gspec.Config, genesis, pow.NewFaker(), db, 1, func(i int, gen *core.BlockGen) {
		switch i {
		case 0:
			// In block 1, addr1 sends addr2 some king.
			tx, _ := types.SignTx(types.NewTransaction(gen.TxNonce(addr1), addr2, big.NewInt(500), params.TxGas, new(big.Int).SetInt64(1), nil), signer, key1)
			gen.AddTx(tx)
		}
	})

	// Import the chain. This runs all block validation rules.
	blockchain, _ := core.NewBlockChain(gspec.Config, vm.Config{}, db, pow.NewFaker())

	if i, err := blockchain.InsertChain(chain); err != nil {
		fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
		return
	}

	state, _ := blockchain.State()
	fmt.Printf("Transfer Info====>\n")
	fmt.Printf("last block: #%d \n", blockchain.CurrentBlock().Number())
	fmt.Printf("last block's gaspool:%d \n", blockchain.CurrentBlock().Header().GasLimit)
	fmt.Printf("balance of addr1:%s\n", state.GetBalance(addr1))
	fmt.Printf("balance of addr2:%s\n", state.GetBalance(addr2))
	fmt.Printf("transfer receipts: %#v\n", receipts[0][0])

	// t.Log("last block: #", blockchain.CurrentBlock().Number())
	// t.Log("last block's gaspool;: #", blockchain.CurrentBlock().Header().GasLimit)
	// t.Log("balance of addr1:", state.GetBalance(addr1))
	// t.Log("balance of addr2:", state.GetBalance(addr2))

	// Output:
	// last block: #1
	// balance of addr1: 999500
	// balance of addr2: 500
}

//2.测试外部账户创建合约

//func TestCreateContract() {
func TestCreateContract(t *testing.T) {
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		db      = memdb.New()
	)

	// Ensure that key1 has some funds in the genesis block.
	gspec := &core.Genesis{
		Config: &params.ChainConfig{ChainID: new(big.Int).SetInt64(1)},
		Alloc:  core.GenesisAlloc{addr1: {Balance: big.NewInt(1000000)}},
	}
	genesis := gspec.MustCommit(db)

	/*contract source code  complie version：0.4.25
	pragma solidity ^0.4.0;
	contract Test {
	    event Fun(uint256 c);
	    function fun(uint256 a,uint256 b) returns(uint256 c){
	        emit Fun(a+b);
	        return a+b;
	    }
	}
	*/
	/*
		代码指令由三部分构成：部署代码+合约代码+Auxdata
		部署代码：创建合约时运行部署代码
		合约代码：合约创建成功之后当它的方法被调用时，运行合约代码
		(可选）Auxdata：源码的加密指纹，用来验证。这只是数据，永远不会被EVM执行
		  部署代码有两个主要作用：
		运行构造器函数，并设置初始化内存变量（就像合约的拥有者）
		计算合约代码，并返回给EVM
	*/
	code := "6080604052348015600f57600080fd5b5060d78061001e6000396000f300608060405260043610603e5763ffffffff7c0100000000000000000000000000000000000000000000000000000000600035041663e9a58c4081146043575b600080fd5b348015604e57600080fd5b50605b600435602435606d565b60408051918252519081900360200190f35b60408051838301815290516000917f23f54f87f4b5ab78f25d15d4f83e12dc9c218476be07d632a5715acb97dcc736919081900360200190a15001905600a165627a7a723058202a9f2f6b64bf01b04f128ed3e492948eabcb837e52bee887380a5d41557afa320029"
	codeByteData := common.Hex2Bytes(code)

	var tx *types.Transaction
	signer := types.NewInitialSigner(new(big.Int).SetInt64(1))
	chain, _ := core.GenerateChain(gspec.Config, genesis, pow.NewFaker(), db, 1, func(i int, gen *core.BlockGen) {
		switch i {

		case 0:
			// In block 1, addr1 create contract
			initcreateContractGas, _ := core.IntrinsicGas(codeByteData, true)           //53000
			storageContranctDataGas := uint64(len(codeByteData)) * params.CreateDataGas //187*200=37400
			//注意：如果设置amount大于0，evm执行则会返回reverted错误
			tx, _ = types.SignTx(types.NewContractCreation(gen.TxNonce(addr1), big.NewInt(0), initcreateContractGas+storageContranctDataGas, new(big.Int).SetInt64(1), codeByteData), signer, key1)
			gen.AddTx(tx)
		}
	})

	// Import the chain. This runs all block validation rules.
	blockchain, _ := core.NewBlockChain(gspec.Config, vm.Config{}, db, pow.NewFaker())

	if i, err := blockchain.InsertChain(chain); err != nil {
		fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
		return
	}

	state, _ := blockchain.State()
	msg, _ := tx.AsMessage(types.NewInitialSigner(gspec.Config.ChainID))
	contractAddr := crypto.CreateAddress(msg.From(), tx.Nonce())

	obj := state.GetOrNewStateObject(contractAddr)

	if state.GetCode(contractAddr) == nil || common.Bytes2Hex(state.GetCode(contractAddr)) == "" {
		fmt.Println("create contract failed!")
		//t.Errof("create contract failed!")
	}

	fmt.Printf("last block: #%d\n", blockchain.CurrentBlock().Number())
	fmt.Printf("invoke Address: %s \n", msg.From().String())
	fmt.Printf("contract Address: %s \n", contractAddr.String())
	fmt.Printf("contract Address stateObject: %#v \n", obj)
	fmt.Printf("contract code : %s \n", common.Bytes2Hex(state.GetCode(contractAddr)))

	// t.Log("last block: ", blockchain.CurrentBlock().Number())
	// t.Log("invoke Address: ", msg.From().String())
	// t.Log("contract Address: ", contractAddr.String())
	// t.Log("contract Address stateObject: ", obj)
	// t.Log("contract code : ", common.Bytes2Hex(state.GetCode(contractAddr)))

	// Output:
	/*
		last block: #1
		invoke Address: 0x71562b71999873DB5b286dF957af199Ec94617F7
		contract Address: 0x3A220f351252089D385b29beca14e27F204c296A
		contract Address stateObject:
		        &state.stateObject{address:common.Address{0x3a, 0x22, 0xf, 0x35, 0x12, 0x52, 0x8, 0x9d, 0x38, 0x5b, 0x29, 0xbe, 0xca, 0x14, 0xe2, 0x7f, 0x20, 0x4c, 0x29, 0x6a},
				addrHash:common.Hash{0x1c, 0xb2, 0x58, 0x37, 0x48, 0xc2, 0x6e, 0x89, 0xef, 0x19, 0xc2, 0xa8, 0x52, 0x9b, 0x5, 0xa2, 0x70, 0xf7, 0x35, 0x55, 0x3b, 0x4d, 0x44, 0xb6, 0xf2, 0xa1, 0x89, 0x49, 0x87, 0xa7, 0x1c, 0x8b},
				data:state.Account{Nonce:0x1, Balance:(*big.Int)(0xc420143c40), Root:common.Hash{0x56, 0xe8, 0x1f, 0x17, 0x1b, 0xcc, 0x55, 0xa6, 0xff, 0x83, 0x45, 0xe6, 0x92, 0xc0, 0xf8, 0x6e, 0x5b, 0x48, 0xe0, 0x1b, 0x99, 0x6c, 0xad, 0xc0, 0x1, 0x62, 0x2f, 0xb5, 0xe3, 0x63, 0xb4, 0x21},
				CodeHash:[]uint8{0x76, 0xe6, 0x42, 0x2c, 0x57, 0xd6, 0x90, 0x20, 0xc2, 0x42, 0x35, 0x2a, 0xc0, 0x80, 0xec, 0x76, 0xeb, 0xa5, 0x42, 0xdd, 0x7f, 0x3b, 0x24, 0xef, 0x51, 0x6a, 0xc0, 0xe9, 0x5, 0x32, 0x9f, 0xcd}},
				db:(*state.StateDB)(0xc4200aaee0), dbErr:error(nil), trie:state.Trie(nil), code:state.Code(nil), cachedStorage:state.Storage{}, dirtyStorage:state.Storage{}, dirtyCode:false, suicided:false, deleted:false}
		contract code : 608060405260043610603e5763ffffffff7c0100000000000000000000000000000000000000000000000000000000600035041663e9a58c4081146043575b600080fd5b348015604e57600080fd5b50605b600435602435606d565b60408051918252519081900360200190f35b01905600a165627a7a72305820b57f94d369e0a016482485a940360c6f1ba5f6962bac923a98467d68bc9907e70029
	*/
}

//3.测试外部账户调用之前创建好的智能合约
//func TestInvokeUserContract() {
func TestInvokeUserContract(t *testing.T) {
	var (
		key1, _ = crypto.HexToECDSA("b71c71a67e1177ad4e901695e1b4b9ee17ae16c6668d313eac2f96dbcda3f291")
		addr1   = crypto.PubkeyToAddress(key1.PublicKey)
		db      = memdb.New()
	)

	// Ensure that key1 has some funds in the genesis block.
	gspec := &core.Genesis{
		Config: &params.ChainConfig{ChainID: new(big.Int).SetInt64(1)},
		Alloc:  core.GenesisAlloc{addr1: {Balance: big.NewInt(1000000)}},
	}
	genesis := gspec.MustCommit(db)

	/*contract source code  complie version：0.4.25
	pragma solidity ^0.4.0;
	contract Test {
	    event Fun(uint256 c);
	    function fun(uint256 a,uint256 b) returns(uint256 c){
	        emit Fun(a+b);
	        return a+b;
	    }
	}
	*/
	/*
		代码指令由三部分构成：部署代码+合约代码+Auxdata
		部署代码：创建合约时运行部署代码
		合约代码：合约创建成功之后当它的方法被调用时，运行合约代码
		(可选）Auxdata：源码的加密指纹，用来验证。这只是数据，永远不会被EVM执行
		  部署代码有两个主要作用：
		运行构造器函数，并设置初始化内存变量（就像合约的拥有者）
		计算合约代码，并返回给EVM
	*/
	//contract code
	//
	code := "6080604052348015600f57600080fd5b5060d78061001e6000396000f300608060405260043610603e5763ffffffff7c0100000000000000000000000000000000000000000000000000000000600035041663e9a58c4081146043575b600080fd5b348015604e57600080fd5b50605b600435602435606d565b60408051918252519081900360200190f35b60408051838301815290516000917f23f54f87f4b5ab78f25d15d4f83e12dc9c218476be07d632a5715acb97dcc736919081900360200190a15001905600a165627a7a723058202a9f2f6b64bf01b04f128ed3e492948eabcb837e52bee887380a5d41557afa320029"
	codeByteData := common.Hex2Bytes(code)

	//input code of invoke contract
	//inputdata格式：调用合约的方法地址+参数列表（参数1+参数2..）
	invokeInputCode := "e9a58c4000000000000000000000000000000000000000000000000000000000000000030000000000000000000000000000000000000000000000000000000000000002"
	inputByteData := common.Hex2Bytes(invokeInputCode)

	var createContractTx *types.Transaction
	var invokeContractTx *types.Transaction
	var contractAddr common.Address

	//signer
	signer := types.NewInitialSigner(new(big.Int).SetInt64(1))

	//Gas calc
	initcreateContractGas, _ := core.IntrinsicGas(codeByteData, true)           //53000
	storageContranctDataGas := uint64(len(codeByteData)) * params.CreateDataGas //187*200=37400

	chain, receipts := core.GenerateChain(gspec.Config, genesis, pow.NewFaker(), db, 2, func(i int, gen *core.BlockGen) {
		switch i {

		case 0:
			// In block 1, addr1 create contract

			//注意：如果设置amount大于0，evm执行则会返回reverted错误
			createContractTx, _ = types.SignTx(types.NewContractCreation(gen.TxNonce(addr1), big.NewInt(0), initcreateContractGas+storageContranctDataGas, new(big.Int).SetInt64(1), codeByteData), signer, key1)
			gen.AddTx(createContractTx)
			msg, _ := createContractTx.AsMessage(types.NewInitialSigner(gspec.Config.ChainID))
			contractAddr = crypto.CreateAddress(msg.From(), createContractTx.Nonce())
		case 1:
			invokeContractTx, _ = types.SignTx(types.NewTransaction(gen.TxNonce(addr1), contractAddr, big.NewInt(0), initcreateContractGas+1000, new(big.Int).SetInt64(1), inputByteData), signer, key1)
			gen.AddTx(invokeContractTx)
		}
	})

	// Import the chain. This runs all block validation rules.
	blockchain, _ := core.NewBlockChain(gspec.Config, vm.Config{}, db, pow.NewFaker())

	if i, err := blockchain.InsertChain(chain); err != nil {
		fmt.Printf("insert error (block %d): %v\n", chain[i].NumberU64(), err)
		return
	}

	state, _ := blockchain.State()
	obj := state.GetOrNewStateObject(contractAddr)

	if state.GetCode(contractAddr) == nil || common.Bytes2Hex(state.GetCode(contractAddr)) == "" {
		fmt.Println("create contract failed!")
		return
		//t.Errof("create contract failed!")
	}

	fmt.Printf("last block: #%d\n", blockchain.CurrentBlock().Number())
	fmt.Printf("Create contract Info======> \n")
	fmt.Printf("contract Address: %s \n", contractAddr.String())
	fmt.Printf("contract Address stateObject: %#v \n", obj)
	fmt.Printf("contract code : %s \n", common.Bytes2Hex(state.GetCode(contractAddr)))
	fmt.Printf("Create contract receipt: %#v \n", receipts[0][0])
	fmt.Printf("Invoke contract Info======> \n")
	fmt.Printf("Invoke contract receipt:%#v \n ", receipts[1][0])
	fmt.Printf("Invoke contract logs[0].data:%#v \n ", receipts[1][0].Logs[0].Data)

}

// func main() {
// 	//TestExternalAccountTransfer()
// 	//TestCreateContract()
// 	TestInvokeUserContract()

// }
