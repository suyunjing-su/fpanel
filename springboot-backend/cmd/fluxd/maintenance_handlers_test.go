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

	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/maintenance"
)

func TestMaintenanceRunListRequiresAdminAndReturnsNewestFirst(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "maintenance-routes.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO maintenance_run_events(job,period_key,status,detail,started_at,completed_at) VALUES
		('hourly_traffic_statistics','2026-08-02T10','succeeded','old',100,200),
		('monthly_traffic_reset','2026-08-02','failed','new',300,400)`); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	registerMaintenanceRoutes(mux, maintenance.NewRepository(db), func(r *http.Request) bool { return r.Header.Get("X-Admin") == "1" })

	forbidden := httptest.NewRecorder()
	mux.ServeHTTP(forbidden, httptest.NewRequest(http.MethodPost, "/api/v1/maintenance/run/list", bytes.NewBufferString(`{}`)))
	if forbidden.Code != http.StatusForbidden {
		t.Fatalf("status=%d body=%s", forbidden.Code, forbidden.Body.String())
	}

	request := httptest.NewRequest(http.MethodPost, "/api/v1/maintenance/run/list", bytes.NewBufferString(`{"limit":1}`))
	request.Header.Set("X-Admin", "1")
	response := httptest.NewRecorder()
	mux.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int                    `json:"code"`
		Data []maintenance.RunEvent `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || len(envelope.Data) != 1 || envelope.Data[0].Detail != "new" || envelope.Data[0].Status != "failed" {
		t.Fatalf("unexpected response: %#v", envelope)
	}
}
