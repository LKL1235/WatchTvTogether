package api

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"watchtogether/internal/cache/memory"
	"watchtogether/internal/config"
	"watchtogether/internal/store/sqlite"
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
	req.Header.Set("Authorization", "Bearer "+token)
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

func numericField(t *testing.T, body []byte, field string) float64 {
	t.Helper()
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatal(err)
	}
	value, _ := payload[field].(float64)
	return value
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
