package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"watchtogether/internal/auth"
	"watchtogether/internal/store"
	"watchtogether/pkg/apierr"
)

type authHandler struct {
	auth  *auth.Service
	users store.UserStore
}

type authRequest struct {
	Username  string `json:"username"`
	Password  string `json:"password"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

func registerAuthRoutes(router *gin.Engine, deps Dependencies, authSvc *auth.Service) {
	h := &authHandler{auth: authSvc, users: deps.UserStore}
	api := router.Group("/api")
	api.POST("/auth/register", h.register)
	api.POST("/auth/login", h.login)
	api.POST("/auth/refresh", h.refresh)
	api.POST("/auth/logout", requireAuth(authSvc), h.logout)
	api.GET("/users/me", requireAuth(authSvc), h.me)
}

func (h *authHandler) register(c *gin.Context) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid register request"))
		return
	}
	user, tokens, err := h.auth.Register(c.Request.Context(), req.Username, req.Password, req.Nickname, req.AvatarURL)
	if err != nil {
		respondAuthError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user, "tokens": tokens})
}

func (h *authHandler) login(c *gin.Context) {
	var req authRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid login request"))
		return
	}
	user, tokens, err := h.auth.Login(c.Request.Context(), req.Username, req.Password)
	if err != nil {
		respondAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user, "tokens": tokens})
}

func (h *authHandler) refresh(c *gin.Context) {
	var req refreshRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.RefreshToken == "" {
		apierr.Abort(c, apierr.InvalidRequest("invalid refresh request"))
		return
	}
	tokens, err := h.auth.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		respondAuthError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"tokens": tokens})
}

func (h *authHandler) logout(c *gin.Context) {
	var req logoutRequest
	_ = c.ShouldBindJSON(&req)
	if err := h.auth.Logout(c.Request.Context(), auth.ExtractBearer(c.GetHeader("Authorization")), req.RefreshToken); err != nil {
		respondAuthError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authHandler) me(c *gin.Context) {
	user, err := h.users.GetByID(c.Request.Context(), currentClaims(c).UserID)
	if err != nil {
		writeError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": user})
}

func respondAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		apierr.Abort(c, apierr.InvalidRequest("invalid username or password"))
	case errors.Is(err, auth.ErrInvalidToken):
		apierr.Abort(c, apierr.Unauthorized("invalid token"))
	case errors.Is(err, store.ErrConflict):
		apierr.Abort(c, apierr.Conflict("resource already exists"))
	default:
		apierr.Abort(c, err)
	}
}
