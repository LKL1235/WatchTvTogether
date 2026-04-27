// Package ytdlp 提供 yt-dlp 工具的可用性检查与基础封装。
//
// 设计原则：服务启动时调用 CheckAvailability()，若 yt-dlp 不在系统 PATH 中，
// 则返回 Available=false 的 Capabilities，上层业务据此禁用
// "从 YouTube / Bilibili 等站点导入视频"功能，而非拒绝服务启动。
package ytdlp

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Capabilities 描述当前系统上 yt-dlp 工具的可用状态。
type Capabilities struct {
	// Available 表示 yt-dlp 是否在系统 PATH 中且可正常执行。
	// 为 false 时，从 YouTube / Bilibili 等 yt-dlp 支持站点导入视频的功能将被禁用；
	// 直链（mp4/mkv 等）下载与 m3u8（FFmpeg）下载不受影响。
	Available bool

	// Version 为 yt-dlp 的版本字符串（例如 "2024.03.10"），
	// 仅当 Available 为 true 时有效。
	Version string
}

// CheckAvailability 检查系统中 yt-dlp 工具是否可用。
//
// 检测流程：
//  1. exec.LookPath 确认可执行文件存在于 PATH；
//  2. 执行 `yt-dlp --version` 并读取版本字符串，
//     以双重确认工具确实可运行（而非权限问题或损坏的二进制）。
//
// 函数内部设有 10 秒超时（yt-dlp 首次运行可能需要检查更新）。
func CheckAvailability() Capabilities {
	if _, err := exec.LookPath("yt-dlp"); err != nil {
		return Capabilities{Available: false}
	}

	version := runVersion()
	return Capabilities{
		Available: version != "",
		Version:   version,
	}
}

// runVersion 执行 yt-dlp --version 并返回版本字符串。
// 失败时返回空字符串。
func runVersion() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, "yt-dlp", "--version")
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}

	return strings.TrimSpace(out.String())
}
