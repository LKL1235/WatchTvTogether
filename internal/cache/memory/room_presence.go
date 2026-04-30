package memory

import (
	"context"
	"sync"
	"time"

	"watchtogether/internal/cache"
	"watchtogether/internal/store"
)

type RoomPresence struct {
	mu sync.Mutex
	// roomID -> userID -> username
	members map[string]map[string]string
	// userID -> roomID
	userRoom map[string]string
	// roomIDs that currently have at least one member
	activeRooms map[string]struct{}

	pendingEmpty map[string]struct{}

	lastCleanupAt time.Time
	cleanupLock   bool
}

func NewRoomPresence() *RoomPresence {
	return &RoomPresence{
		members:      make(map[string]map[string]string),
		userRoom:     make(map[string]string),
		activeRooms:  make(map[string]struct{}),
		pendingEmpty: make(map[string]struct{}),
	}
}

func (p *RoomPresence) JoinRoom(ctx context.Context, roomID, userID, username string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()

	if prev, ok := p.userRoom[userID]; ok && prev != roomID {
		return "", store.ErrConflict
	}

	m, ok := p.members[roomID]
	if !ok {
		m = make(map[string]string)
		p.members[roomID] = m
	}
	m[userID] = username
	p.userRoom[userID] = roomID
	p.activeRooms[roomID] = struct{}{}
	delete(p.pendingEmpty, roomID)
	return "", nil
}

func (p *RoomPresence) LeaveRoom(ctx context.Context, roomID, userID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.removeMemberLocked(roomID, userID)
	return nil
}

func (p *RoomPresence) RemoveMember(ctx context.Context, roomID, userID string) error {
	return p.LeaveRoom(ctx, roomID, userID)
}

func (p *RoomPresence) removeMemberLocked(roomID, userID string) {
	m, ok := p.members[roomID]
	if !ok {
		return
	}
	delete(m, userID)
	if len(m) == 0 {
		delete(p.members, roomID)
		delete(p.activeRooms, roomID)
		p.pendingEmpty[roomID] = struct{}{}
	}
	if p.userRoom[userID] == roomID {
		delete(p.userRoom, userID)
	}
}

func (p *RoomPresence) ListMembers(ctx context.Context, roomID string) ([]cache.RoomMember, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	m, ok := p.members[roomID]
	if !ok {
		return nil, nil
	}
	out := make([]cache.RoomMember, 0, len(m))
	for uid, name := range m {
		out = append(out, cache.RoomMember{UserID: uid, Username: name})
	}
	return out, nil
}

func (p *RoomPresence) MemberCount(ctx context.Context, roomID string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if m, ok := p.members[roomID]; ok {
		return len(m), nil
	}
	return 0, nil
}

func (p *RoomPresence) GetUserRoom(ctx context.Context, userID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.userRoom[userID], nil
}

func (p *RoomPresence) DeleteRoomPresence(ctx context.Context, roomID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	m, ok := p.members[roomID]
	if !ok {
		return nil
	}
	for uid := range m {
		if p.userRoom[uid] == roomID {
			delete(p.userRoom, uid)
		}
	}
	delete(p.members, roomID)
	delete(p.activeRooms, roomID)
	delete(p.pendingEmpty, roomID)
	return nil
}

func (p *RoomPresence) PendingEmptyRooms(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.pendingEmpty))
	for id := range p.pendingEmpty {
		out = append(out, id)
	}
	return out, nil
}

func (p *RoomPresence) ClearPendingEmpty(ctx context.Context, roomID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.pendingEmpty, roomID)
	return nil
}

func (p *RoomPresence) GetLastRoomCleanupAt(ctx context.Context) (time.Time, error) {
	if err := ctx.Err(); err != nil {
		return time.Time{}, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lastCleanupAt, nil
}

func (p *RoomPresence) SetLastRoomCleanupAt(ctx context.Context, t time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastCleanupAt = t.UTC()
	return nil
}

func (p *RoomPresence) TryAcquireCleanupLock(ctx context.Context, ttl time.Duration) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cleanupLock {
		return false, nil
	}
	p.cleanupLock = true
	go func() {
		time.Sleep(ttl)
		p.mu.Lock()
		p.cleanupLock = false
		p.mu.Unlock()
	}()
	return true, nil
}

func (p *RoomPresence) ReleaseCleanupLock(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.cleanupLock = false
	return nil
}

func (p *RoomPresence) ListRoomsWithMembers(ctx context.Context) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]string, 0, len(p.activeRooms))
	for id := range p.activeRooms {
		out = append(out, id)
	}
	return out, nil
}
