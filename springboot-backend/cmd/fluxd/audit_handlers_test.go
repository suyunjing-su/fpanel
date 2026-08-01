package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/audit"
	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestAuditListRouteRequiresAdmin(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "audit-handler.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := audit.NewRepository(db)
	if err := repository.Record(context.Background(), audit.Event{Action: "post", ResourceType: "node/create", Outcome: "success", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerAuditRoutes(mux, repository, func(r *http.Request) bool { return r.Header.Get("X-Admin") == "1" })

	denied := httptest.NewRecorder()
	mux.ServeHTTP(denied, httptest.NewRequest(http.MethodPost, "/api/v1/audit/list", bytes.NewBufferString(`{"page":1,"pageSize":50}`)))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d", denied.Code)
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/audit/list", bytes.NewBufferString(`{"page":1,"pageSize":50}`))
	request.Header.Set("X-Admin", "1")
	allowed := httptest.NewRecorder()
	mux.ServeHTTP(allowed, request)
	if allowed.Code != http.StatusOK {
		t.Fatalf("admin status=%d body=%s", allowed.Code, allowed.Body.String())
	}
	if !strings.Contains(allowed.Body.String(), `"resourceType":"node/create"`) {
		t.Fatalf("audit event missing from response: %s", allowed.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/audit/list", bytes.NewBufferString(`{"page":1,"pageSize":201}`))
	request.Header.Set("X-Admin", "1")
	invalid := httptest.NewRecorder()
	mux.ServeHTTP(invalid, request)
	if invalid.Code != http.StatusBadRequest {
		t.Fatalf("invalid pagination status=%d", invalid.Code)
	}
}
