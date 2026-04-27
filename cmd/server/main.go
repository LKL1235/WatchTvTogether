package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"watchtogether/internal/config"
	"watchtogether/pkg/depscheck"
)

func main() {
	cfg := config.Load()
	paths := depscheck.Paths{
		FFmpeg:  cfg.FFmpegPath,
		FFprobe: cfg.FFprobePath,
		YtDlp:   cfg.YtDlpPath,
		Aria2c:  cfg.Aria2cPath,
	}
	ctx := context.Background()
	report := depscheck.All(ctx, paths)
	logDepsReport(report)
	if !report.AllOK() {
		log.Println("warning: not all external tools are available; admin download and magnet features may be limited")
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		rep := depscheck.All(r.Context(), paths)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if !rep.AllOK() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		_ = json.NewEncoder(w).Encode(rep)
	})
	mux.HandleFunc("GET /api/deps", func(w http.ResponseWriter, r *http.Request) {
		rep := depscheck.All(r.Context(), paths)
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_ = json.NewEncoder(w).Encode(rep)
	})

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           withGETOnly(mux),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	}()
	log.Printf("listening on %s (GET /readyz for dependency status)", cfg.Addr)

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutdownCtx)
}

func withGETOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func logDepsReport(r depscheck.Report) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		log.Printf("dependency check: %+v", r)
		return
	}
	log.Printf("dependency check:\n%s", b)
}
