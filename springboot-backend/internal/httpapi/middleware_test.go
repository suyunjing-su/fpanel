package httpapi

import (
	"bufio"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/observability"
)

func TestSystemInfoBypassesJWTAndPreservesHijacker(t *testing.T) {
	called := false
	handler := Middleware(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		observability.NewMetrics(),
		auth.New("0123456789abcdef0123456789abcdef", time.Hour),
		nil,
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			called = true
			if _, ok := w.(http.Hijacker); !ok {
				t.Fatal("middleware response writer does not implement http.Hijacker")
			}
			w.WriteHeader(http.StatusSwitchingProtocols)
		}),
	)
	request := httptest.NewRequest(http.MethodGet, "/system-info", nil)
	response := &hijackRecorder{ResponseRecorder: httptest.NewRecorder()}
	handler.ServeHTTP(response, request)
	if !called {
		t.Fatal("system-info handler was rejected before node authentication")
	}
	if response.Code != http.StatusSwitchingProtocols {
		t.Fatalf("status = %d, want %d", response.Code, http.StatusSwitchingProtocols)
	}
}

type hijackRecorder struct {
	*httptest.ResponseRecorder
}

func (w *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	client, server := net.Pipe()
	_ = client.Close()
	return server, bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)), nil
}
