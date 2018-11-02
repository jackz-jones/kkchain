package impl

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"sync"

	"github.com/gogo/protobuf/proto"
	"github.com/gogo/protobuf/types"
	"github.com/invin/kkchain/p2p"
	"github.com/invin/kkchain/p2p/chain"
	"github.com/invin/kkchain/p2p/protobuf"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
)

var (
	errEmptyMessage = errors.New("empty message")
	errVerifySign   = errors.New("invalid signature")
)

// Connection represents a connection to remote peer
type Connection struct {
	conn       net.Conn
	n          p2p.Network
	h          p2p.Host
	mux        sync.Mutex
	remotePeer p2p.ID
	verified   bool
}

// NewConnection creates a new connection object
func NewConnection(conn net.Conn, n p2p.Network, h p2p.Host) *Connection {
	return &Connection{
		conn:     conn,
		n:        n,
		h:        h,
		verified: false,
	}
}

// Verified returns if the connection is verified or not
func (c *Connection) Verified() bool {
	return c.verified
}

// SetVerified sets flag after handshaking is ok
func (c *Connection) SetVerified() {
	c.verified = true
}

// RemoteAddr returns the address of remote peer
func (c *Connection) RemoteAddr() net.Addr {
	return c.conn.RemoteAddr()
}

// LocalAddr returns local address
func (c *Connection) LocalAddr() net.Addr {
	return c.conn.LocalAddr()
}

// Close closes current connection
func (c *Connection) Close() error {
	return c.conn.Close()
}

// RemotePeer gets remote peer's ID
func (c *Connection) RemotePeer() p2p.ID {
	return c.remotePeer
}

// WriteMessage writes pb message to the connection
func (c *Connection) WriteMessage(message proto.Message, protocol string) error {
	// Sign the message
	signed, err := c.prepareMessage(message)
	if err != nil {
		return err
	}

	// set protocol
	signed.Protocol = protocol
	return c.write(c.conn, signed, &c.mux)
}

// prepareMessage prepares the message before sending
func (c *Connection) prepareMessage(message proto.Message) (*protobuf.Message, error) {
	if message == nil {
		return nil, errEmptyMessage
	}

	raw, err := types.MarshalAny(message)
	if err != nil {
		return nil, err
	}

	id := protobuf.ID(c.h.ID())

	signature, err := c.n.Sign(
		SerializeMessage(&id, raw.Value),
	)

	// var signature []byte
	if err != nil {
		return nil, err
	}

	msg := &protobuf.Message{
		Message:   raw,
		Sender:    &id,
		Signature: signature,
	}

	return msg, nil
}

// write marshals, signs and sends a message over a stream.
func (c *Connection) write(w io.Writer, message *protobuf.Message, writerMutex *sync.Mutex) error {
	bytes, err := proto.Marshal(message)
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	// Serialize size.
	buffer := make([]byte, 4)
	binary.BigEndian.PutUint32(buffer, uint32(len(bytes)))

	buffer = append(buffer, bytes...)
	totalSize := len(buffer)

	// Write until all bytes have been written.
	bytesWritten, totalBytesWritten := 0, 0

	writerMutex.Lock()
	defer writerMutex.Unlock()

	bw, isBuffered := w.(*bufio.Writer)
	if isBuffered && (bw.Buffered() > 0) && (bw.Available() < totalSize) {
		if err := bw.Flush(); err != nil {
			return err
		}
	}

	for totalBytesWritten < len(buffer) && err == nil {
		bytesWritten, err = w.Write(buffer[totalBytesWritten:])
		if err != nil {
			log.Errorf("stream: failed to write entire buffer, err: %v", err)
		}
		totalBytesWritten += bytesWritten
	}

	if err != nil {
		return errors.Wrap(err, "stream: failed to write to socket")
	}

	return nil
}

// ReadMessage reads a pb message from connection
func (c *Connection) ReadMessage() (proto.Message, string, error) {
	var err error

	// Read until all header bytes have been read.
	buffer := make([]byte, 4)

	bytesRead, totalBytesRead := 0, 0

	for totalBytesRead < 4 && err == nil {
		bytesRead, err = c.conn.Read(buffer[totalBytesRead:])
		totalBytesRead += bytesRead
	}

	if err != nil {
		return nil, "", err
	}

	// Decode message size.
	size := binary.BigEndian.Uint32(buffer)

	if size == 0 {
		return nil, "", errEmptyMessage
	}

	// Message size at most is limited to 4MB. If a big message need be sent,
	// consider partitioning to message into chunks of 4MB.
	if size > 4e+6 {
		return nil, "", errors.Errorf("message has length of %d which is either broken or too large", size)
	}

	// Read until all message bytes have been read.
	buffer = make([]byte, size)

	bytesRead, totalBytesRead = 0, 0

	for totalBytesRead < int(size) && err == nil {
		bytesRead, err = c.conn.Read(buffer[totalBytesRead:])
		totalBytesRead += bytesRead
	}

	// Deserialize message.
	msg := new(protobuf.Message)

	err = proto.Unmarshal(buffer, msg)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to unmarshal message")
	}

	// Check if any of the message headers are invalid or null.
	if msg.Message == nil || msg.Sender == nil || msg.Sender.PublicKey == nil || len(msg.Sender.Address) == 0 || msg.Signature == nil {
		return nil, "", errors.New("received an invalid message (either no message, no sender, or no signature) from a peer")
	}

	// Verify signature of the message
	if !c.n.Verify(msg.Sender.PublicKey, SerializeMessage(msg.Sender, msg.Message.Value), msg.Signature) {
		return nil, "", errVerifySign
	}

	// Set remote peer if not set
	if c.remotePeer.PublicKey == nil {
		c.remotePeer = p2p.ID(*msg.Sender)
	}

	return c.parseMessage(msg)
}

//ParseMessage parses message
func (c *Connection) parseMessage(msg *protobuf.Message) (proto.Message, string, error) {
	// unmarshal message
	var ptr types.DynamicAny
	if err := types.UnmarshalAny(msg.Message, &ptr); err != nil {
		log.Errorf("failed to unmarshal protobuf msg,error: %v", err)
		return nil, "", err
	}
	return ptr.Message, msg.Protocol, nil
}

// only use for chain request
func (c *Connection) SendChainMsg(msgType int32, msgData interface{}) error {
	switch msgData.(type) {
	case []byte:
		newMsg := &chain.DataMsg{
			Data: msgData.([]byte),
		}
		msg := chain.NewMessage(chain.Message_Type(msgType), newMsg)
		return c.WriteMessage(msg, "/kkchain/p2p/chain/1.0.0")
	case *chain.GetBlockHeadersMsg:
		msg := chain.NewMessage(chain.Message_Type(msgType), msgData.(*chain.GetBlockHeadersMsg))
		return c.WriteMessage(msg, "/kkchain/p2p/chain/1.0.0")
	case *chain.GetBlocksMsg:
		msg := chain.NewMessage(chain.Message_Type(msgType), msgData.(*chain.GetBlocksMsg))
		return c.WriteMessage(msg, "/kkchain/p2p/chain/1.0.0")
	}
	return nil
}
