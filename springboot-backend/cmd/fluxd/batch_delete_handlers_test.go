package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/batchdelete"
	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/forwards"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
	"github.com/suyunjing-su/fpanel/backend/internal/users"
)

type batchWakeCounter struct{ count int }

func (w *batchWakeCounter) Wake() { w.count++ }

func TestUserBatchDeleteReturnsPartialResult(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "batch.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,token_version,expires_at,status,created_at,updated_at) VALUES(1,'member','hash','user',1,0,1,1,1)`); err != nil {
		t.Fatal(err)
	}

	nodeRepo := nodes.NewRepository(db)
	tunnelRepo := tunnels.NewRepository(db, nodeRepo)
	wake := &batchWakeCounter{}
	mux := http.NewServeMux()
	registerBatchDeleteRoutes(mux, users.NewRepository(db), nodeRepo, tunnelRepo, forwards.NewRepository(db, nodeRepo, tunnelRepo), wake, func(*http.Request) bool { return true })

	request := httptest.NewRequest(http.MethodPost, "/api/v1/user/batch-delete", bytes.NewBufferString(`{"ids":[1,999,1]}`))
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int                `json:"code"`
		Data batchdelete.Result `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || envelope.Data.TotalCount != 2 || envelope.Data.SuccessCount != 1 || envelope.Data.FailureCount != 1 {
		t.Fatalf("unexpected response: %#v", envelope)
	}
	if len(envelope.Data.Failures) != 1 || envelope.Data.Failures[0].ID != 999 || envelope.Data.Failures[0].Name != "ID 999" {
		t.Fatalf("unexpected failures: %#v", envelope.Data.Failures)
	}
	if wake.count != 1 {
		t.Fatalf("refresh wake count = %d, want 1", wake.count)
	}
	var count int
	if err := db.QueryRow("SELECT COUNT(1) FROM users WHERE id=1").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("successfully deleted user still exists")
	}
}

func TestBatchDeleteRejectsEmptySelectionWithoutWake(t *testing.T) {
	wake := &batchWakeCounter{}
	mux := http.NewServeMux()
	registerBatchDeleteHandler(mux, "/batch", true, func(*http.Request) bool { return true }, wake, func(*http.Request, int64) (string, error) {
		t.Fatal("remove called for empty selection")
		return "", nil
	})
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/batch", bytes.NewBufferString(`{"ids":[]}`)))
	if response.Code != http.StatusBadRequest || wake.count != 0 {
		t.Fatalf("status=%d wake=%d body=%s", response.Code, wake.count, response.Body.String())
	}
}
