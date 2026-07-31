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
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/runtimecontrols"
)

type wakeRecorder struct{ calls int }

func (r *wakeRecorder) Wake() { r.calls++ }

func TestRuntimeControlRoutesRequireAdminAndPersist(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "routes.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'node','10.0.0.1','203.0.113.1',10000,20000,'secret',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	refreshes := &wakeRecorder{}
	mux := http.NewServeMux()
	registerRuntimeControlRoutes(mux, runtimecontrols.NewRepository(db), refreshes, func(r *http.Request) bool {
		return r.Header.Get("X-Test-Admin") == "true"
	})

	response := serveRuntimeRequest(mux, "/api/v1/node-group/create", `{"name":"pool","strategy":"fifo","maxFails":1,"failTimeoutMs":1000,"status":1,"members":[{"nodeId":1,"priority":100,"backup":0,"sortIndex":0}]}`, false)
	if response.Code != http.StatusForbidden {
		t.Fatalf("non-admin status = %d, body=%s", response.Code, response.Body.String())
	}
	response = serveRuntimeRequest(mux, "/api/v1/node-group/create", `{"name":"pool","strategy":"fifo","maxFails":1,"failTimeoutMs":1000,"status":1,"members":[{"nodeId":1,"priority":100,"backup":0,"sortIndex":0}]}`, true)
	if response.Code != http.StatusOK {
		t.Fatalf("create status = %d, body=%s", response.Code, response.Body.String())
	}
	var created httpapi.APIResponse
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil || created.Code != 0 {
		t.Fatalf("invalid create response: %s, %v", response.Body.String(), err)
	}
	if refreshes.calls != 1 {
		t.Fatalf("refresh wake calls = %d, want 1", refreshes.calls)
	}

	response = serveRuntimeRequest(mux, "/api/v1/node-group/list", `{}`, true)
	if response.Code != http.StatusOK {
		t.Fatalf("list status = %d, body=%s", response.Code, response.Body.String())
	}
	var listed struct {
		Code int `json:"code"`
		Data []struct {
			Name    string `json:"name"`
			Members []struct {
				NodeID int64 `json:"nodeId"`
			} `json:"members"`
		} `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &listed); err != nil {
		t.Fatal(err)
	}
	if listed.Code != 0 || len(listed.Data) != 1 || listed.Data[0].Name != "pool" || len(listed.Data[0].Members) != 1 || listed.Data[0].Members[0].NodeID != 1 {
		t.Fatalf("unexpected list response: %s", response.Body.String())
	}

	response = serveRuntimeRequest(mux, "/api/v1/endpoint-group/create", `{"name":"invalid","strategy":"fifo","maxFails":0,"failTimeoutMs":1,"probeIntervalMs":1,"probeTimeoutMs":1,"status":1,"endpoints":[]}`, true)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("invalid endpoint group status = %d, body=%s", response.Code, response.Body.String())
	}
}

func serveRuntimeRequest(handler http.Handler, path, body string, admin bool) *httptest.ResponseRecorder {
	request := httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	if admin {
		request.Header.Set("X-Test-Admin", "true")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	return response
}
