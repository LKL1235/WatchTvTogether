package sqlite

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaFS embed.FS

func Open(ctx context.Context, path string) (*sql.DB, error) {
	if path != ":memory:" && !strings.HasPrefix(path, "file:") {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite directory: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	if err := initSchema(ctx, db); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func initSchema(ctx context.Context, db *sql.DB) error {
	schema, err := schemaFS.ReadFile("schema.sql")
	if err != nil {
		return fmt.Errorf("read schema: %w", err)
	}
	if _, err := db.ExecContext(ctx, string(schema)); err != nil {
		return fmt.Errorf("init schema: %w", err)
	}
	return nil
}
