package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/observability"
)

func TestSubscriptionUsageRoute(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "subscription.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	hash, err := auth.HashPassword("member-pass")
	if err != nil {
		t.Fatal(err)
	}
	expiresAt := time.Now().Add(time.Hour).UnixMilli()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,expires_at,flow_quota_bytes,ingress_bytes,egress_bytes,status,created_at,updated_at) VALUES(1,'member',?,'user',?,1000,200,300,1,1,1)`, hash, expiresAt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnels(id,name,type,flow,status,created_at,updated_at) VALUES(1,'primary',1,1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO user_tunnels(id,user_id,tunnel_id,flow_quota_bytes,ingress_bytes,egress_bytes,expires_at,status,created_at,updated_at) VALUES(10,1,1,800,100,150,?,1,1,1)`, expiresAt); err != nil {
		t.Fatal(err)
	}

	repository := auth.NewRepository(db)
	mux := http.NewServeMux()
	registerOpenAPIRoutes(mux, repository)
	handler := httpapi.Middleware(slog.New(slog.NewTextHandler(io.Discard, nil)), observability.NewMetrics(db), auth.New("0123456789abcdef0123456789abcdef", time.Hour), nil, mux, repository)

	request := httptest.NewRequest(http.MethodGet, "/api/v1/open_api/sub_store?tunnel=10", nil)
	request.SetBasicAuth("member", "member-pass")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("subscription status=%d body=%s", response.Code, response.Body.String())
	}
	expected := "upload=150; download=100; total=800; expire=" + strconv.FormatInt(expiresAt/1000, 10)
	if got := response.Header().Get("Subscription-Userinfo"); got != expected || response.Body.String() != expected {
		t.Fatalf("subscription response header=%q body=%q want=%q", got, response.Body.String(), expected)
	}
	if response.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("cache control=%q", response.Header().Get("Cache-Control"))
	}

	legacy := httptest.NewRequest(http.MethodGet, "/api/v1/open_api/sub_store?user=member&pwd=member-pass", nil)
	legacyResponse := httptest.NewRecorder()
	handler.ServeHTTP(legacyResponse, legacy)
	if legacyResponse.Code != http.StatusOK || legacyResponse.Header().Get("Subscription-Userinfo") != "upload=300; download=200; total=1000; expire="+strconv.FormatInt(expiresAt/1000, 10) {
		t.Fatalf("legacy subscription status=%d header=%q body=%s", legacyResponse.Code, legacyResponse.Header().Get("Subscription-Userinfo"), legacyResponse.Body.String())
	}

	unauthorized := httptest.NewRequest(http.MethodGet, "/api/v1/open_api/sub_store", nil)
	unauthorized.SetBasicAuth("member", "wrong")
	unauthorizedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unauthorizedResponse, unauthorized)
	if unauthorizedResponse.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", unauthorizedResponse.Code, unauthorizedResponse.Body.String())
	}

	missingTunnel := httptest.NewRequest(http.MethodGet, "/api/v1/open_api/sub_store?tunnel=999", nil)
	missingTunnel.SetBasicAuth("member", "member-pass")
	missingTunnelResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingTunnelResponse, missingTunnel)
	if missingTunnelResponse.Code != http.StatusNotFound {
		t.Fatalf("missing tunnel status=%d body=%s", missingTunnelResponse.Code, missingTunnelResponse.Body.String())
	}
}
