package ffmpeg

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type Metadata struct {
	Duration float64
	Format   string
}

type ProbeOption func(*probeConfig)

type probeConfig struct {
	binary  string
	timeout time.Duration
}

func Probe(ctx context.Context, path string, opts ...ProbeOption) (Metadata, error) {
	cfg := probeConfig{binary: "ffprobe", timeout: 10 * time.Second}
	for _, opt := range opts {
		opt(&cfg)
	}
	if strings.TrimSpace(path) == "" {
		return Metadata{}, errors.New("ffmpeg: file path is required")
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, cfg.binary, "-v", "quiet", "-print_format", "json", "-show_format", path).Output()
	if err != nil {
		return Metadata{}, fmt.Errorf("ffprobe metadata: %w", err)
	}
	var payload struct {
		Format struct {
			Duration   string `json:"duration"`
			FormatName string `json:"format_name"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return Metadata{}, fmt.Errorf("parse ffprobe output: %w", err)
	}
	duration, _ := strconv.ParseFloat(payload.Format.Duration, 64)
	return Metadata{Duration: duration, Format: firstFormat(payload.Format.FormatName)}, nil
}

func ExtractPoster(ctx context.Context, inputPath, outputPath string, duration float64, opts ...ProbeOption) error {
	cfg := probeConfig{binary: "ffmpeg", timeout: 30 * time.Second}
	for _, opt := range opts {
		opt(&cfg)
	}
	seek := "0"
	if duration > 0 {
		seek = strconv.FormatFloat(duration/2, 'f', 3, 64)
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, cfg.binary, "-y", "-ss", seek, "-i", inputPath, "-vframes", "1", "-q:v", "2", outputPath)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract poster: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func WithBinary(binary string) ProbeOption {
	return func(cfg *probeConfig) {
		if binary != "" {
			cfg.binary = binary
		}
	}
}

func WithTimeout(timeout time.Duration) ProbeOption {
	return func(cfg *probeConfig) {
		if timeout > 0 {
			cfg.timeout = timeout
		}
	}
}

func firstFormat(raw string) string {
	if raw == "" {
		return ""
	}
	format := strings.Split(raw, ",")[0]
	if format == "mov" || format == "mp4" || strings.Contains(raw, "mp4") {
		return "mp4"
	}
	if format == "" {
		return strings.TrimPrefix(strings.ToLower(filepath.Ext(raw)), ".")
	}
	return format
}
