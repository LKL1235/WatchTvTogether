package model

import "time"

type UserRole string

const (
	UserRoleAdmin UserRole = "admin"
	UserRoleUser  UserRole = "user"
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Nickname     string    `json:"nickname,omitempty"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	Role         UserRole  `json:"role"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type RoomVisibility string

const (
	RoomVisibilityPublic  RoomVisibility = "public"
	RoomVisibilityPrivate RoomVisibility = "private"
)

type Room struct {
	ID           string         `json:"id"`
	Name         string         `json:"name"`
	OwnerID      string         `json:"owner_id"`
	Visibility   RoomVisibility `json:"visibility"`
	PasswordHash string         `json:"-"`
	CurrentVideo string         `json:"current_video_id,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

type VideoStatus string

const (
	VideoStatusProcessing VideoStatus = "processing"
	VideoStatusReady      VideoStatus = "ready"
	VideoStatusError      VideoStatus = "error"
)

type Video struct {
	ID         string      `json:"id"`
	Title      string      `json:"title"`
	FilePath   string      `json:"file_path"`
	PosterPath string      `json:"poster_path,omitempty"`
	Duration   float64     `json:"duration"`
	Format     string      `json:"format"`
	Size       int64       `json:"size"`
	SourceURL  string      `json:"source_url,omitempty"`
	Status     VideoStatus `json:"status"`
	CreatedAt  time.Time   `json:"created_at"`
	UpdatedAt  time.Time   `json:"updated_at"`
}

type DownloadTaskStatus string

const (
	DownloadTaskPending   DownloadTaskStatus = "pending"
	DownloadTaskRunning   DownloadTaskStatus = "running"
	DownloadTaskCompleted DownloadTaskStatus = "completed"
	DownloadTaskFailed    DownloadTaskStatus = "failed"
	DownloadTaskCanceled  DownloadTaskStatus = "canceled"
)

type DownloadTask struct {
	ID        string             `json:"id"`
	UserID    string             `json:"user_id"`
	SourceURL string             `json:"source_url"`
	VideoID   string             `json:"video_id,omitempty"`
	Progress  float64            `json:"progress"`
	Status    DownloadTaskStatus `json:"status"`
	Error     string             `json:"error,omitempty"`
	CreatedAt time.Time          `json:"created_at"`
	UpdatedAt time.Time          `json:"updated_at"`
}

type PlaybackAction string

const (
	PlaybackActionPlay   PlaybackAction = "play"
	PlaybackActionPause  PlaybackAction = "pause"
	PlaybackActionSeek   PlaybackAction = "seek"
	PlaybackActionNext   PlaybackAction = "next"
	PlaybackActionSwitch PlaybackAction = "switch"
)

type RoomState struct {
	RoomID    string         `json:"room_id"`
	VideoID   string         `json:"video_id,omitempty"`
	Queue     []string       `json:"queue,omitempty"`
	Action    PlaybackAction `json:"action"`
	Position  float64        `json:"position"`
	UpdatedBy string         `json:"updated_by,omitempty"`
	UpdatedAt time.Time      `json:"updated_at"`
}
