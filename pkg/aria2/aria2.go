// Package aria2 提供 aria2c 工具（aria2 RPC 服务）的可用性检查与基础封装。
//
// 设计原则：服务启动时调用 CheckAvailability()，若 aria2c 不在系统 PATH 中，
// 则返回 Available=false 的 Capabilities，上层业务据此禁用
// 磁力链接 / BT 种子下载功能，而非拒绝服务启动。
package aria2

import (
	"context"
	"os/exec"
	"time"
)

// Capabilities 描述当前系统上 aria2c 工具的可用状态。
type Capabilities struct {
	// Available 表示 aria2c 是否在系统 PATH 中且可正常执行。
	// 为 false 时，磁力链接与 BT 种子下载功能将被禁用；
	// 直链、m3u8、yt-dlp 等其他下载方式不受影响。
	Available bool

	// Version 为 aria2c 的版本字符串，仅当 Available 为 true 时有效。
	Version string
}

// CheckAvailability 检查系统中 aria2c 工具是否可用。
func CheckAvailability() Capabilities {
	if _, err := exec.LookPath("aria2c"); err != nil {
		return Capabilities{Available: false}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "aria2c", "--version")
	out, err := cmd.Output()
	if err != nil {
		return Capabilities{Available: false}
	}

	// 版本输出首行形如："aria2 version 1.36.0"
	version := parseVersion(string(out))
	return Capabilities{
		Available: true,
		Version:   version,
	}
}

func parseVersion(output string) string {
	for _, line := range splitLines(output) {
		if len(line) > 0 {
			// 首行即版本行
			return line
		}
	}
	return ""
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
