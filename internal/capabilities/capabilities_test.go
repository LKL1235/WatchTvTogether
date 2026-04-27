package capabilities_test

import (
	"testing"

	"github.com/example/watchtogether/internal/capabilities"
)

func TestCheck_ReturnsConsistentCapabilities(t *testing.T) {
	caps := capabilities.Check()

	// Features 中的 HLSDownload/PosterGeneration 应与 FFmpegAvailable 一致
	if caps.Features.HLSDownload != caps.FFmpeg.FFmpegAvailable {
		t.Errorf("HLSDownload(%v) 应与 FFmpegAvailable(%v) 一致",
			caps.Features.HLSDownload, caps.FFmpeg.FFmpegAvailable)
	}
	if caps.Features.PosterGeneration != caps.FFmpeg.FFmpegAvailable {
		t.Errorf("PosterGeneration(%v) 应与 FFmpegAvailable(%v) 一致",
			caps.Features.PosterGeneration, caps.FFmpeg.FFmpegAvailable)
	}
	if caps.Features.MetadataExtraction != caps.FFmpeg.FFprobeAvailable {
		t.Errorf("MetadataExtraction(%v) 应与 FFprobeAvailable(%v) 一致",
			caps.Features.MetadataExtraction, caps.FFmpeg.FFprobeAvailable)
	}
	if caps.Features.YtDlpImport != caps.YtDlp.Available {
		t.Errorf("YtDlpImport(%v) 应与 YtDlp.Available(%v) 一致",
			caps.Features.YtDlpImport, caps.YtDlp.Available)
	}
	if caps.Features.MagnetDownload != caps.Aria2.Available {
		t.Errorf("MagnetDownload(%v) 应与 Aria2.Available(%v) 一致",
			caps.Features.MagnetDownload, caps.Aria2.Available)
	}
}

func TestCheck_Idempotent(t *testing.T) {
	first := capabilities.Check()
	second := capabilities.Check()

	if first.FFmpeg.FFmpegAvailable != second.FFmpeg.FFmpegAvailable {
		t.Error("多次调用 Check 的 FFmpegAvailable 结果不一致")
	}
	if first.YtDlp.Available != second.YtDlp.Available {
		t.Error("多次调用 Check 的 YtDlp.Available 结果不一致")
	}
}
