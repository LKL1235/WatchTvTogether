package api

import (
	"net/http"
	"path/filepath"
	"strings"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"watchtogether/internal/auth"
	"watchtogether/internal/cache"
	"watchtogether/internal/capabilities"
	"watchtogether/internal/config"
	"watchtogether/internal/download"
	roomhub "watchtogether/internal/room"
	"watchtogether/internal/store"
	"watchtogether/pkg/apierr"
	"watchtogether/pkg/corsutil"
)

type Dependencies struct {
	Config config.Config
	// StaticRoot, when set, serves the built SPA (e.g. Vite dist) and HTML5 fallback for non-API routes.
	StaticRoot        string
	UserStore         store.UserStore
	RoomStore         store.RoomStore
	VideoStore        store.VideoStore
	DownloadTaskStore store.DownloadTaskStore
	SessionCache      cache.SessionCache
	RoomStateCache    cache.RoomStateCache
	PubSub            cache.PubSub
	Capabilities      capabilities.Report
	DownloadService   *download.Service
}

func NewRouter(deps Dependencies) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(cors.New(corsutil.GinConfig(deps.Config.CorsOrigins)))
	roomhub.InitWebSocketCheckOrigin(deps.Config.CorsOrigins)

	if root := strings.TrimSpace(deps.StaticRoot); root != "" {
		assetsDir := filepath.Join(root, "assets")
		router.Static("/assets", assetsDir)
		router.GET("/", func(c *gin.Context) {
			c.File(filepath.Join(root, "index.html"))
		})
		router.NoRoute(func(c *gin.Context) {
			path := c.Request.URL.Path
			if strings.HasPrefix(path, "/api/") ||
				strings.HasPrefix(path, "/ws/") ||
				strings.HasPrefix(path, "/static/") ||
				path == "/healthz" {
				apierr.Abort(c, apierr.NotFound("route not found"))
				return
			}
			if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
				c.File(filepath.Join(root, "index.html"))
				return
			}
			apierr.Abort(c, apierr.NotFound("route not found"))
		})
	} else {
		router.NoRoute(func(c *gin.Context) {
			apierr.Abort(c, apierr.NotFound("route not found"))
		})
	}

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":          "ok",
			"storage_backend": deps.Config.StorageBackend,
			"cache_backend":   deps.Config.CacheBackend,
		})
	})

	router.StaticFS("/static/videos", http.Dir(deps.Config.StorageDir))
	router.StaticFS("/static/posters", http.Dir(deps.Config.PosterDir))

	authService := auth.NewService(deps.UserStore, deps.SessionCache, deps.Config)
	hubs := roomhub.NewManager(deps.PubSub, deps.RoomStateCache)
	registerCapabilityRoutes(router, deps)
	registerAuthRoutes(router, deps, authService)
	registerRoomRoutes(router, deps, authService, hubs)
	registerDownloadRoutes(router, deps, authService)
	registerVideoRoutes(router, deps, authService)

	return router
}
