package rawdb

import (
	"encoding/json"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/params"
	"github.com/invin/kkchain/rlp"

	log "github.com/sirupsen/logrus"
)

// ReadDatabaseVersion retrieves the version number of the database.
func ReadDatabaseVersion(db DatabaseReader) int {
	var version int
	enc, _ := db.Get(databaseVerisionKey)
	rlp.DecodeBytes(enc, &version)

	return version
}

// WriteDatabaseVersion stores the version number of the database
func WriteDatabaseVersion(db DatabaseWriter, version int) {
	enc, _ := rlp.EncodeToBytes(version)
	if err := db.Put(databaseVerisionKey, enc); err != nil {
		log.Fatalf("Failed to store the database version,err: %v", err)
	}
}

// ReadChainConfig retrieves the consensus settings based on the given genesis hash.
func ReadChainConfig(db DatabaseReader, hash common.Hash) *params.ChainConfig {
	data, _ := db.Get(configKey(hash))
	if len(data) == 0 {
		return nil
	}
	var config params.ChainConfig
	if err := json.Unmarshal(data, &config); err != nil {
		log.WithFields(log.Fields{
			"hash": hash,
			"err":  err,
		}).Error("Invalid chain config JSON")
		return nil
	}
	return &config
}

// WriteChainConfig writes the chain config settings to the database.
func WriteChainConfig(db DatabaseWriter, hash common.Hash, cfg *params.ChainConfig) {
	if cfg == nil {
		return
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		log.Fatalf("Failed to JSON encode chain config,err: %v", err)
	}
	if err := db.Put(configKey(hash), data); err != nil {
		log.Fatalf("Failed to store chain config,err: %v", err)
	}
}

// ReadPreimage retrieves a single preimage of the provided hash.
func ReadPreimage(db DatabaseReader, hash common.Hash) []byte {
	data, _ := db.Get(preimageKey(hash))
	return data
}

// WritePreimages writes the provided set of preimages to the database. `number` is the
// current block number, and is used for debug messages only.
func WritePreimages(db DatabaseWriter, number uint64, preimages map[common.Hash][]byte) {
	for hash, preimage := range preimages {
		if err := db.Put(preimageKey(hash), preimage); err != nil {
			log.Fatalf("Failed to store trie preimage,err: %v", err)
		}
	}
}
