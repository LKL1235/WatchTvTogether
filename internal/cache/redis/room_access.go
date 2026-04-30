package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const roomAccessKeyTTL = 30 * 24 * time.Hour

// RoomAccess implements cache.RoomAccessCache using a Redis set per room.
type RoomAccess struct {
	client goredis.UniversalClient
}

func NewRoomAccess(client goredis.UniversalClient) *RoomAccess {
	return &RoomAccess{client: client}
}

func roomAccessKey(roomID string) string {
	return "room:access:" + roomID
}

func (a *RoomAccess) GrantRoomAccess(ctx context.Context, roomID, userID string) error {
	if roomID == "" || userID == "" {
		return nil
	}
	key := roomAccessKey(roomID)
	pipe := a.client.TxPipeline()
	pipe.SAdd(ctx, key, userID)
	pipe.Expire(ctx, key, roomAccessKeyTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (a *RoomAccess) HasRoomAccess(ctx context.Context, roomID, userID string) (bool, error) {
	if roomID == "" || userID == "" {
		return false, nil
	}
	ok, err := a.client.SIsMember(ctx, roomAccessKey(roomID), userID).Result()
	return ok, err
}

func (a *RoomAccess) DeleteRoomAccess(ctx context.Context, roomID string) error {
	if roomID == "" {
		return nil
	}
	return a.client.Del(ctx, roomAccessKey(roomID)).Err()
}
