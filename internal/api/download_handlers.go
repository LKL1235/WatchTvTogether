package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	authsvc "watchtogether/internal/auth"
	"watchtogether/internal/download"
	"watchtogether/pkg/apierr"
	"watchtogether/pkg/corsutil"
)

var downloadWSUpgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
}

type downloadHandler struct {
	deps    Dependencies
	service *download.Service
	auth    *authsvc.Service
}

type createDownloadRequest struct {
	URL       string `json:"url"`
	SourceURL string `json:"source_url"`
}

type downloadListResponse struct {
	Items any `json:"items"`
}

func registerDownloadRoutes(router *gin.Engine, deps Dependencies, authService *authsvc.Service) {
	downloadService := deps.DownloadService
	if downloadService == nil {
		return
	}
	downloadWSUpgrader.CheckOrigin = corsutil.CheckOrigin(deps.Config.CorsOrigins)
	h := &downloadHandler{deps: deps, service: downloadService, auth: authService}
	admin := router.Group("/api/admin", requireAuth(authService), requireAdmin)
	admin.POST("/downloads", h.create)
	admin.GET("/downloads", h.list)
	admin.GET("/downloads/:taskId", h.get)
	admin.DELETE("/downloads/:taskId", h.cancel)
	router.GET("/ws/admin/downloads", h.ws)
}

func (h *downloadHandler) create(c *gin.Context) {
	var req createDownloadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid download request"))
		return
	}
	sourceURL := req.URL
	if sourceURL == "" {
		sourceURL = req.SourceURL
	}
	task, err := h.service.Enqueue(c.Request.Context(), currentClaims(c).UserID, sourceURL)
	if err != nil {
		respondDownloadError(c, err)
		return
	}
	c.JSON(http.StatusCreated, task)
}

func (h *downloadHandler) list(c *gin.Context) {
	tasks, err := h.deps.DownloadTaskStore.List(c.Request.Context())
	if err != nil {
		respondStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, downloadListResponse{Items: tasks})
}

func (h *downloadHandler) get(c *gin.Context) {
	task, err := h.deps.DownloadTaskStore.GetByID(c.Request.Context(), strings.TrimSpace(c.Param("taskId")))
	if err != nil {
		respondStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, task)
}

func (h *downloadHandler) cancel(c *gin.Context) {
	if err := h.service.Cancel(c.Request.Context(), strings.TrimSpace(c.Param("taskId"))); err != nil {
		respondStoreError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *downloadHandler) ws(c *gin.Context) {
	if h.deps.PubSub == nil {
		apierr.Abort(c, apierr.Internal("download updates are unavailable"))
		return
	}
	claims, err := h.auth.ParseToken(c.Request.Context(), bearerFromQueryOrHeader(c), authsvc.TokenTypeAccess)
	if err != nil {
		apierr.Abort(c, apierr.Unauthorized("valid access token required"))
		return
	}
	if claims.Role != "admin" {
		apierr.Abort(c, apierr.Forbidden("admin role required"))
		return
	}
	conn, err := downloadWSUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer conn.Close()
	ch, unsubscribe, err := h.deps.PubSub.Subscribe(c.Request.Context(), download.UpdatesChannel)
	if err != nil {
		return
	}
	defer unsubscribe()
	for {
		select {
		case <-c.Request.Context().Done():
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		}
	}
}

func respondDownloadError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, download.ErrUnsupportedSource), errors.Is(err, download.ErrToolUnavailable):
		apierr.Abort(c, apierr.InvalidRequest(err.Error()))
	default:
		apierr.Abort(c, err)
	}
}
