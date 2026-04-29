package room

import (
	"context"
	"errors"
	"time"

	"watchtogether/internal/cache"
	"watchtogether/internal/model"
)

var ErrForbidden = errors.New("forbidden")

type User struct {
	ID       string         `json:"id"`
	Username string         `json:"username"`
	Role     model.UserRole `json:"role"`
	IsOwner  bool           `json:"is_owner"`
}

type Message struct {
	Type      string               `json:"type"`
	Action    model.PlaybackAction `json:"action,omitempty"`
	Event     string               `json:"event,omitempty"`
	Position  float64              `json:"position,omitempty"`
	VideoID   string               `json:"video_id,omitempty"`
	Queue     []string             `json:"queue,omitempty"`
	Timestamp int64                `json:"timestamp,omitempty"`
	Payload   any                  `json:"payload,omitempty"`
	User      *User                `json:"user,omitempty"`
}

type Snapshot struct {
	RoomID      string           `json:"room_id"`
	State       *model.RoomState `json:"state,omitempty"`
	Queue       []string         `json:"queue"`
	ViewerCount int              `json:"viewer_count"`
}

type Publisher interface {
	PublishRoomMessage(ctx context.Context, roomID string, name string, data any) error
}

type Service struct {
	states    cache.RoomStateCache
	publisher Publisher
	now       func() time.Time
}

func NewService(states cache.RoomStateCache, publisher Publisher) *Service {
	return &Service{states: states, publisher: publisher, now: time.Now}
}

func (s *Service) Snapshot(ctx context.Context, roomID string) Snapshot {
	snapshot := Snapshot{
		RoomID:      roomID,
		Queue:       []string{},
		ViewerCount: 0,
	}
	if state, err := s.states.GetRoomState(ctx, roomID); err == nil {
		snapshot.State = state
		snapshot.Queue = append(snapshot.Queue, state.Queue...)
		if len(snapshot.Queue) == 0 && state.VideoID != "" {
			snapshot.Queue = []string{state.VideoID}
		}
	}
	return snapshot
}

func (s *Service) ApplyControl(ctx context.Context, roomID string, user User, msg Message) (Message, error) {
	if user.Role != model.UserRoleAdmin && !user.IsOwner {
		return Message{}, ErrForbidden
	}
	now := s.now().UTC()
	queue := append([]string(nil), msg.Queue...)
	if len(queue) == 0 && msg.VideoID != "" {
		queue = []string{msg.VideoID}
	}
	state := &model.RoomState{
		RoomID:    roomID,
		VideoID:   msg.VideoID,
		Queue:     queue,
		Action:    msg.Action,
		Position:  msg.Position,
		UpdatedBy: user.ID,
		UpdatedAt: now,
	}
	if err := s.states.SetRoomState(ctx, roomID, state); err != nil {
		return Message{}, err
	}
	sync := Message{
		Type:      "sync",
		Action:    msg.Action,
		Position:  msg.Position,
		VideoID:   msg.VideoID,
		Queue:     queue,
		Timestamp: now.Unix(),
		User:      &user,
	}
	if s.publisher != nil {
		return sync, s.publisher.PublishRoomMessage(ctx, roomID, "room.sync", sync)
	}
	return sync, nil
}

func (s *Service) Destroy(ctx context.Context, roomID string, user *User) error {
	if err := s.states.DeleteRoomState(ctx, roomID); err != nil {
		return err
	}
	return s.PublishRoomEvent(ctx, roomID, "room_deleted", user)
}

func (s *Service) PublishRoomEvent(ctx context.Context, roomID string, event string, user *User) error {
	if s.publisher == nil {
		return nil
	}
	msg := Message{
		Type:  "room_event",
		Event: event,
		User:  user,
	}
	return s.publisher.PublishRoomMessage(ctx, roomID, "room.event", msg)
}
