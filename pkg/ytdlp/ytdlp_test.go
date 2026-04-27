package ytdlp_test

import (
	"os/exec"
	"testing"

	"github.com/example/watchtogether/pkg/ytdlp"
)

func TestCheckAvailability(t *testing.T) {
	caps := ytdlp.CheckAvailability()

	ytdlpInPath := toolInPath("yt-dlp")

	if caps.Available != ytdlpInPath {
		t.Errorf("Available=%v，期望与 PATH 中是否有 yt-dlp(%v) 一致",
			caps.Available, ytdlpInPath)
	}
	if caps.Available && caps.Version == "" {
		t.Error("yt-dlp 可用时，Version 不应为空字符串")
	}
	if !caps.Available && caps.Version != "" {
		t.Errorf("yt-dlp 不可用时，Version 应为空，实际为 %q", caps.Version)
	}
}

func TestCheckAvailability_Idempotent(t *testing.T) {
	first := ytdlp.CheckAvailability()
	second := ytdlp.CheckAvailability()

	if first.Available != second.Available {
		t.Error("多次调用 CheckAvailability 结果不一致")
	}
	if first.Version != second.Version {
		t.Errorf("多次调用版本字符串不一致: %q vs %q", first.Version, second.Version)
	}
}

func toolInPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
