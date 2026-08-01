package runtimecontrols

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/nodehub"
)

type groupPortProber struct {
	mu          sync.Mutex
	unavailable map[int64]string
	calls       map[int64]int
}

func (p *groupPortProber) ProbePorts(_ context.Context, nodeID int64, request nodehub.PortProbeRequest) (nodehub.PortProbeResponse, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls == nil {
		p.calls = make(map[int64]int)
	}
	p.calls[nodeID]++
	result := nodehub.PortProbeResult{Port: request.Ports[0], Available: true}
	if reason := p.unavailable[nodeID]; reason != "" {
		result.Available = false
		result.Error = reason
	}
	return nodehub.PortProbeResponse{Results: []nodehub.PortProbeResult{result}}, nil
}

func TestNodeGroupBindingSynchronizesTunnelTopology(t *testing.T) {
	db := openTestDatabase(t)
	execFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES
		(1,'node-a','10.0.0.1','203.0.113.1',10000,20000,'a',1,1,1),
		(2,'node-b','10.0.0.2','203.0.113.2',10000,20000,'b',1,1,1),
		(3,'node-c','10.0.0.3','203.0.113.3',10000,20000,'c',1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES(1,'direct',1,1,1,1,1,1)`)

	repository := NewRepository(db, nil)
	groupID, err := repository.CreateNodeGroup(context.Background(), NodeGroupRequest{
		Name:        "entry-pool",
		Strategy:    "round",
		MaxFails:    2,
		FailTimeout: 45000,
		Status:      1,
		Members: []NodeGroupMember{
			{NodeID: 1, Priority: 100, SortIndex: 0},
			{NodeID: 2, Priority: 50, Backup: 1, SortIndex: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	bindingID, err := repository.CreateTunnelNodeGroupBinding(context.Background(), TunnelNodeGroupBindingRequest{
		TunnelID:  1,
		GroupID:   groupID,
		ChainType: 1,
		Port:      7000,
		Strategy:  "round",
		Protocol:  "tcp",
	})
	if err != nil {
		t.Fatal(err)
	}

	assertExpandedNode(t, db, bindingID, 1, 100, 0, 2, 45000)
	assertExpandedNode(t, db, bindingID, 2, 50, 1, 2, 45000)
	execFixture(t, db, `UPDATE tunnel_nodes SET ingress_bytes=1234,health_status=0 WHERE tunnel_id=1 AND node_id=1`)

	err = repository.UpdateNodeGroup(context.Background(), UpdateNodeGroupRequest{
		ID: groupID,
		NodeGroupRequest: NodeGroupRequest{
			Name:        "entry-pool",
			Strategy:    "fifo",
			MaxFails:    3,
			FailTimeout: 60000,
			Status:      1,
			Members: []NodeGroupMember{
				{NodeID: 1, Priority: 80, SortIndex: 0},
				{NodeID: 3, Priority: 40, Backup: 1, SortIndex: 1},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	assertExpandedNode(t, db, bindingID, 1, 80, 0, 3, 60000)
	assertExpandedNode(t, db, bindingID, 3, 40, 1, 3, 60000)
	var ingress int64
	var health int
	if err := db.QueryRow(`SELECT ingress_bytes,health_status FROM tunnel_nodes WHERE tunnel_id=1 AND node_id=1`).Scan(&ingress, &health); err != nil {
		t.Fatal(err)
	}
	if ingress != 1234 || health != 0 {
		t.Fatalf("runtime counters were reset: ingress=%d health=%d", ingress, health)
	}
	var removed int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tunnel_nodes WHERE tunnel_id=1 AND node_id=2`).Scan(&removed); err != nil {
		t.Fatal(err)
	}
	if removed != 0 {
		t.Fatal("removed member remained in tunnel topology")
	}
	var refreshes int
	if err := db.QueryRow(`SELECT COUNT(*) FROM node_config_refreshes WHERE node_id IN (1,2,3)`).Scan(&refreshes); err != nil {
		t.Fatal(err)
	}
	if refreshes != 3 {
		t.Fatalf("member changes did not enqueue all affected nodes: %d", refreshes)
	}

	execFixture(t, db, `INSERT INTO users(id,username,password_hash,role,expires_at,status,created_at,updated_at) VALUES(1,'admin','hash','admin',0,1,1,1)`)
	execFixture(t, db, `INSERT INTO forwards(id,user_id,name,tunnel_id,remote_addr,strategy,status,sort_index,created_at,updated_at) VALUES(1,1,'forward',1,'192.0.2.10:443','fifo',1,0,1,1)`)
	execFixture(t, db, `INSERT INTO forward_ports(forward_id,node_id,port) VALUES(1,1,10000)`)
	prober := &groupPortProber{unavailable: make(map[int64]string)}
	repository = NewRepository(db, prober)
	err = repository.UpdateNodeGroup(context.Background(), UpdateNodeGroupRequest{
		ID: groupID,
		NodeGroupRequest: NodeGroupRequest{
			Name: "entry-pool", Strategy: "fifo", MaxFails: 3, FailTimeout: 60000, Status: 1,
			Members: []NodeGroupMember{
				{NodeID: 1, Priority: 80, SortIndex: 0},
				{NodeID: 3, Priority: 40, Backup: 1, SortIndex: 1},
			},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	var ports int
	if err := db.QueryRow(`SELECT COUNT(*) FROM forward_ports WHERE forward_id=1`).Scan(&ports); err != nil {
		t.Fatal(err)
	}
	if ports != 2 {
		t.Fatalf("entry group did not synchronize forward ports: %d", ports)
	}
	if prober.calls[3] != 1 || prober.calls[1] != 0 {
		t.Fatalf("unexpected entry group port probes: %#v", prober.calls)
	}

	prober.unavailable[2] = "UDP: address already in use"
	err = repository.UpdateNodeGroup(context.Background(), UpdateNodeGroupRequest{
		ID: groupID,
		NodeGroupRequest: NodeGroupRequest{
			Name: "entry-pool", Strategy: "fifo", MaxFails: 3, FailTimeout: 60000, Status: 1,
			Members: []NodeGroupMember{
				{NodeID: 1, Priority: 80, SortIndex: 0},
				{NodeID: 2, Priority: 30, Backup: 1, SortIndex: 1},
			},
		},
	})
	if err == nil || !strings.Contains(err.Error(), "node 2") {
		t.Fatalf("occupied port was accepted for entry group: %v", err)
	}
	var node2Topology, node2Ports int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tunnel_nodes WHERE tunnel_id=1 AND node_id=2`).Scan(&node2Topology); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM forward_ports WHERE forward_id=1 AND node_id=2`).Scan(&node2Ports); err != nil {
		t.Fatal(err)
	}
	if node2Topology != 0 || node2Ports != 0 {
		t.Fatalf("failed entry group update was not rolled back: topology=%d ports=%d", node2Topology, node2Ports)
	}

	groups, err := repository.ListNodeGroups(context.Background())
	if err != nil || len(groups) != 1 || len(groups[0].Members) != 2 {
		t.Fatalf("unexpected node groups: %#v, %v", groups, err)
	}
	bindings, err := repository.ListTunnelNodeGroupBindings(context.Background())
	if err != nil || len(bindings) != 1 || bindings[0].ID != bindingID {
		t.Fatalf("unexpected bindings: %#v, %v", bindings, err)
	}
	if err := repository.DeleteTunnelNodeGroupBinding(context.Background(), bindingID); err != nil {
		t.Fatal(err)
	}
	var expanded int
	if err := db.QueryRow(`SELECT COUNT(*) FROM tunnel_nodes WHERE group_binding_id=?`, bindingID).Scan(&expanded); err != nil {
		t.Fatal(err)
	}
	if expanded != 0 {
		t.Fatal("binding deletion did not remove expanded nodes")
	}
	if err := repository.DeleteNodeGroup(context.Background(), groupID); err != nil {
		t.Fatal(err)
	}
}

func TestNodeGroupBindingRejectsManualTopologyConflict(t *testing.T) {
	db := openTestDatabase(t)
	execFixture(t, db, `INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'node','10.0.0.1','203.0.113.1',10000,20000,'secret',1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES(1,'direct',1,1,1,1,1,1)`)
	execFixture(t, db, `INSERT INTO tunnel_nodes(tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol) VALUES(1,1,1,7000,'fifo',0,'tcp')`)
	repository := NewRepository(db, nil)
	groupID, err := repository.CreateNodeGroup(context.Background(), NodeGroupRequest{
		Name: "pool", Strategy: "fifo", MaxFails: 1, FailTimeout: 1000, Status: 1,
		Members: []NodeGroupMember{{NodeID: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = repository.CreateTunnelNodeGroupBinding(context.Background(), TunnelNodeGroupBindingRequest{
		TunnelID: 1, GroupID: groupID, ChainType: 1, Port: 7001, Strategy: "fifo", Protocol: "tcp",
	})
	if err == nil {
		t.Fatal("manual topology conflict was accepted")
	}
	var bindings int
	if queryErr := db.QueryRow(`SELECT COUNT(*) FROM tunnel_node_group_bindings`).Scan(&bindings); queryErr != nil {
		t.Fatal(queryErr)
	}
	if bindings != 0 {
		t.Fatal("failed binding was not rolled back")
	}
}

func assertExpandedNode(t *testing.T, db *sql.DB, bindingID, nodeID int64, priority, backup, maxFails int, failTimeout int64) {
	t.Helper()
	var actualBinding int64
	var actualPriority, actualBackup, actualMaxFails int
	var actualFailTimeout int64
	if err := db.QueryRow(`SELECT group_binding_id,group_priority,group_backup,group_max_fails,group_fail_timeout_ms FROM tunnel_nodes WHERE tunnel_id=1 AND node_id=?`, nodeID).Scan(&actualBinding, &actualPriority, &actualBackup, &actualMaxFails, &actualFailTimeout); err != nil {
		t.Fatal(err)
	}
	if actualBinding != bindingID || actualPriority != priority || actualBackup != backup || actualMaxFails != maxFails || actualFailTimeout != failTimeout {
		t.Fatalf("unexpected expanded node settings: binding=%d priority=%d backup=%d maxFails=%d failTimeout=%d", actualBinding, actualPriority, actualBackup, actualMaxFails, actualFailTimeout)
	}
}

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func execFixture(t *testing.T, db *sql.DB, statement string) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), statement); err != nil {
		t.Fatal(err)
	}
}
