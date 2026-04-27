package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"watchtogether/internal/model"
	"watchtogether/internal/store"
)

type UserStore struct {
	db *sql.DB
}

func NewUserStore(db *sql.DB) *UserStore {
	return &UserStore{db: db}
}

func (s *UserStore) GetByID(ctx context.Context, id string) (*model.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, nickname, avatar_url, role, created_at, updated_at FROM users WHERE id = ?`, id))
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id, username, password_hash, nickname, avatar_url, role, created_at, updated_at FROM users WHERE username = ?`, username))
}

func (s *UserStore) Create(ctx context.Context, user *model.User) error {
	now := utcNow()
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (id, username, password_hash, nickname, avatar_url, role, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		user.ID, user.Username, user.PasswordHash, user.Nickname, user.AvatarURL, string(user.Role), formatTime(user.CreatedAt), formatTime(user.UpdatedAt))
	return wrapConstraint(err)
}

func (s *UserStore) Update(ctx context.Context, user *model.User) error {
	user.UpdatedAt = utcNow()
	res, err := s.db.ExecContext(ctx, `UPDATE users SET username = ?, password_hash = ?, nickname = ?, avatar_url = ?, role = ?, updated_at = ? WHERE id = ?`,
		user.Username, user.PasswordHash, user.Nickname, user.AvatarURL, string(user.Role), formatTime(user.UpdatedAt), user.ID)
	if err != nil {
		return wrapConstraint(err)
	}
	return requireRows(res)
}

type RoomStore struct {
	db *sql.DB
}

func NewRoomStore(db *sql.DB) *RoomStore {
	return &RoomStore{db: db}
}

func (s *RoomStore) Create(ctx context.Context, room *model.Room) error {
	now := utcNow()
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO rooms (id, name, owner_id, visibility, password_hash, current_video_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		room.ID, room.Name, room.OwnerID, string(room.Visibility), room.PasswordHash, room.CurrentVideo, formatTime(room.CreatedAt), formatTime(room.UpdatedAt))
	return wrapConstraint(err)
}

func (s *RoomStore) GetByID(ctx context.Context, id string) (*model.Room, error) {
	return scanRoom(s.db.QueryRowContext(ctx, `SELECT id, name, owner_id, visibility, password_hash, current_video_id, created_at, updated_at FROM rooms WHERE id = ?`, id))
}

func (s *RoomStore) List(ctx context.Context, opts store.ListRoomsOpts) ([]*model.Room, int, error) {
	limit, offset := normalizePage(opts.Limit, opts.Offset)
	where := []string{}
	args := []any{}
	if opts.Query != "" {
		where = append(where, "name LIKE ?")
		args = append(args, "%"+opts.Query+"%")
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rooms`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, owner_id, visibility, password_hash, current_video_id, created_at, updated_at FROM rooms`+whereSQL+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	rooms := []*model.Room{}
	for rows.Next() {
		room, err := scanRoom(rows)
		if err != nil {
			return nil, 0, err
		}
		rooms = append(rooms, room)
	}
	return rooms, total, rows.Err()
}

func (s *RoomStore) Update(ctx context.Context, room *model.Room) error {
	room.UpdatedAt = utcNow()
	res, err := s.db.ExecContext(ctx, `UPDATE rooms SET name = ?, owner_id = ?, visibility = ?, password_hash = ?, current_video_id = ?, updated_at = ? WHERE id = ?`,
		room.Name, room.OwnerID, string(room.Visibility), room.PasswordHash, room.CurrentVideo, formatTime(room.UpdatedAt), room.ID)
	if err != nil {
		return wrapConstraint(err)
	}
	return requireRows(res)
}

func (s *RoomStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM rooms WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRows(res)
}

type VideoStore struct {
	db *sql.DB
}

func NewVideoStore(db *sql.DB) *VideoStore {
	return &VideoStore{db: db}
}

func (s *VideoStore) Create(ctx context.Context, video *model.Video) error {
	now := utcNow()
	if video.ID == "" {
		video.ID = uuid.NewString()
	}
	if video.Status == "" {
		video.Status = model.VideoStatusProcessing
	}
	if video.CreatedAt.IsZero() {
		video.CreatedAt = now
	}
	video.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO videos (id, title, file_path, poster_path, duration, format, size, source_url, status, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		video.ID, video.Title, video.FilePath, video.PosterPath, video.Duration, video.Format, video.Size, video.SourceURL, string(video.Status), formatTime(video.CreatedAt), formatTime(video.UpdatedAt))
	return wrapConstraint(err)
}

func (s *VideoStore) GetByID(ctx context.Context, id string) (*model.Video, error) {
	return scanVideo(s.db.QueryRowContext(ctx, `SELECT id, title, file_path, poster_path, duration, format, size, source_url, status, created_at, updated_at FROM videos WHERE id = ?`, id))
}

func (s *VideoStore) List(ctx context.Context, opts store.ListVideosOpts) ([]*model.Video, int, error) {
	limit, offset := normalizePage(opts.Limit, opts.Offset)
	where := []string{}
	args := []any{}
	if opts.Status != "" {
		where = append(where, "status = ?")
		args = append(args, opts.Status)
	}
	if opts.Query != "" {
		where = append(where, "title LIKE ?")
		args = append(args, "%"+opts.Query+"%")
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, file_path, poster_path, duration, format, size, source_url, status, created_at, updated_at FROM videos`+whereSQL+` ORDER BY created_at DESC LIMIT ? OFFSET ?`, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	videos := []*model.Video{}
	for rows.Next() {
		video, err := scanVideo(rows)
		if err != nil {
			return nil, 0, err
		}
		videos = append(videos, video)
	}
	return videos, total, rows.Err()
}

func (s *VideoStore) UpdateStatus(ctx context.Context, id string, status model.VideoStatus) error {
	res, err := s.db.ExecContext(ctx, `UPDATE videos SET status = ?, updated_at = ? WHERE id = ?`, string(status), formatTime(utcNow()), id)
	if err != nil {
		return err
	}
	return requireRows(res)
}

func (s *VideoStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM videos WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRows(res)
}

type DownloadTaskStore struct {
	db *sql.DB
}

func NewDownloadTaskStore(db *sql.DB) *DownloadTaskStore {
	return &DownloadTaskStore{db: db}
}

func (s *DownloadTaskStore) Create(ctx context.Context, task *model.DownloadTask) error {
	now := utcNow()
	if task.ID == "" {
		task.ID = uuid.NewString()
	}
	if task.Status == "" {
		task.Status = model.DownloadTaskPending
	}
	if task.CreatedAt.IsZero() {
		task.CreatedAt = now
	}
	task.UpdatedAt = now
	_, err := s.db.ExecContext(ctx, `INSERT INTO download_tasks (id, user_id, source_url, video_id, progress, status, error, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		task.ID, task.UserID, task.SourceURL, task.VideoID, task.Progress, string(task.Status), task.Error, formatTime(task.CreatedAt), formatTime(task.UpdatedAt))
	return wrapConstraint(err)
}

func (s *DownloadTaskStore) GetByID(ctx context.Context, id string) (*model.DownloadTask, error) {
	return scanDownloadTask(s.db.QueryRowContext(ctx, `SELECT id, user_id, source_url, video_id, progress, status, error, created_at, updated_at FROM download_tasks WHERE id = ?`, id))
}

func (s *DownloadTaskStore) List(ctx context.Context) ([]*model.DownloadTask, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, user_id, source_url, video_id, progress, status, error, created_at, updated_at FROM download_tasks ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	tasks := []*model.DownloadTask{}
	for rows.Next() {
		task, err := scanDownloadTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, task)
	}
	return tasks, rows.Err()
}

func (s *DownloadTaskStore) UpdateProgress(ctx context.Context, id string, progress float64, status model.DownloadTaskStatus) error {
	res, err := s.db.ExecContext(ctx, `UPDATE download_tasks SET progress = ?, status = ?, updated_at = ? WHERE id = ?`, progress, string(status), formatTime(utcNow()), id)
	if err != nil {
		return err
	}
	return requireRows(res)
}

func (s *DownloadTaskStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM download_tasks WHERE id = ?`, id)
	if err != nil {
		return err
	}
	return requireRows(res)
}

func scanUser(scanner interface {
	Scan(dest ...any) error
}) (*model.User, error) {
	var user model.User
	var role string
	var createdAt, updatedAt string
	if err := scanner.Scan(&user.ID, &user.Username, &user.PasswordHash, &user.Nickname, &user.AvatarURL, &role, &createdAt, &updatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	user.Role = model.UserRole(role)
	user.CreatedAt = parseTime(createdAt)
	user.UpdatedAt = parseTime(updatedAt)
	return &user, nil
}

func scanRoom(scanner interface {
	Scan(dest ...any) error
}) (*model.Room, error) {
	var room model.Room
	var visibility string
	var createdAt, updatedAt string
	if err := scanner.Scan(&room.ID, &room.Name, &room.OwnerID, &visibility, &room.PasswordHash, &room.CurrentVideo, &createdAt, &updatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	room.Visibility = model.RoomVisibility(visibility)
	room.CreatedAt = parseTime(createdAt)
	room.UpdatedAt = parseTime(updatedAt)
	return &room, nil
}

func scanVideo(scanner interface {
	Scan(dest ...any) error
}) (*model.Video, error) {
	var video model.Video
	var status string
	var createdAt, updatedAt string
	if err := scanner.Scan(&video.ID, &video.Title, &video.FilePath, &video.PosterPath, &video.Duration, &video.Format, &video.Size, &video.SourceURL, &status, &createdAt, &updatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	video.Status = model.VideoStatus(status)
	video.CreatedAt = parseTime(createdAt)
	video.UpdatedAt = parseTime(updatedAt)
	return &video, nil
}

func scanDownloadTask(scanner interface {
	Scan(dest ...any) error
}) (*model.DownloadTask, error) {
	var task model.DownloadTask
	var status string
	var createdAt, updatedAt string
	if err := scanner.Scan(&task.ID, &task.UserID, &task.SourceURL, &task.VideoID, &task.Progress, &status, &task.Error, &createdAt, &updatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	task.Status = model.DownloadTaskStatus(status)
	task.CreatedAt = parseTime(createdAt)
	task.UpdatedAt = parseTime(updatedAt)
	return &task, nil
}

func normalizePage(limit, offset int) (int, int) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}

func utcNow() time.Time {
	return time.Now().UTC().Truncate(time.Microsecond)
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339Nano)
}

func parseTime(value string) time.Time {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return t
}

func wrapNotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return store.ErrNotFound
	}
	return err
}

func wrapConstraint(err error) error {
	if err == nil {
		return nil
	}
	if strings.Contains(err.Error(), "constraint failed") || strings.Contains(err.Error(), "UNIQUE constraint failed") {
		return fmt.Errorf("%w: %v", store.ErrConflict, err)
	}
	return err
}

func requireRows(res sql.Result) error {
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return store.ErrNotFound
	}
	return nil
}
