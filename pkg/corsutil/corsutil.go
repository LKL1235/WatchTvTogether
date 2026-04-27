// Package corsutil 提供与浏览器跨域（CORS / WebSocket Origin）一致的来源校验，供 HTTP 与 WS 复用。
package corsutil

import (
	"net/http"
	"strings"

	"github.com/gin-contrib/cors"
)

// GinConfig 根据允许的来源列表返回 gin 的 CORS 配置。allowed 为空或仅含通配时允许任意来源（与未配置时行为一致）。
func GinConfig(allowed []string) cors.Config {
	cfg := cors.Config{
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization", "Range", "X-Requested-With"},
		ExposeHeaders:    []string{"Content-Length", "Content-Range", "Accept-Ranges"},
		AllowCredentials: false,
		AllowWebSockets:  true,
	}
	if allowAll(allowed) {
		cfg.AllowAllOrigins = true
		return cfg
	}
	cfg.AllowOrigins = normalizeOrigins(allowed)
	return cfg
}

// CheckOrigin 返回适用于 gorilla/websocket.Upgrader 的 CheckOrigin 函数，规则与 GinConfig 一致。
func CheckOrigin(allowed []string) func(r *http.Request) bool {
	if allowAll(allowed) {
		return func(r *http.Request) bool { return true }
	}
	list := normalizeOrigins(allowed)
	seen := make(map[string]struct{}, len(list))
	for _, o := range list {
		seen[o] = struct{}{}
	}
	return func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		if origin == "" {
			return false
		}
		_, ok := seen[origin]
		return ok
	}
}

func allowAll(allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	for _, s := range allowed {
		if strings.TrimSpace(s) == "*" {
			return true
		}
	}
	return false
}

func normalizeOrigins(allowed []string) []string {
	var out []string
	for _, s := range allowed {
		s = strings.TrimSpace(s)
		if s == "" || s == "*" {
			continue
		}
		out = append(out, strings.TrimRight(s, "/"))
	}
	return out
}
