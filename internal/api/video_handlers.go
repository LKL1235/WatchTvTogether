package api

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	authsvc "watchtogether/internal/auth"
	"watchtogether/internal/model"
	"watchtogether/internal/store"
	"watchtogether/pkg/apierr"
)

type videoHandler struct {
	deps Dependencies
}

type videoListResponse struct {
	Items []*model.Video `json:"items"`
	Total int            `json:"total"`
}

func registerVideoRoutes(router *gin.Engine, deps Dependencies, authService *authsvc.Service) {
	h := &videoHandler{deps: deps}
	api := router.Group("/api", requireAuth(authService))
	api.GET("/videos", h.list)
	api.GET("/videos/:id", h.get)
	api.DELETE("/admin/videos/:id", requireAdmin, h.delete)
	api.GET("/videos/:id/file", h.file)
}

func (h *videoHandler) list(c *gin.Context) {
	videos, total, err := h.deps.VideoStore.List(c.Request.Context(), store.ListVideosOpts{
		Limit:  parseInt(c.Query("limit"), 20),
		Offset: parseInt(c.Query("offset"), 0),
		Query:  strings.TrimSpace(c.Query("q")),
		Status: model.VideoStatus(strings.TrimSpace(c.Query("status"))),
	})
	if err != nil {
		respondStoreError(c, err)
		return
	}
	c.JSON(http.StatusOK, videoListResponse{Items: videos, Total: total})
}

func (h *videoHandler) get(c *gin.Context) {
	video, ok := h.loadVideo(c)
	if !ok {
		return
	}
	c.JSON(http.StatusOK, video)
}

func (h *videoHandler) file(c *gin.Context) {
	video, ok := h.loadVideo(c)
	if !ok {
		return
	}
	if !isInsideDir(h.deps.Config.StorageDir, video.FilePath) {
		apierr.Abort(c, apierr.Forbidden("video file is outside storage directory"))
		return
	}
	c.FileAttachment(video.FilePath, filepath.Base(video.FilePath))
}

func (h *videoHandler) delete(c *gin.Context) {
	video, ok := h.loadVideo(c)
	if !ok {
		return
	}
	if err := h.deps.VideoStore.Delete(c.Request.Context(), video.ID); err != nil {
		respondStoreError(c, err)
		return
	}
	removeIfInside(h.deps.Config.StorageDir, video.FilePath)
	if strings.HasPrefix(video.PosterPath, "/static/posters/") {
		removeIfInside(h.deps.Config.PosterDir, filepath.Join(h.deps.Config.PosterDir, strings.TrimPrefix(video.PosterPath, "/static/posters/")))
	}
	c.Status(http.StatusNoContent)
}

func (h *videoHandler) loadVideo(c *gin.Context) (*model.Video, bool) {
	video, err := h.deps.VideoStore.GetByID(c.Request.Context(), strings.TrimSpace(c.Param("id")))
	if err != nil {
		respondStoreError(c, err)
		return nil, false
	}
	return video, true
}

func parseOptionalStatus(raw string) (model.VideoStatus, bool) {
	switch model.VideoStatus(strings.TrimSpace(raw)) {
	case "", model.VideoStatusProcessing, model.VideoStatusReady, model.VideoStatusError:
		return model.VideoStatus(strings.TrimSpace(raw)), true
	default:
		return "", false
	}
}

func isInsideDir(root, candidate string) bool {
	rootAbs, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	candidateAbs, err := filepath.Abs(candidate)
	if err != nil {
		return false
	}
	rel, err := filepath.Rel(rootAbs, candidateAbs)
	return err == nil && rel != "." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func removeIfInside(root, candidate string) {
	if candidate == "" || !isInsideDir(root, candidate) {
		return
	}
	if err := os.Remove(candidate); err != nil && !errors.Is(err, os.ErrNotExist) {
		_ = strconv.ErrSyntax
	}
}
