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
	StorageBackendPostgres = "postgres"
	CacheBackendMemory     = "memory"
	CacheBackendRedis      = "redis"
)

type Config struct {
	Addr           string `yaml:"addr"`
	StorageBackend string `yaml:"storage_backend"`
	PostgresDSN    string `yaml:"postgres_dsn"`
	CacheBackend   string `yaml:"cache_backend"`
	RedisAddr      string `yaml:"redis_addr"`
	// RedisURL is optional; when set, it takes precedence over RedisAddr.
	RedisURL          string        `yaml:"redis_url"`
	JWTSecret         string        `yaml:"jwt_secret"`
	JWTAccessTTL      time.Duration `yaml:"-"`
	JWTRefreshTTL     time.Duration `yaml:"-"`
	JWTAccessTTLRaw   string        `yaml:"jwt_access_ttl"`
	JWTRefreshTTLRaw  string        `yaml:"jwt_refresh_ttl"`
	StorageDir        string        `yaml:"storage_dir"`
	PosterDir         string        `yaml:"poster_dir"`
	DownloadWorkers   int           `yaml:"download_workers"`
	Aria2RPCURL       string        `yaml:"aria2_rpc_url"`
	Aria2Secret       string        `yaml:"aria2_secret"`
	CorsOrigins       []string      `yaml:"cors_origins"`
	AblyRootKey       string        `yaml:"ably_root_key"`
	AblyTokenTTL      time.Duration `yaml:"-"`
	AblyTokenTTLRaw   string        `yaml:"ably_token_ttl"`
	AblyChannelPrefix string        `yaml:"ably_channel_prefix"`
}

func Default() Config {
	return Config{
		Addr:              ":8080",
		StorageBackend:    StorageBackendPostgres,
		PostgresDSN:       "postgres://user:pass@localhost:5432/watchtogether?sslmode=disable",
		CacheBackend:      CacheBackendMemory,
		RedisAddr:         "localhost:6379",
		JWTSecret:         "change-me-in-production",
		JWTAccessTTLRaw:   "15m",
		JWTRefreshTTLRaw:  "168h",
		StorageDir:        "./data/videos",
		PosterDir:         "./data/posters",
		DownloadWorkers:   2,
		Aria2RPCURL:       "http://localhost:6800/jsonrpc",
		AblyTokenTTLRaw:   "30m",
		AblyChannelPrefix: "watchtogether",
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
	// Many PaaS set PORT; use it when ADDR is not explicitly set.
	addrEnv := strings.TrimSpace(os.Getenv("ADDR"))
	portEnv := strings.TrimSpace(os.Getenv("PORT"))
	if addrEnv != "" {
		cfg.Addr = addrEnv
	} else if portEnv != "" {
		cfg.Addr = ":" + portEnv
	}
	setString(&cfg.StorageBackend, "STORAGE_BACKEND")
	// Priority: POSTGRES_URL (Vercel) > POSTGRES_DSN > DATABASE_URL.
	if v := strings.TrimSpace(os.Getenv("POSTGRES_URL")); v != "" {
		cfg.PostgresDSN = v
	} else if v := strings.TrimSpace(os.Getenv("POSTGRES_DSN")); v != "" {
		cfg.PostgresDSN = v
	} else if v := strings.TrimSpace(os.Getenv("DATABASE_URL")); v != "" {
		// Many hosts inject DATABASE_URL as a fallback.
		cfg.PostgresDSN = v
	}
	setString(&cfg.CacheBackend, "CACHE_BACKEND")
	setString(&cfg.RedisAddr, "REDIS_ADDR")
	setString(&cfg.RedisURL, "REDIS_URL")
	setString(&cfg.JWTSecret, "JWT_SECRET")
	setString(&cfg.JWTAccessTTLRaw, "JWT_ACCESS_TTL")
	setString(&cfg.JWTRefreshTTLRaw, "JWT_REFRESH_TTL")
	setString(&cfg.StorageDir, "STORAGE_DIR")
	setString(&cfg.PosterDir, "POSTER_DIR")
	setInt(&cfg.DownloadWorkers, "DOWNLOAD_WORKERS")
	setString(&cfg.Aria2RPCURL, "ARIA2_RPC_URL")
	setString(&cfg.Aria2Secret, "ARIA2_SECRET")
	setString(&cfg.AblyRootKey, "ABLY_ROOT_KEY")
	setString(&cfg.AblyTokenTTLRaw, "ABLY_TOKEN_TTL")
	setString(&cfg.AblyChannelPrefix, "ABLY_CHANNEL_PREFIX")
	if v := strings.TrimSpace(os.Getenv("CORS_ORIGINS")); v != "" {
		cfg.CorsOrigins = splitCSV(v)
	}
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		p := strings.TrimSpace(part)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
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
	case StorageBackendPostgres:
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
	ablyTTL, err := parseDuration(c.AblyTokenTTLRaw)
	if err != nil {
		return fmt.Errorf("ably_token_ttl: %w", err)
	}
	if ablyTTL <= 0 {
		return errors.New("ably_token_ttl must be greater than 0")
	}
	if ablyTTL > 24*time.Hour {
		return errors.New("ably_token_ttl must not exceed 24h")
	}
	c.AblyTokenTTL = ablyTTL
	c.AblyChannelPrefix = strings.TrimSpace(c.AblyChannelPrefix)
	if c.AblyChannelPrefix == "" {
		c.AblyChannelPrefix = "watchtogether"
	}
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
