package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

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
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id, email, username, password_hash, nickname, avatar_url, role, created_at, updated_at FROM users WHERE id = $1`, id))
}

func (s *UserStore) GetByUsername(ctx context.Context, username string) (*model.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id, email, username, password_hash, nickname, avatar_url, role, created_at, updated_at FROM users WHERE LOWER(username) = LOWER($1)`, username))
}

func (s *UserStore) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `SELECT id, email, username, password_hash, nickname, avatar_url, role, created_at, updated_at FROM users WHERE LOWER(email) = LOWER($1)`, email))
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO users (id, email, username, password_hash, nickname, avatar_url, role, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		user.ID, user.Email, user.Username, user.PasswordHash, user.Nickname, user.AvatarURL, string(user.Role), user.CreatedAt, user.UpdatedAt)
	return wrapConstraint(err)
}

func (s *UserStore) Update(ctx context.Context, user *model.User) error {
	user.UpdatedAt = utcNow()
	res, err := s.db.ExecContext(ctx, `UPDATE users SET email = $1, username = $2, password_hash = $3, nickname = $4, avatar_url = $5, role = $6, updated_at = $7 WHERE id = $8`,
		user.Email, user.Username, user.PasswordHash, user.Nickname, user.AvatarURL, string(user.Role), user.UpdatedAt, user.ID)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO rooms (id, name, owner_id, visibility, password_hash, current_video_id, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		room.ID, room.Name, room.OwnerID, string(room.Visibility), room.PasswordHash, room.CurrentVideo, room.CreatedAt, room.UpdatedAt)
	return wrapConstraint(err)
}

func (s *RoomStore) GetByID(ctx context.Context, id string) (*model.Room, error) {
	return scanRoom(s.db.QueryRowContext(ctx, `SELECT id, name, owner_id, visibility, password_hash, current_video_id, created_at, updated_at FROM rooms WHERE id = $1`, id))
}

func (s *RoomStore) List(ctx context.Context, opts store.ListRoomsOpts) ([]*model.Room, int, error) {
	limit, offset := normalizePage(opts.Limit, opts.Offset)
	where := []string{}
	args := []any{}
	if opts.Query != "" {
		args = append(args, "%"+opts.Query+"%")
		where = append(where, fmt.Sprintf("name ILIKE $%d", len(args)))
	}
	if opts.OwnerID != "" {
		args = append(args, opts.OwnerID)
		where = append(where, fmt.Sprintf("owner_id = $%d", len(args)))
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM rooms`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, owner_id, visibility, password_hash, current_video_id, created_at, updated_at FROM rooms`+whereSQL+fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
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
	res, err := s.db.ExecContext(ctx, `UPDATE rooms SET name = $1, owner_id = $2, visibility = $3, password_hash = $4, current_video_id = $5, updated_at = $6 WHERE id = $7`,
		room.Name, room.OwnerID, string(room.Visibility), room.PasswordHash, room.CurrentVideo, room.UpdatedAt, room.ID)
	if err != nil {
		return wrapConstraint(err)
	}
	return requireRows(res)
}

func (s *RoomStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM rooms WHERE id = $1`, id)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO videos (id, title, file_path, poster_path, duration, format, size, source_url, status, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`,
		video.ID, video.Title, video.FilePath, video.PosterPath, video.Duration, video.Format, video.Size, video.SourceURL, string(video.Status), video.CreatedAt, video.UpdatedAt)
	return wrapConstraint(err)
}

func (s *VideoStore) GetByID(ctx context.Context, id string) (*model.Video, error) {
	return scanVideo(s.db.QueryRowContext(ctx, `SELECT id, title, file_path, poster_path, duration, format, size, source_url, status, created_at, updated_at FROM videos WHERE id = $1`, id))
}

func (s *VideoStore) List(ctx context.Context, opts store.ListVideosOpts) ([]*model.Video, int, error) {
	limit, offset := normalizePage(opts.Limit, opts.Offset)
	where := []string{}
	args := []any{}
	if opts.Status != "" {
		args = append(args, string(opts.Status))
		where = append(where, fmt.Sprintf("status = $%d", len(args)))
	}
	if opts.Query != "" {
		args = append(args, "%"+opts.Query+"%")
		where = append(where, fmt.Sprintf("title ILIKE $%d", len(args)))
	}
	whereSQL := ""
	if len(where) > 0 {
		whereSQL = " WHERE " + strings.Join(where, " AND ")
	}

	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM videos`+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, `SELECT id, title, file_path, poster_path, duration, format, size, source_url, status, created_at, updated_at FROM videos`+whereSQL+fmt.Sprintf(` ORDER BY created_at DESC LIMIT $%d OFFSET $%d`, len(args)-1, len(args)), args...)
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

func (s *VideoStore) Update(ctx context.Context, video *model.Video) error {
	video.UpdatedAt = utcNow()
	res, err := s.db.ExecContext(ctx, `UPDATE videos SET title = $1, file_path = $2, poster_path = $3, duration = $4, format = $5, size = $6, source_url = $7, status = $8, updated_at = $9 WHERE id = $10`,
		video.Title, video.FilePath, video.PosterPath, video.Duration, video.Format, video.Size, video.SourceURL, string(video.Status), video.UpdatedAt, video.ID)
	if err != nil {
		return wrapConstraint(err)
	}
	return requireRows(res)
}

func (s *VideoStore) UpdateStatus(ctx context.Context, id string, status model.VideoStatus) error {
	res, err := s.db.ExecContext(ctx, `UPDATE videos SET status = $1, updated_at = $2 WHERE id = $3`, string(status), utcNow(), id)
	if err != nil {
		return err
	}
	return requireRows(res)
}

func (s *VideoStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM videos WHERE id = $1`, id)
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
	_, err := s.db.ExecContext(ctx, `INSERT INTO download_tasks (id, user_id, source_url, video_id, progress, status, error, created_at, updated_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		task.ID, task.UserID, task.SourceURL, task.VideoID, task.Progress, string(task.Status), task.Error, task.CreatedAt, task.UpdatedAt)
	return wrapConstraint(err)
}

func (s *DownloadTaskStore) GetByID(ctx context.Context, id string) (*model.DownloadTask, error) {
	return scanDownloadTask(s.db.QueryRowContext(ctx, `SELECT id, user_id, source_url, video_id, progress, status, error, created_at, updated_at FROM download_tasks WHERE id = $1`, id))
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
	res, err := s.db.ExecContext(ctx, `UPDATE download_tasks SET progress = $1, status = $2, updated_at = $3 WHERE id = $4`, progress, string(status), utcNow(), id)
	if err != nil {
		return err
	}
	return requireRows(res)
}

func (s *DownloadTaskStore) UpdateResult(ctx context.Context, task *model.DownloadTask) error {
	task.UpdatedAt = utcNow()
	res, err := s.db.ExecContext(ctx, `UPDATE download_tasks SET video_id = $1, progress = $2, status = $3, error = $4, updated_at = $5 WHERE id = $6`,
		task.VideoID, task.Progress, string(task.Status), task.Error, task.UpdatedAt, task.ID)
	if err != nil {
		return err
	}
	return requireRows(res)
}

func (s *DownloadTaskStore) Delete(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM download_tasks WHERE id = $1`, id)
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
	if err := scanner.Scan(&user.ID, &user.Email, &user.Username, &user.PasswordHash, &user.Nickname, &user.AvatarURL, &role, &user.CreatedAt, &user.UpdatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	user.Role = model.UserRole(role)
	return &user, nil
}

func scanRoom(scanner interface {
	Scan(dest ...any) error
}) (*model.Room, error) {
	var room model.Room
	var visibility string
	if err := scanner.Scan(&room.ID, &room.Name, &room.OwnerID, &visibility, &room.PasswordHash, &room.CurrentVideo, &room.CreatedAt, &room.UpdatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	room.Visibility = model.RoomVisibility(visibility)
	return &room, nil
}

func scanVideo(scanner interface {
	Scan(dest ...any) error
}) (*model.Video, error) {
	var video model.Video
	var status string
	if err := scanner.Scan(&video.ID, &video.Title, &video.FilePath, &video.PosterPath, &video.Duration, &video.Format, &video.Size, &video.SourceURL, &status, &video.CreatedAt, &video.UpdatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	video.Status = model.VideoStatus(status)
	return &video, nil
}

func scanDownloadTask(scanner interface {
	Scan(dest ...any) error
}) (*model.DownloadTask, error) {
	var task model.DownloadTask
	var status string
	if err := scanner.Scan(&task.ID, &task.UserID, &task.SourceURL, &task.VideoID, &task.Progress, &status, &task.Error, &task.CreatedAt, &task.UpdatedAt); err != nil {
		return nil, wrapNotFound(err)
	}
	task.Status = model.DownloadTaskStatus(status)
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
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
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
