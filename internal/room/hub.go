package room

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"watchtogether/internal/cache"
	"watchtogether/internal/model"
	"watchtogether/pkg/corsutil"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 8192
)

type User struct {
	ID       string         `json:"id"`
	Username string         `json:"username"`
	Role     model.UserRole `json:"role"`
	IsOwner  bool           `json:"is_owner"`
}

type Message struct {
	Type      string               `json:"type"`
	Action    model.PlaybackAction `json:"action,omitempty"`
	Event     string               `json:"event,omitempty"`
	Position  float64              `json:"position,omitempty"`
	VideoID   string               `json:"video_id,omitempty"`
	Queue     []string             `json:"queue,omitempty"`
	Timestamp int64                `json:"timestamp,omitempty"`
	Payload   any                  `json:"payload,omitempty"`
	User      *User                `json:"user,omitempty"`
}

type Hub struct {
	roomID      string
	pubsub      cache.PubSub
	states      cache.RoomStateCache
	mu          sync.RWMutex
	clients     map[*client]struct{}
	cancel      context.CancelFunc
	closeOnce   sync.Once
	channel     string
	closed      chan struct{}
	allowWrite  func(*User) bool
	sub         <-chan []byte
	unsubscribe func()
}

type Snapshot struct {
	RoomID      string           `json:"room_id"`
	State       *model.RoomState `json:"state,omitempty"`
	Users       []User           `json:"users"`
	Queue       []string         `json:"queue"`
	ViewerCount int              `json:"viewer_count"`
}

type Manager struct {
	pubsub cache.PubSub
	states cache.RoomStateCache
	mu     sync.Mutex
	hubs   map[string]*Hub
}

func NewManager(pubsub cache.PubSub, states cache.RoomStateCache) *Manager {
	return &Manager{pubsub: pubsub, states: states, hubs: make(map[string]*Hub)}
}

func (m *Manager) Get(roomID string) *Hub {
	m.mu.Lock()
	defer m.mu.Unlock()
	if hub := m.hubs[roomID]; hub != nil {
		return hub
	}
	hub := newHub(roomID, m.pubsub, m.states, func(user *User) bool {
		return user.Role == model.UserRoleAdmin || user.IsOwner
	})
	m.hubs[roomID] = hub
	return hub
}

func (m *Manager) Destroy(ctx context.Context, roomID string) error {
	m.mu.Lock()
	hub := m.hubs[roomID]
	delete(m.hubs, roomID)
	m.mu.Unlock()
	if hub != nil {
		hub.Close()
	}
	return m.states.DeleteRoomState(ctx, roomID)
}

func (m *Manager) Snapshot(ctx context.Context, roomID string) Snapshot {
	m.mu.Lock()
	hub := m.hubs[roomID]
	m.mu.Unlock()
	if hub == nil {
		return Snapshot{RoomID: roomID, Users: []User{}, Queue: []string{}}
	}
	return hub.Snapshot(ctx)
}

func newHub(roomID string, pubsub cache.PubSub, states cache.RoomStateCache, allowWrite func(*User) bool) *Hub {
	ctx, cancel := context.WithCancel(context.Background())
	ch, unsubscribe, err := pubsub.Subscribe(ctx, "room:"+roomID)
	if err != nil {
		cancel()
		ch = make(chan []byte)
		unsubscribe = func() {}
	}
	h := &Hub{
		roomID:      roomID,
		pubsub:      pubsub,
		states:      states,
		clients:     make(map[*client]struct{}),
		cancel:      cancel,
		channel:     "room:" + roomID,
		closed:      make(chan struct{}),
		allowWrite:  allowWrite,
		sub:         ch,
		unsubscribe: unsubscribe,
	}
	go h.run(ctx)
	return h
}

func (h *Hub) Close() {
	h.closeOnce.Do(func() {
		h.cancel()
		h.unsubscribe()
		close(h.closed)
		h.mu.Lock()
		for c := range h.clients {
			c.close()
		}
		h.clients = map[*client]struct{}{}
		h.mu.Unlock()
	})
}

func (h *Hub) Serve(ctx *gin.Context, user User) error {
	conn, err := upgrader.Upgrade(ctx.Writer, ctx.Request, nil)
	if err != nil {
		return err
	}
	c := &client{hub: h, conn: conn, send: make(chan []byte, 32), user: user}
	h.add(c)
	snapshot := h.Snapshot(ctx.Request.Context())
	_ = c.writeJSON(Message{Type: "room_snapshot", Payload: snapshot})
	if snapshot.State != nil {
		_ = c.writeJSON(Message{Type: "sync", Action: snapshot.State.Action, Position: snapshot.State.Position, VideoID: snapshot.State.VideoID, Queue: snapshot.Queue, Timestamp: snapshot.State.UpdatedAt.Unix(), Payload: map[string]any{"queue": snapshot.Queue}})
	}
	h.publish(ctx.Request.Context(), Message{Type: "room_event", Event: "user_joined", User: &user})
	go c.writePump()
	go c.readPump()
	return nil
}

func (h *Hub) add(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *Hub) remove(c *client) {
	h.mu.Lock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		c.close()
	}
	h.mu.Unlock()
	h.publish(context.Background(), Message{Type: "room_event", Event: "user_left", User: &c.user})
}

func (h *Hub) Snapshot(ctx context.Context) Snapshot {
	h.mu.RLock()
	users := make([]User, 0, len(h.clients))
	for c := range h.clients {
		users = append(users, c.user)
	}
	h.mu.RUnlock()

	snapshot := Snapshot{
		RoomID:      h.roomID,
		Users:       users,
		Queue:       []string{},
		ViewerCount: len(users),
	}
	if state, err := h.states.GetRoomState(ctx, h.roomID); err == nil {
		snapshot.State = state
		snapshot.Queue = append(snapshot.Queue, state.Queue...)
		if len(snapshot.Queue) == 0 && state.VideoID != "" {
			snapshot.Queue = []string{state.VideoID}
		}
	}
	return snapshot
}

func (h *Hub) run(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg, ok := <-h.sub:
			if !ok {
				return
			}
			h.broadcast(msg)
		}
	}
}

func (h *Hub) broadcast(msg []byte) {
	h.mu.RLock()
	stale := make([]*client, 0)
	for c := range h.clients {
		select {
		case c.send <- append([]byte(nil), msg...):
		default:
			stale = append(stale, c)
		}
	}
	h.mu.RUnlock()
	for _, c := range stale {
		h.remove(c)
	}
}

func (h *Hub) handleClientMessage(ctx context.Context, c *client, payload []byte) error {
	var msg Message
	if err := json.Unmarshal(payload, &msg); err != nil {
		return err
	}
	if msg.Type != "play_control" {
		return errors.New("unsupported message type")
	}
	if !h.allowWrite(&c.user) {
		return errors.New("forbidden")
	}
	now := time.Now().UTC()
	queue := append([]string(nil), msg.Queue...)
	if len(queue) == 0 && msg.VideoID != "" {
		queue = []string{msg.VideoID}
	}
	state := &model.RoomState{
		RoomID:    h.roomID,
		VideoID:   msg.VideoID,
		Queue:     queue,
		Action:    msg.Action,
		Position:  msg.Position,
		UpdatedBy: c.user.ID,
		UpdatedAt: now,
	}
	if err := h.states.SetRoomState(ctx, h.roomID, state); err != nil {
		return err
	}
	h.publish(ctx, Message{
		Type:      "sync",
		Action:    msg.Action,
		Position:  msg.Position,
		VideoID:   msg.VideoID,
		Queue:     queue,
		Timestamp: now.Unix(),
		User:      &c.user,
	})
	return nil
}

func (h *Hub) publish(ctx context.Context, msg Message) {
	b, err := json.Marshal(msg)
	if err != nil {
		return
	}
	_ = h.pubsub.Publish(ctx, h.channel, b)
}

var wsCheckOrigin = corsutil.CheckOrigin(nil)

// InitWebSocketCheckOrigin 设置房间 WebSocket 的 Origin 校验，应与 HTTP CORS 允许列表一致（在创建路由前调用一次）。
func InitWebSocketCheckOrigin(allowed []string) {
	wsCheckOrigin = corsutil.CheckOrigin(allowed)
}

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin:     func(r *http.Request) bool { return wsCheckOrigin(r) },
}

type client struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
	user User
	once sync.Once
}

func (c *client) close() {
	c.once.Do(func() {
		close(c.send)
		_ = c.conn.Close()
	})
}

func (c *client) readPump() {
	defer c.hub.remove(c)
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		return c.conn.SetReadDeadline(time.Now().Add(pongWait))
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			return
		}
		if err := c.hub.handleClientMessage(context.Background(), c, message); err != nil {
			_ = c.writeJSON(Message{Type: "error", Payload: map[string]string{"message": err.Error()}})
		}
	}
}

func (c *client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.hub.remove(c)
	}()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *client) writeJSON(msg Message) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	select {
	case c.send <- b:
		return nil
	default:
		return fmt.Errorf("client send buffer full")
	}
}
