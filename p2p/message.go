package p2p

import (
	"github.com/gogo/protobuf/proto"
)

// MessageHandler is the type of function used to handle messages
type MessageHandler func(Conn, proto.Message)

// TODO: define message ID

