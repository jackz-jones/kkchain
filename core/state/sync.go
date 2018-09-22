package state

import (
	"bytes"

	"github.com/invin/kkchain/common"
	"github.com/invin/kkchain/rlp"
	"github.com/invin/kkchain/trie"
)

// NewStateSync create a new state trie download scheduler.
func NewStateSync(root common.Hash, database trie.DatabaseReader) *trie.Sync {
	var syncer *trie.Sync
	callback := func(leaf []byte, parent common.Hash) error {
		var obj Account
		if err := rlp.Decode(bytes.NewReader(leaf), &obj); err != nil {
			return err
		}
		syncer.AddSubTrie(obj.Root, 64, parent, nil)
		syncer.AddRawEntry(common.BytesToHash(obj.CodeHash), 64, parent)
		return nil
	}
	syncer = trie.NewSync(root, database, callback)
	return syncer
}
