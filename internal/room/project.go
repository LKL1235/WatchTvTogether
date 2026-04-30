package room

import (
	"time"

	"watchtogether/internal/model"
)

// ProjectedPlayback returns watch position for the current video at time now.
// duration is the video length in seconds; if <= 0, upper bound checks are skipped.
func ProjectedPlayback(state *model.RoomState, now time.Time, duration float64) (position float64, atEnd bool) {
	if state == nil {
		return 0, false
	}
	pos := state.Position
	if state.Action != model.PlaybackActionPlay {
		return clampPos(pos, duration), false
	}
	elapsed := now.Sub(state.UpdatedAt).Seconds()
	if elapsed < 0 {
		elapsed = 0
	}
	pos = state.Position + elapsed
	if duration > 0 && pos >= duration {
		return duration, true
	}
	return clampPos(pos, duration), false
}

func clampPos(pos, duration float64) float64 {
	if pos < 0 {
		return 0
	}
	if duration > 0 && pos > duration {
		return duration
	}
	return pos
}

// NextVideoAfterEnd returns the next video id when current playback reaches end of file, based on mode and queue.
func NextVideoAfterEnd(current string, queue []string, mode model.PlaybackMode) (nextID string, hasNext bool) {
	if len(queue) == 0 {
		return "", false
	}
	idx := indexOf(queue, current)
	if idx < 0 {
		return "", false
	}
	switch mode {
	case model.PlaybackModeLoop:
		next := idx + 1
		if next >= len(queue) {
			next = 0
		}
		return queue[next], true
	default:
		if idx+1 >= len(queue) {
			return "", false
		}
		return queue[idx+1], true
	}
}

func indexOf(slice []string, v string) int {
	for i, s := range slice {
		if s == v {
			return i
		}
	}
	return -1
}
