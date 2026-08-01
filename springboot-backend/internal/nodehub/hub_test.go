package nodehub

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/suyunjing-su/fpanel/backend/internal/nodehub/crypto"
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

func TestConnectionMetadataPrefersHandshakeHeaders(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/system-info?version=query&http=0&tls=0&socks=0", nil)
	request.Header.Set("X-Flux-Version", "3.0.26")
	request.Header.Set("X-Flux-Http", "1")
	request.Header.Set("X-Flux-Tls", "1")
	request.Header.Set("X-Flux-Socks", "1")

	version, httpFlag, tlsFlag, socksFlag := connectionMetadata(request)
	if version != "3.0.26" || httpFlag != 1 || tlsFlag != 1 || socksFlag != 1 {
		t.Fatalf("unexpected metadata: %q %d %d %d", version, httpFlag, tlsFlag, socksFlag)
	}
}

func TestConnectionMetadataIgnoresQueryParameters(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/system-info?version=legacy&http=1&tls=1&socks=1", nil)
	version, httpFlag, tlsFlag, socksFlag := connectionMetadata(request)
	if version != "" || httpFlag != 0 || tlsFlag != 0 || socksFlag != 0 {
		t.Fatalf("query metadata was accepted: %q %d %d %d", version, httpFlag, tlsFlag, socksFlag)
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
