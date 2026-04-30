package room

import (
	"context"
	"testing"

	"github.com/google/uuid"

	"watchtogether/internal/cache/memory"
	"watchtogether/internal/model"
	"watchtogether/internal/store"
)

type memRooms struct {
	rooms map[string]*model.Room
}

func (m *memRooms) Create(context.Context, *model.Room) error { return nil }

func (m *memRooms) GetByID(context.Context, string) (*model.Room, error) {
	return nil, store.ErrNotFound
}

func (m *memRooms) Delete(_ context.Context, id string) error {
	delete(m.rooms, id)
	return nil
}

func (m *memRooms) List(_ context.Context, opts store.ListRoomsOpts) ([]*model.Room, int, error) {
	out := make([]*model.Room, 0, len(m.rooms))
	for _, r := range m.rooms {
		out = append(out, r)
	}
	return out, len(out), nil
}

func (m *memRooms) Update(context.Context, *model.Room) error { return nil }

func TestRunEmptyRoomCleanupDeletesPending(t *testing.T) {
	ctx := context.Background()
	states := memory.NewRoomStateCache()
	presence := memory.NewRoomPresence()
	rs := &memRooms{rooms: make(map[string]*model.Room)}
	svc := NewService(states, presence, rs, nil, nil)

	rid := uuid.NewString()
	rs.rooms[rid] = &model.Room{ID: rid, Name: "x"}
	u := User{ID: "u1", Username: "u1"}
	if _, err := svc.Join(ctx, rid, u); err != nil {
		t.Fatal(err)
	}
	if err := svc.Leave(ctx, rid, "u1"); err != nil {
		t.Fatal(err)
	}
	if err := svc.RunEmptyRoomCleanup(ctx); err != nil {
		t.Fatal(err)
	}
	if _, ok := rs.rooms[rid]; ok {
		t.Fatal("room should be deleted from store")
	}
}
