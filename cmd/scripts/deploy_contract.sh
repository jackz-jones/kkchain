#!/bin/bash
if [ "$#" -ne 2 ]; then
    echo "usage  : $0 account_address contract_data"
    printf "example: $0 0xc14a6178ca89638d08d93153d4b20834ee4f5be2 6080604052348015600f57600080fd5b506000805560a2806100226000396000f3fe608060405260043610603e5763ffffffff7c010000000000000000000000000000000000000000000000000000000060003504166390f1f75881146043575b600080fd5b348015604e57600080fd5b50606960048036036020811015606357600080fd5b5035606b565b005b60008054909101905556fea165627a7a72305820461115413f7a52f80865c8c0a3fbe450b17010570ef0fbf37bccb611c504cb950029 \n"
    exit -1
fi

source "helpers_common"
color_printf "Deploy contract for '$1'"

exe curl -X POST -H "Content-Type:application/json" --data '{"jsonrpc":"2.0","method":"eth_sendTransaction","params":[{"from": "'$1'", "gas": "0x46000", "gasPrice": "0x9", "data": "'$2'"}],"id":1}' ${RPC_URL}
