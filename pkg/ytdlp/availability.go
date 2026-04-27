package ytdlp

import (
	"context"
	"time"

	"watchtogether/pkg/toolcheck"
)

func CheckAvailability(ctx context.Context) toolcheck.Result {
	return toolcheck.Check(ctx, "yt-dlp", []string{"--version"}, 5*time.Second)
}
