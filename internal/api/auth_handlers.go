package api

import (
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"watchtogether/internal/auth"
	"watchtogether/internal/config"
	verifyemail "watchtogether/internal/email"
	"watchtogether/internal/emailcode"
	"watchtogether/internal/store"
	"watchtogether/pkg/apierr"
)

type authHandler struct {
	auth       *auth.Service
	users      store.UserStore
	sender     verifyemail.SenderAPI
	codes      *emailcode.Store
	cfg        config.Config
	ipLimiter  *slidingWindowLimiter
	regMu      sync.Mutex
	regLastHit map[string]time.Time // email -> last "exists" response (anti-enumeration spacing)
}

type slidingWindowLimiter struct {
	mu    sync.Mutex
	hits  map[string][]time.Time
	limit int
	win   time.Duration
}

func newIPLimiter(limit int, window time.Duration) *slidingWindowLimiter {
	return &slidingWindowLimiter{
		hits:  make(map[string][]time.Time),
		limit: limit,
		win:   window,
	}
}

func (l *slidingWindowLimiter) allow(ip string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	list := l.hits[ip]
	cutoff := now.Add(-l.win)
	var kept []time.Time
	for _, t := range list {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	if len(kept) >= l.limit {
		return false
	}
	kept = append(kept, now)
	l.hits[ip] = kept
	return true
}

func registerAuthRoutes(router *gin.Engine, deps Dependencies, authSvc *auth.Service) {
	h := &authHandler{
		auth:       authSvc,
		users:      deps.UserStore,
		sender:     deps.EmailSender,
		codes:      deps.EmailCodes,
		cfg:        deps.Config,
		ipLimiter:  newIPLimiter(40, time.Minute),
		regLastHit: make(map[string]time.Time),
	}
	api := router.Group("/api")
	api.POST("/auth/register/code", h.registerSendCode)
	api.POST("/auth/register", h.register)
	api.POST("/auth/password/reset/code", h.resetSendCode)
	api.POST("/auth/password/reset", h.resetPassword)
	api.POST("/auth/login", h.login)
	api.POST("/auth/refresh", h.refresh)
	api.POST("/auth/logout", requireAuth(authSvc), h.logout)
	api.GET("/users/me", requireAuth(authSvc), h.me)
}

type emailOnlyRequest struct {
	Email string `json:"email"`
}

type registerCodeResponse struct {
	ExpiresAt   string `json:"expires_at"`
	RetryAfter  int    `json:"retry_after"`
	RetryAfterS int    `json:"retry_after_s,omitempty"`
}

type registerRequest struct {
	Email     string `json:"email"`
	Username  string `json:"username"`
	Password  string `json:"password"`
	Code      string `json:"code"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatar_url"`
}

type loginRequest struct {
	Login    string `json:"login"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type logoutRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type resetPasswordRequest struct {
	Email       string `json:"email"`
	Code        string `json:"code"`
	NewPassword string `json:"new_password"`
}

func (h *authHandler) registerSendCode(c *gin.Context) {
	var req emailOnlyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid request body"))
		return
	}
	emailNorm, err := auth.NormalizeEmailForRequest(req.Email)
	if err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid email"))
		return
	}
	if !h.ipLimiter.allow(c.ClientIP()) {
		apierr.Abort(c, apierr.TooManyRequests("too many requests"))
		return
	}
	ctx := c.Request.Context()
	if _, e := h.users.GetByEmail(ctx, emailNorm); e == nil {
		h.fakeRegisterCodeOK(c, emailNorm)
		return
	} else if !errors.Is(e, store.ErrNotFound) {
		writeError(c, e)
		return
	}
	if h.codes == nil {
		apierr.Abort(c, apierr.Internal("verification store not configured"))
		return
	}
	if err := h.codes.PrecheckSend(ctx, emailNorm, verifyemail.PurposeRegister, h.cfg.EmailCodeSendInterval, h.cfg.EmailCodeDailyLimit, time.Now()); err != nil {
		respondCodeErr(c, err, h.cfg.EmailCodeSendInterval)
		return
	}
	code, err := emailcode.GenerateNumericCode(h.cfg.EmailCodeLength)
	if err != nil {
		_ = h.codes.RollbackPrecheck(ctx, emailNorm, verifyemail.PurposeRegister, time.Now())
		apierr.Abort(c, apierr.Internal("failed to generate code"))
		return
	}
	if h.sender == nil || !h.sender.Enabled() {
		_ = h.codes.RollbackPrecheck(ctx, emailNorm, verifyemail.PurposeRegister, time.Now())
		log.Printf("email: cannot send register code (resend disabled), email=%s audit=%s", redactLogEmail(emailNorm), emailcode.HashForAudit(code))
		apierr.Abort(c, apierr.Internal("email delivery is not configured"))
		return
	}
	resendID, err := h.sender.SendVerificationCode(ctx, emailNorm, verifyemail.PurposeRegister, code)
	if err != nil {
		_ = h.codes.RollbackPrecheck(ctx, emailNorm, verifyemail.PurposeRegister, time.Now())
		log.Printf("email: register send failed: %v", err)
		apierr.Abort(c, apierr.Internal("failed to send email"))
		return
	}
	if err := h.codes.CommitSend(ctx, emailNorm, verifyemail.PurposeRegister, code, h.cfg.EmailCodeTTL, h.cfg.EmailCodeSendInterval, time.Now()); err != nil {
		log.Printf("email: commit register code failed: %v", err)
		apierr.Abort(c, apierr.Internal("failed to persist verification"))
		return
	}
	log.Printf("email: register code sent id=%s to=%s", resendID, redactLogEmail(emailNorm))
	writeRegisterCodeOK(c, h.cfg.EmailCodeTTL, h.cfg.EmailCodeSendInterval)
}

func (h *authHandler) fakeRegisterCodeOK(c *gin.Context, emailNorm string) {
	h.regMu.Lock()
	last := h.regLastHit[emailNorm]
	now := time.Now()
	if now.Sub(last) < h.cfg.EmailCodeSendInterval {
		wait := h.cfg.EmailCodeSendInterval - now.Sub(last)
		h.regMu.Unlock()
		sec := int(wait.Round(time.Second) / time.Second)
		if sec < 1 {
			sec = 1
		}
		c.Header("Retry-After", strconv.Itoa(sec))
		apierr.Abort(c, apierr.TooManyRequests("please wait before retrying"))
		return
	}
	h.regLastHit[emailNorm] = now
	h.regMu.Unlock()
	writeRegisterCodeOK(c, h.cfg.EmailCodeTTL, h.cfg.EmailCodeSendInterval)
}

func writeRegisterCodeOK(c *gin.Context, ttl, interval time.Duration) {
	exp := time.Now().UTC().Add(ttl)
	retrySec := int(interval.Round(time.Second) / time.Second)
	if retrySec < 1 {
		retrySec = 60
	}
	c.JSON(http.StatusOK, registerCodeResponse{
		ExpiresAt:   exp.Format(time.RFC3339),
		RetryAfter:  retrySec,
		RetryAfterS: retrySec,
	})
}

func (h *authHandler) resetSendCode(c *gin.Context) {
	var req emailOnlyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid request body"))
		return
	}
	emailNorm, err := auth.NormalizeEmailForRequest(req.Email)
	if err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid email"))
		return
	}
	if !h.ipLimiter.allow(c.ClientIP()) {
		apierr.Abort(c, apierr.TooManyRequests("too many requests"))
		return
	}
	ctx := c.Request.Context()
	if h.codes == nil {
		apierr.Abort(c, apierr.Internal("verification store not configured"))
		return
	}
	if err := h.codes.PrecheckSend(ctx, emailNorm, verifyemail.PurposeResetPassword, h.cfg.EmailCodeSendInterval, h.cfg.EmailCodeDailyLimit, time.Now()); err != nil {
		respondCodeErr(c, err, h.cfg.EmailCodeSendInterval)
		return
	}
	u, err := h.users.GetByEmail(ctx, emailNorm)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			_ = h.codes.RollbackPrecheck(ctx, emailNorm, verifyemail.PurposeResetPassword, time.Now())
			writeRegisterCodeOK(c, h.cfg.EmailCodeTTL, h.cfg.EmailCodeSendInterval)
			return
		}
		_ = h.codes.RollbackPrecheck(ctx, emailNorm, verifyemail.PurposeResetPassword, time.Now())
		writeError(c, err)
		return
	}
	_ = u
	code, err := emailcode.GenerateNumericCode(h.cfg.EmailCodeLength)
	if err != nil {
		_ = h.codes.RollbackPrecheck(ctx, emailNorm, verifyemail.PurposeResetPassword, time.Now())
		apierr.Abort(c, apierr.Internal("failed to generate code"))
		return
	}
	if h.sender == nil || !h.sender.Enabled() {
		_ = h.codes.RollbackPrecheck(ctx, emailNorm, verifyemail.PurposeResetPassword, time.Now())
		log.Printf("email: cannot send reset code (resend disabled), email=%s", redactLogEmail(emailNorm))
		apierr.Abort(c, apierr.Internal("email delivery is not configured"))
		return
	}
	resendID, err := h.sender.SendVerificationCode(ctx, emailNorm, verifyemail.PurposeResetPassword, code)
	if err != nil {
		_ = h.codes.RollbackPrecheck(ctx, emailNorm, verifyemail.PurposeResetPassword, time.Now())
		log.Printf("email: reset send failed: %v", err)
		apierr.Abort(c, apierr.Internal("failed to send email"))
		return
	}
	if err := h.codes.CommitSend(ctx, emailNorm, verifyemail.PurposeResetPassword, code, h.cfg.EmailCodeTTL, h.cfg.EmailCodeSendInterval, time.Now()); err != nil {
		log.Printf("email: commit reset code failed: %v", err)
		apierr.Abort(c, apierr.Internal("failed to persist verification"))
		return
	}
	log.Printf("email: reset code sent id=%s to=%s", resendID, redactLogEmail(emailNorm))
	writeRegisterCodeOK(c, h.cfg.EmailCodeTTL, h.cfg.EmailCodeSendInterval)
}

func (h *authHandler) register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid register request"))
		return
	}
	user, tokens, err := h.auth.RegisterWithCode(c.Request.Context(), req.Email, req.Username, req.Password, req.Code, req.Nickname, req.AvatarURL)
	if err != nil {
		respondAuthError(c, err)
		return
	}
	c.JSON(http.StatusCreated, gin.H{"user": user, "tokens": tokens})
}

func (h *authHandler) resetPassword(c *gin.Context) {
	var req resetPasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid request body"))
		return
	}
	if err := h.auth.ResetPassword(c.Request.Context(), req.Email, req.Code, req.NewPassword); err != nil {
		respondAuthError(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

func (h *authHandler) login(c *gin.Context) {
	var req loginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apierr.Abort(c, apierr.InvalidRequest("invalid login request"))
		return
	}
	user, tokens, err := h.auth.Login(c.Request.Context(), req.Login, req.Password)
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

func respondCodeErr(c *gin.Context, err error, fallbackInterval time.Duration) {
	var cool *emailcode.CooldownError
	switch {
	case errors.As(err, &cool) && cool != nil && cool.RetryAfter > 0:
		sec := int(cool.RetryAfter.Round(time.Second) / time.Second)
		if sec < 1 {
			sec = 1
		}
		c.Header("Retry-After", strconv.Itoa(sec))
		apierr.Abort(c, apierr.TooManyRequests("verification code sent too recently"))
	case errors.Is(err, emailcode.ErrDailyLimit):
		apierr.Abort(c, apierr.TooManyRequests("daily verification email limit reached"))
	default:
		apierr.Abort(c, err)
	}
}

func respondAuthError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		apierr.Abort(c, apierr.InvalidRequest("invalid username/email or password"))
	case errors.Is(err, auth.ErrInvalidToken):
		apierr.Abort(c, apierr.Unauthorized("invalid token"))
	case errors.Is(err, auth.ErrInvalidEmail):
		apierr.Abort(c, apierr.InvalidRequest("invalid email"))
	case errors.Is(err, auth.ErrWeakPassword):
		apierr.Abort(c, apierr.InvalidRequest("password does not meet requirements"))
	case errors.Is(err, auth.ErrInvalidUsername):
		apierr.Abort(c, apierr.InvalidRequest("invalid username (use 3-40 chars: a-z, 0-9, _)"))
	case errors.Is(err, emailcode.ErrInvalidCode):
		apierr.Abort(c, apierr.InvalidRequest("invalid verification code"))
	case errors.Is(err, emailcode.ErrExpired):
		apierr.Abort(c, apierr.InvalidRequest("verification code expired"))
	case errors.Is(err, emailcode.ErrMaxAttempts):
		apierr.Abort(c, apierr.InvalidRequest("too many incorrect verification attempts"))
	case errors.Is(err, store.ErrConflict):
		apierr.Abort(c, apierr.Conflict("email or username already in use"))
	default:
		apierr.Abort(c, err)
	}
}

func redactLogEmail(e string) string {
	e = strings.TrimSpace(strings.ToLower(e))
	if e == "" {
		return ""
	}
	at := strings.LastIndex(e, "@")
	if at <= 0 {
		return "***"
	}
	return e[:1] + "***@" + e[at+1:]
}
