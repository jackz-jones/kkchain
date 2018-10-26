#!/bin/bash
if [ "$#" -ne 3 ]; then
    echo "usage  : $0 from_account to_account eth_in_0x_gwei"
    printf "example: $0 0x7e32ad78c1b2d1f88b361f2d48d7a4126c2be9fa 0xc14a6178ca89638d08d93153d4b20834ee4f5be2 0x76c0 \n"
    exit -1
fi

source "helpers_common"
color_printf "Transfer '$3' ETH from '$1' to '$2' "

exe curl -X POST -H "Content-Type:application/json" --data '{"jsonrpc":"2.0","method":"eth_sendTransaction","params":[{"from": "'$1'", "to": "'$2'", "gas": "0x76c0", "gasPrice": "0x9184e72a000", "value": "'$3'"}],"id":1}' ${RPC_URL}
