package redis

import (
	"context"
	"strconv"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"watchtogether/internal/cache"
)

const (
	roomMemberTTL     = 7 * 24 * time.Hour
	cleanupLockTTL    = 2 * time.Minute
	globalKeyTTL      = 30 * 24 * time.Hour
)

// RoomPresence implements cache.RoomPresence for Redis.
type RoomPresence struct {
	client goredis.UniversalClient
	ttl    time.Duration
}

func NewRoomPresence(client goredis.UniversalClient) *RoomPresence {
	return &RoomPresence{client: client, ttl: roomMemberTTL}
}

func (p *RoomPresence) membersKey(roomID string) string {
	return "room:members:" + roomID
}

func (p *RoomPresence) userRoomKey(userID string) string {
	return "user:room:" + userID
}

const keyActiveRooms = "room:active"
const keyPendingEmpty = "room:pending_empty"
const keyLastCleanup = "global:last_room_cleanup_at"
const keyCleanupLock = "global:room_cleanup_lock"

// joinScriptMigrate atomically leaves the previous room (if any and different), then joins newRoom.
// Returns the left room id as a string, or empty string when the user was not in another room.
const joinScriptMigrate = `
local userRoomKey = KEYS[1]
local newMembersKey = KEYS[2]
local activeKey = KEYS[3]
local pendingKey = KEYS[4]
local newRoom = ARGV[1]
local userId = ARGV[2]
local username = ARGV[3]
local memberTTL = tonumber(ARGV[4])
local globalTTL = tonumber(ARGV[5])

local leftRoom = ''
local oldRoom = redis.call('GET', userRoomKey)
if oldRoom and oldRoom ~= newRoom then
  leftRoom = oldRoom
  local oldMembersKey = 'room:members:' .. oldRoom
  redis.call('HDEL', oldMembersKey, userId)
  local n = redis.call('HLEN', oldMembersKey)
  if n == 0 then
    redis.call('SREM', activeKey, oldRoom)
    redis.call('DEL', oldMembersKey)
    redis.call('SADD', pendingKey, oldRoom)
    redis.call('EXPIRE', pendingKey, globalTTL)
  end
end

redis.call('HSET', newMembersKey, userId, username)
redis.call('EXPIRE', newMembersKey, memberTTL)
redis.call('SET', userRoomKey, newRoom, 'EX', memberTTL)
redis.call('SADD', activeKey, newRoom)
redis.call('EXPIRE', activeKey, memberTTL)
redis.call('SREM', pendingKey, newRoom)
return leftRoom
`

func (p *RoomPresence) JoinRoom(ctx context.Context, roomID, userID, username string) (string, error) {
	res, err := p.client.Eval(ctx, joinScriptMigrate, []string{
		p.userRoomKey(userID),
		p.membersKey(roomID),
		keyActiveRooms,
		keyPendingEmpty,
	}, roomID, userID, username, int(p.ttl.Seconds()), int(globalKeyTTL.Seconds())).Result()
	if err != nil {
		return "", err
	}
	s, _ := res.(string)
	return s, nil
}

func (p *RoomPresence) LeaveRoom(ctx context.Context, roomID, userID string) error {
	return p.removeMember(ctx, roomID, userID)
}

func (p *RoomPresence) RemoveMember(ctx context.Context, roomID, userID string) error {
	return p.removeMember(ctx, roomID, userID)
}

func (p *RoomPresence) removeMember(ctx context.Context, roomID, userID string) error {
	pipe := p.client.TxPipeline()
	pipe.HDel(ctx, p.membersKey(roomID), userID)
	pipe.Del(ctx, p.userRoomKey(userID))
	_, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	n, err := p.client.HLen(ctx, p.membersKey(roomID)).Result()
	if err != nil {
		return err
	}
	if n == 0 {
		_ = p.client.SRem(ctx, keyActiveRooms, roomID).Err()
		_ = p.client.Del(ctx, p.membersKey(roomID)).Err()
		_ = p.client.SAdd(ctx, keyPendingEmpty, roomID).Err()
		_ = p.client.Expire(ctx, keyPendingEmpty, globalKeyTTL).Err()
	}
	return nil
}

func (p *RoomPresence) PendingEmptyRooms(ctx context.Context) ([]string, error) {
	return p.client.SMembers(ctx, keyPendingEmpty).Result()
}

func (p *RoomPresence) ClearPendingEmpty(ctx context.Context, roomID string) error {
	return p.client.SRem(ctx, keyPendingEmpty, roomID).Err()
}

func (p *RoomPresence) ListMembers(ctx context.Context, roomID string) ([]cache.RoomMember, error) {
	m, err := p.client.HGetAll(ctx, p.membersKey(roomID)).Result()
	if err != nil {
		return nil, err
	}
	out := make([]cache.RoomMember, 0, len(m))
	for uid, name := range m {
		out = append(out, cache.RoomMember{UserID: uid, Username: name})
	}
	return out, nil
}

func (p *RoomPresence) MemberCount(ctx context.Context, roomID string) (int, error) {
	n, err := p.client.HLen(ctx, p.membersKey(roomID)).Result()
	return int(n), err
}

func (p *RoomPresence) GetUserRoom(ctx context.Context, userID string) (string, error) {
	s, err := p.client.Get(ctx, p.userRoomKey(userID)).Result()
	if err == goredis.Nil {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return s, nil
}

func (p *RoomPresence) DeleteRoomPresence(ctx context.Context, roomID string) error {
	members, err := p.client.HGetAll(ctx, p.membersKey(roomID)).Result()
	if err != nil {
		return err
	}
	pipe := p.client.TxPipeline()
	for uid := range members {
		pipe.Del(ctx, p.userRoomKey(uid))
	}
	pipe.Del(ctx, p.membersKey(roomID))
	pipe.SRem(ctx, keyActiveRooms, roomID)
	pipe.SRem(ctx, keyPendingEmpty, roomID)
	_, err = pipe.Exec(ctx)
	return err
}

func (p *RoomPresence) GetLastRoomCleanupAt(ctx context.Context) (time.Time, error) {
	s, err := p.client.Get(ctx, keyLastCleanup).Result()
	if err == goredis.Nil {
		return time.Time{}, nil
	}
	if err != nil {
		return time.Time{}, err
	}
	ms, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms).UTC(), nil
}

func (p *RoomPresence) SetLastRoomCleanupAt(ctx context.Context, t time.Time) error {
	ms := strconv.FormatInt(t.UTC().UnixMilli(), 10)
	return p.client.Set(ctx, keyLastCleanup, ms, globalKeyTTL).Err()
}

func (p *RoomPresence) TryAcquireCleanupLock(ctx context.Context, ttl time.Duration) (bool, error) {
	ok, err := p.client.SetNX(ctx, keyCleanupLock, "1", ttl).Result()
	return ok, err
}

func (p *RoomPresence) ReleaseCleanupLock(ctx context.Context) error {
	return p.client.Del(ctx, keyCleanupLock).Err()
}

func (p *RoomPresence) ListRoomsWithMembers(ctx context.Context) ([]string, error) {
	return p.client.SMembers(ctx, keyActiveRooms).Result()
}
