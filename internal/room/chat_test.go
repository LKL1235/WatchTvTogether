package room

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"watchtogether/internal/cache"
	"watchtogether/internal/realtime"
)

type fakeRoomChat struct {
	mu       sync.Mutex
	seq      map[string]int64
	msgs     map[string][]cache.RoomChatMessage
	pending  map[string][]string
	pendN    map[string]int64
	rate     map[string]int64
	failPub  bool
}

func newFakeRoomChat() *fakeRoomChat {
	return &fakeRoomChat{
		seq:     make(map[string]int64),
		msgs:    make(map[string][]cache.RoomChatMessage),
		pending: make(map[string][]string),
		pendN:   make(map[string]int64),
		rate:    make(map[string]int64),
	}
}

func (f *fakeRoomChat) PendingCount(_ context.Context, roomID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.pendN[roomID], nil
}

func (f *fakeRoomChat) setPendingCount(roomID string, n int64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pendN[roomID] = n
}

func (f *fakeRoomChat) NextSeq(_ context.Context, roomID string) (int64, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq[roomID]++
	return f.seq[roomID], nil
}

func (f *fakeRoomChat) AppendMessage(_ context.Context, roomID string, seq int64, payloadJSON string, _ int64) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id := time.Now().Format("150405") + "-0"
	f.msgs[roomID] = append(f.msgs[roomID], cache.RoomChatMessage{
		StreamID: id,
		Seq:      seq,
		Payload:  payloadJSON,
	})
	return id, nil
}

func (f *fakeRoomChat) ListMessagesRev(_ context.Context, roomID, beforeID string, limit int) ([]cache.RoomChatMessage, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	all := f.msgs[roomID]
	out := make([]cache.RoomChatMessage, 0, len(all))
	for i := len(all) - 1; i >= 0; i-- {
		m := all[i]
		if beforeID != "" && m.StreamID >= beforeID {
			continue
		}
		out = append(out, m)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (f *fakeRoomChat) GetPayload(_ context.Context, roomID, streamID string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, m := range f.msgs[roomID] {
		if m.StreamID == streamID {
			return m.Payload, nil
		}
	}
	return "", errors.New("not found")
}

func (f *fakeRoomChat) EnqueuePending(_ context.Context, roomID, streamID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.pending[roomID] = append(f.pending[roomID], streamID)
	f.pendN[roomID] = int64(len(f.pending[roomID]))
	return nil
}

func (f *fakeRoomChat) RemovePending(_ context.Context, roomID, streamID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	next := f.pending[roomID][:0]
	for _, id := range f.pending[roomID] {
		if id != streamID {
			next = append(next, id)
		}
	}
	f.pending[roomID] = next
	f.pendN[roomID] = int64(len(next))
	return nil
}

func (f *fakeRoomChat) PendingStreamIDs(_ context.Context, roomID string, max int64) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	ids := f.pending[roomID]
	if int64(len(ids)) > max {
		ids = ids[:max]
	}
	out := make([]string, len(ids))
	copy(out, ids)
	return out, nil
}

func (f *fakeRoomChat) DeleteRoom(_ context.Context, roomID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.msgs, roomID)
	delete(f.seq, roomID)
	delete(f.pending, roomID)
	delete(f.pendN, roomID)
	return nil
}

func (f *fakeRoomChat) AllowChatSend(_ context.Context, roomID, userID string, maxPerSecond int) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	k := roomID + ":" + userID
	f.rate[k]++
	return f.rate[k] <= int64(maxPerSecond), nil
}

func (f *fakeRoomChat) ScanPendingRooms(_ context.Context, _ uint64, _ int64) ([]string, uint64, error) {
	return nil, 0, nil
}

type recordingPublisher struct {
	mu    sync.Mutex
	calls []string
	err   error
}

func (p *recordingPublisher) PublishRoomMessage(_ context.Context, _ string, name string, _ any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, name)
	return p.err
}

func testChatLimits() ChatLimits {
	return ChatLimits{
		StreamMaxLen:       100,
		AblyPublishRetry:   0,
		AblyPublishTimeout: 2 * time.Second,
		AblyPendingMax:     2,
		MaxPayloadBytes:    65536,
		MaxTextRunes:       10,
		RatePerSecond:      3,
	}
}

func TestSendChat_RequiresRedis(t *testing.T) {
	svc := NewService(nil, nil, nil, nil, nil, nil, nil, testChatLimits())
	_, err := svc.SendChat(context.Background(), "r1", User{ID: "u1"}, "hi")
	if !errors.Is(err, ErrChatRequiresRedis) {
		t.Fatalf("got %v", err)
	}
}

func TestSendChat_Validation(t *testing.T) {
	chat := newFakeRoomChat()
	svc := NewService(nil, nil, nil, nil, nil, nil, chat, testChatLimits())
	ctx := context.Background()

	_, err := svc.SendChat(ctx, "r1", User{ID: "u1"}, "   ")
	if !errors.Is(err, ErrChatTextEmpty) {
		t.Fatalf("empty: %v", err)
	}

	long := strings.Repeat("字", 11)
	_, err = svc.SendChat(ctx, "r1", User{ID: "u1"}, long)
	if !errors.Is(err, ErrChatTextTooLong) {
		t.Fatalf("long: %v", err)
	}
}

func TestSendChat_PendingFull(t *testing.T) {
	chat := newFakeRoomChat()
	chat.setPendingCount("r1", 2)
	svc := NewService(nil, nil, nil, nil, nil, nil, chat, testChatLimits())
	_, err := svc.SendChat(context.Background(), "r1", User{ID: "u1"}, "hi")
	if !errors.Is(err, ErrChatPendingFull) {
		t.Fatalf("got %v", err)
	}
}

func TestSendChat_OkPublishesAbly(t *testing.T) {
	chat := newFakeRoomChat()
	pub := &recordingPublisher{}
	svc := NewService(nil, nil, nil, nil, nil, pub, chat, testChatLimits())
	res, err := svc.SendChat(context.Background(), "r1", User{ID: "u1", Username: "alice"}, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if res.Realtime != "ok" {
		t.Fatalf("realtime=%q", res.Realtime)
	}
	if res.Message.Seq != 1 || res.Message.Text != "hello" {
		t.Fatalf("message=%+v", res.Message)
	}
	pub.mu.Lock()
	defer pub.mu.Unlock()
	if len(pub.calls) != 1 || pub.calls[0] != realtime.MessageRoomChat {
		t.Fatalf("publish calls=%v", pub.calls)
	}
}

func TestSendChat_DeferredOnPublishFailure(t *testing.T) {
	chat := newFakeRoomChat()
	pub := &recordingPublisher{err: errors.New("ably down")}
	svc := NewService(nil, nil, nil, nil, nil, pub, chat, testChatLimits())
	res, err := svc.SendChat(context.Background(), "r1", User{ID: "u1"}, "hello")
	if err != nil {
		t.Fatal(err)
	}
	if res.Realtime != "deferred" {
		t.Fatalf("realtime=%q", res.Realtime)
	}
	if chat.pendN["r1"] != 1 {
		t.Fatalf("pending=%d", chat.pendN["r1"])
	}
}

func TestListChat_ReturnsMessages(t *testing.T) {
	chat := newFakeRoomChat()
	ctx := context.Background()
	payload, _ := json.Marshal(ChatMessage{Type: "chat", Seq: 1, RoomID: "r1", Text: "a", SentAt: 1})
	_, _ = chat.AppendMessage(ctx, "r1", 1, string(payload), 100)
	svc := NewService(nil, nil, nil, nil, nil, nil, chat, testChatLimits())
	items, hasMore, err := svc.ListChat(ctx, "r1", "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if hasMore || len(items) != 1 || items[0].Text != "a" {
		t.Fatalf("items=%+v hasMore=%v", items, hasMore)
	}
}
