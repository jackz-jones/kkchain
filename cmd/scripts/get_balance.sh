#!/bin/bash
if [ "$#" -ne 1 ]; then
    echo "usage  : $0 account_address"
    printf "example: $0 0xe084ac0a7b047871a2fd44e94e6375b80a7f2460\n"
    exit -1
fi

source "helpers_common"
color_printf "Get balance of account '$1'"

exe curl -X POST -H "Content-Type:application/json" --data '{"jsonrpc":"2.0","method":"eth_getBalance","params":["'$1'","pending"],"id":1}' ${RPC_URL}
