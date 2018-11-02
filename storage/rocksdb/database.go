package rocksdb

import (
	"errors"

	rdb "github.com/invin/gorocksdb"
	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/storage"
)

var (
	errOpenDatabase = errors.New("failed to open rocksdb database")
	errKeyNotFound  = errors.New("failed to find the key")
)

// RDBDatabase wraps read/write operations with rocksdb
type RDBDatabase struct {
	dir          string // directory of database
	db           *rdb.DB
	readOptions  *rdb.ReadOptions
	writeOptions *rdb.WriteOptions
}

// New creates a RDBDatabase
func New(dir string, applyOpts func(opts *rdb.Options)) (*RDBDatabase, error) {
	// Create default options
	opts := rdb.NewDefaultOptions()
	opts.SetCreateIfMissing(true)

	if applyOpts != nil {
		applyOpts(opts)
	}

	// Open database
	db, err := rdb.OpenDb(opts, dir)

	if err != nil {
		return nil, errOpenDatabase
	}

	return &RDBDatabase{
		dir:          dir,
		db:           db,
		readOptions:  rdb.NewDefaultReadOptions(),
		writeOptions: rdb.NewDefaultWriteOptions()}, nil
}

// Path returns the path to the database directory.
func (r *RDBDatabase) Path() string {
	return r.dir
}

// Put writes a key-value pair to the database
func (r *RDBDatabase) Put(key []byte, value []byte) error {
	//fmt.Println("ROCKSDB WRITE: ", key, value)
	return r.db.Put(r.writeOptions, key, value)
}

// Delete deletes a key-value pair from the database
func (r *RDBDatabase) Delete(key []byte) error {
	return r.db.Delete(r.writeOptions, key)
}

// Get gets value with key from database
func (r *RDBDatabase) Get(key []byte) ([]byte, error) {
	val, err := r.db.Get(r.readOptions, key)
	if err != nil {
		return nil, err
	}

	defer val.Free()

	//fmt.Println("ROCKSDB GET: ", val.Data())
	if val.Nil() {
		return nil, errKeyNotFound
	}

	return common.CopyBytes(val.Data()), nil
}

// Has checks if value with key exists in the database
func (r *RDBDatabase) Has(key []byte) (bool, error) {
	_, err := r.Get(key)
	if err != nil {
		return false, nil
	}

	return true, nil
}

// Close closes database
func (r *RDBDatabase) Close() {
	r.db.Close()
}

// RDB returns the underlying database
func (r *RDBDatabase) RDB() *rdb.DB {
	return r.db
}

// NewBatch is ignored for memory database
func (r *RDBDatabase) NewBatch() storage.Batch {
	return &rdbBatch{db: r.db, writeOptions: r.writeOptions, writeBatch: rdb.NewWriteBatch()}
}

type rdbBatch struct {
	db           *rdb.DB
	writeOptions *rdb.WriteOptions
	writeBatch   *rdb.WriteBatch
}

// Put puts key value pair to for batch list
func (b *rdbBatch) Put(key, value []byte) error {
	b.writeBatch.Put(key, value)

	return nil
}

// Delete appends a delete operation to the batch list
func (b *rdbBatch) Delete(key []byte) error {
	b.writeBatch.Delete(key)

	return nil
}

// Write writes all the operations in the batch list to underlying database
func (b *rdbBatch) Write() error {
	return b.db.Write(b.writeOptions, b.writeBatch)
}

// ValueSize returns the size
func (b *rdbBatch) ValueSize() int {
	// FIXME
	return b.writeBatch.Count()
}

// Reset reset the batch list
func (b *rdbBatch) Reset() {
	b.writeBatch.Destroy()
}
