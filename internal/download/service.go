package download

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"watchtogether/internal/cache"
	"watchtogether/internal/capabilities"
	"watchtogether/internal/config"
	"watchtogether/internal/model"
	"watchtogether/internal/store"
	"watchtogether/pkg/ffmpeg"
)

var (
	ErrUnsupportedSource = errors.New("download: unsupported source")
	ErrToolUnavailable   = errors.New("download: required tool unavailable")
)

type Service struct {
	tasks        store.DownloadTaskStore
	videos       store.VideoStore
	cfg          config.Config
	caps         capabilities.Report
	pubsub       cache.PubSub
	client       *http.Client
	queue        chan string
	inFlight     map[string]context.CancelFunc
	mu           sync.Mutex
	metadataFunc func(context.Context, string) (ffmpeg.Metadata, error)
}

type Option func(*Service)

const UpdatesChannel = "admin:downloads"

func NewService(tasks store.DownloadTaskStore, videos store.VideoStore, cfg config.Config, caps capabilities.Report) *Service {
	return &Service{
		tasks:    tasks,
		videos:   videos,
		cfg:      cfg,
		caps:     caps,
		client:   &http.Client{Timeout: 0},
		queue:    make(chan string, 100),
		inFlight: map[string]context.CancelFunc{},
		metadataFunc: func(ctx context.Context, path string) (ffmpeg.Metadata, error) {
			return ffmpeg.Probe(ctx, path)
		},
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(s *Service) {
		if client != nil {
			s.client = client
		}
	}
}

func WithMetadataFunc(fn func(context.Context, string) (ffmpeg.Metadata, error)) Option {
	return func(s *Service) {
		if fn != nil {
			s.metadataFunc = fn
		}
	}
}

func WithPubSub(pubsub cache.PubSub) Option {
	return func(s *Service) {
		s.pubsub = pubsub
	}
}

func NewServiceWithOptions(tasks store.DownloadTaskStore, videos store.VideoStore, cfg config.Config, caps capabilities.Report, opts ...Option) *Service {
	service := NewService(tasks, videos, cfg, caps)
	for _, opt := range opts {
		opt(service)
	}
	return service
}

func (s *Service) Start(ctx context.Context, workers int) {
	if workers <= 0 {
		workers = 2
	}
	for i := 0; i < workers; i++ {
		go s.worker(ctx)
	}
}

func (s *Service) Enqueue(ctx context.Context, userID, sourceURL string) (*model.DownloadTask, error) {
	sourceURL = strings.TrimSpace(sourceURL)
	if sourceURL == "" {
		return nil, ErrUnsupportedSource
	}
	if err := s.validateSource(sourceURL); err != nil {
		return nil, err
	}
	task := &model.DownloadTask{
		UserID:    userID,
		SourceURL: sourceURL,
		Status:    model.DownloadTaskPending,
	}
	if err := s.tasks.Create(ctx, task); err != nil {
		return nil, err
	}
	select {
	case s.queue <- task.ID:
	default:
		go func() { s.queue <- task.ID }()
	}
	return task, nil
}

func (s *Service) Cancel(ctx context.Context, id string) error {
	s.mu.Lock()
	cancel := s.inFlight[id]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return s.updateResult(ctx, &model.DownloadTask{ID: id, Progress: 0, Status: model.DownloadTaskCanceled})
}

func (s *Service) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-s.queue:
			s.runTask(ctx, id)
		}
	}
}

func (s *Service) runTask(parent context.Context, id string) {
	ctx, cancel := context.WithCancel(parent)
	s.mu.Lock()
	s.inFlight[id] = cancel
	s.mu.Unlock()
	defer func() {
		cancel()
		s.mu.Lock()
		delete(s.inFlight, id)
		s.mu.Unlock()
	}()

	task, err := s.tasks.GetByID(ctx, id)
	if err != nil || task.Status == model.DownloadTaskCanceled {
		return
	}
	if err := s.updateProgress(ctx, id, 1, model.DownloadTaskRunning); err != nil {
		return
	}
	filePath, err := s.download(ctx, task)
	if err != nil {
		s.failTask(context.Background(), id, err)
		return
	}
	video, err := s.createVideo(ctx, task.SourceURL, filePath)
	if err != nil {
		s.failTask(context.Background(), id, err)
		return
	}
	task.VideoID = video.ID
	task.Progress = 100
	task.Status = model.DownloadTaskCompleted
	task.Error = ""
	_ = s.updateResult(context.Background(), task)
}

func (s *Service) validateSource(rawURL string) error {
	switch classifySource(rawURL) {
	case "direct":
		return nil
	case "hls":
		if !s.caps.Features.HLSDownload {
			return fmt.Errorf("%w: ffmpeg is required for HLS downloads", ErrToolUnavailable)
		}
		return nil
	case "magnet":
		if !s.caps.Features.MagnetDownload {
			return fmt.Errorf("%w: aria2c is required for magnet downloads", ErrToolUnavailable)
		}
		return nil
	case "site":
		if !s.caps.Features.YTDLPImport {
			return fmt.Errorf("%w: yt-dlp is required for site imports", ErrToolUnavailable)
		}
		return nil
	default:
		return ErrUnsupportedSource
	}
}

func (s *Service) download(ctx context.Context, task *model.DownloadTask) (string, error) {
	switch classifySource(task.SourceURL) {
	case "direct":
		return s.downloadDirect(ctx, task)
	case "hls":
		return s.downloadHLS(ctx, task)
	case "magnet":
		return s.downloadMagnet(ctx, task)
	case "site":
		return s.downloadWithYTDLP(ctx, task)
	default:
		return "", ErrUnsupportedSource
	}
}

func (s *Service) downloadHLS(ctx context.Context, task *model.DownloadTask) (string, error) {
	if err := os.MkdirAll(s.storagePath(), 0o755); err != nil {
		return "", err
	}
	filePath := filepath.Join(s.storagePath(), time.Now().UTC().Format("20060102")+"_"+task.ID+".mp4")
	cmd := commandContext(ctx, "ffmpeg", "-y", "-i", task.SourceURL, "-c", "copy", filePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(filePath)
		return "", fmt.Errorf("ffmpeg HLS download: %w: %s", err, strings.TrimSpace(string(out)))
	}
	_ = s.updateProgress(context.Background(), task.ID, 95, model.DownloadTaskRunning)
	return filePath, nil
}

func (s *Service) downloadWithYTDLP(ctx context.Context, task *model.DownloadTask) (string, error) {
	if err := os.MkdirAll(s.storagePath(), 0o755); err != nil {
		return "", err
	}
	template := filepath.Join(s.storagePath(), time.Now().UTC().Format("20060102")+"_"+task.ID+".%(ext)s")
	cmd := commandContext(ctx, "yt-dlp", "--no-playlist", "--newline", "-o", template, task.SourceURL)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", err
	}
	var scanErr error
	var wg sync.WaitGroup
	scan := func(r io.Reader) {
		defer wg.Done()
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			if progress := parseYTDLPProgress(scanner.Text()); progress > 0 {
				_ = s.updateProgress(context.Background(), task.ID, min(95, progress*95/100), model.DownloadTaskRunning)
			}
		}
		if err := scanner.Err(); err != nil && scanErr == nil {
			scanErr = err
		}
	}
	wg.Add(2)
	go scan(stdout)
	go scan(stderr)
	waitErr := cmd.Wait()
	wg.Wait()
	if scanErr != nil {
		return "", scanErr
	}
	if waitErr != nil {
		return "", fmt.Errorf("yt-dlp download: %w", waitErr)
	}
	matches, err := filepath.Glob(filepath.Join(s.storagePath(), "*_"+task.ID+".*"))
	if err != nil || len(matches) == 0 {
		return "", errors.New("yt-dlp completed but output file was not found")
	}
	_ = s.updateProgress(context.Background(), task.ID, 95, model.DownloadTaskRunning)
	return matches[0], nil
}

func (s *Service) downloadMagnet(ctx context.Context, task *model.DownloadTask) (string, error) {
	if err := os.MkdirAll(s.storagePath(), 0o755); err != nil {
		return "", err
	}
	gid, err := s.aria2Call(ctx, task.ID, "aria2.addUri", []any{[]string{task.SourceURL}, map[string]string{"dir": s.storagePath()}})
	if err != nil {
		return "", err
	}
	gidValue, _ := gid.(string)
	if gidValue == "" {
		return "", errors.New("aria2 did not return a gid")
	}
	return s.waitForAria2(ctx, task, gidValue)
}

func (s *Service) aria2Call(ctx context.Context, id, method string, params []any) (any, error) {
	if s.cfg.Aria2Secret != "" {
		params = append([]any{"token:" + s.cfg.Aria2Secret}, params...)
	}
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, s.cfg.Aria2RPCURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("aria2 RPC returned %s", resp.Status)
	}
	var payload struct {
		Result any `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Error != nil {
		return nil, fmt.Errorf("aria2 RPC error %d: %s", payload.Error.Code, payload.Error.Message)
	}
	return payload.Result, nil
}

func (s *Service) waitForAria2(ctx context.Context, task *model.DownloadTask, gid string) (string, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	result, err := s.aria2Call(ctx, task.ID, "aria2.tellStatus", []any{gid, []string{"status", "completedLength", "totalLength", "files", "errorMessage"}})
	if err != nil {
		return "", err
	}
	if ready, filePath, err := s.handleAria2Status(ctx, task, result); ready || err != nil {
		return filePath, err
	}
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-ticker.C:
			result, err := s.aria2Call(ctx, task.ID, "aria2.tellStatus", []any{gid, []string{"status", "completedLength", "totalLength", "files", "errorMessage"}})
			if err != nil {
				return "", err
			}
			if ready, filePath, err := s.handleAria2Status(ctx, task, result); ready || err != nil {
				return filePath, err
			}
		}
	}
}

func (s *Service) handleAria2Status(ctx context.Context, task *model.DownloadTask, result any) (bool, string, error) {
	status, filePath, progress, message := parseAria2Status(result)
	if progress > 0 {
		_ = s.updateProgress(ctx, task.ID, min(95, progress*95/100), model.DownloadTaskRunning)
	}
	switch status {
	case "complete":
		if filePath == "" {
			return true, "", errors.New("aria2 completed without a file path")
		}
		_ = s.updateProgress(ctx, task.ID, 95, model.DownloadTaskRunning)
		return true, filePath, nil
	case "error", "removed":
		if message == "" {
			message = "aria2 download failed"
		}
		return true, "", errors.New(message)
	default:
		return false, "", nil
	}
}

func (s *Service) downloadDirect(ctx context.Context, task *model.DownloadTask) (string, error) {
	if err := os.MkdirAll(s.storagePath(), 0o755); err != nil {
		return "", err
	}
	ext := extensionFromURL(task.SourceURL)
	name := time.Now().UTC().Format("20060102") + "_" + task.ID + ext
	filePath := filepath.Join(s.storagePath(), name)
	tmpPath := filePath + ".part"
	start, err := partialSize(tmpPath)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, task.SourceURL, nil)
	if err != nil {
		return "", err
	}
	if start > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", start))
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if start > 0 && resp.StatusCode == http.StatusOK {
		start = 0
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || (start > 0 && resp.StatusCode != http.StatusPartialContent) {
		return "", fmt.Errorf("download source returned %s", resp.Status)
	}
	flags := os.O_CREATE | os.O_WRONLY
	if start > 0 {
		flags |= os.O_APPEND
	} else {
		flags |= os.O_TRUNC
	}
	out, err := os.OpenFile(tmpPath, flags, 0o644)
	if err != nil {
		return "", err
	}
	defer out.Close()

	total := resp.ContentLength
	if start > 0 && total > 0 {
		total += start
	}
	written, err := copyWithProgress(ctx, out, resp.Body, start, total, func(progress float64) {
		_ = s.updateProgress(context.Background(), task.ID, progress, model.DownloadTaskRunning)
	})
	if err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := os.Rename(tmpPath, filePath); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}
	if total < 0 && written > 0 {
		_ = s.updateProgress(context.Background(), task.ID, 95, model.DownloadTaskRunning)
	}
	return filePath, nil
}

func (s *Service) createVideo(ctx context.Context, sourceURL, filePath string) (*model.Video, error) {
	info, statErr := os.Stat(filePath)
	if statErr != nil {
		return nil, statErr
	}
	videoID := uuid.NewString()
	video := &model.Video{
		ID:        videoID,
		Title:     titleFromSource(sourceURL, filePath),
		FilePath:  filePath,
		Format:    strings.TrimPrefix(strings.ToLower(filepath.Ext(filePath)), "."),
		Size:      info.Size(),
		SourceURL: sourceURL,
		Status:    model.VideoStatusReady,
	}
	if s.caps.FFprobe {
		if meta, err := s.metadataFunc(ctx, filePath); err == nil {
			if meta.Duration > 0 {
				video.Duration = meta.Duration
			}
			if meta.Format != "" {
				video.Format = meta.Format
			}
		}
	}
	if s.caps.Features.PosterGeneration {
		posterFile := videoID + ".jpg"
		posterPath := filepath.Join(s.cfg.PosterDir, posterFile)
		if err := os.MkdirAll(s.cfg.PosterDir, 0o755); err == nil {
			if err := ffmpeg.ExtractPoster(ctx, filePath, posterPath, video.Duration); err == nil {
				video.PosterPath = "/static/posters/" + posterFile
			}
		}
	}
	if err := s.videos.Create(ctx, video); err != nil {
		return nil, err
	}
	return video, nil
}

func (s *Service) failTask(ctx context.Context, id string, err error) {
	_ = s.updateResult(ctx, &model.DownloadTask{
		ID:       id,
		Progress: 0,
		Status:   model.DownloadTaskFailed,
		Error:    err.Error(),
	})
}

func (s *Service) updateProgress(ctx context.Context, id string, progress float64, status model.DownloadTaskStatus) error {
	if err := s.tasks.UpdateProgress(ctx, id, progress, status); err != nil {
		return err
	}
	s.publishTask(ctx, id)
	return nil
}

func (s *Service) updateResult(ctx context.Context, task *model.DownloadTask) error {
	if err := s.tasks.UpdateResult(ctx, task); err != nil {
		return err
	}
	s.publishTask(ctx, task.ID)
	return nil
}

func (s *Service) publishTask(ctx context.Context, id string) {
	if s.pubsub == nil {
		return
	}
	task, err := s.tasks.GetByID(ctx, id)
	if err != nil {
		return
	}
	payload, err := json.Marshal(map[string]any{
		"type": "download_task",
		"task": task,
	})
	if err != nil {
		return
	}
	_ = s.pubsub.Publish(ctx, UpdatesChannel, payload)
}

func (s *Service) storagePath() string {
	return filepath.Clean(s.cfg.StorageDir)
}

func classifySource(rawURL string) string {
	lower := strings.ToLower(strings.TrimSpace(rawURL))
	if strings.HasPrefix(lower, "magnet:") {
		return "magnet"
	}
	parsed, err := url.Parse(lower)
	if err != nil || parsed.Scheme == "" {
		return ""
	}
	path := strings.ToLower(parsed.Path)
	if strings.HasSuffix(path, ".m3u8") {
		return "hls"
	}
	switch filepath.Ext(path) {
	case ".mp4", ".mkv", ".webm", ".mov", ".avi":
		return "direct"
	}
	return "site"
}

func extensionFromURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ".mp4"
	}
	ext := strings.ToLower(filepath.Ext(parsed.Path))
	switch ext {
	case ".mp4", ".mkv", ".webm", ".mov", ".avi":
		return ext
	default:
		return ".mp4"
	}
}

func titleFromPath(filePath string) string {
	base := filepath.Base(filePath)
	ext := filepath.Ext(base)
	return strings.TrimSuffix(base, ext)
}

func titleFromSource(sourceURL, filePath string) string {
	parsed, err := url.Parse(sourceURL)
	if err == nil {
		base := filepath.Base(parsed.Path)
		if base != "." && base != "/" && base != "" {
			return strings.TrimSuffix(base, filepath.Ext(base))
		}
	}
	return titleFromPath(filePath)
}

func progressPercent(completed, total string) float64 {
	done, err := strconv.ParseFloat(completed, 64)
	if err != nil || done <= 0 {
		return 0
	}
	all, err := strconv.ParseFloat(total, 64)
	if err != nil || all <= 0 {
		return 0
	}
	return done * 100 / all
}

func firstAria2FilePath(files []map[string]any) string {
	for _, file := range files {
		rawPath, _ := file["path"].(string)
		if rawPath != "" {
			return rawPath
		}
	}
	return ""
}

func parseAria2Status(result any) (string, string, float64, string) {
	payload, _ := result.(map[string]any)
	status, _ := payload["status"].(string)
	completed, _ := payload["completedLength"].(string)
	total, _ := payload["totalLength"].(string)
	message, _ := payload["errorMessage"].(string)
	files, _ := payload["files"].([]any)
	typedFiles := make([]map[string]any, 0, len(files))
	for _, file := range files {
		if typed, ok := file.(map[string]any); ok {
			typedFiles = append(typedFiles, typed)
		}
	}
	return status, firstAria2FilePath(typedFiles), progressPercent(completed, total), message
}

func parseYTDLPProgress(line string) float64 {
	line = strings.TrimSpace(line)
	if !strings.Contains(line, "%") {
		return 0
	}
	fields := strings.Fields(line)
	for _, field := range fields {
		if !strings.HasSuffix(field, "%") {
			continue
		}
		value, err := strconv.ParseFloat(strings.TrimSuffix(field, "%"), 64)
		if err == nil {
			return value
		}
	}
	return 0
}

func partialSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info.Size(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return 0, err
}

func copyWithProgress(ctx context.Context, dst io.Writer, src io.Reader, existing, total int64, report func(float64)) (int64, error) {
	buf := make([]byte, 64*1024)
	written := existing
	last := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		n, readErr := src.Read(buf)
		if n > 0 {
			m, writeErr := dst.Write(buf[:n])
			written += int64(m)
			if writeErr != nil {
				return written, writeErr
			}
			if total > 0 && time.Since(last) > 200*time.Millisecond {
				report(min(95, float64(written)*95/float64(total)))
				last = time.Now()
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				if total > 0 {
					report(95)
				}
				return written, nil
			}
			return written, readErr
		}
	}
}

func commandContext(ctx context.Context, name string, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, args...)
}
