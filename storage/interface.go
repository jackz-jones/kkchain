package storage

// Batch size
const IdealBatchSize = 100 * 1024

// Putter wraps database write operation
type Putter interface {
	Put(key []byte, value []byte) error
}

// Deleter wraps database delete operation
type Deleter interface {
	Delete(key []byte) error
}

// Database wraps all database releated operations. 
// All methods are safe for concurrent use.
type Database interface {
	Putter
	Deleter
	Get(key []byte) ([]byte, error)
	Has(key []byte) (bool, error)
	Close()
	NewBatch() Batch
}

// Batch is a write-only database that commits changes to its host database
// when Write is called. Batch cannot be used concurrently.
type Batch interface {
	Putter
	Deleter
	ValueSize() int // Amount of data in the batch
	Write() error
	// Reet resets the batch for reuse
	Reset()
}
