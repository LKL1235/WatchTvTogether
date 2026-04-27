package ffmpeg_test

import (
	"os/exec"
	"testing"

	"github.com/example/watchtogether/pkg/ffmpeg"
)

func TestCheckAvailability(t *testing.T) {
	caps := ffmpeg.CheckAvailability()

	// 根据系统实际情况断言：
	// 若 ffmpeg 在 PATH 中，期望 Available=true 且 Version 非空；
	// 若不在 PATH，期望 Available=false 且 Version 为空。

	ffmpegInPath := toolInPath("ffmpeg")
	if caps.FFmpegAvailable != ffmpegInPath {
		t.Errorf("FFmpegAvailable=%v, 期望与 PATH 中是否有 ffmpeg(%v) 一致",
			caps.FFmpegAvailable, ffmpegInPath)
	}
	if caps.FFmpegAvailable && caps.FFmpegVersion == "" {
		t.Error("ffmpeg 可用时，FFmpegVersion 不应为空字符串")
	}
	if !caps.FFmpegAvailable && caps.FFmpegVersion != "" {
		t.Errorf("ffmpeg 不可用时，FFmpegVersion 应为空，实际为 %q", caps.FFmpegVersion)
	}

	ffprobeInPath := toolInPath("ffprobe")
	if caps.FFprobeAvailable != ffprobeInPath {
		t.Errorf("FFprobeAvailable=%v, 期望与 PATH 中是否有 ffprobe(%v) 一致",
			caps.FFprobeAvailable, ffprobeInPath)
	}
	if caps.FFprobeAvailable && caps.FFprobeVersion == "" {
		t.Error("ffprobe 可用时，FFprobeVersion 不应为空字符串")
	}
}

func TestCheckAvailability_Idempotent(t *testing.T) {
	// 多次调用结果应一致
	first := ffmpeg.CheckAvailability()
	second := ffmpeg.CheckAvailability()

	if first.FFmpegAvailable != second.FFmpegAvailable {
		t.Error("多次调用 CheckAvailability 结果不一致（FFmpeg）")
	}
	if first.FFprobeAvailable != second.FFprobeAvailable {
		t.Error("多次调用 CheckAvailability 结果不一致（FFprobe）")
	}
}

func toolInPath(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}
