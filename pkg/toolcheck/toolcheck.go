package toolcheck

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

const DefaultTimeout = 5 * time.Second

type Result struct {
	Name      string `json:"name"`
	Found     bool   `json:"found"`
	Available bool   `json:"available"`
	Path      string `json:"path,omitempty"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

func Check(ctx context.Context, name string, args []string, timeout time.Duration) Result {
	result := Result{Name: name}
	path, err := exec.LookPath(name)
	if err != nil {
		result.Error = "not found in PATH"
		return result
	}
	result.Found = true
	result.Path = path

	if len(args) == 0 {
		args = []string{"--version"}
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			result.Error = "version check timed out"
		} else {
			result.Error = err.Error()
		}
		return result
	}
	result.Available = true
	result.Version = firstLine(out.String())
	return result
}

func firstLine(value string) string {
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}
