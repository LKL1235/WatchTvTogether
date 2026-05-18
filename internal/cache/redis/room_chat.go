package redis

import (
	"context"
	"strconv"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"watchtogether/internal/cache"
)

const roomChatStreamPrefix = "room:chat:stream:"
const roomChatSeqPrefix = "room:chat:seq:"
const roomChatPendingPrefix = "room:chat:ably_pending:"
const roomChatRatePrefix = "room:chat:ratelimit:"

// RoomChat implements cache.RoomChat using Redis Streams and a ZSET outbox for Ably retries.
type RoomChat struct {
	client goredis.UniversalClient
}

func NewRoomChat(client goredis.UniversalClient) *RoomChat {
	return &RoomChat{client: client}
}

func chatStreamKey(roomID string) string { return roomChatStreamPrefix + roomID }
func chatSeqKey(roomID string) string     { return roomChatSeqPrefix + roomID }
func chatPendingKey(roomID string) string {
	return roomChatPendingPrefix + roomID
}
func chatRateKey(roomID, userID string) string {
	return roomChatRatePrefix + roomID + ":" + userID
}

func (c *RoomChat) PendingCount(ctx context.Context, roomID string) (int64, error) {
	n, err := c.client.ZCard(ctx, chatPendingKey(roomID)).Result()
	return n, err
}

func (c *RoomChat) NextSeq(ctx context.Context, roomID string) (int64, error) {
	return c.client.Incr(ctx, chatSeqKey(roomID)).Result()
}

func (c *RoomChat) AppendMessage(ctx context.Context, roomID string, seq int64, payloadJSON string, streamMaxLen int64) (string, error) {
	args := &goredis.XAddArgs{
		Stream: chatStreamKey(roomID),
		Values: map[string]any{
			"seq":     strconv.FormatInt(seq, 10),
			"payload": payloadJSON,
		},
	}
	if streamMaxLen > 0 {
		args.MaxLen = streamMaxLen
		args.Approx = true
	}
	return c.client.XAdd(ctx, args).Result()
}

func (c *RoomChat) ListMessagesRev(ctx context.Context, roomID string, beforeID string, limit int) ([]cache.RoomChatMessage, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	key := chatStreamKey(roomID)
	var raw []goredis.XMessage
	var err error
	if strings.TrimSpace(beforeID) == "" {
		raw, err = c.client.XRevRangeN(ctx, key, "+", "-", int64(limit)).Result()
	} else {
		// Entries strictly older than beforeID, newest first.
		raw, err = c.client.XRevRangeN(ctx, key, "("+beforeID, "-", int64(limit)).Result()
	}
	if err != nil {
		return nil, err
	}
	out := make([]cache.RoomChatMessage, 0, len(raw))
	for _, m := range raw {
		seqStr := stringifyRedisMapValue(m.Values["seq"])
		seq, _ := strconv.ParseInt(seqStr, 10, 64)
		payload, _ := m.Values["payload"].(string)
		out = append(out, cache.RoomChatMessage{StreamID: m.ID, Seq: seq, Payload: payload})
	}
	return out, nil
}

func stringifyRedisMapValue(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case []byte:
		return string(t)
	default:
		return ""
	}
}

func (c *RoomChat) GetPayload(ctx context.Context, roomID, streamID string) (string, error) {
	msgs, err := c.client.XRange(ctx, chatStreamKey(roomID), streamID, streamID).Result()
	if err != nil {
		return "", err
	}
	if len(msgs) == 0 {
		return "", goredis.Nil
	}
	p, _ := msgs[0].Values["payload"].(string)
	return p, nil
}

func (c *RoomChat) EnqueuePending(ctx context.Context, roomID, streamID string) error {
	// Score = unix seconds; worker picks entries with score <= now (all due for MVP).
	score := float64(time.Now().Unix())
	return c.client.ZAdd(ctx, chatPendingKey(roomID), goredis.Z{Score: score, Member: streamID}).Err()
}

func (c *RoomChat) RemovePending(ctx context.Context, roomID, streamID string) error {
	return c.client.ZRem(ctx, chatPendingKey(roomID), streamID).Err()
}

func (c *RoomChat) PendingStreamIDs(ctx context.Context, roomID string, max int64) ([]string, error) {
	if max <= 0 {
		max = 50
	}
	return c.client.ZRange(ctx, chatPendingKey(roomID), 0, max-1).Result()
}

func (c *RoomChat) DeleteRoom(ctx context.Context, roomID string) error {
	pipe := c.client.TxPipeline()
	pipe.Del(ctx, chatStreamKey(roomID))
	pipe.Del(ctx, chatSeqKey(roomID))
	pipe.Del(ctx, chatPendingKey(roomID))
	_, err := pipe.Exec(ctx)
	return err
}

func (c *RoomChat) AllowChatSend(ctx context.Context, roomID, userID string, maxPerSecond int) (bool, error) {
	if maxPerSecond <= 0 {
		return true, nil
	}
	key := chatRateKey(roomID, userID)
	n, err := c.client.Incr(ctx, key).Result()
	if err != nil {
		return false, err
	}
	if n == 1 {
		_ = c.client.Expire(ctx, key, time.Second).Err()
	}
	return n <= int64(maxPerSecond), nil
}

// ScanPendingRooms returns Redis keys for pending ZSETs using SCAN (cursor 0 to restart).
func (c *RoomChat) ScanPendingRooms(ctx context.Context, cursor uint64, count int64) ([]string, uint64, error) {
	if count <= 0 {
		count = 50
	}
	keys, next, err := c.client.Scan(ctx, cursor, roomChatPendingPrefix+"*", count).Result()
	if err != nil {
		return nil, 0, err
	}
	return keys, next, nil
}
