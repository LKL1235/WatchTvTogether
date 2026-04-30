package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"watchtogether/internal/api"
	"watchtogether/internal/cache"
	"watchtogether/internal/cache/memory"
	rediscache "watchtogether/internal/cache/redis"
	"watchtogether/internal/capabilities"
	"watchtogether/internal/config"
	"watchtogether/internal/download"
	"watchtogether/internal/email"
	"watchtogether/internal/emailcode"
	ablyrealtime "watchtogether/internal/realtime/ably"
	"watchtogether/internal/store"
	"watchtogether/internal/store/postgres"
)

type stores struct {
	db            *sql.DB
	users         store.UserStore
	rooms         store.RoomStore
	videos        store.VideoStore
	downloadTasks store.DownloadTaskStore
}

type caches struct {
	close        func() error
	redis        goredis.UniversalClient
	sessions     cache.SessionCache
	roomStates   cache.RoomStateCache
	roomPresence cache.RoomPresence
	roomAccess   cache.RoomAccessCache
	pubsub       cache.PubSub
}

func main() {
	if err := run(); err != nil {
		log.Fatalf("server stopped: %v", err)
	}
}

func run() error {
	cfg, err := config.Load("config.yaml")
	if err != nil {
		return err
	}

	st, err := newStores(cfg)
	if err != nil {
		return err
	}
	defer st.db.Close()

	ca, err := newCaches(cfg)
	if err != nil {
		return err
	}
	if ca.close != nil {
		defer ca.close()
	}

	caps := capabilities.Check(context.Background())
	capabilities.Log(caps)
	rootCtx, stopDownloads := context.WithCancel(context.Background())
	defer stopDownloads()
	downloads := download.NewServiceWithOptions(st.downloadTasks, st.videos, cfg, caps, download.WithPubSub(ca.pubsub))
	downloads.Start(rootCtx, cfg.DownloadWorkers)

	realtime, err := ablyrealtime.NewService(cfg)
	if err != nil {
		return err
	}

	emailSender := email.NewSender(cfg)
	var emailCodes *emailcode.Store
	if ca.redis != nil {
		emailCodes = emailcode.NewStore(ca.redis)
	} else {
		emailCodes = emailcode.NewStore(nil)
	}

	router := api.NewRouter(api.Dependencies{
		Config:            cfg,
		UserStore:         st.users,
		RoomStore:         st.rooms,
		VideoStore:        st.videos,
		DownloadTaskStore: st.downloadTasks,
		EmailSender:       emailSender,
		EmailCodes:        emailCodes,
		SessionCache:      ca.sessions,
		RoomStateCache:    ca.roomStates,
		RoomPresence:      ca.roomPresence,
		RoomAccess:        ca.roomAccess,
		PubSub:            ca.pubsub,
		Realtime:          realtime,
		Capabilities:      caps,
		DownloadService:   downloads,
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("watchtogether listening on %s", cfg.Addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		return err
	case <-stop:
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(ctx)
	}
}

func newStores(cfg config.Config) (*stores, error) {
	switch cfg.StorageBackend {
	case "postgres":
		db, err := postgres.Open(context.Background(), cfg.PostgresDSN)
		if err != nil {
			return nil, err
		}
		return &stores{
			db:            db,
			users:         postgres.NewUserStore(db),
			rooms:         postgres.NewRoomStore(db),
			videos:        postgres.NewVideoStore(db),
			downloadTasks: postgres.NewDownloadTaskStore(db),
		}, nil
	default:
		return nil, errors.New("unsupported storage backend: " + cfg.StorageBackend)
	}
}

func newCaches(cfg config.Config) (*caches, error) {
	switch cfg.CacheBackend {
	case "memory":
		return &caches{
			sessions:     memory.NewSessionCache(),
			roomStates:   memory.NewRoomStateCache(),
			roomPresence: memory.NewRoomPresence(),
			roomAccess:   memory.NewRoomAccess(),
			pubsub:       memory.NewPubSub(),
		}, nil
	case "redis":
		client, err := rediscache.NewClient(cfg.RedisAddr, cfg.RedisURL)
		if err != nil {
			return nil, err
		}
		if err := client.Ping(context.Background()).Err(); err != nil {
			_ = client.Close()
			return nil, err
		}
		return &caches{
			close:        client.Close,
			redis:        client,
			sessions:     rediscache.NewSessionCache(client),
			roomStates:   rediscache.NewRoomStateCache(client),
			roomPresence: rediscache.NewRoomPresence(client),
			roomAccess:   rediscache.NewRoomAccess(client),
			pubsub:       rediscache.NewPubSub(client),
		}, nil
	default:
		return nil, errors.New("unsupported cache backend: " + cfg.CacheBackend)
	}
}
