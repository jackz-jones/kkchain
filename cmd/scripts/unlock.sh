#!/bin/bash
if [ "$#" -ne 2 ]; then
    echo "usage  : $0 account_address wallet_password"
    printf "example: $0 0xe084ac0a7b047871a2fd44e94e6375b80a7f2460 123456\n"
    exit -1
fi

source "helpers_common"
color_printf "Unlock wallet for account '$1'"

exe curl -X POST -H "Content-Type:application/json" --data '{"jsonrpc":"2.0","method":"personal_unlockAccount","params":["'$1'","'$2'", 86400],"id":1}' ${RPC_URL}
