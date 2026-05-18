package cache

import (
	"context"
	"strings"
	"time"

	"watchtogether/internal/model"
)

type SessionCache interface {
	SetRefreshToken(ctx context.Context, userID, token string, ttl time.Duration) error
	GetRefreshToken(ctx context.Context, userID string) (string, error)
	DeleteRefreshToken(ctx context.Context, userID string) error
	BlacklistToken(ctx context.Context, jti string, ttl time.Duration) error
	IsBlacklisted(ctx context.Context, jti string) (bool, error)
}

type RoomStateCache interface {
	SetRoomState(ctx context.Context, roomID string, state *model.RoomState) error
	GetRoomState(ctx context.Context, roomID string) (*model.RoomState, error)
	DeleteRoomState(ctx context.Context, roomID string) error
}

type PubSub interface {
	Publish(ctx context.Context, channel string, payload []byte) error
	Subscribe(ctx context.Context, channel string) (<-chan []byte, func(), error)
}

// RoomChat persists room chat messages in Redis Streams (see docs/room_chat_realtime_design_zh.md).
// Production wiring uses Redis; when RoomChat is nil (e.g. some tests), chat HTTP APIs return 503.
type RoomChat interface {
	PendingCount(ctx context.Context, roomID string) (int64, error)
	NextSeq(ctx context.Context, roomID string) (int64, error)
	AppendMessage(ctx context.Context, roomID string, seq int64, payloadJSON string, streamMaxLen int64) (streamID string, err error)
	ListMessagesRev(ctx context.Context, roomID string, beforeID string, limit int) ([]RoomChatMessage, error)
	GetPayload(ctx context.Context, roomID, streamID string) (string, error)
	EnqueuePending(ctx context.Context, roomID, streamID string) error
	RemovePending(ctx context.Context, roomID, streamID string) error
	PendingStreamIDs(ctx context.Context, roomID string, max int64) ([]string, error)
	DeleteRoom(ctx context.Context, roomID string) error
	AllowChatSend(ctx context.Context, roomID, userID string, maxPerSecond int) (bool, error)
	ScanPendingRooms(ctx context.Context, cursor uint64, count int64) ([]string, uint64, error)
}

// RoomChatMessage is one entry returned from the chat stream (payload is application JSON).
type RoomChatMessage struct {
	StreamID string
	Seq      int64
	Payload  string
}

// ParseRoomChatPendingKey extracts room id from the Redis pending ZSET key used for Ably retries.
func ParseRoomChatPendingKey(key string) (roomID string, ok bool) {
	const prefix = "room:chat:ably_pending:"
	if !strings.HasPrefix(key, prefix) {
		return "", false
	}
	return strings.TrimPrefix(key, prefix), true
}
