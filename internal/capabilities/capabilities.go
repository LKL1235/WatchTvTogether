package capabilities

import (
	"context"
	"log"
	"time"
)

// Report is returned by GET /api/capabilities.
// Historically this included ffmpeg / yt-dlp / aria2 probes and feature flags for
// server-side downloads; those tools and flows were removed for serverless (e.g. Vercel)
// deployment. The JSON shape is intentionally minimal — clients must not rely on removed fields.
type Report struct {
	Features Features `json:"features"`
}

// Features is kept as an object for forward-compatible clients; it is empty until new
// capability flags are introduced.
type Features struct{}

func Check(ctx context.Context) Report {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_ = ctx
	return Report{Features: Features{}}
}

func Log(report Report) {
	log.Printf("capabilities: features=%#v (server-side download tooling removed)", report.Features)
}
