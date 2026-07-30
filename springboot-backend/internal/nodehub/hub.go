package nodehub

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/bqlpfy/flux-panel/backend/internal/nodehub/crypto"
	"github.com/bqlpfy/flux-panel/backend/internal/nodes"
	"github.com/gorilla/websocket"
)

var errNodeOffline = errors.New("node is offline")

type Hub struct {
	log      *slog.Logger
	nodes    *nodes.Repository
	upgrader websocket.Upgrader
	mu       sync.RWMutex
	sessions map[int64]*session
}

type session struct {
	nodeID      int64
	conn        *websocket.Conn
	cipher      *crypto.Cipher
	write       sync.Mutex
	mu          sync.Mutex
	closed      bool
	wait        map[string]chan CommandResponse
	onTelemetry func(SystemInfo)
}

func New(log *slog.Logger, repository *nodes.Repository) *Hub {
	return &Hub{
		log:   log,
		nodes: repository,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  16 << 10,
			WriteBufferSize: 16 << 10,
			CheckOrigin:     func(*http.Request) bool { return true },
		},
		sessions: make(map[int64]*session),
	}
}

func (h *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	secret := r.URL.Query().Get("secret")
	if secret == "" {
		secret = r.Header.Get("Authorization")
	}
	if len(secret) > 7 && strings.HasPrefix(secret, "Bearer ") {
		secret = strings.TrimSpace(secret[7:])
	}
	if secret == "" {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	identity, err := h.authenticateNode(r.Context(), secret)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	conn, err := h.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	cipher, err := crypto.New(secret)
	if err != nil {
		_ = conn.Close()
		return
	}
	s := &session{nodeID: identity.id, conn: conn, cipher: cipher, wait: make(map[string]chan CommandResponse), onTelemetry: func(info SystemInfo) {
		_ = h.nodes.SetTelemetry(r.Context(), identity.id, info.Uptime, info.BytesReceived, info.BytesTransmitted, info.CPUUsage, info.MemoryUsage)
	}}
	h.replace(identity.id, s)
	defer h.remove(identity.id, s)
	h.markOnline(r.Context(), identity.id, r.URL.Query())
	s.readLoop(h.log)
}

type nodeIdentity struct{ id int64 }

func (h *Hub) authenticateNode(ctx context.Context, secret string) (nodeIdentity, error) {
	var id int64
	if err := h.nodes.LookupSecret(ctx, secret, &id); err != nil {
		return nodeIdentity{}, err
	}
	return nodeIdentity{id: id}, nil
}

func (h *Hub) replace(id int64, current *session) {
	h.mu.Lock()
	old := h.sessions[id]
	h.sessions[id] = current
	h.mu.Unlock()
	if old != nil {
		old.close()
	}
}

func (h *Hub) remove(id int64, current *session) {
	h.mu.Lock()
	isCurrent := h.sessions[id] == current
	if isCurrent {
		delete(h.sessions, id)
	}
	h.mu.Unlock()
	current.close()
	if isCurrent {
		if err := h.nodes.SetStatus(context.Background(), id, 0, ""); err != nil {
			h.log.Warn("failed to mark node offline", "node_id", id, "error", err)
		}
	}
}

func (h *Hub) markOnline(ctx context.Context, id int64, query url.Values) {
	version := query.Get("version")
	_ = h.nodes.SetStatus(ctx, id, 1, version)
}

func (s *session) readLoop(log *slog.Logger) {
	defer s.close()
	s.conn.SetReadLimit(16 << 20)
	_ = s.conn.SetReadDeadline(time.Now().Add(35 * time.Second))
	s.conn.SetPongHandler(func(string) error {
		return s.conn.SetReadDeadline(time.Now().Add(35 * time.Second))
	})
	for {
		messageType, payload, err := s.conn.ReadMessage()
		if err != nil {
			return
		}
		if messageType != websocket.TextMessage {
			continue
		}
		plain, err := decodeMessage(s.cipher, payload)
		if err != nil {
			log.Warn("invalid node message", "node_id", s.nodeID, "error", err)
			continue
		}
		var telemetry SystemInfo
		if json.Unmarshal(plain, &telemetry) == nil && telemetry.MemoryUsage != 0 {
			if s.onTelemetry != nil {
				s.onTelemetry(telemetry)
			}
			_ = s.sendPlain([]byte(`{"type":"call"}`))
			continue
		}
		var response CommandResponse
		if err := json.Unmarshal(plain, &response); err != nil || response.RequestID == "" {
			continue
		}
		s.mu.Lock()
		waiter := s.wait[response.RequestID]
		if waiter != nil {
			delete(s.wait, response.RequestID)
		}
		s.mu.Unlock()
		if waiter != nil {
			waiter <- response
		}
	}
}

func (s *session) sendPlain(payload []byte) error {
	s.write.Lock()
	defer s.write.Unlock()
	_ = s.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	encoded, err := encodeMessage(s.cipher, payload)
	if err != nil {
		return err
	}
	return s.conn.WriteMessage(websocket.TextMessage, encoded)
}

func (s *session) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	for id, waiter := range s.wait {
		delete(s.wait, id)
		close(waiter)
	}
	s.mu.Unlock()
	_ = s.conn.Close()
}

func (h *Hub) Command(ctx context.Context, nodeID int64, command CommandMessage) (CommandResponse, error) {
	h.mu.RLock()
	s := h.sessions[nodeID]
	h.mu.RUnlock()
	if s == nil {
		return CommandResponse{}, errNodeOffline
	}
	if command.RequestID == "" {
		command.RequestID = fmt.Sprintf("%d", time.Now().UnixNano())
	}
	payload, err := json.Marshal(command)
	if err != nil {
		return CommandResponse{}, err
	}
	waiter := make(chan CommandResponse, 1)
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return CommandResponse{}, errNodeOffline
	}
	s.wait[command.RequestID] = waiter
	s.mu.Unlock()
	if err := s.sendPlain(payload); err != nil {
		s.mu.Lock()
		delete(s.wait, command.RequestID)
		s.mu.Unlock()
		return CommandResponse{}, err
	}
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case response, ok := <-waiter:
		if !ok {
			return CommandResponse{}, errNodeOffline
		}
		return response, nil
	case <-ctx.Done():
		return CommandResponse{}, ctx.Err()
	case <-timer.C:
		return CommandResponse{}, context.DeadlineExceeded
	}
}

func (h *Hub) String() string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return fmt.Sprintf("%d", len(h.sessions))
}
