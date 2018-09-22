package rawdb


// DatabaseReader wraps the Has and Get method of a backing data store.
type DatabaseReader interface {
	// Has checks if the key exists or not
	Has(key []byte) (bool, error)

	// Get reads the data with key
	Get(key []byte)([]byte, error)
}

// DatabaseWriter wraps the Put method of a backing data storage
type DatabaseWriter interface {
	// Put puts data to storage
	Put(key, val []byte) error
}

// DatabaseDeleter wraps the Delete method of a backing data store
type DatabaseDeleter interface {
	// Delete removes the data with the key
	Delete(key []byte) error
}