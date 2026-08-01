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
	"strings"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/forwards"
	"github.com/suyunjing-su/fpanel/backend/internal/httpapi"
	"github.com/suyunjing-su/fpanel/backend/internal/nodehub"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/observability"
	"github.com/suyunjing-su/fpanel/backend/internal/siteconfig"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
)

type recordingPinger struct {
	requests []nodehub.TCPPingRequest
	nodeIDs  []int64
}

func (p *recordingPinger) TCPPing(_ context.Context, nodeID int64, request nodehub.TCPPingRequest) (nodehub.TCPPingResponse, error) {
	p.nodeIDs = append(p.nodeIDs, nodeID)
	p.requests = append(p.requests, request)
	return nodehub.TCPPingResponse{IP: request.IP, Port: request.Port, Success: true, AverageTime: 12.5}, nil
}

func TestNodeInstallCommandUsesSiteConfigAndHiddenSecret(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "node-tools.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'edge','2001:db8::1','2001:db8::1',1000,2000,'sec''ret',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := siteconfig.NewRepository(db).Update(context.Background(), map[string]string{"ip": "http://[2001:db8::10]:8443/", "protocol_type": "https"}); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	nodeRepo := nodes.NewRepository(db)
	tunnelRepo := tunnels.NewRepository(db, nodeRepo)
	registerNodeToolsRoutes(mux, nodeRepo, tunnelRepo, forwards.NewRepository(db, nodeRepo, tunnelRepo, nil), siteconfig.NewRepository(db), &recordingPinger{}, func(*http.Request) bool { return true })

	response := httptest.NewRecorder()
	mux.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/node/install", bytes.NewBufferString(`{"id":1}`)))
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Code int    `json:"code"`
		Data string `json:"data"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Code != 0 || !strings.Contains(envelope.Data, "install.sh -a 'https://[2001:db8::10]:8443' -s 'sec'\\''ret'") {
		t.Fatalf("unexpected command: %q", envelope.Data)
	}
}

func TestTunnelDiagnoseRunsTopologyPings(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "tunnel-diagnose.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insertDiagnosisFixture(t, db)
	pinger := &recordingPinger{}
	nodeRepo := nodes.NewRepository(db)
	result, err := diagnoseTunnel(context.Background(), tunnels.NewRepository(db, nodeRepo), nodeRepo, pinger, 1)
	if err != nil {
		t.Fatal(err)
	}
	if result.TunnelName != "mesh" || result.TunnelType != "隧道转发" || len(result.Results) != 3 {
		t.Fatalf("unexpected result: %#v", result)
	}
	if pinger.requests[0].IP != "chain.example.com" || pinger.requests[0].Port != 7001 || pinger.nodeIDs[0] != 1 {
		t.Fatalf("entry ping mismatch ids=%v reqs=%#v", pinger.nodeIDs, pinger.requests)
	}
	if pinger.requests[2].IP != "www.google.com" || pinger.requests[2].Port != 443 || result.Results[2].FromChainType != 3 {
		t.Fatalf("exit ping mismatch result=%#v reqs=%#v", result.Results[2], pinger.requests)
	}
}

func TestForwardDiagnoseRequiresOwnerAndTargetsRemoteAddresses(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "forward-diagnose.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insertDiagnosisFixture(t, db)
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,token_version,expires_at,status,created_at,updated_at) VALUES(1,'member','hash','user',1,4102444800000,1,1,1),(2,'other','hash','user',1,4102444800000,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,status,created_at,updated_at) VALUES(1,1,'web',1,'example.com:443,[2001:db8::20]:8443',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,1500)`); err != nil {
		t.Fatal(err)
	}
	pinger := &recordingPinger{}
	nodeRepo := nodes.NewRepository(db)
	tunnelRepo := tunnels.NewRepository(db, nodeRepo)
	forwardRepo := forwards.NewRepository(db, nodeRepo, tunnelRepo, nil)
	result, err := diagnoseForward(context.Background(), forwardRepo, tunnelRepo, nodeRepo, pinger, 1, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if result.ForwardName != "web" || len(result.Results) != 4 {
		t.Fatalf("unexpected forward result: %#v", result)
	}
	last := pinger.requests[len(pinger.requests)-1]
	if last.IP != "2001:db8::20" || last.Port != 8443 {
		t.Fatalf("IPv6 remote target was not diagnosed: %#v", pinger.requests)
	}
	if _, err := diagnoseForward(context.Background(), forwardRepo, tunnelRepo, nodeRepo, pinger, 1, 2, false); err == nil {
		t.Fatal("non-owner forward diagnosis was allowed")
	}
}

func TestForwardDiagnoseRouteUsesAuthenticatedIdentity(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "forward-route.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	insertDiagnosisFixture(t, db)
	passwordHash, err := auth.HashPassword("passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,token_version,expires_at,status,created_at,updated_at) VALUES(1,'member',?,'user',1,4102444800000,1,1,1)`, passwordHash); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,status,created_at,updated_at) VALUES(1,1,'web',1,'example.com:443',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,1500)`); err != nil {
		t.Fatal(err)
	}
	nodeRepo := nodes.NewRepository(db)
	tunnelRepo := tunnels.NewRepository(db, nodeRepo)
	mux := http.NewServeMux()
	registerNodeToolsRoutes(mux, nodeRepo, tunnelRepo, forwards.NewRepository(db, nodeRepo, tunnelRepo, nil), siteconfig.NewRepository(db), &recordingPinger{}, func(*http.Request) bool { return false })
	manager := auth.New("0123456789abcdef0123456789abcdef", time.Hour)
	token, err := manager.Issue(auth.Identity{UserID: 1, Username: "member", Role: "user", RoleID: 1, TokenVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	handler := httpapi.Middleware(slog.New(slog.NewTextHandler(io.Discard, nil)), observability.NewMetrics(db), manager, nil, mux, auth.NewRepository(db))
	request := authenticatedRequest(http.MethodPost, "/api/v1/forward/diagnose", `{"forwardId":1}`, token)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func insertDiagnosisFixture(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES
		(1,'entry','entry.example.com','entry.example.com',1000,9000,'entry-secret',1,1,1),
		(2,'chain','chain.example.com','chain.example.com',1000,9000,'chain-secret',1,1,1),
		(3,'exit','exit.example.com','exit.example.com',1000,9000,'exit-secret',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES(1,'mesh',2,1,1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnel_nodes(id,tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol,flow_quota_bytes,speed_limit_mbps,health_status,bandwidth_overloaded) VALUES
		(1,1,1,1,7000,'fifo',0,'tcp',0,0,1,0),
		(2,1,2,2,7000,'fifo',1,'udp',0,0,1,0),
		(3,1,3,3,7000,'fifo',0,'tcp',0,0,1,0)`); err != nil {
		t.Fatal(err)
	}
}
