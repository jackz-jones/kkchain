package impl

import (
	"fmt"
	"net"

	"github.com/invin/kkchain/config"
	"github.com/invin/kkchain/core"
	"github.com/invin/kkchain/crypto"
	"github.com/invin/kkchain/crypto/blake2b"
	"github.com/invin/kkchain/crypto/ed25519"
	"github.com/invin/kkchain/p2p"
	"github.com/invin/kkchain/p2p/chain"
	"github.com/invin/kkchain/p2p/dht"

	"encoding/hex"

	"github.com/jbenet/goprocess"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var (
	errServerStopped = errors.New("server stopped")
	MaxPeers         = 1000
)

// Network represents the whole stack of p2p communication between peers
type Network struct {
	// Configuration
	conf p2p.Config

	// Host to manage connections
	host p2p.Host

	// Address to listen on
	listenAddr string

	// Node's keypair.
	keys *crypto.KeyPair

	// Message modules
	dht   *dht.DHT
	chain *chain.Chain

	// Bootstrap seed nodes
	bootstrapNodes []string

	// process to manager other child processes
	proc goprocess.Process

	bc *core.BlockChain
}

func DefaultConfig() p2p.Config {
	return p2p.Config{
		SignaturePolicy: ed25519.New(),
		HashPolicy:      blake2b.New(),
	}
}

// NewNetwork creates a new Network instance with the specified configuration
func NewNetwork(networkConfig *config.NetworkConfig, dhtConfig *config.DhtConfig, bc *core.BlockChain) *Network {
	keys, _ := p2p.LoadNodeKeyFromFileOrCreateNew(networkConfig.PrivateKey)
	id := p2p.CreateID(networkConfig.Listen, keys.PublicKey)

	n := &Network{
		conf:           DefaultConfig(),
		keys:           keys,
		listenAddr:     networkConfig.Listen,
		bc:             bc,
		bootstrapNodes: networkConfig.Seeds,
	}

	n.host = NewHost(id, n)

	// Create submodules
	n.chain = chain.New(n.host, n.bc)
	n.dht = dht.New(dhtConfig, n.host)

	return n
}

// Start kicks off the p2p stack
func (n *Network) Start() error {
	if n.keys == nil {
		return fmt.Errorf("Server.PrivateKey must be set to a non-nil key")
	}

	// Use goprocess to setup process tree
	n.proc = goprocess.WithTeardown(func() error {
		log.Info("Tear down network")
		return nil
	})

	// Start DHT as child of network process
	n.proc.Go(func(p goprocess.Process) {
		n.dht.Start()
		select {
		case <-p.Closing():
			log.Info("closing dht")
			n.dht.Stop()
			n.host.RemoveAllConnection()
		}
	})

	//start txs,block,synce handle  loop as child of network process
	n.proc.Go(func(p goprocess.Process) {
		n.chain.Start(MaxPeers)
		select {
		case <-p.Closing():
			log.Info("closing handle loop")
			n.chain.Stop()
		}
	})

	// Listen
	if n.listenAddr != "" {
		if err := n.startListening(); err != nil {
			return err
		}
	} else {
		log.Warning("P2P server will be useless, not listening")
	}

	// Start process to connect seed nodes
	n.proc.Go(func(p goprocess.Process) {
		n.bootstrap(p)
	})

	return nil
}

// Conf gets configurations
func (n *Network) Conf() p2p.Config {
	return n.conf
}

// Bootstraps returns seed nodes
func (n *Network) Bootstraps() []string {
	return n.bootstrapNodes
}

// Stop stops the p2p stack
func (n *Network) Stop() {
	n.host.RemoveAllConnection()
	n.proc.Close()
}

// startListening starts listen
func (n *Network) startListening() error {
	addr, err := dht.ToNetAddr(n.listenAddr)
	if err != nil {
		return err
	}

	// Enter listen mode
	listener, err := net.Listen(addr.Network(), addr.String())
	if err != nil {
		return err
	}

	laddr := listener.Addr().(*net.TCPAddr)
	n.listenAddr = laddr.String()

	// Setup process to kill listener on demand
	n.proc.Go(func(p goprocess.Process) {
		select {
		case <-p.Closing():
			// cause listener.Accept to stop blocking so it can breakout the loop
			log.Info("close listener")
			listener.Close()
		}
	})

	// Run listenr process
	n.proc.Go(func(p goprocess.Process) {
		for {
			if conn, err := listener.Accept(); err == nil {
				c := NewConnection(conn, n, n.host)
				n.host.OnConnectionCreated(c, p2p.Inbound)
			} else {
				// if we're about to shutdown, no need to continue with the loop
				select {
				case <-p.Closing():
					log.Info("Shutting down server")
					return
				default:
					log.Errorf("failed to listen accept,error: %v", err)
				}
			}
		}
	})

	return nil
}

// Bootstrap connects to seed nodes
func (n *Network) bootstrap(p goprocess.Process) {
	for _, node := range n.bootstrapNodes {
		// Check if we're asked to shutdown
		select {
		case <-p.Closing():
			return
		default:

			// check the seed is not self
			self := hex.EncodeToString(n.host.ID().PublicKey) + "@" + n.host.ID().Address
			if node == self {
				log.Warn("refuse to connect self")
			} else {
				log.Infof("ready to connect %s", node)
			}
		}

		// Parse peer address to get IP
		peer, err := dht.ParsePeerAddr(node)
		if err != nil {
			log.WithFields(log.Fields{
				"nodeAddr": node,
				"error":    err,
			}).Error("failed to parse peer address from bootstrap nodes")
			continue
		}

		// Reuse connection if it's already connected
		conn, _ := n.host.Connection(peer.ID)
		if conn != nil {
			continue
		}

		// Connect to peer node
		go func() {
			_, err := n.host.Connect(peer.Address)
			if err != nil {
				log.Errorf("failed to connect boost node: %s", peer.String())
				return
			}
		}()
	}
}

// Sign signs a message
func (n *Network) Sign(message []byte) ([]byte, error) {
	return n.keys.Sign(n.conf.SignaturePolicy, n.conf.HashPolicy, message)
}

// Verify verifies the message
func (n *Network) Verify(publicKey []byte, message []byte, signature []byte) bool {
	return crypto.Verify(n.conf.SignaturePolicy, n.conf.HashPolicy, publicKey, message, signature)
}

// Proc returns network process
func (n *Network) Proc() goprocess.Process {
	return n.proc
}
