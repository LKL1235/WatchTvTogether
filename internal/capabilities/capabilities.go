package capabilities

import (
	"context"
	"log"
	"time"

	"watchtogether/pkg/aria2"
	"watchtogether/pkg/ffmpeg"
	"watchtogether/pkg/toolcheck"
	"watchtogether/pkg/ytdlp"
)

type ToolStatus struct {
	Available bool   `json:"available"`
	Version   string `json:"version,omitempty"`
	Error     string `json:"error,omitempty"`
}

type Features struct {
	HLSDownload      bool `json:"hls_download"`
	PosterGeneration bool `json:"poster_generation"`
	MetadataExtract  bool `json:"metadata_extract"`
	YTDLPImport      bool `json:"ytdlp_import"`
	MagnetDownload   bool `json:"magnet_download"`
}

type Report struct {
	FFmpeg   bool       `json:"ffmpeg"`
	FFprobe  bool       `json:"ffprobe"`
	YTDLP    bool       `json:"ytdlp"`
	Aria2    bool       `json:"aria2"`
	Tools    ToolReport `json:"tools"`
	Features Features   `json:"features"`
}

type ToolReport struct {
	FFmpeg  ToolStatus `json:"ffmpeg"`
	FFprobe ToolStatus `json:"ffprobe"`
	YTDLP   ToolStatus `json:"ytdlp"`
	Aria2   ToolStatus `json:"aria2"`
}

func Check(ctx context.Context) Report {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	ffmpegResult := ffmpeg.CheckAvailability(ctx)
	ytdlpResult := ytdlp.CheckAvailability(ctx)
	aria2Result := aria2.CheckAvailability(ctx)

	report := Report{
		FFmpeg:  ffmpegResult.FFmpeg.Available,
		FFprobe: ffmpegResult.FFprobe.Available,
		YTDLP:   ytdlpResult.Available,
		Aria2:   aria2Result.Available,
		Tools: ToolReport{
			FFmpeg:  fromTool(ffmpegResult.FFmpeg),
			FFprobe: fromTool(ffmpegResult.FFprobe),
			YTDLP:   fromTool(ytdlpResult),
			Aria2:   fromTool(aria2Result),
		},
	}
	report.Features = Features{
		HLSDownload:      report.FFmpeg,
		PosterGeneration: report.FFmpeg,
		MetadataExtract:  report.FFprobe,
		YTDLPImport:      report.YTDLP,
		MagnetDownload:   report.Aria2,
	}
	return report
}

func Log(report Report) {
	log.Printf("capabilities: ffmpeg=%t ffprobe=%t yt-dlp=%t aria2c=%t hls_download=%t poster_generation=%t metadata_extract=%t ytdlp_import=%t magnet_download=%t",
		report.FFmpeg,
		report.FFprobe,
		report.YTDLP,
		report.Aria2,
		report.Features.HLSDownload,
		report.Features.PosterGeneration,
		report.Features.MetadataExtract,
		report.Features.YTDLPImport,
		report.Features.MagnetDownload,
	)
}

func fromTool(tool toolcheck.Result) ToolStatus {
	status := ToolStatus{
		Available: tool.Available,
		Version:   tool.Version,
	}
	if tool.Error != "" {
		status.Error = tool.Error
	}
	return status
}
