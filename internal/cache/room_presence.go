package cache

import (
	"context"
	"time"
)

// RoomMember is a user currently present in a room (Redis-backed presence).
type RoomMember struct {
	UserID   string
	Username string
}

// RoomPresence tracks room membership and the reverse index user -> room.
// A user may appear in at most one room at a time (enforced by JoinRoom).
type RoomPresence interface {
	JoinRoom(ctx context.Context, roomID, userID, username string) (leftRoomID string, err error)
	LeaveRoom(ctx context.Context, roomID, userID string) error
	RemoveMember(ctx context.Context, roomID, userID string) error
	ListMembers(ctx context.Context, roomID string) ([]RoomMember, error)
	MemberCount(ctx context.Context, roomID string) (int, error)
	GetUserRoom(ctx context.Context, userID string) (roomID string, err error)

	DeleteRoomPresence(ctx context.Context, roomID string) error

	// PendingEmptyRooms returns room ids that became member-empty and await DB/redis teardown.
	PendingEmptyRooms(ctx context.Context) ([]string, error)
	ClearPendingEmpty(ctx context.Context, roomID string) error

	GetLastRoomCleanupAt(ctx context.Context) (time.Time, error)
	SetLastRoomCleanupAt(ctx context.Context, t time.Time) error
	TryAcquireCleanupLock(ctx context.Context, ttl time.Duration) (bool, error)
	ReleaseCleanupLock(ctx context.Context) error

	ListRoomsWithMembers(ctx context.Context) ([]string, error)
}
