package nodehub

import (
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

func TestConnectionMetadataFallsBackToQuery(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/system-info?version=legacy&http=1&tls=0&socks=1", nil)
	version, httpFlag, tlsFlag, socksFlag := connectionMetadata(request)
	if version != "legacy" || httpFlag != 1 || tlsFlag != 0 || socksFlag != 1 {
		t.Fatalf("unexpected metadata: %q %d %d %d", version, httpFlag, tlsFlag, socksFlag)
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
