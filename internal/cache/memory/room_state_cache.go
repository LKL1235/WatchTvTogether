package memory

import (
	"context"
	"sync"
	"time"

	"watchtogether/internal/model"
	"watchtogether/internal/store"
)

type RoomStateCache struct {
	mu     sync.RWMutex
	states map[string]*model.RoomState
}

func NewRoomStateCache() *RoomStateCache {
	return &RoomStateCache{states: make(map[string]*model.RoomState)}
}

func (c *RoomStateCache) SetRoomState(ctx context.Context, roomID string, state *model.RoomState) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	copyState := *state
	if copyState.UpdatedAt.IsZero() {
		copyState.UpdatedAt = time.Now().UTC()
	}
	copyState.RoomID = roomID
	c.states[roomID] = &copyState
	return nil
}

func (c *RoomStateCache) GetRoomState(ctx context.Context, roomID string) (*model.RoomState, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	state, ok := c.states[roomID]
	if !ok {
		return nil, store.ErrNotFound
	}
	copyState := *state
	return &copyState, nil
}

func (c *RoomStateCache) DeleteRoomState(ctx context.Context, roomID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.states, roomID)
	return nil
}
