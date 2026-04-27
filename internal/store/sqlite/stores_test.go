package sqlite_test

import (
	"context"
	"testing"

	"watchtogether/internal/store/sqlite"
	"watchtogether/internal/store/testutil"
)

func TestStores(t *testing.T) {
	testutil.RunStoreSuite(t, func(t *testing.T) testutil.Suite {
		t.Helper()
		ctx := context.Background()
		db, err := sqlite.Open(ctx, ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := db.Close(); err != nil {
				t.Errorf("close db: %v", err)
			}
		})

		return testutil.Suite{
			Users:         sqlite.NewUserStore(db),
			Rooms:         sqlite.NewRoomStore(db),
			Videos:        sqlite.NewVideoStore(db),
			DownloadTasks: sqlite.NewDownloadTaskStore(db),
		}
	})
}
