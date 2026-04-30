package store

import (
	"context"
	"errors"

	"watchtogether/internal/model"
)

var (
	ErrNotFound = errors.New("store: not found")
	ErrConflict = errors.New("store: conflict")
)

type ListRoomsOpts struct {
	Limit   int
	Offset  int
	Query   string
	OwnerID string
}

type ListVideosOpts struct {
	Limit  int
	Offset int
	Query  string
	Status model.VideoStatus
}

type UserStore interface {
	GetByID(ctx context.Context, id string) (*model.User, error)
	GetByUsername(ctx context.Context, username string) (*model.User, error)
	GetByEmail(ctx context.Context, email string) (*model.User, error)
	Create(ctx context.Context, user *model.User) error
	Update(ctx context.Context, user *model.User) error
}

type RoomStore interface {
	Create(ctx context.Context, room *model.Room) error
	GetByID(ctx context.Context, id string) (*model.Room, error)
	List(ctx context.Context, opts ListRoomsOpts) ([]*model.Room, int, error)
	Update(ctx context.Context, room *model.Room) error
	Delete(ctx context.Context, id string) error
}

type VideoStore interface {
	Create(ctx context.Context, video *model.Video) error
	GetByID(ctx context.Context, id string) (*model.Video, error)
	List(ctx context.Context, opts ListVideosOpts) ([]*model.Video, int, error)
	Update(ctx context.Context, video *model.Video) error
	UpdateStatus(ctx context.Context, id string, status model.VideoStatus) error
	Delete(ctx context.Context, id string) error
}

type DownloadTaskStore interface {
	Create(ctx context.Context, task *model.DownloadTask) error
	GetByID(ctx context.Context, id string) (*model.DownloadTask, error)
	List(ctx context.Context) ([]*model.DownloadTask, error)
	UpdateProgress(ctx context.Context, id string, progress float64, status model.DownloadTaskStatus) error
	UpdateResult(ctx context.Context, task *model.DownloadTask) error
	Delete(ctx context.Context, id string) error
}
