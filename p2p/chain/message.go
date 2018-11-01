package chain

// NewMessage creates a new message object
func NewMessage(typ Message_Type, data interface{}) *Message {
	var m *Message
	switch data.(type) {
	case *ChainStatusMsg:
		m = &Message{
			Type:           typ,
			ChainStatusMsg: data.(*ChainStatusMsg),
		}
		break
	case *DataMsg:
		m = &Message{
			Type:    typ,
			DataMsg: data.(*DataMsg),
		}
		break
	case *GetBlockHeadersMsg:
		m = &Message{
			Type:               typ,
			GetBlockHeadersMsg: data.(*GetBlockHeadersMsg),
		}
		break
	case *GetBlocksMsg:
		m = &Message{
			Type:         typ,
			GetBlocksMsg: data.(*GetBlocksMsg),
		}
		break
	default:
		return nil
	}
	return m
}
