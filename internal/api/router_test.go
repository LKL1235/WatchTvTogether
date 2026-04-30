package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	ablysdk "github.com/ably/ably-go/ably"

	"watchtogether/internal/capabilities"
	"watchtogether/internal/download"
	"watchtogether/internal/model"
	"watchtogether/internal/room"
	"watchtogether/pkg/ffmpeg"
)

func TestAuthRoomAndAblyHTTPFlow(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	deps := testDeps(db)
	realtime := newFakeRealtime("watchtogether")
	deps.Realtime = realtime
	router := NewRouter(deps)
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

	memberJoinA := postJSON(t, server.URL+"/api/rooms/"+roomID+"/join", memberAccess, map[string]string{})
	if memberJoinA.Code != http.StatusOK {
		t.Fatalf("member join A status = %d body = %s", memberJoinA.Code, memberJoinA.Body.String())
	}
	snapMembers := postJSON(t, server.URL+"/api/rooms/"+roomID+"/snapshot", memberAccess, map[string]string{})
	if snapMembers.Code != http.StatusOK {
		t.Fatalf("snapshot with members status = %d body = %s", snapMembers.Code, snapMembers.Body.String())
	}
	if numericField(t, snapMembers.Body.Bytes(), "viewer_count") != 1 {
		t.Fatalf("snapshot viewer_count want 1: %s", snapMembers.Body.String())
	}
	roomB := postJSON(t, server.URL+"/api/rooms", ownerAccess, map[string]string{
		"name":       "Second Room",
		"visibility": "public",
	})
	if roomB.Code != http.StatusCreated {
		t.Fatalf("create room B status = %d body = %s", roomB.Code, roomB.Body.String())
	}
	roomBID := stringField(t, roomB.Body.Bytes(), "id")
	conflictJoin := postJSON(t, server.URL+"/api/rooms/"+roomBID+"/join", memberAccess, map[string]string{})
	if conflictJoin.Code != http.StatusConflict {
		t.Fatalf("join second room should conflict, got %d body = %s", conflictJoin.Code, conflictJoin.Body.String())
	}
	leaveA := postJSON(t, server.URL+"/api/rooms/"+roomID+"/leave", memberAccess, map[string]string{})
	if leaveA.Code != http.StatusNoContent {
		t.Fatalf("leave A status = %d body = %s", leaveA.Code, leaveA.Body.String())
	}
	joinB := postJSON(t, server.URL+"/api/rooms/"+roomBID+"/join", memberAccess, map[string]string{})
	if joinB.Code != http.StatusOK {
		t.Fatalf("join B status = %d body = %s", joinB.Code, joinB.Body.String())
	}

	memberControl := postJSON(t, server.URL+"/api/rooms/"+roomID+"/control", memberAccess, map[string]any{
		"action":   "seek",
		"position": 42.5,
		"video_id": "video-1",
		"queue":    []string{"video-1", "video-2"},
	})
	if memberControl.Code != http.StatusForbidden {
		t.Fatalf("member control status = %d body = %s", memberControl.Code, memberControl.Body.String())
	}

	ownerControl := postJSON(t, server.URL+"/api/rooms/"+roomID+"/control", ownerAccess, map[string]any{
		"action":   "seek",
		"position": 42.5,
		"video_id": "video-1",
		"queue":    []string{"video-1", "video-2"},
	})
	if ownerControl.Code != http.StatusOK {
		t.Fatalf("owner control status = %d body = %s", ownerControl.Code, ownerControl.Body.String())
	}
	if got := stringField(t, ownerControl.Body.Bytes(), "type"); got != "sync" {
		t.Fatalf("control type = %q body = %s", got, ownerControl.Body.String())
	}
	if len(realtime.published) != 1 || realtime.published[0].Name != "room.sync" {
		t.Fatalf("published messages = %#v", realtime.published)
	}

	state := getJSON(t, server.URL+"/api/rooms/"+roomID+"/state", ownerAccess)
	if state.Code != http.StatusOK {
		t.Fatalf("state status = %d body = %s", state.Code, state.Body.String())
	}
	if got := numericField(t, state.Body.Bytes(), "position"); got != 42.5 {
		t.Fatalf("state position = %v", got)
	}
	if got := stringSliceField(t, state.Body.Bytes(), "queue"); len(got) != 2 || got[1] != "video-2" {
		t.Fatalf("state queue = %#v body = %s", got, state.Body.String())
	}

	snapshot := postJSON(t, server.URL+"/api/rooms/"+roomID+"/snapshot", memberAccess, map[string]string{})
	if snapshot.Code != http.StatusOK {
		t.Fatalf("snapshot status = %d body = %s", snapshot.Code, snapshot.Body.String())
	}
	if got := stringSliceField(t, snapshot.Body.Bytes(), "queue"); len(got) != 2 || got[0] != "video-1" {
		t.Fatalf("snapshot queue = %#v body = %s", got, snapshot.Body.String())
	}
	if numericField(t, snapshot.Body.Bytes(), "viewer_count") != 0 {
		t.Fatalf("snapshot viewer_count after member moved rooms should be 0: %s", snapshot.Body.String())
	}
	tokenResp := postJSON(t, server.URL+"/api/ably/token", memberAccess, map[string]string{
		"room_id": roomID,
		"purpose": "room",
	})
	if tokenResp.Code != http.StatusOK {
		t.Fatalf("ably token status = %d body = %s", tokenResp.Code, tokenResp.Body.String())
	}
	if got := stringField(t, tokenResp.Body.Bytes(), "capability"); !strings.Contains(got, `"watchtogether:room:`) || strings.Contains(got, "publish") {
		t.Fatalf("unexpected token capability: %s", got)
	}
	if route := getJSON(t, server.URL+"/ws/room/"+roomID, ownerAccess); route.Code != http.StatusNotFound {
		t.Fatalf("room websocket route status = %d body = %s", route.Code, route.Body.String())
	}

	owner, err := deps.UserStore.GetByID(context.Background(), userIDFrom(t, register.Body.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	owner.Role = model.UserRoleAdmin
	if err := deps.UserStore.Update(context.Background(), owner); err != nil {
		t.Fatal(err)
	}
	adminLogin := postJSON(t, server.URL+"/api/auth/login", "", map[string]string{
		"username": "owner",
		"password": "password123",
	})
	adminToken := tokenFrom(t, adminLogin.Body.Bytes(), "access_token")
	debug := getJSON(t, server.URL+"/api/admin/debug/rooms", adminToken)
	if debug.Code != http.StatusOK {
		t.Fatalf("debug rooms status = %d body = %s", debug.Code, debug.Body.String())
	}
	items := objectSliceField(t, debug.Body.Bytes(), "items")
	var dbgRoom map[string]any
	for _, it := range items {
		rm, ok := it["room"].(map[string]any)
		if !ok {
			continue
		}
		if id, _ := rm["id"].(string); id == roomID {
			dbgRoom = it
			break
		}
	}
	if dbgRoom == nil {
		t.Fatalf("debug rooms missing room %s in %#v", roomID, items)
	}
	if got, _ := dbgRoom["viewer_count"].(float64); got != 0 {
		t.Fatalf("debug room viewer_count = %#v", dbgRoom)
	}
	if got, _ := dbgRoom["queue"].([]any); len(got) != 2 || got[1] != "video-2" {
		t.Fatalf("debug room queue = %#v", dbgRoom)
	}
}

func TestPrivateRoomRequiresPasswordForJoinSnapshotAndToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := openTestDB(t)
	deps := testDeps(db)
	realtime := newFakeRealtime("watchtogether")
	deps.Realtime = realtime
	router := NewRouter(deps)
	server := httptest.NewServer(router)
	defer server.Close()

	ownerRegister := postJSON(t, server.URL+"/api/auth/register", "", map[string]string{
		"username": "owner",
		"password": "password123",
	})
	ownerAccess := tokenFrom(t, ownerRegister.Body.Bytes(), "access_token")
	create := postJSON(t, server.URL+"/api/rooms", ownerAccess, map[string]string{
		"name":       "Secret Movie",
		"visibility": "private",
		"password":   "room-pass",
	})
	if create.Code != http.StatusCreated {
		t.Fatalf("create private room status = %d body = %s", create.Code, create.Body.String())
	}
	roomID := stringField(t, create.Body.Bytes(), "id")

	memberRegister := postJSON(t, server.URL+"/api/auth/register", "", map[string]string{
		"username": "member",
		"password": "password123",
	})
	memberAccess := tokenFrom(t, memberRegister.Body.Bytes(), "access_token")

	for name, resp := range map[string]*httptest.ResponseRecorder{
		"join":     postJSON(t, server.URL+"/api/rooms/"+roomID+"/join", memberAccess, map[string]string{"password": "bad"}),
		"snapshot": postJSON(t, server.URL+"/api/rooms/"+roomID+"/snapshot", memberAccess, map[string]string{"password": "bad"}),
		"token":    postJSON(t, server.URL+"/api/ably/token", memberAccess, map[string]string{"room_id": roomID, "purpose": "room", "password": "bad"}),
	} {
		if resp.Code != http.StatusForbidden {
			t.Fatalf("%s status = %d body = %s", name, resp.Code, resp.Body.String())
		}
	}
	for name, resp := range map[string]*httptest.ResponseRecorder{
		"join":     postJSON(t, server.URL+"/api/rooms/"+roomID+"/join", memberAccess, map[string]string{"password": "room-pass"}),
		"snapshot": postJSON(t, server.URL+"/api/rooms/"+roomID+"/snapshot", memberAccess, map[string]string{"password": "room-pass"}),
		"token":    postJSON(t, server.URL+"/api/ably/token", memberAccess, map[string]string{"room_id": roomID, "purpose": "room", "password": "room-pass"}),
	} {
		if resp.Code != http.StatusOK {
			t.Fatalf("%s status = %d body = %s", name, resp.Code, resp.Body.String())
		}
	}
	if ownerSnapshot := postJSON(t, server.URL+"/api/rooms/"+roomID+"/snapshot", ownerAccess, map[string]string{}); ownerSnapshot.Code != http.StatusOK {
		t.Fatalf("owner snapshot status = %d body = %s", ownerSnapshot.Code, ownerSnapshot.Body.String())
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

	created := postJSON(t, server.URL+"/api/admin/downloads", token, map[string]string{
		"source_url": source.URL + "/movie.mp4",
	})
	if created.Code != http.StatusCreated {
		t.Fatalf("create download status = %d body = %s", created.Code, created.Body.String())
	}
	taskID := stringField(t, created.Body.Bytes(), "id")
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

	if route := getJSON(t, server.URL+"/ws/admin/downloads", token); route.Code != http.StatusNotFound {
		t.Fatalf("admin downloads websocket route status = %d body = %s", route.Code, route.Body.String())
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

func stringSliceField(t *testing.T, body []byte, field string) []string {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	raw, _ := payload[field].([]any)
	values := make([]string, 0, len(raw))
	for _, item := range raw {
		value, _ := item.(string)
		values = append(values, value)
	}
	return values
}

func objectSliceField(t *testing.T, body []byte, field string) []map[string]any {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	raw, _ := payload[field].([]any)
	values := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		value, _ := item.(map[string]any)
		values = append(values, value)
	}
	return values
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

type fakeRealtime struct {
	prefix    string
	published []publishedMessage
}

type publishedMessage struct {
	RoomID string
	Name   string
	Data   room.Message
}

func newFakeRealtime(prefix string) *fakeRealtime {
	return &fakeRealtime{prefix: prefix}
}

func (f *fakeRealtime) ChannelName(roomID string) string {
	return f.prefix + ":room:" + roomID + ":control"
}

func (f *fakeRealtime) RoomCapability(roomID string) (string, error) {
	return `{"` + f.ChannelName(roomID) + `":["subscribe","presence","history"]}`, nil
}

func (f *fakeRealtime) RequestRoomToken(ctx context.Context, roomID, clientID string) (*ablysdk.TokenDetails, error) {
	capability, err := f.RoomCapability(roomID)
	if err != nil {
		return nil, err
	}
	return &ablysdk.TokenDetails{
		Token:      "fake-token",
		Expires:    time.Now().Add(30 * time.Minute).UnixMilli(),
		Issued:     time.Now().UnixMilli(),
		Capability: capability,
		ClientID:   clientID,
	}, nil
}

func (f *fakeRealtime) PublishRoomMessage(ctx context.Context, roomID, name string, data any) error {
	msg, _ := data.(room.Message)
	f.published = append(f.published, publishedMessage{RoomID: roomID, Name: name, Data: msg})
	return nil
}
