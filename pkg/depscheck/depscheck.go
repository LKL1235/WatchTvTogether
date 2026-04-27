package depscheck

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const execTimeout = 8 * time.Second

// Status describes whether an external tool is present and usable.
type Status struct {
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Version string `json:"version,omitempty"`
	Path    string `json:"path,omitempty"`
	Error   string `json:"error,omitempty"`
}

// Report is returned by All for health checks and startup diagnostics.
type Report struct {
	FFmpeg  Status `json:"ffmpeg"`
	FFprobe Status `json:"ffprobe"`
	YtDlp   Status `json:"yt_dlp"`
	Aria2   Status `json:"aria2"`
}

// Paths selects which executables to probe. Zero values use common defaults.
type Paths struct {
	FFmpeg  string
	FFprobe string
	YtDlp   string
	Aria2c  string
}

// resolve returns the executable name to run.
func (p Paths) resolve(env, def string) string {
	switch env {
	case "ffmpeg":
		if p.FFmpeg != "" {
			return p.FFmpeg
		}
	case "ffprobe":
		if p.FFprobe != "" {
			return p.FFprobe
		}
	case "yt_dlp", "ytdlp":
		if p.YtDlp != "" {
			return p.YtDlp
		}
	case "aria2", "aria2c":
		if p.Aria2c != "" {
			return p.Aria2c
		}
	}
	return def
}

// All checks FFmpeg (version ≥ 6), ffprobe, yt-dlp, and aria2c in one pass.
func All(ctx context.Context, p Paths) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	ffmpegName := p.resolve("ffmpeg", "ffmpeg")
	ffprobeName := p.resolve("ffprobe", "ffprobe")
	ytdlpName := p.resolve("ytdlp", "yt-dlp")
	aria2cName := p.resolve("aria2c", "aria2c")

	r := Report{
		FFmpeg:  checkFFmpeg(ctx, ffmpegName),
		FFprobe: checkFFprobe(ctx, ffprobeName),
		YtDlp:   checkYtdlp(ctx, ytdlpName),
		Aria2:   checkAria2(ctx, aria2cName),
	}
	if r.FFprobe.Name == "" {
		r.FFprobe.Name = "ffprobe"
	}
	if r.FFprobe.Path == "" {
		r.FFprobe.Path = ffprobeName
	}
	return r
}

// AllOK returns true if every required tool is available (including FFmpeg major ≥ 6).
func (r Report) AllOK() bool {
	return r.FFmpeg.OK && r.FFprobe.OK && r.YtDlp.OK && r.Aria2.OK
}

func runOut(ctx context.Context, name string, arg ...string) (string, error) {
	cctx, cancel := context.WithTimeout(ctx, execTimeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, name, arg...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if cctx.Err() == context.DeadlineExceeded {
			return string(out), fmt.Errorf("timeout running %q: %w", name, cctx.Err())
		}
		if len(out) > 0 {
			return string(out), fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return "", err
	}
	return string(out), nil
}

var ffmpegVersionLine = regexp.MustCompile(`ffmpeg version (\d+)(?:\.(\d+))?(?:\.(\d+))?`)

func checkFFmpeg(ctx context.Context, name string) Status {
	s := Status{Name: "ffmpeg", Path: name}
	out, err := runOut(ctx, name, "-version")
	if err != nil {
		s.Error = err.Error()
		return s
	}
	lines := strings.Split(out, "\n")
	first := ""
	if len(lines) > 0 {
		first = lines[0]
	}
	m := ffmpegVersionLine.FindStringSubmatch(first)
	if m == nil {
		s.Error = "could not parse ffmpeg version (need FFmpeg 6+)"
		return s
	}
	major, _ := strconv.Atoi(m[1])
	if major < 6 {
		s.Error = fmt.Sprintf("FFmpeg %d is too old (need 6+)", major)
		s.Version = strings.TrimSpace(first)
		return s
	}
	s.OK = true
	s.Version = strings.TrimSpace(first)
	return s
}

var ffprobeVersionLine = regexp.MustCompile(`ffprobe version (\S+)`)

func checkFFprobe(ctx context.Context, name string) Status {
	s := Status{Name: "ffprobe", Path: name}
	out, err := runOut(ctx, name, "-version")
	if err != nil {
		s.Error = err.Error()
		return s
	}
	line := strings.Split(out, "\n")
	first := ""
	if len(line) > 0 {
		first = line[0]
	}
	if m := ffprobeVersionLine.FindStringSubmatch(first); m != nil {
		s.Version = m[1]
	} else {
		s.Version = strings.TrimSpace(first)
	}
	s.OK = true
	if s.Version == "" {
		s.Error = "could not parse ffprobe version output"
		s.OK = false
	}
	return s
}

func checkYtdlp(ctx context.Context, name string) Status {
	s := Status{Name: "yt-dlp", Path: name}
	out, err := runOut(ctx, name, "--version")
	if err != nil {
		// some installs print help to stderr only; --version is standard
		if errors.Is(err, exec.ErrNotFound) {
			s.Error = "yt-dlp not found in PATH"
		} else {
			s.Error = err.Error()
		}
		return s
	}
	s.OK = true
	s.Version = strings.TrimSpace(out)
	return s
}

var aria2VersionLine = regexp.MustCompile(`aria2 version ([0-9.]+)`)

func checkAria2(ctx context.Context, name string) Status {
	s := Status{Name: "aria2c", Path: name}
	out, err := runOut(ctx, name, "--version")
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			s.Error = "aria2c not found in PATH"
		} else {
			s.Error = err.Error()
		}
		return s
	}
	lines := strings.Split(out, "\n")
	ver := ""
	if len(lines) > 0 {
		if m := aria2VersionLine.FindStringSubmatch(lines[0]); m != nil {
			ver = m[1]
		} else {
			ver = strings.TrimSpace(lines[0])
		}
	}
	s.OK = true
	s.Version = ver
	if s.Version == "" {
		s.Error = "could not parse aria2c version"
		s.OK = false
	}
	return s
}
