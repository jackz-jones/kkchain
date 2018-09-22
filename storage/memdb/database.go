package memdb 

import (
	"errors"
	"sync"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/storage"

)

// error definitions
var (
	errEntryNotFound					= errors.New("entry not found")
)

// MemDatabase is a test memmory database, it's used for testing only
// Do not use it for production env
type MemDatabase struct {
	db map[string][]byte
	lock sync.RWMutex
}

// New creates a MemDatabase object 
func New() *MemDatabase {
	return &MemDatabase{
		db: make(map[string][]byte),
	}
}

// NewWithCap creates a MemDatabase object with capacity 
func NewWithCap(size int) *MemDatabase {
	return &MemDatabase{
		db: make(map[string][]byte, size),
	}
}

// Put puts a key-value pair to the database
func (db *MemDatabase) Put(key []byte, value []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	db.db[string(key)] = common.CopyBytes(value)

	return nil
}

// Delete deletes a key-value pair from the database
func (db *MemDatabase) Delete(key []byte) error {
	db.lock.Lock()
	defer db.lock.Unlock()

	delete(db.db, string(key))

	return nil
}

// Get read value with key from the database
func (db *MemDatabase) Get(key []byte) ([]byte, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	if entry, ok := db.db[string(key)]; ok {
		return common.CopyBytes(entry), nil
	}

	return nil, errEntryNotFound
}

// Has checks if the database contains the key
func (db *MemDatabase) Has(key []byte) (bool, error) {
	db.lock.RLock()
	defer db.lock.RUnlock()

	_, ok := db.db[string(key)]
	return ok, nil
}

// Keys returns all the keys contained in the database
func (db *MemDatabase) Keys() [][]byte {
	db.lock.RLock()
	defer db.lock.RUnlock()

	keys := make([][]byte, 0)
	for key := range db.db {
		keys = append(keys, []byte(key))
	}

	return keys
}


// Close closes the database
func (db *MemDatabase) Close() {}

// Len returns the length of key-value pairs
func (db *MemDatabase) Len() int {
	db.lock.RLock()
	defer db.lock.RUnlock()

	return len(db.db)
}

// NewBatch is ignored for memory database
func (db *MemDatabase) NewBatch() storage.Batch {
	return &memBatch{db: db}
}

type memBatch struct {
	db *MemDatabase
	writes []kv
	size int
}

type kv struct {
	k, v []byte
	del bool
}

// Put puts key value pair to for batch list
func (b *memBatch) Put(key, value []byte) error {
	b.writes = append(b.writes, kv{common.CopyBytes(key), common.CopyBytes(value), false})
	b.size += len(value)
	return nil
}

// Delete appends a delete operation to the batch list
func (b *memBatch) Delete(key []byte) error {
	b.writes = append(b.writes, kv{common.CopyBytes(key), nil, true})
	b.size++
	return nil
}

// Write writes all the operations in the batch list to underlying database
func (b *memBatch) Write() error {
	b.db.lock.Lock()
	defer b.db.lock.Unlock()

	for _, kv := range b.writes {
		if kv.del {
			delete(b.db.db, string(kv.k))
			continue
		}
		b.db.db[string(kv.k)] = kv.v
	}

	return nil
}

// ValueSize returns the size
func (b *memBatch) ValueSize() int {
	return b.size
}

// Reset reset the batch list
func (b *memBatch) Reset() {
	b.writes = b.writes[:0]
	b.size = 0
}