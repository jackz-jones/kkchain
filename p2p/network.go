package p2p

import (
	"io"

	"github.com/gogo/protobuf/proto"
	"github.com/invin/kkchain/crypto"
	"github.com/jbenet/goprocess"
)

// ConnDir defines connection direction
type ConnDir int

const (
	// Inbound connection
	Inbound ConnDir = iota
	// Outbound connection
	Outbound
)

// Config defines configurations for a basic network instance
type Config struct {
	SignaturePolicy crypto.SignaturePolicy
	HashPolicy      crypto.HashPolicy
}

// Network defines the interface for network communication
type Network interface {
	// Start kicks off the network stack
	Start() error

	// Get configurations
	Conf() Config

	// Stop the network stack
	Stop()

	// Sign message
	Sign(message []byte) ([]byte, error)

	// Verify message
	Verify(publicKey []byte, message []byte, signature []byte) bool

	// Main process
	Proc() goprocess.Process
}

// Conn wraps connection related operations, such as reading and writing
// messages
type Conn interface {
	io.Closer

	// Read message
	ReadMessage() (proto.Message, string, error)

	// Write message
	WriteMessage(proto.Message, string) error

	// Returns remote peer
	RemotePeer() ID

	// Verified flag
	Verified() bool

	// Set verified
	SetVerified()

	SendChainMsg(msgType int32, msgData interface{}) error
}

// Notifiee is an interface for an object wishing to receive
// notifications from network. Notifiees should take care not to register other
// notifiees inside of a notification.  They should also take care to do as
// little work as possible within their notification, putting any blocking work
// out into a goroutine.
type Notifiee interface {
	Connected(Conn)    // called when a connection opened
	Disconnected(Conn) // called when a connection closed
}
