package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	authsvc "watchtogether/internal/auth"
	"watchtogether/internal/model"
	roomhub "watchtogether/internal/room"
	"watchtogether/internal/store"
)

type debugHandler struct {
	deps        Dependencies
	roomService *roomhub.Service
}

type debugRoomsResponse struct {
	Items []*debugRoomResponse `json:"items"`
	Total int                  `json:"total"`
}

type debugRoomResponse struct {
	Room        *model.Room      `json:"room"`
	State       *model.RoomState `json:"state"`
	Users       []roomhub.User   `json:"users"`
	Queue       []string         `json:"queue"`
	ViewerCount int              `json:"viewer_count"`
}

func registerDebugRoutes(router *gin.Engine, deps Dependencies, authService *authsvc.Service, rooms *roomhub.Service) {
	h := &debugHandler{deps: deps, roomService: rooms}
	admin := router.Group("/api/admin", requireAuth(authService), requireAdmin)
	admin.GET("/debug/rooms", h.rooms)
}

func (h *debugHandler) rooms(c *gin.Context) {
	ctx := c.Request.Context()
	rooms, total, err := h.deps.RoomStore.List(ctx, store.ListRoomsOpts{Limit: 100})
	if err != nil {
		respondStoreError(c, err)
		return
	}

	items := make([]*debugRoomResponse, 0, len(rooms))
		for _, room := range rooms {
		snapshot := h.roomService.Snapshot(ctx, room.ID)
		state := snapshot.State
		if state == nil {
			var err error
			state, err = h.deps.RoomStateCache.GetRoomState(ctx, room.ID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				respondStoreError(c, err)
				return
			}
		}
		queue := append([]string(nil), snapshot.Queue...)
		if len(queue) == 0 && state != nil {
			queue = append(queue, state.Queue...)
		}
		if len(queue) == 0 && state != nil && state.VideoID != "" {
			queue = []string{state.VideoID}
		}
		if state == nil {
			state = &model.RoomState{
				RoomID:   room.ID,
				VideoID:  room.CurrentVideo,
				Queue:    queue,
				Action:   model.PlaybackActionPause,
				Position: 0,
			}
		}

		var users []roomhub.User
		if h.deps.RoomPresence != nil {
			members, err := h.deps.RoomPresence.ListMembers(ctx, room.ID)
			if err != nil {
				respondStoreError(c, err)
				return
			}
			users = make([]roomhub.User, 0, len(members))
			for _, m := range members {
				users = append(users, roomhub.User{ID: m.UserID, Username: m.Username})
			}
		}

		items = append(items, &debugRoomResponse{
			Room:        room,
			State:       state,
			Users:       users,
			Queue:       queue,
			ViewerCount: snapshot.ViewerCount,
		})
	}

	c.JSON(http.StatusOK, debugRoomsResponse{Items: items, Total: total})
}
