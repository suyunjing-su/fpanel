package nodehub

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/nodehub/crypto"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
)

func TestTransportPingCommandsUseProtocolCommandTypes(t *testing.T) {
	upgrader := websocket.Upgrader{}
	commands := make(chan string, 3)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		cipher, err := crypto.New("secret")
		if err != nil {
			return
		}
		for i := 0; i < 3; i++ {
			_, payload, err := conn.ReadMessage()
			if err != nil {
				return
			}
			plain, err := decodeMessage(cipher, payload)
			if err != nil {
				return
			}
			var command CommandMessage
			if err := json.Unmarshal(plain, &command); err != nil {
				return
			}
			commands <- command.Type
			responseData := TCPPingResponse{IP: "target.example.com", Port: 443, Success: true}
			encodedData, err := json.Marshal(responseData)
			if err != nil {
				return
			}
			response, err := json.Marshal(CommandResponse{Type: command.Type, Success: true, Data: json.RawMessage(encodedData), RequestID: command.RequestID})
			if err != nil {
				return
			}
			encoded, err := encodeMessage(cipher, response)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.TextMessage, encoded); err != nil {
				return
			}
		}
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	serverURL.Scheme = "ws"
	client, _, err := websocket.DefaultDialer.Dial(serverURL.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cipher, err := crypto.New("secret")
	if err != nil {
		t.Fatal(err)
	}
	hub := &Hub{sessions: map[int64]*session{1: {nodeID: 1, conn: client, cipher: cipher, readTimeout: time.Second, wait: make(map[string]chan CommandResponse)}}}
	session := hub.sessions[1]
	go session.readLoop(slog.New(slog.NewTextHandler(io.Discard, nil)))
	defer session.close()

	request := TransportPingRequest{IP: "target.example.com", Port: 443, Count: 1, Timeout: 1000}
	if _, err := hub.UDPPing(context.Background(), 1, request); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.QUICPing(context.Background(), 1, request); err != nil {
		t.Fatal(err)
	}
	if _, err := hub.KCPPing(context.Background(), 1, request); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"UdpPing", "QuicPing", "KcpPing"} {
		select {
		case actual := <-commands:
			if actual != expected {
				t.Fatalf("command type=%q, want %q", actual, expected)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s", expected)
		}
	}
}

func TestNodeSessionSynchronizesPolicyAndPersistsTelemetry(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "nodehub-session.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository := nodes.NewRepository(db)
	nodeID, err := repository.Create(context.Background(), nodes.CreateRequest{
		Name:      "edge",
		ServerIP:  "edge.example.com",
		PortStart: 1000,
		PortEnd:   2000,
		HTTP:      1,
		TLS:       0,
		Socks:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	secret, err := repository.SecretForInstall(context.Background(), nodeID)
	if err != nil {
		t.Fatal(err)
	}

	hub := New(logger, repository)
	server := httptest.NewServer(hub)
	defer server.Close()
	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	serverURL.Scheme = "ws"
	headers := http.Header{}
	headers.Set("Authorization", "Bearer "+secret)
	headers.Set("X-Flux-Version", "3.0.31")
	headers.Set("X-Flux-Http", "0")
	headers.Set("X-Flux-Tls", "1")
	headers.Set("X-Flux-Socks", "0")
	client, _, err := websocket.DefaultDialer.Dial(serverURL.String(), headers)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cipher, err := crypto.New(secret)
	if err != nil {
		t.Fatal(err)
	}

	_, encryptedCommand, err := client.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	plainCommand, err := decodeMessage(cipher, encryptedCommand)
	if err != nil {
		t.Fatal(err)
	}
	var command CommandMessage
	if err := json.Unmarshal(plainCommand, &command); err != nil {
		t.Fatal(err)
	}
	if command.Type != "SetProtocol" {
		t.Fatalf("command type=%q, want SetProtocol", command.Type)
	}
	var policy map[string]int
	if err := json.Unmarshal(command.Data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy["http"] != 1 || policy["tls"] != 0 || policy["socks"] != 1 || len(policy) != 3 {
		t.Fatalf("policy=%v, want Panel stored policy", policy)
	}
	response, err := json.Marshal(CommandResponse{Type: command.Type, Success: true, RequestID: command.RequestID})
	if err != nil {
		t.Fatal(err)
	}
	encryptedResponse, err := encodeMessage(cipher, response)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.WriteMessage(websocket.TextMessage, encryptedResponse); err != nil {
		t.Fatal(err)
	}

	telemetry, err := json.Marshal(SystemInfo{
		Uptime:           120,
		BytesReceived:    1000,
		BytesTransmitted: 500,
		CPUUsage:         12.5,
		MemoryUsage:      34.5,
		DiskUsage:        56.5,
		ControllerStatuses: []ControllerStatus{{
			Address: "https://primary.example.com",
			Active:  true,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	encryptedTelemetry, err := encodeMessage(cipher, telemetry)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.WriteMessage(websocket.TextMessage, encryptedTelemetry); err != nil {
		t.Fatal(err)
	}

	var node nodes.Node
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		node, err = repository.Get(context.Background(), nodeID)
		if err == nil && node.Uptime == 120 && node.BytesReceived == 1000 && node.BytesTransmitted == 500 && node.CPUUsage == 12.5 && node.MemoryUsage == 34.5 && node.DiskUsage == 56.5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != 1 || node.Version != "3.0.31" || node.HTTP != 1 || node.TLS != 0 || node.Socks != 1 || len(node.Controllers) != 1 {
		t.Fatalf("node session did not converge: %#v", node)
	}
	listed, err := repository.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].DiskUsage != 56.5 || listed[0].BytesReceived != 1000 || listed[0].BytesTransmitted != 500 {
		t.Fatalf("node list omitted persisted telemetry: %#v", listed)
	}
	payload, err := json.Marshal(listed)
	if err != nil {
		t.Fatal(err)
	}
	var apiNodes []map[string]any
	if err := json.Unmarshal(payload, &apiNodes); err != nil {
		t.Fatal(err)
	}
	if len(apiNodes) != 1 || apiNodes[0]["disk_usage"] != 56.5 || apiNodes[0]["bytes_received"] != float64(1000) || apiNodes[0]["bytes_transmitted"] != float64(500) {
		t.Fatalf("node list JSON omitted telemetry: %s", payload)
	}

	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	deadline = time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		node, err = repository.Get(context.Background(), nodeID)
		if err == nil && node.Status == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != 0 {
		t.Fatalf("node remained online after WebSocket disconnect: %#v", node)
	}

	reconnected, _, err := websocket.DefaultDialer.Dial(serverURL.String(), headers)
	if err != nil {
		t.Fatal(err)
	}
	defer reconnected.Close()
	_, encryptedCommand, err = reconnected.ReadMessage()
	if err != nil {
		t.Fatal(err)
	}
	plainCommand, err = decodeMessage(cipher, encryptedCommand)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(plainCommand, &command); err != nil {
		t.Fatal(err)
	}
	if command.Type != "SetProtocol" {
		t.Fatalf("reconnect command type=%q, want SetProtocol", command.Type)
	}
	if err := json.Unmarshal(command.Data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy["http"] != 1 || policy["tls"] != 0 || policy["socks"] != 1 || len(policy) != 3 {
		t.Fatalf("reconnect policy=%v, want Panel stored policy", policy)
	}
	response, err = json.Marshal(CommandResponse{Type: command.Type, Success: true, RequestID: command.RequestID})
	if err != nil {
		t.Fatal(err)
	}
	encryptedResponse, err = encodeMessage(cipher, response)
	if err != nil {
		t.Fatal(err)
	}
	if err := reconnected.WriteMessage(websocket.TextMessage, encryptedResponse); err != nil {
		t.Fatal(err)
	}
}

func TestSetProtocolPolicyUsesPanelStoredValues(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "nodehub.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository := nodes.NewRepository(db)
	nodeID, err := repository.Create(context.Background(), nodes.CreateRequest{
		Name:      "edge",
		ServerIP:  "edge.example.com",
		PortStart: 1000,
		PortEnd:   2000,
		HTTP:      1,
		TLS:       0,
		Socks:     1,
	})
	if err != nil {
		t.Fatal(err)
	}

	upgrader := websocket.Upgrader{}
	commands := make(chan CommandMessage, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		cipher, err := crypto.New("secret")
		if err != nil {
			return
		}
		_, payload, err := conn.ReadMessage()
		if err != nil {
			return
		}
		plain, err := decodeMessage(cipher, payload)
		if err != nil {
			return
		}
		var command CommandMessage
		if err := json.Unmarshal(plain, &command); err != nil {
			return
		}
		commands <- command
		response, err := json.Marshal(CommandResponse{Type: command.Type, Success: true, RequestID: command.RequestID})
		if err != nil {
			return
		}
		encoded, err := encodeMessage(cipher, response)
		if err == nil {
			_ = conn.WriteMessage(websocket.TextMessage, encoded)
		}
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	serverURL.Scheme = "ws"
	client, _, err := websocket.DefaultDialer.Dial(serverURL.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	cipher, err := crypto.New("secret")
	if err != nil {
		t.Fatal(err)
	}
	hub := New(logger, repository)
	session := &session{nodeID: nodeID, conn: client, cipher: cipher, readTimeout: time.Second, wait: make(map[string]chan CommandResponse)}
	hub.sessions[nodeID] = session
	go session.readLoop(logger)
	defer session.close()

	if err := hub.SetProtocolPolicy(context.Background(), nodeID); err != nil {
		t.Fatal(err)
	}
	select {
	case command := <-commands:
		if command.Type != "SetProtocol" {
			t.Fatalf("command type=%q, want SetProtocol", command.Type)
		}
		var policy map[string]int
		if err := json.Unmarshal(command.Data, &policy); err != nil {
			t.Fatal(err)
		}
		if policy["http"] != 1 || policy["tls"] != 0 || policy["socks"] != 1 || len(policy) != 3 {
			t.Fatalf("policy=%v, want complete stored policy", policy)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for SetProtocol command")
	}
}

func TestConnectionVersionUsesHandshakeHeader(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/system-info?version=query", nil)
	request.Header.Set("X-Flux-Version", "3.0.26")

	if version := connectionVersion(request); version != "3.0.26" {
		t.Fatalf("version=%q, want 3.0.26", version)
	}
}

func TestConnectionVersionIgnoresQueryParameters(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/system-info?version=legacy", nil)
	if version := connectionVersion(request); version != "" {
		t.Fatalf("query version was accepted: %q", version)
	}
}

func TestNodeHandshakeRequiresBearerHeaderAndRejectsBrowserOrigins(t *testing.T) {
	hub := New(slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	queryRequest := httptest.NewRequest(http.MethodGet, "/system-info?secret=node-secret", nil)
	if token := bearerToken(queryRequest); token != "" {
		t.Fatalf("query secret was accepted: %q", token)
	}
	headerRequest := httptest.NewRequest(http.MethodGet, "/system-info", nil)
	headerRequest.Header.Set("Authorization", "Bearer node-secret")
	if token := bearerToken(headerRequest); token != "node-secret" {
		t.Fatalf("bearer token=%q", token)
	}
	browserRequest := httptest.NewRequest(http.MethodGet, "/system-info", nil)
	browserRequest.Header.Set("Origin", "https://attacker.example")
	if hub.upgrader.CheckOrigin(browserRequest) {
		t.Fatal("browser origin was accepted")
	}
	if !hub.upgrader.CheckOrigin(headerRequest) {
		t.Fatal("non-browser agent request was rejected")
	}
}

func TestReadLoopRenewsDeadlineOnTextMessages(t *testing.T) {
	upgrader := websocket.Upgrader{}
	connected := make(chan *websocket.Conn, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connected <- conn
	}))
	defer server.Close()

	serverURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	serverURL.Scheme = "ws"
	client, _, err := websocket.DefaultDialer.Dial(serverURL.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	peer := <-connected
	defer peer.Close()

	cipher, err := crypto.New("secret")
	if err != nil {
		t.Fatal(err)
	}
	s := &session{conn: client, cipher: cipher, readTimeout: 40 * time.Millisecond, wait: make(map[string]chan CommandResponse)}
	done := make(chan struct{})
	go func() {
		s.readLoop(slog.New(slog.NewTextHandler(io.Discard, nil)))
		close(done)
	}()

	for i := 0; i < 5; i++ {
		if err := peer.WriteMessage(websocket.TextMessage, []byte("invalid")); err != nil {
			t.Fatal(err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	select {
	case <-done:
		t.Fatal("active WebSocket expired while text messages were arriving")
	default:
	}
	_ = peer.Close()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("read loop did not stop after peer closed")
	}
}
