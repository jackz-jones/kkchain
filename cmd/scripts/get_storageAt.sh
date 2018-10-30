#!/bin/bash
if [ "$#" -ne 2 ]; then
    echo "usage  : $0 contract_address slot"
    printf "example: $0 0xb0637bef056e16763753e92bf2a5f2ba2bed18f1 0\n"
    exit -1
fi

source "helpers_common"
color_printf "Get storage at slot '$2' of contract '$1'"

exe curl -X POST -H "Content-Type:application/json" --data '{"jsonrpc":"2.0","method":"kkc_getStorageAt","params":["'$1'", "'$2'", "latest"],"id":1}' ${RPC_URL}