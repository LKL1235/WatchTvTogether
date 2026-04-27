package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	StorageBackendSQLite   = "sqlite"
	StorageBackendPostgres = "postgres"
	CacheBackendMemory     = "memory"
	CacheBackendRedis      = "redis"
)

type Config struct {
	Addr             string        `yaml:"addr"`
	StorageBackend   string        `yaml:"storage_backend"`
	SQLitePath       string        `yaml:"sqlite_path"`
	PostgresDSN      string        `yaml:"postgres_dsn"`
	CacheBackend     string        `yaml:"cache_backend"`
	RedisAddr        string        `yaml:"redis_addr"`
	JWTSecret        string        `yaml:"jwt_secret"`
	JWTAccessTTL     time.Duration `yaml:"-"`
	JWTRefreshTTL    time.Duration `yaml:"-"`
	JWTAccessTTLRaw  string        `yaml:"jwt_access_ttl"`
	JWTRefreshTTLRaw string        `yaml:"jwt_refresh_ttl"`
	StorageDir       string        `yaml:"storage_dir"`
	PosterDir        string        `yaml:"poster_dir"`
	DownloadWorkers  int           `yaml:"download_workers"`
	Aria2RPCURL      string        `yaml:"aria2_rpc_url"`
	Aria2Secret      string        `yaml:"aria2_secret"`
}

func Default() Config {
	return Config{
		Addr:             ":8080",
		StorageBackend:   StorageBackendSQLite,
		SQLitePath:       "./data/watchtogether.db",
		PostgresDSN:      "postgres://user:pass@localhost:5432/watchtogether?sslmode=disable",
		CacheBackend:     CacheBackendMemory,
		RedisAddr:        "localhost:6379",
		JWTSecret:        "change-me-in-production",
		JWTAccessTTLRaw:  "15m",
		JWTRefreshTTLRaw: "168h",
		StorageDir:       "./data/videos",
		PosterDir:        "./data/posters",
		DownloadWorkers:  2,
		Aria2RPCURL:      "http://localhost:6800/jsonrpc",
	}
}

func Load(path string) (Config, error) {
	cfg := Default()
	if path != "" {
		if err := loadFile(path, &cfg); err != nil {
			return Config{}, err
		}
	}
	applyEnv(&cfg)
	if err := cfg.normalize(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func loadFile(path string, cfg *Config) error {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read config file: %w", err)
	}
	if err := yaml.Unmarshal(b, cfg); err != nil {
		return fmt.Errorf("parse config file: %w", err)
	}
	return nil
}

func applyEnv(cfg *Config) {
	setString(&cfg.Addr, "ADDR")
	setString(&cfg.StorageBackend, "STORAGE_BACKEND")
	setString(&cfg.SQLitePath, "SQLITE_PATH")
	setString(&cfg.PostgresDSN, "POSTGRES_DSN")
	setString(&cfg.CacheBackend, "CACHE_BACKEND")
	setString(&cfg.RedisAddr, "REDIS_ADDR")
	setString(&cfg.JWTSecret, "JWT_SECRET")
	setString(&cfg.JWTAccessTTLRaw, "JWT_ACCESS_TTL")
	setString(&cfg.JWTRefreshTTLRaw, "JWT_REFRESH_TTL")
	setString(&cfg.StorageDir, "STORAGE_DIR")
	setString(&cfg.PosterDir, "POSTER_DIR")
	setInt(&cfg.DownloadWorkers, "DOWNLOAD_WORKERS")
	setString(&cfg.Aria2RPCURL, "ARIA2_RPC_URL")
	setString(&cfg.Aria2Secret, "ARIA2_SECRET")
}

func setString(target *string, key string) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		*target = v
	}
}

func setInt(target *int, key string) {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil {
			*target = parsed
		}
	}
}

func (c *Config) normalize() error {
	c.StorageBackend = strings.ToLower(strings.TrimSpace(c.StorageBackend))
	c.CacheBackend = strings.ToLower(strings.TrimSpace(c.CacheBackend))
	switch c.StorageBackend {
	case StorageBackendSQLite, StorageBackendPostgres:
	default:
		return fmt.Errorf("unsupported storage_backend %q", c.StorageBackend)
	}
	switch c.CacheBackend {
	case CacheBackendMemory, CacheBackendRedis:
	default:
		return fmt.Errorf("unsupported cache_backend %q", c.CacheBackend)
	}
	accessTTL, err := parseDuration(c.JWTAccessTTLRaw)
	if err != nil {
		return fmt.Errorf("jwt_access_ttl: %w", err)
	}
	refreshTTL, err := parseDuration(c.JWTRefreshTTLRaw)
	if err != nil {
		return fmt.Errorf("jwt_refresh_ttl: %w", err)
	}
	c.JWTAccessTTL = accessTTL
	c.JWTRefreshTTL = refreshTTL
	if c.DownloadWorkers <= 0 {
		c.DownloadWorkers = 2
	}
	return nil
}

func parseDuration(raw string) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("duration is empty")
	}
	d, err := time.ParseDuration(raw)
	if err == nil {
		return d, nil
	}
	if strings.HasSuffix(raw, "d") {
		days, convErr := strconv.Atoi(strings.TrimSuffix(raw, "d"))
		if convErr == nil {
			return time.Duration(days) * 24 * time.Hour, nil
		}
	}
	return 0, err
}
