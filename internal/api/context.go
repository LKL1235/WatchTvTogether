package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"watchtogether/internal/auth"
	"watchtogether/internal/model"
	"watchtogether/internal/store"
	"watchtogether/pkg/apierr"
)

const claimsContextKey = "auth_claims"

func requireAuth(authSvc *auth.Service) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := auth.ExtractBearer(c.GetHeader("Authorization"))
		claims, err := authSvc.ParseToken(c.Request.Context(), token, auth.TokenTypeAccess)
		if err != nil {
			apierr.Abort(c, apierr.Unauthorized("authentication required"))
			return
		}
		c.Set(claimsContextKey, claims)
		c.Next()
	}
}

func requireAdmin(c *gin.Context) {
	if currentClaims(c).Role != model.UserRoleAdmin {
		apierr.Abort(c, apierr.Forbidden("admin role required"))
		return
	}
	c.Next()
}

func currentClaims(c *gin.Context) *auth.Claims {
	v, ok := c.Get(claimsContextKey)
	if !ok {
		return &auth.Claims{}
	}
	claims, ok := v.(*auth.Claims)
	if !ok {
		return &auth.Claims{}
	}
	return claims
}

func currentUser(c *gin.Context) *auth.Claims {
	claims := currentClaims(c)
	if claims.UserID == "" {
		return nil
	}
	return claims
}

func CurrentUser(c *gin.Context) *auth.Claims {
	return currentUser(c)
}

func bearerFromQueryOrHeader(c *gin.Context) string {
	if token := strings.TrimSpace(c.Query("token")); token != "" {
		return token
	}
	return auth.ExtractBearer(c.GetHeader("Authorization"))
}

func writeError(c *gin.Context, err error) {
	if err == nil {
		return
	}
	apierr.Abort(c, mapError(err))
}

func mapError(err error) error {
	switch {
	case errors.Is(err, auth.ErrInvalidCredentials):
		return apierr.Unauthorized("invalid username or password")
	case errors.Is(err, auth.ErrInvalidToken):
		return apierr.Unauthorized("invalid token")
	case errors.Is(err, store.ErrNotFound):
		return apierr.NotFound("resource not found")
	case errors.Is(err, store.ErrConflict):
		return apierr.Conflict("resource already exists")
	default:
		return err
	}
}

func respondStoreError(c *gin.Context, err error) {
	writeError(c, err)
}

func writeCreated(c *gin.Context, body any) {
	c.JSON(http.StatusCreated, body)
}
