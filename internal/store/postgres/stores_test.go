package postgres_test

import (
	"context"
	"os"
	"testing"

	"watchtogether/internal/store/postgres"
	"watchtogether/internal/store/testutil"
)

func TestStores(t *testing.T) {
	dsn := os.Getenv("POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_TEST_DSN is not set")
	}

	testutil.RunStoreSuite(t, func(t *testing.T) testutil.Suite {
		t.Helper()
		ctx := context.Background()
		db, err := postgres.Open(ctx, dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_, _ = db.ExecContext(ctx, "TRUNCATE videos, rooms, users")
			if err := db.Close(); err != nil {
				t.Errorf("close db: %v", err)
			}
		})

		return testutil.Suite{
			Users:  postgres.NewUserStore(db),
			Rooms:  postgres.NewRoomStore(db),
			Videos: postgres.NewVideoStore(db),
		}
	})
}
