package redis

import (
	"context"
	"testing"

	miniredis "github.com/alicebob/miniredis/v2"
	goredis "github.com/redis/go-redis/v9"

	"watchtogether/internal/cache"
)

func newTestRoomChat(t *testing.T) (*RoomChat, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := goredis.NewClient(&goredis.Options{Addr: mr.Addr()})
	return NewRoomChat(client), mr
}

func TestRoomChat_AppendListDelete(t *testing.T) {
	rc, _ := newTestRoomChat(t)
	ctx := context.Background()
	roomID := "room-a"

	seq, err := rc.NextSeq(ctx, roomID)
	if err != nil || seq != 1 {
		t.Fatalf("seq=%d err=%v", seq, err)
	}
	sid, err := rc.AppendMessage(ctx, roomID, seq, `{"text":"hi"}`, 10)
	if err != nil || sid == "" {
		t.Fatalf("append: %s %v", sid, err)
	}

	items, err := rc.ListMessagesRev(ctx, roomID, "", 10)
	if err != nil || len(items) != 1 || items[0].Payload != `{"text":"hi"}` {
		t.Fatalf("list: %+v err=%v", items, err)
	}

	older, err := rc.ListMessagesRev(ctx, roomID, sid, 10)
	if err != nil || len(older) != 0 {
		t.Fatalf("before cursor: %+v err=%v", older, err)
	}

	if err := rc.DeleteRoom(ctx, roomID); err != nil {
		t.Fatal(err)
	}
	items, err = rc.ListMessagesRev(ctx, roomID, "", 10)
	if err != nil || len(items) != 0 {
		t.Fatalf("after delete: %+v err=%v", items, err)
	}
}

func TestRoomChat_PendingQueue(t *testing.T) {
	rc, _ := newTestRoomChat(t)
	ctx := context.Background()
	roomID := "room-b"

	if err := rc.EnqueuePending(ctx, roomID, "1-0"); err != nil {
		t.Fatal(err)
	}
	n, err := rc.PendingCount(ctx, roomID)
	if err != nil || n != 1 {
		t.Fatalf("count=%d err=%v", n, err)
	}
	ids, err := rc.PendingStreamIDs(ctx, roomID, 10)
	if err != nil || len(ids) != 1 || ids[0] != "1-0" {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if err := rc.RemovePending(ctx, roomID, "1-0"); err != nil {
		t.Fatal(err)
	}
	n, _ = rc.PendingCount(ctx, roomID)
	if n != 0 {
		t.Fatalf("pending=%d", n)
	}
}

func TestRoomChat_AllowChatSendRateLimit(t *testing.T) {
	rc, _ := newTestRoomChat(t)
	ctx := context.Background()
	roomID, userID := "room-c", "u1"

	for i := 0; i < 2; i++ {
		ok, err := rc.AllowChatSend(ctx, roomID, userID, 2)
		if err != nil || !ok {
			t.Fatalf("attempt %d: ok=%v err=%v", i, ok, err)
		}
	}
	ok, err := rc.AllowChatSend(ctx, roomID, userID, 2)
	if err != nil || ok {
		t.Fatalf("third: ok=%v err=%v", ok, err)
	}
}

func TestParseRoomChatPendingKey(t *testing.T) {
	rid, ok := cache.ParseRoomChatPendingKey("room:chat:ably_pending:abc")
	if !ok || rid != "abc" {
		t.Fatalf("rid=%q ok=%v", rid, ok)
	}
	_, ok = cache.ParseRoomChatPendingKey("other")
	if ok {
		t.Fatal("expected false")
	}
}
