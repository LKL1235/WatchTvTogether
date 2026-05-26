package api

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"

	"watchtogether/internal/auth"
	"watchtogether/internal/cache"
	"watchtogether/internal/config"
	"watchtogether/internal/email"
	"watchtogether/internal/emailcode"
	"watchtogether/internal/realtime"
	roomhub "watchtogether/internal/room"
	"watchtogether/internal/store"
	"watchtogether/pkg/apierr"
	"watchtogether/pkg/corsutil"
)

type Dependencies struct {
	Config         config.Config
	UserStore   store.UserStore
	RoomStore   store.RoomStore
	EmailSender email.SenderAPI
	EmailCodes     *emailcode.Store
	SessionCache   cache.SessionCache
	RoomStateCache cache.RoomStateCache
	RoomPresence   cache.RoomPresence
	RoomAccess     cache.RoomAccessCache
	PubSub         cache.PubSub
	RoomChat       cache.RoomChat
	Realtime       realtime.Service
}

func NewRouter(deps Dependencies) *gin.Engine {
	router := gin.New()
	router.Use(gin.Logger(), gin.Recovery())
	router.Use(cors.New(corsutil.GinConfig(deps.Config.CorsOrigins)))

	router.NoRoute(func(c *gin.Context) {
		apierr.Abort(c, apierr.NotFound("route not found"))
	})

	router.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status":          "ok",
			"storage_backend": deps.Config.StorageBackend,
			"cache_backend":   deps.Config.CacheBackend,
		})
	})

	authService := auth.NewService(deps.UserStore, deps.SessionCache, deps.EmailCodes, deps.Config)
	rooms := roomhub.NewService(deps.RoomStateCache, deps.RoomPresence, deps.RoomStore, deps.RoomAccess, deps.Realtime, deps.RoomChat, roomhub.ChatLimitsFromConfig(deps.Config))
	if deps.RoomChat != nil && deps.Realtime != nil {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				bg, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
				_ = rooms.ProcessGlobalChatPending(bg)
				cancel()
			}
		}()
	}
	authService.SetAfterLogin(func(ctx context.Context) error {
		go func() {
			bg, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			if err := rooms.MaybeRunGlobalCleanup(bg); err != nil {
				log.Printf("room global cleanup: %v", err)
			}
		}()
		return nil
	})
	registerAuthRoutes(router, deps, authService)
	registerAdminRoomRoutes(router, deps, authService, rooms)
	registerRoomRoutes(router, deps, authService, rooms)
	registerDebugRoutes(router, deps, authService, rooms)

	return router
}
