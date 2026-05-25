package room

import (
	"context"
	"errors"
	"log"
	"strings"
	"time"

	"watchtogether/internal/cache"
	"watchtogether/internal/model"
	"watchtogether/internal/store"
)

var ErrForbidden = errors.New("forbidden")

// ErrStaleControl is returned when client control_version is older than server state.
var ErrStaleControl = errors.New("stale control version")

type User struct {
	ID       string         `json:"id"`
	Username string         `json:"username"`
	Nickname string         `json:"nickname,omitempty"`
	Role     model.UserRole `json:"role"`
	IsOwner  bool           `json:"is_owner"`
}

type Message struct {
	Type            string               `json:"type"`
	Action          model.PlaybackAction `json:"action,omitempty"`
	Event           string               `json:"event,omitempty"`
	Position        float64              `json:"position,omitempty"`
	VideoID         string               `json:"video_id,omitempty"`
	Queue           []string             `json:"queue,omitempty"`
	Timestamp       int64                `json:"timestamp,omitempty"`
	ControlVersion  int64                `json:"control_version,omitempty"`
	PlaybackMode    model.PlaybackMode   `json:"playback_mode,omitempty"`
	UpdatedAt       int64                `json:"updated_at,omitempty"`
	Payload         any                  `json:"payload,omitempty"`
	User            *User                `json:"user,omitempty"`
}

type Snapshot struct {
	RoomID      string           `json:"room_id"`
	State       *model.RoomState `json:"state,omitempty"`
	Queue       []string         `json:"queue"`
	ViewerCount int              `json:"viewer_count"`
}

type Publisher interface {
	PublishRoomMessage(ctx context.Context, roomID string, name string, data any) error
}

type Service struct {
	states     cache.RoomStateCache
	presence   cache.RoomPresence
	rooms      store.RoomStore
	roomAccess cache.RoomAccessCache
	publisher  Publisher
	roomChat   cache.RoomChat
	chat       ChatLimits
	now        func() time.Time
}

func NewService(states cache.RoomStateCache, presence cache.RoomPresence, rooms store.RoomStore, roomAccess cache.RoomAccessCache, publisher Publisher, roomChat cache.RoomChat, chat ChatLimits) *Service {
	return &Service{
		states:     states,
		presence:   presence,
		rooms:      rooms,
		roomAccess: roomAccess,
		publisher:  publisher,
		roomChat:   roomChat,
		chat:       chat,
		now:        time.Now,
	}
}

func (s *Service) memberCount(ctx context.Context, roomID string) int {
	if s.presence == nil {
		return 0
	}
	n, err := s.presence.MemberCount(ctx, roomID)
	if err != nil {
		return 0
	}
	return n
}

// Snapshot returns playback snapshot with projected position for play state.
func (s *Service) Snapshot(ctx context.Context, roomID string) Snapshot {
	state, _ := s.ProjectedRoomState(ctx, roomID)
	snap := Snapshot{
		RoomID:      roomID,
		Queue:       []string{},
		ViewerCount: s.memberCount(ctx, roomID),
	}
	if state != nil {
		snap.State = state
		snap.Queue = append(snap.Queue, state.Queue...)
		if len(snap.Queue) == 0 && state.VideoID != "" {
			snap.Queue = []string{state.VideoID}
		}
	}
	return snap
}

// ProjectedRoomState loads Redis state and applies time-based progress projection when playing.
func (s *Service) ProjectedRoomState(ctx context.Context, roomID string) (*model.RoomState, error) {
	state, err := s.states.GetRoomState(ctx, roomID)
	if err != nil {
		return nil, err
	}
	return s.projectState(ctx, state), nil
}

func (s *Service) projectState(ctx context.Context, base *model.RoomState) *model.RoomState {
	if base == nil {
		return nil
	}
	out := *base
	baseUpdated := base.UpdatedAt
	duration := base.VideoDuration
	now := s.now().UTC()
	pos, atEnd := ProjectedPlayback(&out, now, duration)
	out.Position = pos
	out.UpdatedAt = now
	out.BaseUpdatedAt = baseUpdated
	if atEnd && duration > 0 {
		mode := out.PlaybackMode
		if mode == "" {
			mode = model.PlaybackModeSequential
		}
		if nextID, ok := NextVideoAfterEnd(base.VideoID, out.Queue, mode); ok {
			out.VideoID = nextID
			out.Position = 0
			out.Action = model.PlaybackActionPause
			out.VideoDuration = 0
		} else {
			out.Action = model.PlaybackActionPause
			out.Position = duration
		}
	}
	return &out
}

func resolveControlDuration(msg ControlInput, prev *model.RoomState, videoID string) float64 {
	if msg.VideoDuration > 0 {
		return msg.VideoDuration
	}
	if prev != nil && videoID != "" && videoID == prev.VideoID && prev.VideoDuration > 0 {
		return prev.VideoDuration
	}
	return 0
}

// Join adds the user to room presence; returns previous room id if the user was moved.
func (s *Service) Join(ctx context.Context, roomID string, user User) (previousRoomID string, err error) {
	if s.presence == nil {
		return "", nil
	}
	return s.presence.JoinRoom(ctx, roomID, user.ID, user.Username)
}

// Leave removes the user from room presence.
func (s *Service) Leave(ctx context.Context, roomID string, userID string) error {
	if s.presence == nil {
		return nil
	}
	return s.presence.LeaveRoom(ctx, roomID, userID)
}

type ControlInput struct {
	Action         model.PlaybackAction
	Position       float64
	VideoID        string
	Queue          []string
	PlaybackMode   model.PlaybackMode
	ClientVersion  int64
	VideoDuration  float64
}

func (s *Service) ApplyControl(ctx context.Context, roomID string, user User, msg ControlInput) (Message, error) {
	if user.Role != model.UserRoleAdmin && !user.IsOwner {
		return Message{}, ErrForbidden
	}
	now := s.now().UTC()
	prev, err := s.states.GetRoomState(ctx, roomID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return Message{}, err
	}
	if prev != nil && msg.ClientVersion > 0 && msg.ClientVersion < prev.ControlVersion {
		return Message{}, ErrStaleControl
	}

	queue := append([]string(nil), msg.Queue...)
	videoID := strings.TrimSpace(msg.VideoID)
	mode := model.PlaybackModeSequential
	if prev != nil && prev.PlaybackMode != "" {
		mode = prev.PlaybackMode
	}
	if msg.PlaybackMode == model.PlaybackModeSequential || msg.PlaybackMode == model.PlaybackModeLoop {
		mode = msg.PlaybackMode
	}

	switch msg.Action {
	case model.PlaybackActionNext:
		baseVID := videoID
		if baseVID == "" && prev != nil {
			baseVID = prev.VideoID
		}
		if len(queue) == 0 && prev != nil {
			queue = append(queue, prev.Queue...)
		}
		if nextID, ok := nextInQueue(baseVID, queue, mode); ok {
			videoID = nextID
			msg.Position = 0
			msg.Action = model.PlaybackActionPause
		}
	case model.PlaybackActionSwitch:
		if videoID == "" && len(queue) > 0 {
			videoID = queue[0]
		}
	}

	if len(queue) == 0 && videoID != "" {
		queue = []string{videoID}
	}

	cv := int64(1)
	if prev != nil {
		cv = prev.ControlVersion + 1
	}

	duration := resolveControlDuration(msg, prev, videoID)

	state := &model.RoomState{
		RoomID:         roomID,
		VideoID:        videoID,
		Queue:          queue,
		Action:         msg.Action,
		Position:       msg.Position,
		PlaybackMode:   mode,
		VideoDuration:  duration,
		ControlVersion: cv,
		UpdatedBy:      user.ID,
		UpdatedAt:      now,
	}
	if err := s.states.SetRoomState(ctx, roomID, state); err != nil {
		return Message{}, err
	}
	sync := Message{
		Type:            "sync",
		Action:          msg.Action,
		Position:        msg.Position,
		VideoID:         videoID,
		Queue:           queue,
		Timestamp:       now.Unix(),
		ControlVersion:  state.ControlVersion,
		PlaybackMode:    state.PlaybackMode,
		UpdatedAt:       state.UpdatedAt.Unix(),
		User:            &user,
	}
	if s.publisher != nil {
		return sync, s.publisher.PublishRoomMessage(ctx, roomID, "room.sync", sync)
	}
	return sync, nil
}

func nextInQueue(current string, queue []string, mode model.PlaybackMode) (string, bool) {
	if len(queue) == 0 {
		return "", false
	}
	idx := indexOf(queue, current)
	if idx < 0 {
		return queue[0], true
	}
	switch mode {
	case model.PlaybackModeLoop:
		next := idx + 1
		if next >= len(queue) {
			next = 0
		}
		return queue[next], true
	default:
		if idx+1 >= len(queue) {
			return "", false
		}
		return queue[idx+1], true
	}
}

// Destroy removes cached room state and presence and notifies clients.
func (s *Service) Destroy(ctx context.Context, roomID string, user *User) error {
	return s.CloseRoom(ctx, roomID, user)
}

// CloseRoom deletes Redis room state, presence, and notifies Ably.
func (s *Service) CloseRoom(ctx context.Context, roomID string, user *User) error {
	if err := s.states.DeleteRoomState(ctx, roomID); err != nil {
		return err
	}
	if s.presence != nil {
		_ = s.presence.DeleteRoomPresence(ctx, roomID)
	}
	if s.roomAccess != nil {
		_ = s.roomAccess.DeleteRoomAccess(ctx, roomID)
	}
	s.deleteRoomChat(ctx, roomID)
	return s.PublishRoomEvent(ctx, roomID, "room_closed", user)
}

func (s *Service) PublishRoomEvent(ctx context.Context, roomID string, event string, user *User) error {
	if s.publisher == nil {
		return nil
	}
	msg := Message{
		Type:  "room_event",
		Event: event,
		User:  user,
	}
	return s.publisher.PublishRoomMessage(ctx, roomID, "room.event", msg)
}

func (s *Service) KickMember(ctx context.Context, roomID string, targetUserID string, actor *User) error {
	if s.presence != nil {
		if err := s.presence.RemoveMember(ctx, roomID, targetUserID); err != nil {
			return err
		}
	}
	target := &User{ID: targetUserID}
	return s.PublishRoomEvent(ctx, roomID, "user_kicked", target)
}

const loginCleanupInterval = 5 * time.Minute

// MaybeRunGlobalCleanup runs empty-room cleanup when last run was long enough ago and lock acquired.
func (s *Service) MaybeRunGlobalCleanup(ctx context.Context) error {
	if s.presence == nil || s.rooms == nil {
		return nil
	}
	last, err := s.presence.GetLastRoomCleanupAt(ctx)
	if err != nil {
		return err
	}
	if !last.IsZero() && s.now().Sub(last) < loginCleanupInterval {
		return nil
	}
	ok, err := s.presence.TryAcquireCleanupLock(ctx, 2*time.Minute)
	if err != nil || !ok {
		return err
	}
	defer func() { _ = s.presence.ReleaseCleanupLock(ctx) }()

	lastAge := "首次"
	if !last.IsZero() {
		lastAge = s.now().Sub(last).Round(time.Second).String()
	}
	trigger := emptyRoomCleanupTrigger{
		lastCleanupAgo:    lastAge,
		minInterval:       loginCleanupInterval,
		lockAcquired:      true,
		triggeredByGlobal: true,
	}
	if err := s.RunEmptyRoomCleanup(ctx, trigger); err != nil {
		return err
	}
	_ = s.ProcessGlobalChatPending(ctx)
	return s.presence.SetLastRoomCleanupAt(ctx, s.now().UTC())
}

func mergeUniqueRoomIDs(parts ...[]string) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, part := range parts {
		for _, id := range part {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			out = append(out, id)
		}
	}
	return out
}

type emptyRoomCleanupTrigger struct {
	lastCleanupAgo    string
	minInterval       time.Duration
	lockAcquired      bool
	triggeredByGlobal bool
}

type emptyRoomCleanupEntry struct {
	roomID    string
	existence string
	online    int
}

func roomByIDFromList(rooms []*model.Room) map[string]*model.Room {
	out := make(map[string]*model.Room, len(rooms))
	for _, r := range rooms {
		if r != nil && r.ID != "" {
			out[r.ID] = r
		}
	}
	return out
}

func roomExistenceLabel(roomID string, byID map[string]*model.Room, now time.Time) string {
	r, ok := byID[roomID]
	if !ok || r == nil || r.CreatedAt.IsZero() {
		return "未知"
	}
	return now.Sub(r.CreatedAt).Round(time.Second).String()
}

func logEmptyRoomCleanupResults(cleaned, skipped []emptyRoomCleanupEntry, pendingN, activeN, dbN, candidatesN int, trigger emptyRoomCleanupTrigger) {
	for _, e := range cleaned {
		log.Printf("room cleanup: 已清理 room_id=%s 存在时间=%s 在线人数=%d", e.roomID, e.existence, e.online)
	}
	for _, e := range skipped {
		log.Printf("room cleanup: 不需清理 room_id=%s 存在时间=%s 在线人数=%d", e.roomID, e.existence, e.online)
	}
	lockState := "未获取"
	if trigger.lockAcquired {
		lockState = "已获取"
	}
	source := "手动或测试调用"
	if trigger.triggeredByGlobal {
		source = "登录后全局清理(MaybeRunGlobalCleanup)"
	}
	log.Printf(
		"room cleanup: 触发条件 来源=%s 距上次清理=%s 最小间隔=%s 清理锁=%s 扫描pending=%d 扫描active=%d 扫描db=%d 候选房间=%d 已清理=%d 不需清理=%d 规则=在线人数为0时关闭房间",
		source,
		trigger.lastCleanupAgo,
		trigger.minInterval,
		lockState,
		pendingN,
		activeN,
		dbN,
		candidatesN,
		len(cleaned),
		len(skipped),
	)
}

// RunEmptyRoomCleanup closes rooms with no online members: pending-empty set, active member sets, and DB-listed rooms.
func (s *Service) RunEmptyRoomCleanup(ctx context.Context, trigger ...emptyRoomCleanupTrigger) error {
	if s.presence == nil || s.rooms == nil {
		return nil
	}
	var trig emptyRoomCleanupTrigger
	if len(trigger) > 0 {
		trig = trigger[0]
	}
	pending, err := s.presence.PendingEmptyRooms(ctx)
	if err != nil {
		return err
	}
	active, err := s.presence.ListRoomsWithMembers(ctx)
	if err != nil {
		return err
	}
	dbRooms, _, err := s.rooms.List(ctx, store.ListRoomsOpts{Limit: 500, Offset: 0})
	if err != nil {
		return err
	}
	dbIDs := make([]string, 0, len(dbRooms))
	for _, r := range dbRooms {
		if r != nil {
			dbIDs = append(dbIDs, r.ID)
		}
	}
	byID := roomByIDFromList(dbRooms)
	now := s.now()
	candidates := mergeUniqueRoomIDs(pending, active, dbIDs)
	var cleaned, skipped []emptyRoomCleanupEntry
	for _, roomID := range candidates {
		n, err := s.presence.MemberCount(ctx, roomID)
		if err != nil {
			continue
		}
		entry := emptyRoomCleanupEntry{
			roomID:    roomID,
			existence: roomExistenceLabel(roomID, byID, now),
			online:    n,
		}
		if n != 0 {
			_ = s.presence.ClearPendingEmpty(ctx, roomID)
			skipped = append(skipped, entry)
			continue
		}
		if err := s.closeEmptyRoom(ctx, roomID); err != nil {
			return err
		}
		_ = s.presence.ClearPendingEmpty(ctx, roomID)
		cleaned = append(cleaned, entry)
	}
	logEmptyRoomCleanupResults(cleaned, skipped, len(pending), len(active), len(dbIDs), len(candidates), trig)
	return nil
}

func (s *Service) closeEmptyRoom(ctx context.Context, roomID string) error {
	if err := s.rooms.Delete(ctx, roomID); err != nil && !errors.Is(err, store.ErrNotFound) {
		return err
	}
	_ = s.states.DeleteRoomState(ctx, roomID)
	if s.presence != nil {
		_ = s.presence.DeleteRoomPresence(ctx, roomID)
	}
	if s.roomAccess != nil {
		_ = s.roomAccess.DeleteRoomAccess(ctx, roomID)
	}
	s.deleteRoomChat(ctx, roomID)
	return s.PublishRoomEvent(ctx, roomID, "room_closed", nil)
}

// EnrichRoomFromCache merges Redis playback fields into a DB room for API responses.
func (s *Service) EnrichRoomFromCache(ctx context.Context, room *model.Room) {
	if room == nil || s.states == nil {
		return
	}
	st, err := s.states.GetRoomState(ctx, room.ID)
	if err != nil || st == nil {
		return
	}
	if st.VideoID != "" {
		room.CurrentVideo = st.VideoID
	}
}
