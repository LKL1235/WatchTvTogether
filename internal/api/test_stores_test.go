package api

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"watchtogether/internal/cache/memory"
	"watchtogether/internal/config"
	"watchtogether/internal/emailcode"
	"watchtogether/internal/model"
	"watchtogether/internal/store"
)

type memoryStores struct {
	mu    sync.RWMutex
	users map[string]*model.User
	rooms map[string]*model.Room
}

func newMemoryStores() *memoryStores {
	return &memoryStores{
		users: make(map[string]*model.User),
		rooms: make(map[string]*model.Room),
	}
}

func openTestDB(t *testing.T) *memoryStores {
	t.Helper()
	return newMemoryStores()
}

func testDeps(stores *memoryStores) Dependencies {
	cfg := config.Default()
	cfg.JWTSecret = "test-secret"
	cfg.JWTAccessTTL = time.Hour
	cfg.JWTRefreshTTL = 24 * time.Hour
	cfg.EmailCodeTTL = 10 * time.Minute
	cfg.EmailCodeSendInterval = 60 * time.Second
	cfg.EmailCodeDailyLimit = 5
	cfg.EmailCodeMaxAttempts = 5
	cfg.EmailCodeLength = 6
	cfg.AblyJWTTTL = 30 * time.Minute
	return Dependencies{
		Config:         cfg,
		UserStore:      stores,
		RoomStore:   memoryRoomStore{stores},
		EmailSender: testHookEmailSender{},
		EmailCodes:     emailcode.NewStore(nil),
		SessionCache:   memory.NewSessionCache(),
		RoomStateCache: memory.NewRoomStateCache(),
		RoomPresence:   memory.NewRoomPresence(),
		RoomAccess:     memory.NewRoomAccess(),
		PubSub:         memory.NewPubSub(),
		RoomChat:       nil,
	}
}

func (s *memoryStores) GetByID(_ context.Context, id string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	user, ok := s.users[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	copyUser := *user
	return &copyUser, nil
}

func (s *memoryStores) GetByEmail(_ context.Context, email string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	want := strings.ToLower(strings.TrimSpace(email))
	for _, user := range s.users {
		if strings.ToLower(strings.TrimSpace(user.Email)) == want {
			copyUser := *user
			return &copyUser, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *memoryStores) GetByUsername(_ context.Context, username string) (*model.User, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	want := strings.ToLower(strings.TrimSpace(username))
	for _, user := range s.users {
		if strings.ToLower(user.Username) == want {
			copyUser := *user
			return &copyUser, nil
		}
	}
	return nil, store.ErrNotFound
}

func (s *memoryStores) Create(_ context.Context, user *model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.users {
		if existing.Username == user.Username {
			return store.ErrConflict
		}
		if strings.TrimSpace(existing.Email) != "" && strings.EqualFold(existing.Email, user.Email) {
			return store.ErrConflict
		}
	}
	now := time.Now().UTC()
	if user.ID == "" {
		user.ID = uuid.NewString()
	}
	if user.Role == "" {
		user.Role = model.UserRoleUser
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = now
	}
	user.UpdatedAt = now
	copyUser := *user
	s.users[user.ID] = &copyUser
	return nil
}

func (s *memoryStores) Update(_ context.Context, user *model.User) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.users[user.ID]; !ok {
		return store.ErrNotFound
	}
	user.UpdatedAt = time.Now().UTC()
	copyUser := *user
	s.users[user.ID] = &copyUser
	return nil
}

func (s *memoryStores) CreateRoom(_ context.Context, room *model.Room) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if room.ID == "" {
		room.ID = uuid.NewString()
	}
	if room.Visibility == "" {
		room.Visibility = model.RoomVisibilityPublic
	}
	if room.CreatedAt.IsZero() {
		room.CreatedAt = now
	}
	room.UpdatedAt = now
	copyRoom := *room
	s.rooms[room.ID] = &copyRoom
	return nil
}

func (s *memoryStores) GetRoomByID(_ context.Context, id string) (*model.Room, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	room, ok := s.rooms[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	copyRoom := *room
	return &copyRoom, nil
}

func (s *memoryStores) ListRooms(_ context.Context, opts store.ListRoomsOpts) ([]*model.Room, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var rooms []*model.Room
	query := strings.ToLower(strings.TrimSpace(opts.Query))
	for _, room := range s.rooms {
		if opts.OwnerID != "" && room.OwnerID != opts.OwnerID {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(room.Name), query) {
			continue
		}
		copyRoom := *room
		rooms = append(rooms, &copyRoom)
	}
	total := len(rooms)
	offset := opts.Offset
	if offset > len(rooms) {
		offset = len(rooms)
	}
	limit := opts.Limit
	if limit <= 0 || offset+limit > len(rooms) {
		limit = len(rooms) - offset
	}
	return rooms[offset : offset+limit], total, nil
}

func (s *memoryStores) UpdateRoom(_ context.Context, room *model.Room) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rooms[room.ID]; !ok {
		return store.ErrNotFound
	}
	room.UpdatedAt = time.Now().UTC()
	copyRoom := *room
	s.rooms[room.ID] = &copyRoom
	return nil
}

func (s *memoryStores) DeleteRoom(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.rooms[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.rooms, id)
	return nil
}

type memoryRoomStore struct{ *memoryStores }

func (s memoryRoomStore) Create(ctx context.Context, room *model.Room) error {
	return s.CreateRoom(ctx, room)
}

func (s memoryRoomStore) GetByID(ctx context.Context, id string) (*model.Room, error) {
	return s.GetRoomByID(ctx, id)
}

func (s memoryRoomStore) List(ctx context.Context, opts store.ListRoomsOpts) ([]*model.Room, int, error) {
	return s.ListRooms(ctx, opts)
}

func (s memoryRoomStore) Update(ctx context.Context, room *model.Room) error {
	return s.UpdateRoom(ctx, room)
}

func (s memoryRoomStore) Delete(ctx context.Context, id string) error {
	return s.DeleteRoom(ctx, id)
}

// testHookEmailSender pretends email is enabled so register/reset code paths can run in tests without Resend.
type testHookEmailSender struct{}

func (testHookEmailSender) Enabled() bool { return true }

func (testHookEmailSender) SendVerificationCode(_ context.Context, _, _, _ string) (string, error) {
	return "test-email-id", nil
}
