package testutil

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"watchtogether/internal/model"
	"watchtogether/internal/store"
)

type Suite struct {
	Users store.UserStore
	Rooms store.RoomStore
}

func RunStoreSuite(t *testing.T, newSuite func(t *testing.T) Suite) {
	t.Helper()

	t.Run("users", func(t *testing.T) {
		ctx := context.Background()
		suite := newSuite(t)
		now := time.Now().UTC()
		user := &model.User{
			ID:           uuid.NewString(),
			Email:        "alice@example.test",
			Username:     "alice",
			PasswordHash: "hash",
			Nickname:     "Alice",
			Role:         model.UserRoleUser,
			CreatedAt:    now,
			UpdatedAt:    now,
		}

		if err := suite.Users.Create(ctx, user); err != nil {
			t.Fatalf("create user: %v", err)
		}
		byID, err := suite.Users.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("get user by id: %v", err)
		}
		if byID.Username != user.Username {
			t.Fatalf("username mismatch: got %q want %q", byID.Username, user.Username)
		}
		byUsername, err := suite.Users.GetByUsername(ctx, user.Username)
		if err != nil {
			t.Fatalf("get user by username: %v", err)
		}
		if byUsername.ID != user.ID {
			t.Fatalf("id mismatch: got %q want %q", byUsername.ID, user.ID)
		}
		byID.Nickname = "Alice Updated"
		if err := suite.Users.Update(ctx, byID); err != nil {
			t.Fatalf("update user: %v", err)
		}
		updated, err := suite.Users.GetByID(ctx, user.ID)
		if err != nil {
			t.Fatalf("get updated user: %v", err)
		}
		if updated.Nickname != "Alice Updated" {
			t.Fatalf("nickname not updated: %q", updated.Nickname)
		}
	})

	t.Run("rooms", func(t *testing.T) {
		ctx := context.Background()
		suite := newSuite(t)
		now := time.Now().UTC()
		room := &model.Room{
			ID:         uuid.NewString(),
			Name:       "Movie Night",
			OwnerID:    "owner-1",
			Visibility: model.RoomVisibilityPublic,
			CreatedAt:  now,
			UpdatedAt:  now,
		}

		if err := suite.Rooms.Create(ctx, room); err != nil {
			t.Fatalf("create room: %v", err)
		}
		got, err := suite.Rooms.GetByID(ctx, room.ID)
		if err != nil {
			t.Fatalf("get room: %v", err)
		}
		if got.Name != room.Name {
			t.Fatalf("room name mismatch: %q", got.Name)
		}
		got.Name = "Updated Room"
		if err := suite.Rooms.Update(ctx, got); err != nil {
			t.Fatalf("update room: %v", err)
		}
		rooms, total, err := suite.Rooms.List(ctx, store.ListRoomsOpts{Limit: 10})
		if err != nil {
			t.Fatalf("list rooms: %v", err)
		}
		if total != 1 || len(rooms) != 1 || rooms[0].Name != "Updated Room" {
			t.Fatalf("unexpected rooms result: total=%d len=%d", total, len(rooms))
		}
		if err := suite.Rooms.Delete(ctx, room.ID); err != nil {
			t.Fatalf("delete room: %v", err)
		}
		if _, err := suite.Rooms.GetByID(ctx, room.ID); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("expected ErrNotFound, got %v", err)
		}
	})
}
