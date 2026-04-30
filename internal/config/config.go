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
	AblyJWTTTLRaw     string        `yaml:"ably_jwt_ttl"`
	AblyJWTTTL        time.Duration `yaml:"-"`
	AblyKeyName       string        `yaml:"-"`
	AblyKeySecret     string        `yaml:"-"`
	AblyChannelPrefix string        `yaml:"ably_channel_prefix"`

	ResendAPIKey string `yaml:"-"`
	EmailFrom    string `yaml:"email_from"`

	EmailCodeTTL             time.Duration `yaml:"-"`
	EmailCodeTTLRaw          string        `yaml:"email_code_ttl"`
	EmailCodeLength          int           `yaml:"email_code_length"`
	EmailCodeSendInterval    time.Duration `yaml:"-"`
	EmailCodeSendIntervalRaw string        `yaml:"email_code_send_interval"`
	EmailCodeDailyLimit      int           `yaml:"email_code_daily_limit"`
	EmailCodeMaxAttempts     int           `yaml:"email_code_max_attempts"`
}

func Default() Config {
	return Config{
		Addr:                     ":8080",
		StorageBackend:           StorageBackendPostgres,
		PostgresDSN:              "postgres://user:pass@localhost:5432/watchtogether?sslmode=disable",
		CacheBackend:             CacheBackendMemory,
		RedisAddr:                "localhost:6379",
		JWTSecret:                "change-me-in-production",
		JWTAccessTTLRaw:          "15m",
		JWTRefreshTTLRaw:         "168h",
		StorageDir:               "./data/videos",
		PosterDir:                "./data/posters",
		DownloadWorkers:          2,
		Aria2RPCURL:              "http://localhost:6800/jsonrpc",
		AblyTokenTTLRaw:          "30m",
		AblyJWTTTLRaw:            "",
		AblyChannelPrefix:        "watchtogether",
		EmailFrom:                "WatchTogether <login@verify.bestlkl.top>",
		EmailCodeTTLRaw:          "10m",
		EmailCodeLength:          6,
		EmailCodeSendIntervalRaw: "60s",
		EmailCodeDailyLimit:      5,
		EmailCodeMaxAttempts:     5,
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
	setString(&cfg.AblyJWTTTLRaw, "ABLY_JWT_TTL")
	setString(&cfg.AblyChannelPrefix, "ABLY_CHANNEL_PREFIX")
	setString(&cfg.ResendAPIKey, "RESEND_API_KEY")
	if v := strings.TrimSpace(os.Getenv("EMAIL_FROM")); v != "" {
		cfg.EmailFrom = v
	} else if v := strings.TrimSpace(os.Getenv("RESEND_FROM")); v != "" {
		cfg.EmailFrom = v
	}
	setString(&cfg.EmailCodeTTLRaw, "EMAIL_CODE_TTL")
	setInt(&cfg.EmailCodeLength, "EMAIL_CODE_LENGTH")
	setString(&cfg.EmailCodeSendIntervalRaw, "EMAIL_CODE_SEND_INTERVAL")
	setInt(&cfg.EmailCodeDailyLimit, "EMAIL_CODE_DAILY_LIMIT")
	setInt(&cfg.EmailCodeMaxAttempts, "EMAIL_CODE_MAX_ATTEMPTS")
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
	jwtTTLRaw := strings.TrimSpace(c.AblyJWTTTLRaw)
	if jwtTTLRaw == "" {
		jwtTTLRaw = c.AblyTokenTTLRaw
	}
	ablyJWTTTL, err := parseDuration(jwtTTLRaw)
	if err != nil {
		return fmt.Errorf("ably_jwt_ttl: %w", err)
	}
	if ablyJWTTTL <= 0 {
		return errors.New("ably_jwt_ttl must be greater than 0")
	}
	if ablyJWTTTL > 24*time.Hour {
		return errors.New("ably_jwt_ttl must not exceed 24h")
	}
	c.AblyJWTTTL = ablyJWTTTL
	if err := parseAblyRootKey(c); err != nil && strings.TrimSpace(c.AblyRootKey) != "" {
		return err
	}
	codeTTL, err := parseDuration(c.EmailCodeTTLRaw)
	if err != nil {
		return fmt.Errorf("email_code_ttl: %w", err)
	}
	if codeTTL <= 0 {
		return errors.New("email_code_ttl must be greater than 0")
	}
	c.EmailCodeTTL = codeTTL
	sendInt, err := parseDuration(c.EmailCodeSendIntervalRaw)
	if err != nil {
		return fmt.Errorf("email_code_send_interval: %w", err)
	}
	if sendInt <= 0 {
		return errors.New("email_code_send_interval must be greater than 0")
	}
	c.EmailCodeSendInterval = sendInt
	if c.EmailCodeLength <= 0 {
		c.EmailCodeLength = 6
	}
	if c.EmailCodeDailyLimit <= 0 {
		c.EmailCodeDailyLimit = 5
	}
	if c.EmailCodeMaxAttempts <= 0 {
		c.EmailCodeMaxAttempts = 5
	}
	c.EmailFrom = strings.TrimSpace(c.EmailFrom)
	if c.EmailFrom == "" {
		c.EmailFrom = "WatchTogether <login@verify.bestlkl.top>"
	}
	c.AblyChannelPrefix = strings.TrimSpace(c.AblyChannelPrefix)
	if c.AblyChannelPrefix == "" {
		c.AblyChannelPrefix = "watchtogether"
	}
	if c.DownloadWorkers <= 0 {
		c.DownloadWorkers = 2
	}
	return nil
}

func parseAblyRootKey(c *Config) error {
	raw := strings.TrimSpace(c.AblyRootKey)
	if raw == "" {
		c.AblyKeyName = ""
		c.AblyKeySecret = ""
		return nil
	}
	idx := strings.Index(raw, ":")
	if idx <= 0 || idx == len(raw)-1 {
		return fmt.Errorf("ably_root_key: expected keyName:keySecret")
	}
	c.AblyKeyName = raw[:idx]
	c.AblyKeySecret = raw[idx+1:]
	if strings.TrimSpace(c.AblyKeyName) == "" || strings.TrimSpace(c.AblyKeySecret) == "" {
		return fmt.Errorf("ably_root_key: keyName and keySecret must be non-empty")
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
