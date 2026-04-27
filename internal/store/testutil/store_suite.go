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
	Users         store.UserStore
	Rooms         store.RoomStore
	Videos        store.VideoStore
	DownloadTasks store.DownloadTaskStore
}

func RunStoreSuite(t *testing.T, newSuite func(t *testing.T) Suite) {
	t.Helper()

	t.Run("users", func(t *testing.T) {
		ctx := context.Background()
		suite := newSuite(t)
		now := time.Now().UTC()
		user := &model.User{
			ID:           uuid.NewString(),
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

	t.Run("videos", func(t *testing.T) {
		ctx := context.Background()
		suite := newSuite(t)
		now := time.Now().UTC()
		video := &model.Video{
			ID:        uuid.NewString(),
			Title:     "Example",
			FilePath:  "/videos/example.mp4",
			Duration:  123,
			Format:    "mp4",
			Size:      1024,
			SourceURL: "https://example.test/video.mp4",
			Status:    model.VideoStatusProcessing,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := suite.Videos.Create(ctx, video); err != nil {
			t.Fatalf("create video: %v", err)
		}
		if err := suite.Videos.UpdateStatus(ctx, video.ID, model.VideoStatusReady); err != nil {
			t.Fatalf("update video status: %v", err)
		}
		got, err := suite.Videos.GetByID(ctx, video.ID)
		if err != nil {
			t.Fatalf("get video: %v", err)
		}
		if got.Status != model.VideoStatusReady {
			t.Fatalf("status mismatch: %q", got.Status)
		}
		videos, total, err := suite.Videos.List(ctx, store.ListVideosOpts{Query: "exam", Limit: 10})
		if err != nil {
			t.Fatalf("list videos: %v", err)
		}
		if total != 1 || len(videos) != 1 {
			t.Fatalf("unexpected videos result: total=%d len=%d", total, len(videos))
		}
		if err := suite.Videos.Delete(ctx, video.ID); err != nil {
			t.Fatalf("delete video: %v", err)
		}
	})

	t.Run("download tasks", func(t *testing.T) {
		ctx := context.Background()
		suite := newSuite(t)
		now := time.Now().UTC()
		task := &model.DownloadTask{
			ID:        uuid.NewString(),
			UserID:    "user-1",
			SourceURL: "https://example.test/video.mp4",
			Progress:  0,
			Status:    model.DownloadTaskPending,
			CreatedAt: now,
			UpdatedAt: now,
		}

		if err := suite.DownloadTasks.Create(ctx, task); err != nil {
			t.Fatalf("create task: %v", err)
		}
		if err := suite.DownloadTasks.UpdateProgress(ctx, task.ID, 42.5, model.DownloadTaskRunning); err != nil {
			t.Fatalf("update task progress: %v", err)
		}
		got, err := suite.DownloadTasks.GetByID(ctx, task.ID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if got.Progress != 42.5 || got.Status != model.DownloadTaskRunning {
			t.Fatalf("unexpected task state: progress=%f status=%s", got.Progress, got.Status)
		}
		tasks, err := suite.DownloadTasks.List(ctx)
		if err != nil {
			t.Fatalf("list tasks: %v", err)
		}
		if len(tasks) != 1 {
			t.Fatalf("unexpected task count: %d", len(tasks))
		}
		if err := suite.DownloadTasks.Delete(ctx, task.ID); err != nil {
			t.Fatalf("delete task: %v", err)
		}
	})
}
