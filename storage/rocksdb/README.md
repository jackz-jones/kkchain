# rocksdb for kkchain

## install for macos
```
$ brew install rocksdb

$ brew list rocksdb

Replace include and library path for your system
$ CGO_CFLAGS="-I/path/to/rocksdb/include" CGO_LDFLAGS="-L/path/to/rocksdb -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd" dep ensure

On my machine: 
$ CGO_CFLAGS="-I/usr/local/Cellar/rocksdb/5.14.3/include/" CGO_LDFLAGS="-L/usr/local/Cellar/rocksdb/5.14.3/lib/ -lrocksdb -lstdc++ -lm -lz -lbz2 -lsnappy -llz4 -lzstd" dep ensure 

```
