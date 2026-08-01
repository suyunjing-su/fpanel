package httpapi

import (
	"bufio"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/audit"
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
		nil,
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

func TestMiddlewareAuditsOnlyRedactedMutations(t *testing.T) {
	manager := auth.New("0123456789abcdef0123456789abcdef", time.Hour)
	identity := auth.Identity{UserID: 42, Username: "operator", Role: "admin", RoleID: 0, TokenVersion: 3}
	token, err := manager.Issue(identity)
	if err != nil {
		t.Fatal(err)
	}
	recorder := &recordingAudit{}
	handler := Middleware(
		slog.New(slog.NewTextHandler(io.Discard, nil)),
		observability.NewMetrics(),
		manager,
		nil,
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if strings.Contains(r.URL.Path, "delete") {
				WriteJSON(w, http.StatusBadRequest, Failure(http.StatusBadRequest, "failed"))
				return
			}
			WriteJSON(w, http.StatusOK, Success(nil))
		}),
		nil,
		recorder,
	)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/user/updatePassword", strings.NewReader(`{"currentPassword":"secret","newPassword":"more-secret"}`))
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Request-ID", "request-123")
	request.RemoteAddr = "192.0.2.10:4567"
	handler.ServeHTTP(httptest.NewRecorder(), request)
	request = httptest.NewRequest(http.MethodPost, "/api/v1/forward/delete", strings.NewReader(`{"id":9}`))
	request.Header.Set("Authorization", token)
	handler.ServeHTTP(httptest.NewRecorder(), request)
	for _, path := range []string{"/api/v1/user/list", "/api/v1/tunnel/user/tunnel", "/api/v1/user/login", "/api/v1/captcha/check", "/api/v1/open_api/sub_store"} {
		request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"password":"must-not-appear"}`))
		if path != "/api/v1/user/login" && !strings.HasPrefix(path, "/api/v1/captcha/") && !strings.HasPrefix(path, "/api/v1/open_api/") {
			request.Header.Set("Authorization", token)
		}
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}

	if len(recorder.events) != 2 {
		t.Fatalf("recorded %d events, want 2", len(recorder.events))
	}
	first := recorder.events[0]
	if first.ActorID == nil || *first.ActorID != 42 || first.ResourceType != "user/updatePassword" || first.Outcome != "success" || first.RequestID != "request-123" || first.RemoteAddr != "192.0.2.10" || first.Detail != "status=200" {
		t.Fatalf("unexpected success event: %+v", first)
	}
	second := recorder.events[1]
	if second.Outcome != "failure" || second.Detail != "status=400" {
		t.Fatalf("unexpected failure event: %+v", second)
	}
	for _, event := range recorder.events {
		if strings.Contains(event.Detail, "secret") || strings.Contains(event.Detail, "password") {
			t.Fatalf("sensitive request data was audited: %+v", event)
		}
	}
}

type recordingAudit struct {
	events []audit.Event
}

func (r *recordingAudit) Record(_ context.Context, event audit.Event) error {
	r.events = append(r.events, event)
	return nil
}

type hijackRecorder struct {
	*httptest.ResponseRecorder
}

func (w *hijackRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	client, server := net.Pipe()
	_ = client.Close()
	return server, bufio.NewReadWriter(bufio.NewReader(server), bufio.NewWriter(server)), nil
}
