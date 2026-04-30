package room

import (
	"testing"
	"time"

	"watchtogether/internal/model"
)

func TestProjectedPlaybackPlayAdvances(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	st := &model.RoomState{
		Action:    model.PlaybackActionPlay,
		Position:  10,
		UpdatedAt: base,
	}
	pos, atEnd := ProjectedPlayback(st, base.Add(5*time.Second), 100)
	if atEnd {
		t.Fatal("should not be at end")
	}
	if pos != 15 {
		t.Fatalf("position = %v want 15", pos)
	}
}

func TestProjectedPlaybackPauseNoAdvance(t *testing.T) {
	base := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	st := &model.RoomState{
		Action:    model.PlaybackActionPause,
		Position:  42,
		UpdatedAt: base,
	}
	pos, _ := ProjectedPlayback(st, base.Add(999*time.Second), 100)
	if pos != 42 {
		t.Fatalf("position = %v want 42", pos)
	}
}

func TestNextVideoAfterEndLoop(t *testing.T) {
	next, ok := NextVideoAfterEnd("a", []string{"a", "b"}, model.PlaybackModeLoop)
	if !ok || next != "b" {
		t.Fatalf("next = %q ok=%v", next, ok)
	}
	next, ok = NextVideoAfterEnd("b", []string{"a", "b"}, model.PlaybackModeLoop)
	if !ok || next != "a" {
		t.Fatalf("wrap next = %q ok=%v", next, ok)
	}
}
