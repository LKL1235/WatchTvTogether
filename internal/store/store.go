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
