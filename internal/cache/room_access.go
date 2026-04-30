package cache

import "context"

// RoomAccessCache stores per-room user IDs that may enter a private room without re-entering the password.
// Entries are revoked when the room is closed or deleted from the application layer.
type RoomAccessCache interface {
	GrantRoomAccess(ctx context.Context, roomID, userID string) error
	HasRoomAccess(ctx context.Context, roomID, userID string) (bool, error)
	DeleteRoomAccess(ctx context.Context, roomID string) error
}
