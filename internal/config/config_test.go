package config_test

import (
	"testing"

	"watchtogether/internal/config"
)

func TestLoad_PostgresDSNExplicitOverridesDatabaseURL(t *testing.T) {
	t.Setenv("POSTGRES_DSN", "postgres://explicit:pass@h:5432/db?sslmode=disable")
	t.Setenv("DATABASE_URL", "postgres://from_env:should_not_win@h:5432/db")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostgresDSN != "postgres://explicit:pass@h:5432/db?sslmode=disable" {
		t.Fatalf("PostgresDSN: got %q want explicit POSTGRES_DSN", cfg.PostgresDSN)
	}
}

func TestLoad_PostgresURLHasHighestPriority(t *testing.T) {
	t.Setenv("POSTGRES_URL", "postgres://from_postgres_url:pass@h:5432/db")
	t.Setenv("POSTGRES_DSN", "postgres://from_postgres_dsn:pass@h:5432/db")
	t.Setenv("DATABASE_URL", "postgres://from_database_url:pass@h:5432/db")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostgresDSN != "postgres://from_postgres_url:pass@h:5432/db" {
		t.Fatalf("PostgresDSN: got %q want POSTGRES_URL", cfg.PostgresDSN)
	}
}

func TestLoad_DatabaseURLUsedWhenNoPostgresURLOrDSN(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://from_env:5432/db")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PostgresDSN != "postgres://from_env:5432/db" {
		t.Fatalf("PostgresDSN: got %q want DATABASE_URL", cfg.PostgresDSN)
	}
}

func TestLoad_DefaultsToPostgresMemoryAndAblySettings(t *testing.T) {
	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StorageBackend != config.StorageBackendPostgres {
		t.Fatalf("storage_backend: got %q want postgres", cfg.StorageBackend)
	}
	if cfg.CacheBackend != config.CacheBackendMemory {
		t.Fatalf("cache_backend: got %q want memory", cfg.CacheBackend)
	}
	if cfg.AblyTokenTTLRaw != "30m" || cfg.AblyTokenTTL <= 0 {
		t.Fatalf("ably token ttl: raw=%q parsed=%s", cfg.AblyTokenTTLRaw, cfg.AblyTokenTTL)
	}
	if cfg.AblyChannelPrefix != "watchtogether" {
		t.Fatalf("ably channel prefix: got %q", cfg.AblyChannelPrefix)
	}
}

func TestLoad_AblyEnv(t *testing.T) {
	t.Setenv("ABLY_ROOT_KEY", "app.key:secret")
	t.Setenv("ABLY_TOKEN_TTL", "10m")
	t.Setenv("ABLY_CHANNEL_PREFIX", "custom")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AblyRootKey != "app.key:secret" {
		t.Fatalf("ably root key not loaded")
	}
	if cfg.AblyTokenTTLRaw != "10m" || cfg.AblyTokenTTL.String() != "10m0s" {
		t.Fatalf("ably token ttl: raw=%q parsed=%s", cfg.AblyTokenTTLRaw, cfg.AblyTokenTTL)
	}
	if cfg.AblyChannelPrefix != "custom" {
		t.Fatalf("ably channel prefix: got %q", cfg.AblyChannelPrefix)
	}
}

func TestLoad_RejectsNonPositiveAblyTokenTTL(t *testing.T) {
	t.Setenv("ABLY_TOKEN_TTL", "0s")

	if _, err := config.Load(""); err == nil {
		t.Fatal("expected ABLY_TOKEN_TTL=0s to fail")
	}
}
