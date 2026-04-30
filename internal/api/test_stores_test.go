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
	mu            sync.RWMutex
	users         map[string]*model.User
	rooms         map[string]*model.Room
	videos        map[string]*model.Video
	downloadTasks map[string]*model.DownloadTask
}

func newMemoryStores() *memoryStores {
	return &memoryStores{
		users:         make(map[string]*model.User),
		rooms:         make(map[string]*model.Room),
		videos:        make(map[string]*model.Video),
		downloadTasks: make(map[string]*model.DownloadTask),
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
	cfg.StorageDir = "."
	cfg.PosterDir = "."
	return Dependencies{
		Config:            cfg,
		UserStore:         stores,
		RoomStore:         memoryRoomStore{stores},
		VideoStore:        memoryVideoStore{stores},
		DownloadTaskStore: memoryDownloadTaskStore{stores},
		EmailSender:       testHookEmailSender{},
		EmailCodes:        emailcode.NewStore(nil),
		SessionCache:      memory.NewSessionCache(),
		RoomStateCache:    memory.NewRoomStateCache(),
		RoomPresence:      memory.NewRoomPresence(),
		RoomAccess:        memory.NewRoomAccess(),
		PubSub:            memory.NewPubSub(),
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

func (s *memoryStores) CreateVideo(_ context.Context, video *model.Video) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if video.ID == "" {
		video.ID = uuid.NewString()
	}
	if video.CreatedAt.IsZero() {
		video.CreatedAt = now
	}
	video.UpdatedAt = now
	copyVideo := *video
	s.videos[video.ID] = &copyVideo
	return nil
}

func (s *memoryStores) GetVideoByID(_ context.Context, id string) (*model.Video, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	video, ok := s.videos[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	copyVideo := *video
	return &copyVideo, nil
}

func (s *memoryStores) ListVideos(_ context.Context, opts store.ListVideosOpts) ([]*model.Video, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var videos []*model.Video
	query := strings.ToLower(strings.TrimSpace(opts.Query))
	for _, video := range s.videos {
		if opts.Status != "" && video.Status != opts.Status {
			continue
		}
		if query != "" && !strings.Contains(strings.ToLower(video.Title), query) {
			continue
		}
		copyVideo := *video
		videos = append(videos, &copyVideo)
	}
	total := len(videos)
	offset := opts.Offset
	if offset > len(videos) {
		offset = len(videos)
	}
	limit := opts.Limit
	if limit <= 0 || offset+limit > len(videos) {
		limit = len(videos) - offset
	}
	return videos[offset : offset+limit], total, nil
}

func (s *memoryStores) UpdateVideo(_ context.Context, video *model.Video) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.videos[video.ID]; !ok {
		return store.ErrNotFound
	}
	video.UpdatedAt = time.Now().UTC()
	copyVideo := *video
	s.videos[video.ID] = &copyVideo
	return nil
}

func (s *memoryStores) UpdateVideoStatus(_ context.Context, id string, status model.VideoStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	video, ok := s.videos[id]
	if !ok {
		return store.ErrNotFound
	}
	copyVideo := *video
	copyVideo.Status = status
	copyVideo.UpdatedAt = time.Now().UTC()
	s.videos[id] = &copyVideo
	return nil
}

func (s *memoryStores) DeleteVideo(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.videos[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.videos, id)
	return nil
}

type memoryVideoStore struct{ *memoryStores }

func (s memoryVideoStore) Create(ctx context.Context, video *model.Video) error {
	return s.CreateVideo(ctx, video)
}

func (s memoryVideoStore) GetByID(ctx context.Context, id string) (*model.Video, error) {
	return s.GetVideoByID(ctx, id)
}

func (s memoryVideoStore) List(ctx context.Context, opts store.ListVideosOpts) ([]*model.Video, int, error) {
	return s.ListVideos(ctx, opts)
}

func (s memoryVideoStore) Update(ctx context.Context, video *model.Video) error {
	return s.UpdateVideo(ctx, video)
}

func (s memoryVideoStore) UpdateStatus(ctx context.Context, id string, status model.VideoStatus) error {
	return s.UpdateVideoStatus(ctx, id, status)
}

func (s memoryVideoStore) Delete(ctx context.Context, id string) error {
	return s.DeleteVideo(ctx, id)
}

func (s *memoryStores) CreateDownloadTask(_ context.Context, task *model.DownloadTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now().UTC()
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	copyTask := *task
	s.downloadTasks[task.ID] = &copyTask
	return nil
}

func (s *memoryStores) GetDownloadTaskByID(_ context.Context, id string) (*model.DownloadTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	task, ok := s.downloadTasks[id]
	if !ok {
		return nil, store.ErrNotFound
	}
	copyTask := *task
	return &copyTask, nil
}

func (s *memoryStores) ListDownloadTasks(_ context.Context) ([]*model.DownloadTask, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	tasks := make([]*model.DownloadTask, 0, len(s.downloadTasks))
	for _, task := range s.downloadTasks {
		copyTask := *task
		tasks = append(tasks, &copyTask)
	}
	return tasks, nil
}

func (s *memoryStores) UpdateDownloadTaskProgress(_ context.Context, id string, progress float64, status model.DownloadTaskStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	task, ok := s.downloadTasks[id]
	if !ok {
		return store.ErrNotFound
	}
	copyTask := *task
	copyTask.Progress = progress
	copyTask.Status = status
	copyTask.UpdatedAt = time.Now().UTC()
	s.downloadTasks[id] = &copyTask
	return nil
}

func (s *memoryStores) UpdateDownloadTaskResult(_ context.Context, task *model.DownloadTask) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.downloadTasks[task.ID]; !ok {
		return store.ErrNotFound
	}
	task.UpdatedAt = time.Now().UTC()
	copyTask := *task
	s.downloadTasks[task.ID] = &copyTask
	return nil
}

func (s *memoryStores) DeleteDownloadTask(_ context.Context, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.downloadTasks[id]; !ok {
		return store.ErrNotFound
	}
	delete(s.downloadTasks, id)
	return nil
}

type memoryDownloadTaskStore struct{ *memoryStores }

func (s memoryDownloadTaskStore) Create(ctx context.Context, task *model.DownloadTask) error {
	return s.CreateDownloadTask(ctx, task)
}

func (s memoryDownloadTaskStore) GetByID(ctx context.Context, id string) (*model.DownloadTask, error) {
	return s.GetDownloadTaskByID(ctx, id)
}

func (s memoryDownloadTaskStore) List(ctx context.Context) ([]*model.DownloadTask, error) {
	return s.ListDownloadTasks(ctx)
}

func (s memoryDownloadTaskStore) UpdateProgress(ctx context.Context, id string, progress float64, status model.DownloadTaskStatus) error {
	return s.UpdateDownloadTaskProgress(ctx, id, progress, status)
}

func (s memoryDownloadTaskStore) UpdateResult(ctx context.Context, task *model.DownloadTask) error {
	return s.UpdateDownloadTaskResult(ctx, task)
}

func (s memoryDownloadTaskStore) Delete(ctx context.Context, id string) error {
	return s.DeleteDownloadTask(ctx, id)
}

// testHookEmailSender pretends email is enabled so register/reset code paths can run in tests without Resend.
type testHookEmailSender struct{}

func (testHookEmailSender) Enabled() bool { return true }

func (testHookEmailSender) SendVerificationCode(_ context.Context, _, _, _ string) (string, error) {
	return "test-email-id", nil
}
