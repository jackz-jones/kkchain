package p2p

// ConnManager defines interface to manage connections
type ConnManager interface {
	// Add a connection after handshake is ok
	AddConnection(id ID, conn Conn) error

	// Get a connection with peer ID
	Connection(id ID) (Conn, error)

	// Get all connection
	GetAllConnection() map[string]Conn

	// Remove a connection
	RemoveConnection(id ID) error

	// Remove all connection
	RemoveAllConnection()
}

// Notifier defines interface for notifications
type Notifier interface {
	// Register notifier
	Register(n Notifiee) error

	// Revoke notifier
	Revoke(n Notifiee) error
}

// Host defines a host for connections, it manages the life circle of each
// connection after verified, and notifies subscribers when connection is added
// and removed from connection map
type Host interface {
	// Connection manager
	ConnManager

	// Notifier
	Notifier

	// Returns ID of local peer
	ID() ID

	// Connect to remote peer
	Connect(address string) (Conn, error)

	// Set message handler
	SetMessageHandler(protocol string, handler MessageHandler) error

	// returns the stream for protocol
	MessageHandler(protocol string) (MessageHandler, error)

	// New connection inbound or outbound is created
	OnConnectionCreated(conn Conn, dir ConnDir)
}
