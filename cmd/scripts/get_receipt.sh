#!/bin/bash
if [ "$#" -ne 1 ]; then
    echo "usage  : $0 tx_id"
    printf "example: $0 0x7680ab52534e316a4e6634e6926050630f9239ad4024162f29c4a80e192eed32\n"
    exit -1
fi

source "helpers_common"
color_printf "Get receipt for tx '$1'"

exe curl -X POST -H "Content-Type:application/json" --data '{"jsonrpc":"2.0","method":"kkc_getTransactionReceipt","params":["'$1'"],"id":1}' ${RPC_URL}
