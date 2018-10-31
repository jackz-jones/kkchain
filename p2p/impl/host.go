package impl

import (
	"net"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/invin/kkchain/p2p"
	"github.com/invin/kkchain/p2p/dht"
	"github.com/invin/kkchain/p2p/handshake"
	log "github.com/sirupsen/logrus"
)

// Host defines a host for connections
type Host struct {
	id p2p.ID

	// connection map
	connections map[string]p2p.Conn

	// max connections allowed
	maxConnections int

	// message handler map
	handlers map[string]p2p.MessageHandler

	// mutex to sync access
	mux sync.Mutex

	// notification listeners
	notifiees map[p2p.Notifiee]struct{}

	// notifier mux
	notifyMux sync.Mutex

	// network
	n p2p.Network
}

// NewHost creates a new host object
func NewHost(id p2p.ID, n p2p.Network) *Host {
	return &Host{
		id:             id,
		connections:    make(map[string]p2p.Conn),
		maxConnections: 32,
		handlers:       make(map[string]p2p.MessageHandler),
		notifiees:      make(map[p2p.Notifiee]struct{}),
		n:              n,
	}
}

// Register registers a notifier
func (h *Host) Register(n p2p.Notifiee) error {
	h.notifyMux.Lock()
	defer h.notifyMux.Unlock()

	_, found := h.notifiees[n]
	if found {
		return errDuplicateNotifiee
	}

	h.notifiees[n] = struct{}{}

	return nil
}

// Revoke revokes a notifier
func (h *Host) Revoke(n p2p.Notifiee) error {
	h.notifyMux.Lock()
	defer h.notifyMux.Unlock()

	_, found := h.notifiees[n]
	if !found {
		return errNotifieeNotFound
	}

	delete(h.notifiees, n)

	return nil
}

// notifyAll runs the notification function on all Notifiees
// example:
//
// h.notifyAll(func(n p2p.Notifiee) {
// 	n.Connected(newConn)
// })
func (h *Host) notifyAll(notification func(n p2p.Notifiee)) {
	h.notifyMux.Lock()
	defer h.notifyMux.Unlock()

	var wg sync.WaitGroup
	for n := range h.notifiees {
		wg.Add(1)
		go func(n p2p.Notifiee) {
			defer wg.Done()
			notification(n)
		}(n)
	}

	wg.Wait()
}

// OnConnectionCreated is called when new connection is available
func (h *Host) OnConnectionCreated(c p2p.Conn, dir p2p.ConnDir) {
	// Loop to handle messages
	go func() {
		defer c.Close()
		// handeshake
		hs := handshake.New(h)
		if err := hs.Handshake(c, dir); err != nil {
			log.Errorf("failed to handshake,error: %v", err)
			return
		}

		pid := c.RemotePeer()
		if err := h.AddConnection(pid, c); err != nil {
			log.Errorf("failed to add conn,error: %v", err)
			return
		}

		// handle other messages
		for {
			msg, protocol, err := c.ReadMessage()
			if err != nil {
				if err != errEmptyMessage {
					log.Errorf("failed to read msg,error: %v", err)
				}
				break
			}

			err = h.dispatchMessage(c, msg, protocol)
			if err != nil {
				log.Errorf("failed to dispatch msg,error: %v", err)
				break
			}
		}

		h.RemoveConnection(pid)
	}()
}

// dispatch message according to protocol
func (h *Host) dispatchMessage(conn p2p.Conn, msg proto.Message, protocol string) error {
	// get stream handler
	handler, err := h.MessageHandler(protocol)
	if err != nil {
		return err
	}

	// handle message
	handler(conn, msg)

	return nil
}

// AddConnection a connection
func (h *Host) AddConnection(id p2p.ID, conn p2p.Conn) error {
	h.mux.Lock()
	defer h.mux.Unlock()

	// Check if there are too many connections
	if len(h.connections) >= h.maxConnections {
		return errConnectionExceedMax
	}

	publicKey := string(id.PublicKey)
	_, found := h.connections[publicKey]

	//return errDuplicateConnection
	if !found {
		h.connections[publicKey] = conn

		// notify a new connection
		h.notifyAll(func(n p2p.Notifiee) {
			n.Connected(conn)
		})

	}

	return nil
}

// Connection get a connection with ID
func (h *Host) Connection(id p2p.ID) (p2p.Conn, error) {
	h.mux.Lock()
	defer h.mux.Unlock()

	publicKey := string(id.PublicKey)
	conn, ok := h.connections[publicKey]

	if !ok {
		return nil, errConnectionNotFound
	}

	return conn, nil
}

// GetAllConnection gets all the connections
func (h *Host) GetAllConnection() map[string]p2p.Conn {
	h.mux.Lock()
	defer h.mux.Unlock()

	// FIXME: return map directly?
	return h.connections
}

// RemoveConnection removes a connection
func (h *Host) RemoveConnection(id p2p.ID) error {
	h.mux.Lock()
	defer h.mux.Unlock()

	publicKey := string(id.PublicKey)
	conn, ok := h.connections[publicKey]

	if !ok {
		return errConnectionNotFound
	}

	conn.Close()
	delete(h.connections, publicKey)

	// Notify subscribers that connection is removed from host
	h.notifyAll(func(n p2p.Notifiee) {
		n.Disconnected(conn)
	})

	return nil
}

// RemoveAllConnection removes all the connection when shutdown
func (h *Host) RemoveAllConnection() {
	h.mux.Lock()
	defer h.mux.Unlock()

	for id, conn := range h.connections {
		// Close connection to reclaim go routines for each connection
		conn.Close()
		delete(h.connections, id)
	}
}

// ID returns the local ID
func (h *Host) ID() p2p.ID {
	return h.id
}

// Connect connects to remote peer
func (h *Host) Connect(address string) (p2p.Conn, error) {
	addr, err := dht.ToNetAddr(address)
	if err != nil {
		return nil, err
	}

	// Dial remote peer
	conn, err := net.Dial(addr.Network(), addr.String())
	if err != nil {
		return nil, err
	}

	c := NewConnection(conn, h.n, h)
	h.OnConnectionCreated(c, p2p.Outbound)
	return c, nil
}

// SetMessageHandler sets handler for handling messages
func (h *Host) SetMessageHandler(protocol string, handler p2p.MessageHandler) error {
	h.mux.Lock()
	defer h.mux.Unlock()

	_, found := h.handlers[protocol]
	if found {
		return errDuplicateStream
	}

	h.handlers[protocol] = handler
	return nil
}

// MessageHandler gets message handler
func (h *Host) MessageHandler(protocol string) (p2p.MessageHandler, error) {
	h.mux.Lock()
	defer h.mux.Unlock()

	handler, ok := h.handlers[protocol]

	if !ok {
		return nil, errStreamNotFound
	}

	return handler, nil
}
