package impl

import (
	"testing"

	"github.com/invin/kkchain/crypto/ed25519"
	"github.com/invin/kkchain/p2p"
	"github.com/invin/kkchain/p2p/protobuf"
)

func TestSerialization(t *testing.T) {
	// Create keypair
	keys := ed25519.RandomKeyPair()
	address := "localhost:12345"
	id := p2p.CreateID(address, keys.PublicKey)

	pid := protobuf.ID(id)
	message := "message1"

	out := SerializeMessage(&pid, []byte(message))
	t.Log("result =", out)
}
