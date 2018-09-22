package handshake

import (
	"context"
	"errors"
	"time"

	"encoding/hex"

	"github.com/gogo/protobuf/proto"
	"github.com/invin/kkchain/p2p"
	log "github.com/sirupsen/logrus"
)

const (
	protocolHandshake = "/kkchain/p2p/handshake/1.0.0"
	handshakeTimeout  = 5 * time.Second
)

var (
	errProtocol    = errors.New("wrong protocol")
	errHandshake   = errors.New("failed to handshake")
	errReadTimeout = errors.New("read message timeout")
)

// Handshake implements protocol for handshaking
type Handshake struct {
	// self
	host p2p.Host
}

// New creates a new Handshake object with the given peer as as the 'local' host
func New(host p2p.Host) *Handshake {
	hs := &Handshake{
		host: host,
	}

	return hs
}

// Handshake sends message for handshake and returns response
// FIXME:
func (hs *Handshake) Handshake(c p2p.Conn, dir p2p.ConnDir) error {
	errc := make(chan error, 2)
	count := 1
	// send handshake message if we're outbound connection
	if dir == p2p.Outbound {
		count = 2
		go func() {
			msg := NewMessage(Message_HELLO)
			BuildHandshake(msg)
			errc <- c.WriteMessage(msg, protocolHandshake)
		}()
	}

	go func() {
		// FIXME:
		if dir == p2p.Outbound {
			time.Sleep(2 * time.Second)
		}

		msg, protocol, err := c.ReadMessage()

		if protocol != protocolHandshake {
			log.Errorf("Unexpected message,protocol: %s", protocol)

			err = errProtocol
		} else {
			hs.handleMessage(c, msg)
			if !c.Verified() {
				err = errHandshake
			}
		}
		errc <- err
	}()

	timeout := time.NewTimer(handshakeTimeout)
	defer timeout.Stop()

	for i := 0; i < count; i++ {
		select {
		case err := <-errc:
			if err != nil {
				return err
			}

		case <-timeout.C:
			return errReadTimeout
		}
	}

	return nil
}

// handleMessage handles messages within the stream
func (hs *Handshake) handleMessage(c p2p.Conn, msg proto.Message) {
	// check message type
	switch message := msg.(type) {
	case *Message:
		hs.doHandleMessage(c, message)
	default:
		c.Close()
		log.Errorf("unexpected message: %v", msg)
	}
}

// doHandleMessage handles messsage
func (hs *Handshake) doHandleMessage(c p2p.Conn, msg *Message) {
	// get handler
	handler := hs.handlerForMsgType(msg.GetType())
	if handler == nil {
		c.Close()
		log.Errorf("unknown message type: %v", msg.GetType())
		return
	}

	// dispatch handler
	// TODO: get context and peer id
	ctx := context.Background()
	pid := c.RemotePeer()

	// successfully recv conn, add it
	// hs.host.AddConnection(pid, c)

	rpmes, err := handler(ctx, pid, msg, c)

	// if nil response, return it before serializing
	if rpmes == nil {
		return
	}

	// hello error resp will return err,so reset conn and remove it
	if err != nil {
		// FIXME: it it necessary？
		// hs.host.RemoveConnection(pid)
		c.Close()
	}

	// send out response msg
	if err = c.WriteMessage(rpmes, protocolHandshake); err != nil {
		log.Errorf("send response error: %s", err)
		return
	}
}

// Connected is called when new connection is established
func (hs *Handshake) Connected(c p2p.Conn) {
	log.Infof("A new connection is created,remote ID: %s", hex.EncodeToString(c.RemotePeer().PublicKey))

}

// Disconnected is called when the connection is closed
func (hs *Handshake) Disconnected(c p2p.Conn) {
	log.Infof("A peer is disconnected,remote ID: %s", hex.EncodeToString(c.RemotePeer().PublicKey))
}
