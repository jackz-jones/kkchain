package impl

import (
	"testing"
	"time"
	// "encoding/hex"

	"github.com/invin/kkchain/crypto/blake2b"
	"github.com/invin/kkchain/crypto/ed25519"
	"github.com/invin/kkchain/p2p"
)

func TestStartStopNetwork(t *testing.T) {
	config := p2p.Config{
		SignaturePolicy: ed25519.New(),
		HashPolicy:      blake2b.New(),
	}

	listen := "/ip4/127.0.0.1/tcp/9999"

	net := NewNetwork("", listen, config, nil)

	if err := net.Start(); err != nil {
		t.Fatal("failed to start network")
	}

	<-time.After(3 * time.Second)

	net.Stop()

	t.Log("network stopped")

	// peerPort := "9999"
	// if *port == "9999" {
	// 	peerPort = "9998"
	// 	remoteKeyPath := "node1.key"
	// 	pri, _ := p2p.LoadNodeKeyFromFile(remoteKeyPath)
	// 	pub, _ := ed25519.New().PrivateToPublic(pri)
	// 	node := "/ip4/127.0.0.1/tcp/" + peerPort
	// 	node = hex.EncodeToString(pub) + "@" + node
	// 	fmt.Printf("remote peer: %s\n", node)
	// 	net.BootstrapNodes = []string{node}
	// }

}
