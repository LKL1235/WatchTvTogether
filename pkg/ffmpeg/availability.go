package ffmpeg

import (
	"context"
	"os/exec"
	"time"

	"watchtogether/pkg/toolcheck"
)

const defaultAvailabilityTimeout = 5 * time.Second

type Availability struct {
	FFmpeg  toolcheck.Result
	FFprobe toolcheck.Result
}

func CheckAvailability(ctx context.Context) Availability {
	return CheckAvailabilityWithTimeout(ctx, defaultAvailabilityTimeout)
}

func CheckAvailabilityWithTimeout(ctx context.Context, timeout time.Duration) Availability {
	return Availability{
		FFmpeg:  toolcheck.Check(ctx, "ffmpeg", []string{"-version"}, timeout),
		FFprobe: toolcheck.Check(ctx, "ffprobe", []string{"-version"}, timeout),
	}
}

func commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
