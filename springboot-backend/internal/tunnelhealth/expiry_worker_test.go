package tunnelhealth

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestExpiryWorkerEnqueuesEachExpiryVersionOnce(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "expiry.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	execExpiryFixture(t, db, `INSERT INTO users(id,username,password_hash,role,expires_at,status,created_at,updated_at) VALUES(1,'user','hash','user',1000,1,1,1)`)
	execExpiryFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'entry','10.0.0.1','203.0.113.1',1000,2000,'secret',1,1,1)`)
	execExpiryFixture(t, db, `INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES(1,'direct',1,1,1,1,1,1)`)
	execExpiryFixture(t, db, `INSERT INTO tunnel_nodes(id,tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol) VALUES(1,1,1,1,7000,'fifo',0,'tcp')`)
	execExpiryFixture(t, db, `INSERT INTO user_tunnels(id,user_id,tunnel_id,expires_at,status,created_at,updated_at) VALUES(1,1,1,2000,1,1,1)`)
	execExpiryFixture(t, db, `INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,status,sort_index,created_at,updated_at) VALUES(1,1,'forward',1,'192.0.2.1:80',1,0,1,1)`)
	execExpiryFixture(t, db, `DELETE FROM node_config_refreshes`)

	worker := NewExpiryWorker(db, nil, logger)
	changed, err := worker.processAt(ctx, 1500)
	if err != nil || !changed {
		t.Fatalf("user expiration was not processed: changed=%v err=%v", changed, err)
	}
	assertExpiryGeneration(t, db, 1)
	changed, err = worker.processAt(ctx, 1500)
	if err != nil || changed {
		t.Fatalf("same expiration was processed twice: changed=%v err=%v", changed, err)
	}
	assertExpiryGeneration(t, db, 1)

	changed, err = worker.processAt(ctx, 2500)
	if err != nil || !changed {
		t.Fatalf("user tunnel expiration was not processed: changed=%v err=%v", changed, err)
	}
	assertExpiryGeneration(t, db, 2)

	execExpiryFixture(t, db, `UPDATE users SET expires_at=3000 WHERE id=1`)
	changed, err = worker.processAt(ctx, 3500)
	if err != nil || !changed {
		t.Fatalf("updated expiration version was not processed: changed=%v err=%v", changed, err)
	}
	var processedExpiry int64
	if err := db.QueryRow(`SELECT expires_at FROM runtime_expiry_states WHERE subject_type='user' AND subject_id=1`).Scan(&processedExpiry); err != nil {
		t.Fatal(err)
	}
	if processedExpiry != 3000 {
		t.Fatalf("unexpected processed expiry %d", processedExpiry)
	}
}

func TestForwardQuotaTriggersRefreshAcrossTunnels(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "forward-trigger.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	execExpiryFixture(t, db, `INSERT INTO users(id,username,password_hash,role,expires_at,forward_quota,status,created_at,updated_at) VALUES(1,'user','hash','user',0,1,1,1,1)`)
	execExpiryFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES
		(1,'entry-a','10.0.0.1','203.0.113.1',1000,2000,'a',1,1,1),
		(2,'entry-b','10.0.0.2','203.0.113.2',1000,2000,'b',1,1,1)`)
	execExpiryFixture(t, db, `INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES
		(1,'a',1,1,1,1,1,1),(2,'b',1,1,1,1,1,1)`)
	execExpiryFixture(t, db, `INSERT INTO tunnel_nodes(id,tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol) VALUES
		(1,1,1,1,7000,'fifo',0,'tcp'),(2,2,1,2,7000,'fifo',0,'tcp')`)
	execExpiryFixture(t, db, `INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,status,sort_index,created_at,updated_at) VALUES
		(1,1,'first',1,'192.0.2.1:80',1,0,1,1),(2,1,'second',2,'192.0.2.2:80',1,1,1,1)`)
	execExpiryFixture(t, db, `DELETE FROM node_config_refreshes`)

	execExpiryFixture(t, db, `UPDATE forwards SET sort_index=2 WHERE id=1`)
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM node_config_refreshes WHERE node_id IN (1,2)`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("global quota change refreshed %d entry nodes, want 2", count)
	}
}

func assertExpiryGeneration(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, expected int64) {
	t.Helper()
	var generation int64
	if err := db.QueryRow(`SELECT generation FROM node_config_refreshes WHERE node_id=1`).Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation != expected {
		t.Fatalf("unexpected refresh generation %d, want %d", generation, expected)
	}
}

func execExpiryFixture(t *testing.T, db interface {
	Exec(string, ...any) (sql.Result, error)
}, statement string) {
	t.Helper()
	if _, err := db.Exec(statement); err != nil {
		t.Fatal(err)
	}
}
