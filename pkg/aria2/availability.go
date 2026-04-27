package aria2

import (
	"context"
	"time"

	"watchtogether/pkg/toolcheck"
)

func CheckAvailability(ctx context.Context) toolcheck.Result {
	return toolcheck.Check(ctx, "aria2c", []string{"--version"}, 5*time.Second)
}
