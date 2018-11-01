package handshake

import (
	"context"

	"github.com/invin/kkchain/p2p"
	"github.com/pkg/errors"
)

// handshakeHandler specifies the signature of functions that handle DHT messages.
type handshakeHandler func(context.Context, p2p.ID, *Message, p2p.Conn) (*Message, error)

func (hs *Handshake) handlerForMsgType(t Message_Type) handshakeHandler {
	switch t {
	case Message_HELLO:
		return hs.handleHello
	case Message_HELLO_OK:
		return hs.handleHelloOK
	case Message_HELLO_ERROR:
		return hs.handleHelloError
	default:
		return nil
	}
}

func (hs *Handshake) handleHello(ctx context.Context, p p2p.ID, pmes *Message, c p2p.Conn) (_ *Message, err error) {
	// Check result and return corresponding code
	ok := true
	var resp *Message

	if ok {
		c.SetVerified()
		resp = NewMessage(Message_HELLO_OK)
		BuildHandshake(resp)
		return resp, nil
	}
	resp = NewMessage(Message_HELLO_ERROR)
	return resp, errors.New("hello error")
}

func (hs *Handshake) handleHelloOK(ctx context.Context, p p2p.ID, pmes *Message, c p2p.Conn) (_ *Message, err error) {
	c.SetVerified()
	return nil, nil
}

func (hs *Handshake) handleHelloError(ctx context.Context, p p2p.ID, pmes *Message, c p2p.Conn) (_ *Message, err error) {

	// TODO: handle error

	return nil, nil
}
