package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestOperationsRoutesRequireAdminAndProduceArtifacts(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "operations.db")
	db, err := database.Open(ctx, path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	mux := http.NewServeMux()
	restart := make(chan struct{}, 1)
	registerOperationsRoutes(mux, db, path, restart, func(r *http.Request) bool { return r.Header.Get("X-Admin") == "1" })

	denied := httptest.NewRecorder()
	mux.ServeHTTP(denied, httptest.NewRequest(http.MethodPost, "/api/v1/operations/backup", nil))
	if denied.Code != http.StatusForbidden {
		t.Fatalf("denied status=%d", denied.Code)
	}

	backup := httptest.NewRecorder()
	backupRequest := httptest.NewRequest(http.MethodPost, "/api/v1/operations/backup", nil)
	backupRequest.Header.Set("X-Admin", "1")
	mux.ServeHTTP(backup, backupRequest)
	if backup.Code != http.StatusOK || backup.Body.Len() < 100 || backup.Header().Get("Content-Disposition") == "" {
		t.Fatalf("backup status=%d size=%d", backup.Code, backup.Body.Len())
	}

	support := httptest.NewRecorder()
	supportRequest := httptest.NewRequest(http.MethodPost, "/api/v1/operations/support-bundle", nil)
	supportRequest.Header.Set("X-Admin", "1")
	mux.ServeHTTP(support, supportRequest)
	if support.Code != http.StatusOK || support.Header().Get("Content-Type") != "application/zip" || support.Body.Len() == 0 {
		t.Fatalf("support status=%d size=%d", support.Code, support.Body.Len())
	}
}

func TestOperationsRestoreStagesValidatedDatabase(t *testing.T) {
	ctx := context.Background()
	directory := t.TempDir()
	path := filepath.Join(directory, "current.db")
	db, err := database.Open(ctx, path, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var backup bytes.Buffer
	if err := database.Backup(ctx, db, &backup); err != nil {
		t.Fatal(err)
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("backup", "backup.db")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(backup.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	restart := make(chan struct{}, 1)
	registerOperationsRoutes(mux, db, path, restart, func(*http.Request) bool { return true })
	request := httptest.NewRequest(http.MethodPost, "/api/v1/operations/restore", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil || payload.Code != 0 {
		t.Fatalf("invalid response: %v %s", err, response.Body.String())
	}
	if _, err := os.Stat(path + ".restore-pending"); err != nil {
		t.Fatal(err)
	}
}
