// Package ffmpeg 提供 FFmpeg / ffprobe 工具的可用性检查与基础封装。
//
// 设计原则：服务启动时调用 CheckAvailability()，若工具不在系统 PATH 中，
// 则返回对应字段为 false 的 Capabilities，上层业务据此禁用相关功能，
// 而不是直接 panic 或拒绝启动。
package ffmpeg

import (
	"bytes"
	"context"
	"os/exec"
	"strings"
	"time"
)

// Capabilities 描述当前系统上 FFmpeg 相关工具的可用状态。
type Capabilities struct {
	// FFmpegAvailable 表示 ffmpeg 是否在系统 PATH 中且可正常执行。
	// 为 false 时，HLS 流合并（m3u8 → mp4）与关键帧海报生成功能将被禁用。
	FFmpegAvailable bool

	// FFprobeAvailable 表示 ffprobe 是否在系统 PATH 中且可正常执行。
	// 为 false 时，视频时长/格式/分辨率的自动元数据提取功能将被禁用。
	FFprobeAvailable bool

	// FFmpegVersion 为 ffmpeg 的版本字符串（例如 "6.1.1"），
	// 仅当 FFmpegAvailable 为 true 时有效。
	FFmpegVersion string

	// FFprobeVersion 为 ffprobe 的版本字符串，
	// 仅当 FFprobeAvailable 为 true 时有效。
	FFprobeVersion string
}

// CheckAvailability 检查系统中 ffmpeg 与 ffprobe 工具是否可用。
//
// 检测流程：
//  1. exec.LookPath 确认可执行文件存在于 PATH；
//  2. 执行 `ffmpeg -version` / `ffprobe -version` 并解析版本号，
//     以双重确认工具确实可运行（而非权限问题或损坏的二进制）。
//
// 函数内部设有 5 秒超时，不会无限阻塞。
func CheckAvailability() Capabilities {
	return Capabilities{
		FFmpegAvailable:  isToolAvailable("ffmpeg"),
		FFmpegVersion:    toolVersion("ffmpeg", "-version"),
		FFprobeAvailable: isToolAvailable("ffprobe"),
		FFprobeVersion:   toolVersion("ffprobe", "-version"),
	}
}

// isToolAvailable 通过 exec.LookPath + 实际执行双重确认工具可用。
func isToolAvailable(name string) bool {
	if _, err := exec.LookPath(name); err != nil {
		return false
	}
	// LookPath 只确认文件存在，还需执行一次确认不是损坏的二进制
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, "-version")
	return cmd.Run() == nil
}

// toolVersion 返回工具版本字符串的第一行中的版本号部分，
// 例如 "ffmpeg version 6.1.1 ..." → "6.1.1"。
// 若工具不可用或解析失败，返回空字符串。
func toolVersion(name, flag string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var out bytes.Buffer
	cmd := exec.CommandContext(ctx, name, flag)
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return ""
	}

	// 取第一行，形如 "ffmpeg version 6.1.1 Copyright ..."
	firstLine := strings.SplitN(out.String(), "\n", 2)[0]
	fields := strings.Fields(firstLine)
	// 版本号通常在第三个 field（index 2）：["ffmpeg", "version", "6.1.1", ...]
	if len(fields) >= 3 {
		return fields[2]
	}
	return firstLine
}
