#!/bin/bash
if [ "$#" -ne 1 ]; then
    echo "usage  : $0 wallet_password"
    printf "example: $0 123456\n"
    exit -1
fi

source "helpers_common"
color_printf "Create a new account with password '$1'"

exe curl -X POST -H "Content-Type:application/json" --data '{"jsonrpc":"2.0","method":"personal_newAccount","params":["'$1'"],"id":1}' ${RPC_URL}
