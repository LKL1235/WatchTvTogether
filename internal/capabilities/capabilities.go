// Package capabilities 汇总所有本地工具的可用性，供服务启动时一次性检测，
// 并暴露 /api/capabilities 接口供前端据此动态禁用不可用功能入口。
package capabilities

import (
	"log/slog"

	"github.com/example/watchtogether/pkg/aria2"
	"github.com/example/watchtogether/pkg/ffmpeg"
	"github.com/example/watchtogether/pkg/ytdlp"
)

// ServiceCapabilities 汇总所有依赖工具的可用状态与衍生功能开关。
type ServiceCapabilities struct {
	// 工具层
	FFmpeg  ffmpeg.Capabilities
	YtDlp  ytdlp.Capabilities
	Aria2  aria2.Capabilities

	// 功能层：由工具可用性推导，供 API 直接序列化返回给前端
	Features Features
}

// Features 描述各项高层功能是否可用。
type Features struct {
	// HLSDownload 需要 ffmpeg 可用（m3u8 合并为 mp4）
	HLSDownload bool `json:"hls_download"`

	// PosterGeneration 需要 ffmpeg 可用（关键帧截图）
	PosterGeneration bool `json:"poster_generation"`

	// MetadataExtraction 需要 ffprobe 可用（时长/分辨率/编码信息）
	MetadataExtraction bool `json:"metadata_extraction"`

	// YtDlpImport 需要 yt-dlp 可用（YouTube/Bilibili 等站点导入）
	YtDlpImport bool `json:"ytdlp_import"`

	// MagnetDownload 需要 aria2c 可用（磁力链接/BT 种子下载）
	MagnetDownload bool `json:"magnet_download"`
}

// Check 在服务启动时调用，检测所有本地工具可用性并打印摘要日志。
// 工具缺失不会导致 panic，仅禁用对应功能。
func Check() ServiceCapabilities {
	ffmpegCaps := ffmpeg.CheckAvailability()
	ytdlpCaps := ytdlp.CheckAvailability()
	aria2Caps := aria2.CheckAvailability()

	caps := ServiceCapabilities{
		FFmpeg: ffmpegCaps,
		YtDlp:  ytdlpCaps,
		Aria2:  aria2Caps,
		Features: Features{
			HLSDownload:        ffmpegCaps.FFmpegAvailable,
			PosterGeneration:   ffmpegCaps.FFmpegAvailable,
			MetadataExtraction: ffmpegCaps.FFprobeAvailable,
			YtDlpImport:        ytdlpCaps.Available,
			MagnetDownload:     aria2Caps.Available,
		},
	}

	logSummary(caps)
	return caps
}

// logSummary 打印工具可用性摘要，方便运维快速确认服务启动状态。
func logSummary(caps ServiceCapabilities) {
	logToolStatus("ffmpeg", caps.FFmpeg.FFmpegAvailable, caps.FFmpeg.FFmpegVersion)
	logToolStatus("ffprobe", caps.FFmpeg.FFprobeAvailable, caps.FFmpeg.FFprobeVersion)
	logToolStatus("yt-dlp", caps.YtDlp.Available, caps.YtDlp.Version)
	logToolStatus("aria2c", caps.Aria2.Available, caps.Aria2.Version)

	if !caps.Features.HLSDownload {
		slog.Warn("ffmpeg 不可用：HLS（m3u8）下载与海报生成功能已禁用")
	}
	if !caps.Features.MetadataExtraction {
		slog.Warn("ffprobe 不可用：自动元数据提取功能已禁用")
	}
	if !caps.Features.YtDlpImport {
		slog.Warn("yt-dlp 不可用：从 YouTube/Bilibili 等站点导入功能已禁用")
	}
	if !caps.Features.MagnetDownload {
		slog.Warn("aria2c 不可用：磁力链接/BT 种子下载功能已禁用")
	}
}

func logToolStatus(name string, available bool, version string) {
	if available {
		slog.Info("工具可用", "tool", name, "version", version)
	} else {
		slog.Warn("工具不可用（功能降级）", "tool", name)
	}
}
