package realtime

import (
	"context"

	ablysdk "github.com/ably/ably-go/ably"
)

type TokenDetails = ablysdk.TokenDetails

const (
	MessageRoomSnapshot = "room.snapshot"
	MessageRoomSync     = "room.sync"
	MessageRoomEvent    = "room.event"
	MessageRoomError    = "room.error"
	MessageRoomControl  = "room.control"
)

type Publisher interface {
	ChannelName(roomID string) string
	PublishRoomMessage(ctx context.Context, roomID string, name string, data any) error
}

type TokenIssuer interface {
	RequestRoomToken(ctx context.Context, roomID string, clientID string) (*ablysdk.TokenDetails, error)
}

type Service interface {
	Publisher
	TokenIssuer
}
