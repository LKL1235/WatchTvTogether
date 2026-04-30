package redis

import (
	"context"
	"encoding/json"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"watchtogether/internal/model"
	"watchtogether/internal/store"
)

const roomStateTTL = 7 * 24 * time.Hour

type RoomStateCache struct {
	client goredis.UniversalClient
}

func NewRoomStateCache(client goredis.UniversalClient) *RoomStateCache {
	return &RoomStateCache{client: client}
}

func (c *RoomStateCache) SetRoomState(ctx context.Context, roomID string, state *model.RoomState) error {
	copyState := *state
	if copyState.UpdatedAt.IsZero() {
		copyState.UpdatedAt = time.Now().UTC()
	}
	copyState.RoomID = roomID
	payload, err := json.Marshal(copyState)
	if err != nil {
		return err
	}
	return c.client.Set(ctx, roomStateKey(roomID), payload, roomStateTTL).Err()
}

func (c *RoomStateCache) GetRoomState(ctx context.Context, roomID string) (*model.RoomState, error) {
	payload, err := c.client.Get(ctx, roomStateKey(roomID)).Bytes()
	if err != nil {
		if err == goredis.Nil {
			return nil, store.ErrNotFound
		}
		return nil, err
	}
	var state model.RoomState
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func (c *RoomStateCache) DeleteRoomState(ctx context.Context, roomID string) error {
	return c.client.Del(ctx, roomStateKey(roomID)).Err()
}

func roomStateKey(roomID string) string {
	return "room:state:" + roomID
}
