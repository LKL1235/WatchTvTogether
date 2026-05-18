package room

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"strings"
	"time"
	"unicode/utf8"

	"watchtogether/internal/cache"
	"watchtogether/internal/config"
	"watchtogether/internal/realtime"
)

// ChatLimits configures room chat (Redis Stream + Ably).
type ChatLimits struct {
	StreamMaxLen       int64
	AblyPublishRetry   int
	AblyPublishTimeout time.Duration
	AblyPendingMax     int
	MaxPayloadBytes    int
	MaxTextRunes       int
	RatePerSecond      int
}

// ChatLimitsFromConfig copies chat-related settings from application config.
func ChatLimitsFromConfig(c config.Config) ChatLimits {
	to := c.ChatAblyPublishTimeout
	if to <= 0 {
		to = 10 * time.Second
	}
	retry := c.ChatAblyPublishRetry
	if retry < 0 {
		retry = 0
	}
	return ChatLimits{
		StreamMaxLen:       int64(nonzero(c.ChatStreamMaxLen, 2000)),
		AblyPublishRetry:   retry,
		AblyPublishTimeout: to,
		AblyPendingMax:     nonzero(c.ChatAblyPendingMax, 500),
		MaxPayloadBytes:    nonzero(c.ChatMaxPayloadBytes, 61440),
		MaxTextRunes:       nonzero(c.ChatMaxTextRunes, 2000),
		RatePerSecond:      nonzero(c.ChatRatePerSecond, 10),
	}
}

func nonzero(v, def int) int {
	if v <= 0 {
		return def
	}
	return v
}

var (
	ErrChatRequiresRedis   = errors.New("room chat requires redis cache backend")
	ErrChatPendingFull     = errors.New("room chat ably pending queue is full")
	ErrChatTextEmpty       = errors.New("chat text is empty")
	ErrChatTextTooLong     = errors.New("chat text exceeds maximum length")
	ErrChatPayloadTooLarge = errors.New("chat message payload exceeds maximum size")
	ErrChatRateLimited     = errors.New("chat rate limit exceeded")
)

type ChatMessage struct {
	Type     string `json:"type"`
	Seq      int64  `json:"seq"`
	StreamID string `json:"stream_id,omitempty"`
	RoomID   string `json:"room_id"`
	User     User   `json:"user"`
	Text     string `json:"text"`
	SentAt   int64  `json:"sent_at"`
}

// ChatSendResult is returned from SendChat for HTTP JSON.
type ChatSendResult struct {
	Message  ChatMessage `json:"message"`
	Realtime string      `json:"realtime"` // "ok" | "deferred"
}

func (s *Service) deleteRoomChat(ctx context.Context, roomID string) {
	if s.roomChat == nil {
		return
	}
	_ = s.roomChat.DeleteRoom(ctx, roomID)
}

// SendChat validates text, stores in Redis Stream, publishes to Ably with bounded retries.
func (s *Service) SendChat(ctx context.Context, roomID string, sender User, plain string) (*ChatSendResult, error) {
	if s.roomChat == nil {
		return nil, ErrChatRequiresRedis
	}
	plain = strings.TrimSpace(plain)
	if plain == "" {
		return nil, ErrChatTextEmpty
	}
	if utf8.RuneCountInString(plain) > s.chat.MaxTextRunes {
		return nil, ErrChatTextTooLong
	}
	npend, err := s.roomChat.PendingCount(ctx, roomID)
	if err != nil {
		return nil, err
	}
	if int(npend) >= s.chat.AblyPendingMax {
		return nil, ErrChatPendingFull
	}
	ok, err := s.roomChat.AllowChatSend(ctx, roomID, sender.ID, s.chat.RatePerSecond)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrChatRateLimited
	}

	seq, err := s.roomChat.NextSeq(ctx, roomID)
	if err != nil {
		return nil, err
	}
	sentAt := s.now().Unix()
	base := ChatMessage{
		Type:   "chat",
		Seq:    seq,
		RoomID: roomID,
		User:   sender,
		Text:   plain,
		SentAt: sentAt,
	}
	payload, err := json.Marshal(base)
	if err != nil {
		return nil, err
	}
	if len(payload) > s.chat.MaxPayloadBytes {
		return nil, ErrChatPayloadTooLarge
	}

	streamID, err := s.roomChat.AppendMessage(ctx, roomID, seq, string(payload), s.chat.StreamMaxLen)
	if err != nil {
		return nil, err
	}

	out := base
	out.StreamID = streamID
	ablyPayload, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	if len(ablyPayload) > s.chat.MaxPayloadBytes {
		_ = s.roomChat.EnqueuePending(ctx, roomID, streamID)
		return &ChatSendResult{Message: out, Realtime: "deferred"}, nil
	}

	pubErr := s.publishAblyChat(ctx, roomID, json.RawMessage(ablyPayload))
	if pubErr == nil {
		return &ChatSendResult{Message: out, Realtime: "ok"}, nil
	}
	if err := s.roomChat.EnqueuePending(ctx, roomID, streamID); err != nil {
		return nil, err
	}
	return &ChatSendResult{Message: out, Realtime: "deferred"}, nil
}

func (s *Service) publishAblyChat(ctx context.Context, roomID string, data any) error {
	if s.publisher == nil {
		return nil
	}
	attempts := 1 + s.chat.AblyPublishRetry
	if attempts < 1 {
		attempts = 1
	}
	timeout := s.chat.AblyPublishTimeout
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	pubCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var lastErr error
	for i := 0; i < attempts; i++ {
		if err := pubCtx.Err(); err != nil {
			lastErr = err
			break
		}
		err := s.publisher.PublishRoomMessage(pubCtx, roomID, realtime.MessageRoomChat, data)
		if err == nil {
			return nil
		}
		lastErr = err
		if i+1 < attempts {
			backoff := time.Duration(40*(1<<i)+rand.IntN(80)) * time.Millisecond
			if backoff > 1500*time.Millisecond {
				backoff = 1500 * time.Millisecond
			}
			select {
			case <-time.After(backoff):
			case <-pubCtx.Done():
				lastErr = pubCtx.Err()
				return lastErr
			}
		}
	}
	return lastErr
}

// ListChat returns recent chat messages (newest first) for HTTP.
func (s *Service) ListChat(ctx context.Context, roomID, beforeID string, limit int) ([]ChatMessage, bool, error) {
	if s.roomChat == nil {
		return nil, false, ErrChatRequiresRedis
	}
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	raw, err := s.roomChat.ListMessagesRev(ctx, roomID, beforeID, limit+1)
	if err != nil {
		return nil, false, err
	}
	hasMore := len(raw) > limit
	if hasMore {
		raw = raw[:limit]
	}
	out := make([]ChatMessage, 0, len(raw))
	for _, m := range raw {
		var w ChatMessage
		if err := json.Unmarshal([]byte(m.Payload), &w); err != nil {
			continue
		}
		w.StreamID = m.StreamID
		out = append(out, w)
	}
	return out, hasMore, nil
}

// ProcessGlobalChatPending attempts Ably republish for pending stream entries (best-effort).
func (s *Service) ProcessGlobalChatPending(ctx context.Context) error {
	if s.roomChat == nil || s.publisher == nil {
		return nil
	}
	var cursor uint64
	for {
		keys, next, err := s.roomChat.ScanPendingRooms(ctx, cursor, 40)
		if err != nil {
			return err
		}
		for _, key := range keys {
			rid, ok := cache.ParseRoomChatPendingKey(key)
			if !ok {
				continue
			}
			_ = s.drainRoomPending(ctx, rid)
		}
		cursor = next
		if cursor == 0 {
			break
		}
	}
	return nil
}

func (s *Service) drainRoomPending(ctx context.Context, roomID string) error {
	ids, err := s.roomChat.PendingStreamIDs(ctx, roomID, 30)
	if err != nil || len(ids) == 0 {
		return err
	}
	for _, sid := range ids {
		payload, err := s.roomChat.GetPayload(ctx, roomID, sid)
		if err != nil || strings.TrimSpace(payload) == "" {
			_ = s.roomChat.RemovePending(ctx, roomID, sid)
			continue
		}
		var w ChatMessage
		if err := json.Unmarshal([]byte(payload), &w); err != nil {
			_ = s.roomChat.RemovePending(ctx, roomID, sid)
			continue
		}
		w.StreamID = sid
		ablyPayload, err := json.Marshal(w)
		if err != nil {
			continue
		}
		if len(ablyPayload) > s.chat.MaxPayloadBytes {
			continue
		}
		if err := s.publishAblyChat(ctx, roomID, json.RawMessage(ablyPayload)); err != nil {
			continue
		}
		_ = s.roomChat.RemovePending(ctx, roomID, sid)
	}
	return nil
}
