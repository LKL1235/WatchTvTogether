package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	authsvc "watchtogether/internal/auth"
	"watchtogether/internal/model"
	roomhub "watchtogether/internal/room"
	"watchtogether/internal/store"
)

type adminRoomHandler struct {
	deps  Dependencies
	auth  *authsvc.Service
	rooms *roomhub.Service
}

type adminRoomListItem struct {
	Room         *model.Room      `json:"room"`
	Owner        *model.User      `json:"owner,omitempty"`
	OnlineCount  int              `json:"online_count"`
	CurrentVideo string           `json:"current_video_id,omitempty"`
	Playback     model.PlaybackAction `json:"playback_action,omitempty"`
	CreatedAt    time.Time        `json:"created_at"`
}

type adminRoomListResponse struct {
	Items []adminRoomListItem `json:"items"`
	Total int                 `json:"total"`
}

func registerAdminRoomRoutes(router *gin.Engine, deps Dependencies, authService *authsvc.Service, rooms *roomhub.Service) {
	h := &adminRoomHandler{deps: deps, auth: authService, rooms: rooms}
	admin := router.Group("/api/admin", requireAuth(authService), requireAdmin)
	admin.GET("/rooms", h.listRooms)
}

func (h *adminRoomHandler) listRooms(c *gin.Context) {
	ctx := c.Request.Context()
	limit := parseInt(c.Query("limit"), 100)
	if limit > 500 {
		limit = 500
	}
	offset := parseInt(c.Query("offset"), 0)
	query := strings.TrimSpace(c.Query("q"))
	rooms, total, err := h.deps.RoomStore.List(ctx, store.ListRoomsOpts{
		Limit:  limit,
		Offset: offset,
		Query:  query,
	})
	if err != nil {
		respondStoreError(c, err)
		return
	}
	items := make([]adminRoomListItem, 0, len(rooms))
	for _, room := range rooms {
		if room == nil {
			continue
		}
		h.rooms.EnrichRoomFromCache(ctx, room)
		online := 0
		if h.deps.RoomPresence != nil {
			n, err := h.deps.RoomPresence.MemberCount(ctx, room.ID)
			if err == nil {
				online = n
			}
		}
		var owner *model.User
		if h.deps.UserStore != nil && room.OwnerID != "" {
			if u, err := h.deps.UserStore.GetByID(ctx, room.OwnerID); err == nil && u != nil {
				copyU := *u
				copyU.PasswordHash = ""
				owner = &copyU
			}
		}
		action := model.PlaybackActionPause
		currentVid := room.CurrentVideo
		if st, err := h.rooms.ProjectedRoomState(ctx, room.ID); err == nil && st != nil {
			action = st.Action
			if st.VideoID != "" {
				currentVid = st.VideoID
			}
		}
		items = append(items, adminRoomListItem{
			Room:         room,
			Owner:        owner,
			OnlineCount:  online,
			CurrentVideo: currentVid,
			Playback:     action,
			CreatedAt:    room.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, adminRoomListResponse{Items: items, Total: total})
}
