package config

import (
	"os"
	"strings"
)

// Config holds process-wide settings from the environment.
type Config struct {
	Addr string
	// Executable paths; empty means look up in PATH with default names.
	FFmpegPath  string
	FFprobePath string
	YtDlpPath   string
	Aria2cPath  string
}

// Load reads configuration from environment variables.
func Load() Config {
	return Config{
		Addr:        getEnv("ADDR", ":8080"),
		FFmpegPath:  strings.TrimSpace(os.Getenv("FFMPEG_PATH")),
		FFprobePath: strings.TrimSpace(os.Getenv("FFPROBE_PATH")),
		YtDlpPath:   strings.TrimSpace(os.Getenv("YT_DLP_PATH")),
		Aria2cPath:  strings.TrimSpace(os.Getenv("ARIA2C_PATH")),
	}
}

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}
