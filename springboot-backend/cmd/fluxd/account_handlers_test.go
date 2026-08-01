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
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/forwards"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/observability"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
	"github.com/suyunjing-su/fpanel/backend/internal/users"
)

func TestAccountPackageResetAndPasswordRoutes(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "account.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	passwordHash, err := auth.HashPassword("current-pass")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,token_version,expires_at,flow_quota_bytes,ingress_bytes,egress_bytes,flow_reset_day,forward_quota,status,created_at,updated_at) VALUES(1,'member',?,'user',1,4102444800000,?,123,456,15,20,1,1,1)`, passwordHash, int64(100)*1024*1024*1024); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'entry','entry.example.com','entry.example.com',1000,2000,'secret',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES(1,'primary',1,1,1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnel_nodes(id,tunnel_id,chain_type,node_id,port,hop_index,flow_quota_bytes,speed_limit_mbps) VALUES(1,1,1,1,1200,0,0,0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO user_tunnels(id,user_id,tunnel_id,flow_quota_bytes,ingress_bytes,egress_bytes,forward_quota,flow_reset_day,expires_at,status,created_at,updated_at) VALUES(1,1,1,?,11,22,20,15,4102444800000,1,1,1)`, int64(100)*1024*1024*1024); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,status,created_at,updated_at) VALUES(1,1,'web',1,'example.com:443',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,1500)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO statistics_flows(id,user_id,flow,total_flow,recorded_at) VALUES(1,1,1,333,?)`, now); err != nil {
		t.Fatal(err)
	}

	userRepo := users.NewRepository(db)
	nodeRepo := nodes.NewRepository(db)
	tunnelRepo := tunnels.NewRepository(db, nodeRepo)
	forwardRepo := forwards.NewRepository(db, nodeRepo, tunnelRepo, nil)
	authRepo := auth.NewRepository(db)
	mux := http.NewServeMux()
	registerAccountRoutes(mux, userRepo, tunnelRepo, forwardRepo, authRepo, &batchWakeCounter{}, func(*http.Request) bool { return true })
	manager := auth.New("0123456789abcdef0123456789abcdef", time.Hour)
	handler := httpapi.Middleware(slog.New(slog.NewTextHandler(io.Discard, nil)), observability.NewMetrics(db), manager, "metrics-token-0123456789abcdef0123", nil, mux, authRepo)
	token, err := manager.Issue(auth.Identity{UserID: 1, Username: "member", Role: "user", RoleID: 1, TokenVersion: 1})
	if err != nil {
		t.Fatal(err)
	}

	request := authenticatedRequest(http.MethodPost, "/api/v1/user/package", `{"ids":[]}`, token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("package status=%d body=%s", response.Code, response.Body.String())
	}
	var packageEnvelope struct {
		Code int `json:"code"`
		Data struct {
			UserInfo          users.User             `json:"userInfo"`
			TunnelPermissions []map[string]any       `json:"tunnelPermissions"`
			Forwards          []map[string]any       `json:"forwards"`
			StatisticsFlows   []users.StatisticsFlow `json:"statisticsFlows"`
		} `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&packageEnvelope); err != nil {
		t.Fatal(err)
	}
	if packageEnvelope.Code != 0 || packageEnvelope.Data.UserInfo.Flow != 100 || len(packageEnvelope.Data.TunnelPermissions) != 1 || len(packageEnvelope.Data.Forwards) != 1 || len(packageEnvelope.Data.StatisticsFlows) != 1 {
		t.Fatalf("unexpected package: %#v", packageEnvelope.Data)
	}

	reset := authenticatedRequest(http.MethodPost, "/api/v1/user/reset", `{"id":1,"type":1}`, token)
	resetResponse := httptest.NewRecorder()
	handler.ServeHTTP(resetResponse, reset)
	if resetResponse.Code != http.StatusOK {
		t.Fatalf("reset user status=%d body=%s", resetResponse.Code, resetResponse.Body.String())
	}
	var inFlow, outFlow int64
	if err := db.QueryRow("SELECT ingress_bytes,egress_bytes FROM users WHERE id=1").Scan(&inFlow, &outFlow); err != nil {
		t.Fatal(err)
	}
	if inFlow != 0 || outFlow != 0 {
		t.Fatalf("user flow not reset: %d/%d", inFlow, outFlow)
	}
	resetTunnel := authenticatedRequest(http.MethodPost, "/api/v1/user/reset", `{"id":1,"type":2}`, token)
	resetTunnelResponse := httptest.NewRecorder()
	handler.ServeHTTP(resetTunnelResponse, resetTunnel)
	if resetTunnelResponse.Code != http.StatusOK {
		t.Fatalf("reset tunnel status=%d body=%s", resetTunnelResponse.Code, resetTunnelResponse.Body.String())
	}
	if err := db.QueryRow("SELECT ingress_bytes,egress_bytes FROM user_tunnels WHERE id=1").Scan(&inFlow, &outFlow); err != nil {
		t.Fatal(err)
	}
	if inFlow != 0 || outFlow != 0 {
		t.Fatalf("user tunnel flow not reset: %d/%d", inFlow, outFlow)
	}

	badPassword := authenticatedRequest(http.MethodPost, "/api/v1/user/updatePassword", `{"newUsername":"member2","currentPassword":"current-pass","newPassword":"next-pass","confirmPassword":"different"}`, token)
	badPasswordResponse := httptest.NewRecorder()
	handler.ServeHTTP(badPasswordResponse, badPassword)
	if badPasswordResponse.Code != http.StatusBadRequest {
		t.Fatalf("confirm mismatch status=%d", badPasswordResponse.Code)
	}

	updatePassword := authenticatedRequest(http.MethodPost, "/api/v1/user/updatePassword", `{"newUsername":"member2","currentPassword":"current-pass","newPassword":"next-pass","confirmPassword":"next-pass"}`, token)
	updatePasswordResponse := httptest.NewRecorder()
	handler.ServeHTTP(updatePasswordResponse, updatePassword)
	if updatePasswordResponse.Code != http.StatusOK {
		t.Fatalf("update password status=%d body=%s", updatePasswordResponse.Code, updatePasswordResponse.Body.String())
	}
	var username string
	var tokenVersion int
	var newHash string
	if err := db.QueryRow("SELECT username,token_version,password_hash FROM users WHERE id=1").Scan(&username, &tokenVersion, &newHash); err != nil {
		t.Fatal(err)
	}
	if username != "member2" || tokenVersion != 2 || !auth.VerifyPassword(newHash, "next-pass") {
		t.Fatalf("password update failed username=%q tokenVersion=%d", username, tokenVersion)
	}

	staleResponse := httptest.NewRecorder()
	handler.ServeHTTP(staleResponse, authenticatedRequest(http.MethodPost, "/api/v1/user/package", `{}`, token))
	if staleResponse.Code != http.StatusUnauthorized {
		t.Fatalf("stale token status=%d body=%s", staleResponse.Code, staleResponse.Body.String())
	}
}

func authenticatedRequest(method, path, body, token string) *http.Request {
	request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	request.Header.Set("Authorization", "Bearer "+token)
	return request
}
