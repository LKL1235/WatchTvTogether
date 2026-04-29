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

func TestLoad_DefaultStillSQLiteMemoryWhenNoConfigFile(t *testing.T) {
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
