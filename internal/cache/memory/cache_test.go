package memory

import (
	"context"
	"testing"
	"time"

	"watchtogether/internal/model"
)

func TestSessionCacheRefreshTokenTTLAndBlacklist(t *testing.T) {
	ctx := context.Background()
	cache := NewSessionCache()

	if err := cache.SetRefreshToken(ctx, "u1", "token", 20*time.Millisecond); err != nil {
		t.Fatalf("SetRefreshToken() error = %v", err)
	}
	got, err := cache.GetRefreshToken(ctx, "u1")
	if err != nil {
		t.Fatalf("GetRefreshToken() error = %v", err)
	}
	if got != "token" {
		t.Fatalf("GetRefreshToken() = %q", got)
	}

	time.Sleep(30 * time.Millisecond)
	got, err = cache.GetRefreshToken(ctx, "u1")
	if err != nil {
		t.Fatalf("GetRefreshToken() after ttl error = %v", err)
	}
	if got != "" {
		t.Fatalf("expected expired refresh token to be removed, got %q", got)
	}

	if err := cache.BlacklistToken(ctx, "jti", 20*time.Millisecond); err != nil {
		t.Fatalf("BlacklistToken() error = %v", err)
	}
	blacklisted, err := cache.IsBlacklisted(ctx, "jti")
	if err != nil {
		t.Fatalf("IsBlacklisted() error = %v", err)
	}
	if !blacklisted {
		t.Fatal("expected token to be blacklisted")
	}
	time.Sleep(30 * time.Millisecond)
	blacklisted, err = cache.IsBlacklisted(ctx, "jti")
	if err != nil {
		t.Fatalf("IsBlacklisted() after ttl error = %v", err)
	}
	if blacklisted {
		t.Fatal("expected blacklist entry to expire")
	}
}

func TestRoomStateCache(t *testing.T) {
	ctx := context.Background()
	cache := NewRoomStateCache()
	state := &model.RoomState{
		RoomID:   "room-1",
		VideoID:  "video-1",
		Action:   model.PlaybackActionPlay,
		Position: 42,
	}

	if err := cache.SetRoomState(ctx, "room-1", state); err != nil {
		t.Fatalf("SetRoomState() error = %v", err)
	}
	got, err := cache.GetRoomState(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetRoomState() error = %v", err)
	}
	if got.VideoID != state.VideoID || got.Position != state.Position {
		t.Fatalf("GetRoomState() = %+v", got)
	}
	got.Position = 1
	again, err := cache.GetRoomState(ctx, "room-1")
	if err != nil {
		t.Fatalf("GetRoomState() second error = %v", err)
	}
	if again.Position != state.Position {
		t.Fatal("room state cache returned mutable internal state")
	}
	if err := cache.DeleteRoomState(ctx, "room-1"); err != nil {
		t.Fatalf("DeleteRoomState() error = %v", err)
	}
	if _, err := cache.GetRoomState(ctx, "room-1"); err == nil {
		t.Fatal("expected missing room state error")
	}
}

func TestPubSubFanout(t *testing.T) {
	ctx := context.Background()
	pubsub := NewPubSub()

	sub1, cancel1, err := pubsub.Subscribe(ctx, "room")
	if err != nil {
		t.Fatalf("Subscribe() sub1 error = %v", err)
	}
	defer cancel1()
	sub2, cancel2, err := pubsub.Subscribe(ctx, "room")
	if err != nil {
		t.Fatalf("Subscribe() sub2 error = %v", err)
	}
	defer cancel2()

	msg := []byte("sync")
	if err := pubsub.Publish(ctx, "room", msg); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	for name, ch := range map[string]<-chan []byte{"sub1": sub1, "sub2": sub2} {
		select {
		case got := <-ch:
			if string(got) != string(msg) {
				t.Fatalf("%s received %q", name, got)
			}
		case <-time.After(time.Second):
			t.Fatalf("%s did not receive fanout message", name)
		}
	}
}
