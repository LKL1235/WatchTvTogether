package memory

import (
	"context"
	"sync"
)

// RoomAccess is an in-memory RoomAccessCache for tests and cache_backend=memory.
type RoomAccess struct {
	mu    sync.RWMutex
	rooms map[string]map[string]struct{} // roomID -> set of userID
}

func NewRoomAccess() *RoomAccess {
	return &RoomAccess{rooms: make(map[string]map[string]struct{})}
}

func (a *RoomAccess) GrantRoomAccess(ctx context.Context, roomID, userID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if roomID == "" || userID == "" {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	m, ok := a.rooms[roomID]
	if !ok {
		m = make(map[string]struct{})
		a.rooms[roomID] = m
	}
	m[userID] = struct{}{}
	return nil
}

func (a *RoomAccess) HasRoomAccess(ctx context.Context, roomID, userID string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	if roomID == "" || userID == "" {
		return false, nil
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	m, ok := a.rooms[roomID]
	if !ok {
		return false, nil
	}
	_, ok = m[userID]
	return ok, nil
}

func (a *RoomAccess) DeleteRoomAccess(ctx context.Context, roomID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.rooms, roomID)
	return nil
}
