package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/nodeconfig"
	"github.com/suyunjing-su/fpanel/backend/internal/nodehub/crypto"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
)

type recordingRefreshes struct {
	nodeIDs []int64
}

func (r *recordingRefreshes) RequestNodes(_ context.Context, nodeIDs []int64) error {
	r.nodeIDs = append(r.nodeIDs, nodeIDs...)
	return nil
}

func TestNodeFullConfigAuthenticationAndShape(t *testing.T) {
	db, nodeRepo, configRepo, logger := openNodeHandlerDatabase(t)
	insertNodeHandlerFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'node','10.0.0.1','203.0.113.1',1000,2000,'node-secret',1,1,1)`)

	request := httptest.NewRequest(http.MethodGet, "/flow/config/all", nil)
	request.Header.Set("Authorization", "Bearer node-secret")
	response := httptest.NewRecorder()
	getNodeFullConfig(response, request, nodeRepo, configRepo, logger)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected full config status %d: %s", response.Code, response.Body.String())
	}
	var document nodeconfig.Document
	if err := json.Unmarshal(response.Body.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document.Services == nil || document.Chains == nil || document.Limiters == nil {
		t.Fatalf("full config does not contain authoritative empty arrays: %s", response.Body.String())
	}

	request = httptest.NewRequest(http.MethodGet, "/flow/config/all", nil)
	request.Header.Set("Authorization", "invalid")
	response = httptest.NewRecorder()
	getNodeFullConfig(response, request, nodeRepo, configRepo, logger)
	if response.Code != http.StatusForbidden || response.Body.String() != "forbidden" {
		t.Fatalf("invalid secret response is unexpected: %d %q", response.Code, response.Body.String())
	}
}

func TestNodeConfigSnapshotReconciliation(t *testing.T) {
	db, nodeRepo, configRepo, logger := openNodeHandlerDatabase(t)
	insertNodeHandlerFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'node','10.0.0.1','203.0.113.1',1000,2000,'node-secret',1,1,1)`)
	expected, err := configRepo.Build(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(expected)
	if err != nil {
		t.Fatal(err)
	}
	refreshes := &recordingRefreshes{}

	request := httptest.NewRequest(http.MethodPost, "/flow/config", bytes.NewReader(payload))
	request.Header.Set("Authorization", "node-secret")
	response := httptest.NewRecorder()
	reconcileNodeConfig(response, request, nodeRepo, configRepo, refreshes, logger)
	if response.Code != http.StatusOK || response.Body.String() != "ok" || len(refreshes.nodeIDs) != 0 {
		t.Fatalf("matching snapshot caused refresh: status=%d body=%q nodes=%v", response.Code, response.Body.String(), refreshes.nodeIDs)
	}

	drifted := nodeconfig.Document{Services: []map[string]any{{"name": "orphan"}}, Chains: []map[string]any{}, Limiters: []map[string]any{}}
	plain, _ := json.Marshal(drifted)
	cipher, err := crypto.New("node-secret")
	if err != nil {
		t.Fatal(err)
	}
	encrypted, err := cipher.Encrypt(plain)
	if err != nil {
		t.Fatal(err)
	}
	wrapper, _ := json.Marshal(map[string]any{"encrypted": true, "data": encrypted, "timestamp": time.Now().Unix()})
	request = httptest.NewRequest(http.MethodPost, "/flow/config", bytes.NewReader(wrapper))
	request.Header.Set("Authorization", "Bearer node-secret")
	response = httptest.NewRecorder()
	reconcileNodeConfig(response, request, nodeRepo, configRepo, refreshes, logger)
	if response.Code != http.StatusOK || response.Body.String() != "ok" || len(refreshes.nodeIDs) != 1 || refreshes.nodeIDs[0] != 1 {
		t.Fatalf("drifted encrypted snapshot was not queued: status=%d body=%q nodes=%v", response.Code, response.Body.String(), refreshes.nodeIDs)
	}

	request = httptest.NewRequest(http.MethodPost, "/flow/config", bytes.NewReader([]byte(`{"services":[]}`)))
	request.Header.Set("Authorization", "invalid")
	response = httptest.NewRecorder()
	reconcileNodeConfig(response, request, nodeRepo, configRepo, refreshes, logger)
	if response.Code != http.StatusOK || response.Body.String() != "ok" || len(refreshes.nodeIDs) != 1 {
		t.Fatalf("unknown node snapshot response is unexpected: %d %q", response.Code, response.Body.String())
	}
}

func openNodeHandlerDatabase(t *testing.T) (*sql.DB, *nodes.Repository, *nodeconfig.Repository, *slog.Logger) {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "handlers.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db, nodes.NewRepository(db), nodeconfig.NewRepository(db), logger
}

func insertNodeHandlerFixture(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}
