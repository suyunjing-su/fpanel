package forwards

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/nodehub"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
)

type recordingPortProber struct {
	mu          sync.Mutex
	unavailable map[Port]string
	requests    map[int64][][]int
	delay       time.Duration
}

func (p *recordingPortProber) ProbePorts(_ context.Context, nodeID int64, request nodehub.PortProbeRequest) (nodehub.PortProbeResponse, error) {
	if p.delay > 0 {
		time.Sleep(p.delay)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.requests == nil {
		p.requests = make(map[int64][][]int)
	}
	p.requests[nodeID] = append(p.requests[nodeID], append([]int(nil), request.Ports...))
	response := nodehub.PortProbeResponse{Results: make([]nodehub.PortProbeResult, 0, len(request.Ports))}
	for _, port := range request.Ports {
		result := nodehub.PortProbeResult{Port: port, Available: true}
		if reason := p.unavailable[Port{NodeID: nodeID, Port: port}]; reason != "" {
			result.Available = false
			result.Error = reason
		}
		response.Results = append(response.Results, result)
	}
	return response, nil
}

func TestAllocatePortsSkipsNodeRuntimeConflicts(t *testing.T) {
	db, repository := newPortTestRepository(t, &recordingPortProber{unavailable: map[Port]string{
		{NodeID: 1, Port: 10000}: "TCP occupied",
		{NodeID: 2, Port: 10001}: "UDP occupied",
	}})
	transaction, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	ports, err := repository.allocatePorts(context.Background(), transaction, entrySpecs(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 || ports[0].Port != 10002 || ports[1].Port != 10002 {
		t.Fatalf("unexpected common port allocation: %#v", ports)
	}
}

func TestAllocatePortsProbesEntryNodesConcurrently(t *testing.T) {
	prober := &recordingPortProber{unavailable: map[Port]string{}, delay: 100 * time.Millisecond}
	db, repository := newPortTestRepository(t, prober)
	transaction, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	port := 10000
	started := time.Now()
	if _, err := repository.allocatePorts(context.Background(), transaction, entrySpecs(), &port, 0); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(started); elapsed >= 180*time.Millisecond {
		t.Fatalf("entry node probes ran serially: %v", elapsed)
	}
}

func TestAllocateRequestedPortReturnsRuntimeConflict(t *testing.T) {
	prober := &recordingPortProber{unavailable: map[Port]string{
		{NodeID: 2, Port: 10000}: "UDP: address already in use",
	}}
	db, repository := newPortTestRepository(t, prober)
	transaction, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	port := 10000
	_, err = repository.allocatePorts(context.Background(), transaction, entrySpecs(), &port, 0)
	if err == nil || !strings.Contains(err.Error(), "node 2") || !strings.Contains(err.Error(), "UDP") {
		t.Fatalf("unexpected requested port error: %v", err)
	}
}

func TestAllocatePortsKeepsCurrentForwardPort(t *testing.T) {
	prober := &recordingPortProber{unavailable: map[Port]string{
		{NodeID: 1, Port: 10000}: "current TCP service",
		{NodeID: 2, Port: 10000}: "current TCP service",
	}}
	db, repository := newPortTestRepository(t, prober)
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,expires_at,status,created_at,updated_at) VALUES(1,'owner','hash','admin',0,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnels(id,name,type,flow,status,created_at,updated_at) VALUES(1,'direct',1,1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,status,created_at,updated_at) VALUES(1,1,'existing',1,'127.0.0.1:80',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,10000),(1,2,10000)`); err != nil {
		t.Fatal(err)
	}
	transaction, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer transaction.Rollback()
	port := 10000
	ports, err := repository.allocatePorts(context.Background(), transaction, entrySpecs(), &port, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(ports) != 2 || len(prober.requests) != 0 {
		t.Fatalf("current reservations were probed or lost: ports=%#v requests=%#v", ports, prober.requests)
	}
}

func newPortTestRepository(t *testing.T, prober PortProber) (*sql.DB, *Repository) {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "ports.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,tcp_listen_addr,udp_listen_addr,created_at,updated_at) VALUES
		(1,'entry-a','127.0.0.1','127.0.0.1',10000,10002,'secret-a',1,'127.0.0.1','127.0.0.1',1,1),
		(2,'entry-b','127.0.0.1','127.0.0.1',10000,10002,'secret-b',1,'127.0.0.1','127.0.0.1',1,1)`); err != nil {
		t.Fatal(err)
	}
	nodeRepo := nodes.NewRepository(db)
	return db, NewRepository(db, nodeRepo, tunnels.NewRepository(db, nodeRepo), prober)
}

func entrySpecs() []tunnels.NodeSpec {
	return []tunnels.NodeSpec{{NodeID: 1}, {NodeID: 2}}
}
