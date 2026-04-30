package realtime

import (
	"context"
	"time"
)

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
	IssueRoomJWT(ctx context.Context, roomID string, clientID string) (token string, expiresAt time.Time, err error)
}

type Service interface {
	Publisher
	TokenIssuer
}
