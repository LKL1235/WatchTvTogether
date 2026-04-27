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
	deps Dependencies
	auth *authsvc.Service
	hubs *roomhub.Manager
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

type roomListResponse struct {
	Items []*model.Room `json:"items"`
	Total int           `json:"total"`
}

func registerRoomRoutes(router *gin.Engine, deps Dependencies, authService *authsvc.Service, hubs *roomhub.Manager) {
	h := &roomHandler{deps: deps, auth: authService, hubs: hubs}
	api := router.Group("/api", requireAuth(authService))
	api.POST("/rooms", h.create)
	api.GET("/rooms", h.list)
	api.GET("/rooms/:roomId", h.get)
	api.DELETE("/rooms/:roomId", h.delete)
	api.POST("/rooms/:roomId/join", h.join)
	api.POST("/rooms/:roomId/kick/:uid", h.kick)
	api.GET("/rooms/:roomId/state", h.state)
	router.GET("/ws/room/:roomId", h.ws)
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
	c.JSON(http.StatusOK, roomListResponse{Items: rooms, Total: total})
}

func (h *roomHandler) get(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
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
	_ = h.hubs.Destroy(c.Request.Context(), room.ID)
	c.Status(http.StatusNoContent)
}

func (h *roomHandler) join(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	if room.Visibility == model.RoomVisibilityPrivate {
		var req joinRoomRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			apierr.Abort(c, apierr.InvalidRequest("invalid JSON body"))
			return
		}
		if !authsvc.CheckPassword(room.PasswordHash, req.Password) {
			apierr.Abort(c, apierr.Forbidden("invalid room password"))
			return
		}
	}
	user := currentUser(c)
	c.JSON(http.StatusOK, roomResponse{Room: room, IsOwner: user != nil && room.OwnerID == user.UserID})
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
	c.Status(http.StatusNoContent)
}

func (h *roomHandler) state(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	state, err := h.deps.RoomStateCache.GetRoomState(c.Request.Context(), room.ID)
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

func (h *roomHandler) ws(c *gin.Context) {
	room, ok := h.loadRoom(c)
	if !ok {
		return
	}
	claims, err := h.auth.ParseToken(c.Request.Context(), bearerFromQueryOrHeader(c), authsvc.TokenTypeAccess)
	if err != nil {
		apierr.Abort(c, apierr.Unauthorized("valid access token required"))
		return
	}
	hub := h.hubs.Get(room.ID)
	_ = hub.Serve(c, roomhub.User{
		ID:       claims.UserID,
		Username: claims.Username,
		Role:     claims.Role,
		IsOwner:  room.OwnerID == claims.UserID,
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
