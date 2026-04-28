package config_test

import (
	"os"
	"testing"

	"watchtogether/internal/config"
)

func TestLoad_VercelForcesPostgresRedisAndUsesDATABASE_URL(t *testing.T) {
	t.Setenv("VERCEL", "1")
	t.Setenv("DATABASE_URL", "postgres://v:test@db.example:5432/app?sslmode=require")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StorageBackend != config.StorageBackendPostgres {
		t.Fatalf("storage_backend: got %q want postgres", cfg.StorageBackend)
	}
	if cfg.CacheBackend != config.CacheBackendRedis {
		t.Fatalf("cache_backend: got %q want redis", cfg.CacheBackend)
	}
	if cfg.PostgresDSN != "postgres://v:test@db.example:5432/app?sslmode=require" {
		t.Fatalf("PostgresDSN: got %q", cfg.PostgresDSN)
	}
}

func TestLoad_VercelPostgresDSNExplicitOverridesDATABASE_URL(t *testing.T) {
	t.Setenv("VERCEL", "1")
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

func TestLoad_LocalDefaultStillSQLiteMemoryWhenNoConfigFile(t *testing.T) {
	// Ensure no accidental VERCEL leakage from parent env in CI
	t.Setenv("VERCEL", "")
	_ = os.Unsetenv("VERCEL")

	cfg, err := config.Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.StorageBackend != config.StorageBackendSQLite {
		t.Fatalf("storage_backend: got %q want sqlite", cfg.StorageBackend)
	}
	if cfg.CacheBackend != config.CacheBackendMemory {
		t.Fatalf("cache_backend: got %q want memory", cfg.CacheBackend)
	}
}
