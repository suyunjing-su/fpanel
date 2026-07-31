package tunnels

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
)

func TestTOTTunnelPersistenceAndSecretRotation(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	if _, err := db.Exec(`INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'entry','10.0.0.1','203.0.113.1',10000,20000,'node-secret',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	repository := NewRepository(db, nodes.NewRepository(db))
	request := CreateRequest{
		Name:         "tot-tunnel",
		Type:         1,
		InNodeID:     []NodeSpec{{NodeID: 1, Port: 7000}},
		Flow:         1,
		TrafficRatio: 1,
		Status:       1,
		TOT: TOTConfig{
			Enabled:              true,
			PathCount:            3,
			MaxPayload:           4096,
			Window:               64,
			RetransmitIntervalMS: 50,
			MaxRetries:           8,
			RecoveryPeriodMS:     500,
			HandshakeTimeoutMS:   2000,
			MaxClockSkewMS:       5000,
			IdleTTLMS:            60000,
			MPTCP:                true,
		},
	}
	id, err := repository.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	var secret string
	if err := db.QueryRow("SELECT tot_secret FROM tunnels WHERE id=?", id).Scan(&secret); err != nil {
		t.Fatal(err)
	}
	if len(secret) != 64 {
		t.Fatalf("unexpected TOT secret length: %d", len(secret))
	}
	items, err := repository.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || !items[0].TOT.Enabled || !items[0].TOT.SecretConfigured {
		t.Fatalf("unexpected public TOT configuration: %#v", items)
	}

	if _, err := db.Exec("DELETE FROM node_config_refreshes"); err != nil {
		t.Fatal(err)
	}
	totBeforeUpdate := 0
	if err := db.QueryRow("SELECT COALESCE(generation,0) FROM node_config_refreshes WHERE node_id=1").Scan(&totBeforeUpdate); err != nil && err.Error() != "sql: no rows in result set" {
		t.Fatal(err)
	}
	request.TOT.Window = 128
	if err := repository.Update(context.Background(), UpdateRequest{ID: id, CreateRequest: request}); err != nil {
		t.Fatal(err)
	}
	var generation int
	if err := db.QueryRow("SELECT generation FROM node_config_refreshes WHERE node_id=1").Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation <= totBeforeUpdate {
		t.Fatalf("TOT update did not enqueue node refresh: before=%d after=%d", totBeforeUpdate, generation)
	}

	beforeRotation := generation
	if err := repository.RotateTOTSecret(context.Background(), id); err != nil {
		t.Fatal(err)
	}
	var rotated string
	if err := db.QueryRow("SELECT tot_secret FROM tunnels WHERE id=?", id).Scan(&rotated); err != nil {
		t.Fatal(err)
	}
	if rotated == secret || len(rotated) != 64 {
		t.Fatal("TOT secret was not rotated")
	}
	if err := db.QueryRow("SELECT generation FROM node_config_refreshes WHERE node_id=1").Scan(&generation); err != nil {
		t.Fatal(err)
	}
	if generation <= beforeRotation {
		t.Fatalf("TOT secret rotation did not enqueue refresh: before=%d after=%d", beforeRotation, generation)
	}
}

func TestTOTValidationRejectsUnsafeValues(t *testing.T) {
	request := CreateRequest{
		Name:     "tot",
		Type:     1,
		InNodeID: []NodeSpec{{NodeID: 1}},
		Flow:     1,
		TOT:      TOTConfig{Enabled: true, MaxPayload: 512},
	}
	if err := validate(request); err == nil {
		t.Fatal("unsafe TOT payload was accepted")
	}
}
