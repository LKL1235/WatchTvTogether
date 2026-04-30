package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	authsvc "watchtogether/internal/auth"
	"watchtogether/internal/model"
	roomhub "watchtogether/internal/room"
	"watchtogether/internal/store"
	"watchtogether/pkg/apierr"
)

type roomHandler struct {
	deps  Dependencies
	auth  *authsvc.Service
	rooms *roomhub.Service
}

type roomResponse struct {
	*model.Room
	IsOwner bool `json:"is_owner,omitempty"`
}

type createRoomRequest struct {
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
	Password   string `json:"password"`
}

type joinRoomRequest struct {
	Password string `json:"password"`
}

type ablyTokenRequest struct {
	RoomID   string `json:"room_id"`
	Purpose  string `json:"purpose"`
	Password string `json:"password"`
}

type controlRoomRequest struct {
	Action         model.PlaybackAction `json:"action"`
	Position       float64              `json:"position"`
	VideoID        string               `json:"video_id"`
	Queue          []string             `json:"queue"`
	PlaybackMode   model.PlaybackMode   `json:"playback_mode"`
	ControlVersion int64                `json:"control_version"`
}

type roomSnapshotRequest struct {
	Password string `json:"password"`
}

type roomSnapshotResponse struct {
	RoomID      string           `json:"room_id"`
	State       *model.RoomState `json:"state"`
	Queue       []string         `json:"queue"`
	ViewerCount int              `json:"viewer_count"`
	Ably        ablyRoomInfo     `json:"ably"`
}

type ablyRoomInfo struct {
	Channel       string `json:"channel"`
	TokenEndpoint string `json:"token_endpoint"`
}

type roomListResponse struct {
	Items []*model.Room `json:"items"`
	Total int           `json:"total"`
}

func registerRoomRoutes(router *gin.Engine, deps Dependencies, authService *authsvc.Service, rooms *roomhub.Service) {
	h := &roomHandler{deps: deps, auth: authService, rooms: rooms}
	api := router.Group("/api", requireAuth(authService))
	api.POST("/ably/token", h.ablyToken)
	api.POST("/rooms", h.create)
	api.GET("/rooms", h.list)
	api.GET("/rooms/:roomId", h.get)
	api.DELETE("/rooms/:roomId", h.delete)
	api.POST("/rooms/:roomId/join", h.join)
	api.POST("/rooms/:roomId/leave", h.leave)
	api.POST("/rooms/:roomId/kick/:uid", h.kick)
	api.GET("/rooms/:roomId/state", h.state)
	api.POST("/rooms/:roomId/control", h.control)
	api.POST("/rooms/:roomId/snapshot", h.snapshot)
}

func (h *roomHandler) create(c *gin.Context) {
	user := currentUser(c)
	var req createRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid JSON body"))
		return
	}
	name := strings.TrimSpace(req.Name)
	if len(name) < 1 || len(name) > 80 {
		apierr.Abort(c, apierr.InvalidRequest("room name is required"))
		return
	}
	visibility := model.RoomVisibility(strings.TrimSpace(req.Visibility))
	if visibility == "" {
		visibility = model.RoomVisibilityPublic
	}
	if visibility != model.RoomVisibilityPublic && visibility != model.RoomVisibilityPrivate {
		apierr.Abort(c, apierr.InvalidRequest("invalid room visibility"))
		return
	}
	room := &model.Room{Name: name, OwnerID: user.UserID, Visibility: visibility}
	if visibility == model.RoomVisibilityPrivate {
		if len(req.Password) < 4 {
			apierr.Abort(c, apierr.InvalidRequest("private room password must be at least 4 characters"))
			return
		}
		hash, err := authsvc.HashPassword(req.Password)
		if err != nil {
			apierr.Abort(c, apierr.Internal("failed to hash room password"))
			return
		}
		room.PasswordHash = hash
	}
	if err := h.deps.RoomStore.Create(c.Request.Context(), room); err != nil {
		respondStoreError(c, err)
		return
	}
	c.JSON(http.StatusCreated, roomResponse{Room: room, IsOwner: true})
}

func (h *roomHandler) list(c *gin.Context) {
	rooms, total, err := h.deps.RoomStore.List(c.Request.Context(), store.ListRoomsOpts{
		Limit:  parseInt(c.Query("limit"), 20),
		Offset: parseInt(c.Query("offset"), 0),
		Query:  strings.TrimSpace(c.Query("q")),
	})
	if err != nil {
		respondStoreError(c, err)
		return
	}
	for _, r := range rooms {
		h.rooms.EnrichRoomFromCache(c.Request.Context(), r)
	}
	c.JSON(http.StatusOK, roomListResponse{Items: rooms, Total: total})
}

func (h *roomHandler) get(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	h.rooms.EnrichRoomFromCache(c.Request.Context(), room)
	user := currentUser(c)
	c.JSON(http.StatusOK, roomResponse{Room: room, IsOwner: user != nil && room.OwnerID == user.UserID})
}

func (h *roomHandler) delete(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	if !canManageRoom(c, room) {
		apierr.Abort(c, apierr.Forbidden("room owner or admin required"))
		return
	}
	if err := h.deps.RoomStore.Delete(c.Request.Context(), room.ID); err != nil {
		respondStoreError(c, err)
		return
	}
	user := roomUserFromClaims(currentUser(c), room)
	if err := h.rooms.CloseRoom(c.Request.Context(), room.ID, &user); err != nil {
		respondStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *roomHandler) join(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	if !h.canAccessRoom(c, room, "") {
		var req joinRoomRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apierr.Abort(c, apierr.InvalidRequest("invalid JSON body"))
			return
		}
		if !h.canAccessRoom(c, room, req.Password) {
			apierr.Abort(c, apierr.Forbidden("invalid room password"))
			return
		}
	}
	user := currentUser(c)
	u := roomUserFromClaims(user, room)
	if _, err := h.rooms.Join(c.Request.Context(), room.ID, u); err != nil {
		if errors.Is(err, store.ErrConflict) {
			apierr.Abort(c, apierr.Conflict("already in another room; leave that room first"))
			return
		}
		respondStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, roomResponse{Room: room, IsOwner: user != nil && room.OwnerID == user.UserID})
}

func (h *roomHandler) leave(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	user := currentUser(c)
	if user == nil {
		apierr.Abort(c, apierr.Unauthorized("authentication required"))
		return
	}
	if err := h.rooms.Leave(c.Request.Context(), room.ID, user.UserID); err != nil {
		respondStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *roomHandler) kick(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	if !canManageRoom(c, room) {
		apierr.Abort(c, apierr.Forbidden("room owner or admin required"))
		return
	}
	if strings.TrimSpace(c.Param("uid")) == "" {
		apierr.Abort(c, apierr.InvalidRequest("user id is required"))
		return
	}
	uid := strings.TrimSpace(c.Param("uid"))
	actor := roomUserFromClaims(currentUser(c), room)
	if err := h.rooms.KickMember(c.Request.Context(), room.ID, uid, &actor); err != nil {
		respondStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *roomHandler) state(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	state, err := h.rooms.ProjectedRoomState(c.Request.Context(), room.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			c.JSON(http.StatusOK, gin.H{"room_id": room.ID, "action": model.PlaybackActionPause, "position": 0})
			return
		}
		respondStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, state)
}

func (h *roomHandler) ablyToken(c *gin.Context) {
	if h.deps.Realtime == nil {
		apierr.Abort(c, apierr.Internal("ably realtime is not configured"))
		return
	}
	var req ablyTokenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid JSON body"))
		return
	}
	if req.Purpose != "" && req.Purpose != "room" {
		apierr.Abort(c, apierr.InvalidRequest("unsupported token purpose"))
		return
	}
	roomID := strings.TrimSpace(req.RoomID)
	if roomID == "" {
		apierr.Abort(c, apierr.InvalidRequest("room_id is required"))
		return
	}
	room, err := h.deps.RoomStore.GetByID(c.Request.Context(), roomID)
	if err != nil {
		respondStoreError(c, err)
		return
	}
	if !h.canAccessRoom(c, room, req.Password) {
		apierr.Abort(c, apierr.Forbidden("invalid room password"))
		return
	}
	token, err := h.deps.Realtime.RequestRoomToken(c.Request.Context(), room.ID, currentClaims(c).UserID)
	if err != nil {
		apierr.Abort(c, apierr.Internal("failed to issue ably token"))
		return
	}
	c.JSON(http.StatusOK, token)
}

func (h *roomHandler) control(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	if !canManageRoom(c, room) {
		apierr.Abort(c, apierr.Forbidden("room owner or admin required"))
		return
	}
	var req controlRoomRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid JSON body"))
		return
	}
	if !validPlaybackAction(req.Action) {
		apierr.Abort(c, apierr.InvalidRequest("invalid playback action"))
		return
	}
	user := roomUserFromClaims(currentUser(c), room)
	msg, err := h.rooms.ApplyControl(c.Request.Context(), room.ID, user, roomhub.ControlInput{
		Action:         req.Action,
		Position:       req.Position,
		VideoID:        strings.TrimSpace(req.VideoID),
		Queue:          cleanQueue(req.Queue),
		PlaybackMode:   req.PlaybackMode,
		ClientVersion:  req.ControlVersion,
	})
	if err != nil {
		if errors.Is(err, roomhub.ErrForbidden) {
			apierr.Abort(c, apierr.Forbidden("room owner or admin required"))
			return
		}
		if errors.Is(err, roomhub.ErrStaleControl) {
			apierr.Abort(c, apierr.Conflict("stale control version"))
			return
		}
		respondStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, msg)
}

func (h *roomHandler) snapshot(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	var req roomSnapshotRequest
	if c.Request.ContentLength != 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			apierr.Abort(c, apierr.InvalidRequest("invalid JSON body"))
			return
		}
	}
	if !h.canAccessRoom(c, room, req.Password) {
		apierr.Abort(c, apierr.Forbidden("invalid room password"))
		return
	}
	snapshot := h.rooms.Snapshot(c.Request.Context(), room.ID)
	state := snapshot.State
	queue := append([]string(nil), snapshot.Queue...)
	if state == nil {
		state = &model.RoomState{
			RoomID:   room.ID,
			VideoID:  room.CurrentVideo,
			Queue:    queue,
			Action:   model.PlaybackActionPause,
			Position: 0,
		}
	}
	if len(queue) == 0 && state.VideoID != "" {
		queue = []string{state.VideoID}
	}
	channel := ""
	if h.deps.Realtime != nil {
		channel = h.deps.Realtime.ChannelName(room.ID)
	}
	c.JSON(http.StatusOK, roomSnapshotResponse{
		RoomID:      room.ID,
		State:       state,
		Queue:       queue,
		ViewerCount: snapshot.ViewerCount,
		Ably: ablyRoomInfo{
			Channel:       channel,
			TokenEndpoint: "/api/ably/token",
		},
	})
}

func (h *roomHandler) loadRoom(c *gin.Context) (*model.Room, bool) {
	roomID := strings.TrimSpace(c.Param("roomId"))
	room, err := h.deps.RoomStore.GetByID(c.Request.Context(), roomID)
	if err != nil {
		respondStoreError(c, err)
		return nil, false
	}
	return room, true
}

func canManageRoom(c *gin.Context, room *model.Room) bool {
	user := currentUser(c)
	if user == nil {
		return false
	}
	return user.Role == model.UserRoleAdmin || room.OwnerID == user.UserID
}

func (h *roomHandler) canAccessRoom(c *gin.Context, room *model.Room, password string) bool {
	if room.Visibility != model.RoomVisibilityPrivate || canManageRoom(c, room) {
		return true
	}
	return authsvc.CheckPassword(room.PasswordHash, password)
}

func roomUserFromClaims(claims *authsvc.Claims, room *model.Room) roomhub.User {
	if claims == nil {
		return roomhub.User{}
	}
	return roomhub.User{
		ID:       claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
		IsOwner:  room.OwnerID == claims.UserID,
	}
}

func validPlaybackAction(action model.PlaybackAction) bool {
	switch action {
	case model.PlaybackActionPlay, model.PlaybackActionPause, model.PlaybackActionSeek, model.PlaybackActionNext, model.PlaybackActionSwitch:
		return true
	default:
		return false
	}
}

func cleanQueue(queue []string) []string {
	out := make([]string, 0, len(queue))
	for _, item := range queue {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func parseInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return fallback
	}
	return n
}
