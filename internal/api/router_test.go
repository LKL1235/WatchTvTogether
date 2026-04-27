package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"watchtogether/internal/cache/memory"
	"watchtogether/internal/capabilities"
	"watchtogether/internal/config"
	"watchtogether/internal/download"
	"watchtogether/internal/model"
	"watchtogether/internal/store/sqlite"
	"watchtogether/pkg/ffmpeg"
)

func TestAuthRoomAndWebSocketFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	router := NewRouter(testDeps(db))
	server := httptest.NewServer(router)
	defer server.Close()

	register := postJSON(t, server.URL+"/api/auth/register", "", map[string]string{
		"username": "owner",
		"password": "password123",
	})
	if register.Code != http.StatusCreated {
		t.Fatalf("register status = %d body = %s", register.Code, register.Body.String())
	}
	ownerAccess := tokenFrom(t, register.Body.Bytes(), "access_token")

	create := postJSON(t, server.URL+"/api/rooms", ownerAccess, map[string]string{
		"name":       "Friday Movie",
		"visibility": "public",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create room status = %d body = %s", create.Code, create.Body.String())
	}
	roomID := stringField(t, create.Body.Bytes(), "id")

	member := postJSON(t, server.URL+"/api/auth/register", "", map[string]string{
		"username": "member",
		"password": "password123",
	})
	memberAccess := tokenFrom(t, member.Body.Bytes(), "access_token")

	ownerWS := dialRoom(t, server.URL, roomID, ownerAccess)
	defer ownerWS.Close()
	memberWS := dialRoom(t, server.URL, roomID, memberAccess)
	defer memberWS.Close()
	_ = readUntil(t, ownerWS, "room_event")
	_ = readUntil(t, memberWS, "room_event")

	writeWS(t, ownerWS, map[string]any{
		"type":     "play_control",
		"action":   "seek",
		"position": 42.5,
		"video_id": "video-1",
	})

	msg := readUntil(t, memberWS, "sync")
	if msg["action"] != "seek" || msg["video_id"] != "video-1" {
		t.Fatalf("unexpected sync message: %#v", msg)
	}

	state := getJSON(t, server.URL+"/api/rooms/"+roomID+"/state", ownerAccess)
	if state.Code != http.StatusOK {
		t.Fatalf("state status = %d body = %s", state.Code, state.Body.String())
	}
	if got := numericField(t, state.Body.Bytes(), "position"); got != 42.5 {
		t.Fatalf("state position = %v", got)
	}
}

func TestCapabilitiesDownloadsAndVideosFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "video/mp4")
		_, _ = w.Write([]byte("fake mp4 payload"))
	}))
	defer source.Close()

	deps := testDeps(db)
	deps.Config.StorageDir = t.TempDir()
	deps.Config.PosterDir = t.TempDir()
	deps.Capabilities = capabilities.Report{
		FFprobe: true,
		Features: capabilities.Features{
			MetadataExtract: true,
		},
	}
	deps.DownloadService = download.NewServiceWithOptions(
		deps.DownloadTaskStore,
		deps.VideoStore,
		deps.Config,
		deps.Capabilities,
		download.WithPubSub(deps.PubSub),
		download.WithMetadataFunc(func(context.Context, string) (ffmpeg.Metadata, error) {
			return ffmpeg.Metadata{Duration: 12.5, Format: "mp4"}, nil
		}),
	)
	deps.DownloadService.Start(context.Background(), 1)
	router := NewRouter(deps)
	server := httptest.NewServer(router)
	defer server.Close()

	register := postJSON(t, server.URL+"/api/auth/register", "", map[string]string{
		"username": "admin",
		"password": "password123",
	})
	adminID := userIDFrom(t, register.Body.Bytes())
	admin, err := deps.UserStore.GetByID(context.Background(), adminID)
	if err != nil {
		t.Fatal(err)
	}
	admin.Role = model.UserRoleAdmin
	if err := deps.UserStore.Update(context.Background(), admin); err != nil {
		t.Fatal(err)
	}
	login := postJSON(t, server.URL+"/api/auth/login", "", map[string]string{
		"username": "admin",
		"password": "password123",
	})
	token := tokenFrom(t, login.Body.Bytes(), "access_token")

	caps := getJSON(t, server.URL+"/api/capabilities", "")
	if caps.Code != http.StatusOK || !boolField(t, caps.Body.Bytes(), "ffprobe") {
		t.Fatalf("capabilities status = %d body = %s", caps.Code, caps.Body.String())
	}

	updates := dialAdminDownloads(t, server.URL, token)
	defer updates.Close()

	created := postJSON(t, server.URL+"/api/admin/downloads", token, map[string]string{
		"source_url": source.URL + "/movie.mp4",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create download status = %d body = %s", created.Code, created.Body.String())
	}
	taskID := stringField(t, created.Body.Bytes(), "id")
	update := readUntil(t, updates, "download_task")
	updateTask, _ := update["task"].(map[string]any)
	if updateTask["id"] != taskID {
		t.Fatalf("unexpected download update: %#v", update)
	}
	task := waitForTask(t, server.URL, token, taskID, model.DownloadTaskCompleted)
	if task.VideoID == "" || task.Progress != 100 {
		t.Fatalf("unexpected completed task: %#v", task)
	}

	videos := getJSON(t, server.URL+"/api/videos?q=movie", token)
	if videos.Code != http.StatusOK || numericField(t, videos.Body.Bytes(), "total") != 1 {
		t.Fatalf("videos status = %d body = %s", videos.Code, videos.Body.String())
	}
	video := getJSON(t, server.URL+"/api/videos/"+task.VideoID, token)
	if video.Code != http.StatusOK || numericField(t, video.Body.Bytes(), "duration") != 12.5 {
		t.Fatalf("video status = %d body = %s", video.Code, video.Body.String())
	}

	videoPath := stringField(t, video.Body.Bytes(), "file_path")
	rangeResp := getWithHeaders(t, server.URL+"/static/videos/"+filepath.Base(videoPath), map[string]string{"Range": "bytes=0-3"})
	if rangeResp.Code != http.StatusPartialContent || rangeResp.Body.String() != "fake" {
		t.Fatalf("range status = %d body = %q", rangeResp.Code, rangeResp.Body.String())
	}

	deleteResp := deleteJSON(t, server.URL+"/api/admin/videos/"+task.VideoID, token)
	if deleteResp.Code != http.StatusNoContent {
		t.Fatalf("delete video status = %d body = %s", deleteResp.Code, deleteResp.Body.String())
	}
	if _, err := os.Stat(videoPath); !os.IsNotExist(err) {
		t.Fatalf("video file should be deleted, stat err = %v", err)
	}
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sqlite.Open(context.Background(), ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func testDeps(db *sql.DB) Dependencies {
	cfg := config.Default()
	cfg.JWTSecret = "test-secret"
	cfg.JWTAccessTTL = time.Hour
	cfg.JWTRefreshTTL = 24 * time.Hour
	cfg.StorageDir = "."
	cfg.PosterDir = "."
	return Dependencies{
		Config:            cfg,
		UserStore:         sqlite.NewUserStore(db),
		RoomStore:         sqlite.NewRoomStore(db),
		VideoStore:        sqlite.NewVideoStore(db),
		DownloadTaskStore: sqlite.NewDownloadTaskStore(db),
		SessionCache:      memory.NewSessionCache(),
		RoomStateCache:    memory.NewRoomStateCache(),
		PubSub:            memory.NewPubSub(),
	}
}

func postJSON(t *testing.T, url, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	http.DefaultServeMux.ServeHTTP(rec, req)
	if strings.HasPrefix(url, "http://") || strings.HasPrefix(url, "https://") {
		httpReq, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
		httpReq.Header.Set("Content-Type", "application/json")
		if token != "" {
			httpReq.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := http.DefaultClient.Do(httpReq)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		rec = httptest.NewRecorder()
		rec.Code = resp.StatusCode
		_, _ = rec.Body.ReadFrom(resp.Body)
	}
	return rec
}

func getJSON(t *testing.T, url, token string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	_, _ = rec.Body.ReadFrom(resp.Body)
	return rec
}

func getWithHeaders(t *testing.T, url string, headers map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest(http.MethodGet, url, nil)
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	for key, values := range resp.Header {
		for _, value := range values {
			rec.Header().Add(key, value)
		}
	}
	_, _ = rec.Body.ReadFrom(resp.Body)
	return rec
}

func deleteJSON(t *testing.T, url, token string) *httptest.ResponseRecorder {
	t.Helper()
	req, _ := http.NewRequest(http.MethodDelete, url, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	rec := httptest.NewRecorder()
	rec.Code = resp.StatusCode
	_, _ = rec.Body.ReadFrom(resp.Body)
	return rec
}

func tokenFrom(t *testing.T, body []byte, name string) string {
	t.Helper()
	var payload struct {
		Tokens map[string]any `json:"tokens"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	token, ok := payload.Tokens[name].(string)
	if !ok || token == "" {
		t.Fatalf("missing token %s in %s", name, body)
	}
	return token
}

func stringField(t *testing.T, body []byte, field string) string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	value, _ := payload[field].(string)
	if value == "" {
		t.Fatalf("missing string field %s in %s", field, body)
	}
	return value
}

func userIDFrom(t *testing.T, body []byte) string {
	t.Helper()
	var payload struct {
		User struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.User.ID == "" {
		t.Fatalf("missing user id in %s", body)
	}
	return payload.User.ID
}

func numericField(t *testing.T, body []byte, field string) float64 {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	value, _ := payload[field].(float64)
	return value
}

func boolField(t *testing.T, body []byte, field string) bool {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	value, _ := payload[field].(bool)
	return value
}

func waitForTask(t *testing.T, baseURL, token, taskID string, want model.DownloadTaskStatus) *model.DownloadTask {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp := getJSON(t, baseURL+"/api/admin/downloads/"+taskID, token)
		if resp.Code != http.StatusOK {
			t.Fatalf("task status = %d body = %s", resp.Code, resp.Body.String())
		}
		var task model.DownloadTask
		if err := json.Unmarshal(resp.Body.Bytes(), &task); err != nil {
			t.Fatal(err)
		}
		if task.Status == want {
			return &task
		}
		if task.Status == model.DownloadTaskFailed {
			t.Fatalf("task failed: %#v", task)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for task %s to reach %s", taskID, want)
	return nil
}

func dialRoom(t *testing.T, baseURL, roomID, token string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws/room/" + roomID + "?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func dialAdminDownloads(t *testing.T, baseURL, token string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(baseURL, "http") + "/ws/admin/downloads?token=" + token
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatal(err)
	}
	return conn
}

func writeWS(t *testing.T, conn *websocket.Conn, payload any) {
	t.Helper()
	if err := conn.WriteJSON(payload); err != nil {
		t.Fatal(err)
	}
}

func readUntil(t *testing.T, conn *websocket.Conn, msgType string) map[string]any {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		var msg map[string]any
		if err := conn.ReadJSON(&msg); err != nil {
			t.Fatalf("read websocket message: %v", err)
		}
		if msg["type"] == msgType {
			return msg
		}
	}
}
