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

	"watchtogether/internal/api"
	"watchtogether/internal/cache"
	"watchtogether/internal/cache/memory"
	"watchtogether/internal/config"
	"watchtogether/internal/store"
	"watchtogether/internal/store/sqlite"
)

type stores struct {
	db            *sql.DB
	users         store.UserStore
	rooms         store.RoomStore
	videos        store.VideoStore
	downloadTasks store.DownloadTaskStore
}

type caches struct {
	sessions   cache.SessionCache
	roomStates cache.RoomStateCache
	pubsub     cache.PubSub
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

	router := api.NewRouter(api.Dependencies{
		Config:            cfg,
		UserStore:         st.users,
		RoomStore:         st.rooms,
		VideoStore:        st.videos,
		DownloadTaskStore: st.downloadTasks,
		SessionCache:      ca.sessions,
		RoomStateCache:    ca.roomStates,
		PubSub:            ca.pubsub,
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
	case "sqlite":
		db, err := sqlite.Open(context.Background(), cfg.SQLitePath)
		if err != nil {
			return nil, err
		}
		return &stores{
			db:            db,
			users:         sqlite.NewUserStore(db),
			rooms:         sqlite.NewRoomStore(db),
			videos:        sqlite.NewVideoStore(db),
			downloadTasks: sqlite.NewDownloadTaskStore(db),
		}, nil
	case "postgres":
		return nil, errors.New("postgres store backend is not implemented yet")
	default:
		return nil, errors.New("unsupported storage backend: " + cfg.StorageBackend)
	}
}

func newCaches(cfg config.Config) (*caches, error) {
	switch cfg.CacheBackend {
	case "memory":
		return &caches{
			sessions:   memory.NewSessionCache(),
			roomStates: memory.NewRoomStateCache(),
			pubsub:     memory.NewPubSub(),
		}, nil
	case "redis":
		return nil, errors.New("redis cache backend is not implemented yet")
	default:
		return nil, errors.New("unsupported cache backend: " + cfg.CacheBackend)
	}
}
