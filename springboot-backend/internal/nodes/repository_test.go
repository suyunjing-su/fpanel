package nodes

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestLegacyNodeRequestRoundTrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "nodes.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository := NewRepository(db)
	id, err := repository.Create(context.Background(), CreateRequest{
		Name:             "edge",
		ServerIP:         "edge.example.com",
		Port:             "1000-2000",
		MaxBandwidthMbps: 1000,
		InterfaceName:    "eth1",
		TCPListenAddr:    "0.0.0.0",
		UDPListenAddr:    "[::]",
	})
	if err != nil {
		t.Fatal(err)
	}

	node, err := repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.IP != "edge.example.com" || node.PortStart != 1000 || node.PortEnd != 2000 || node.Port != "1000-2000" {
		t.Fatalf("legacy request was not normalized: %#v", node)
	}
	if node.InterfaceName != "eth1" || node.TCPListenAddr != "0.0.0.0" || node.UDPListenAddr != "[::]" {
		t.Fatalf("runtime fields did not round trip: %#v", node)
	}

	err = repository.Update(context.Background(), UpdateRequest{
		ID: id,
		CreateRequest: CreateRequest{
			Name:      "edge",
			ServerIP:  "2001:db8::1",
			PortStart: 3000,
			PortEnd:   4000,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err = repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.IP != "2001:db8::1" || node.Port != "3000-4000" || node.TCPListenAddr != "[::]" || node.UDPListenAddr != "[::]" {
		t.Fatalf("native request was not normalized: %#v", node)
	}
}

func TestControllerDiagnosticsRoundTrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "nodes.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository := NewRepository(db)
	id, err := repository.Create(context.Background(), CreateRequest{
		Name:      "edge",
		ServerIP:  "edge.example.com",
		PortStart: 1000,
		PortEnd:   2000,
	})
	if err != nil {
		t.Fatal(err)
	}
	statuses := `[{"address":"https://primary.example.com","active":true,"consecutiveFailures":2,"lastFailureAt":123,"lastError":"unavailable"}]`
	if err := repository.SetTelemetry(context.Background(), id, 10, 20, 30, 4.5, 6.7, statuses); err != nil {
		t.Fatal(err)
	}
	node, err := repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.Uptime != 10 || len(node.Controllers) != 1 || node.Controllers[0].ConsecutiveFailures != 2 || node.Controllers[0].LastError != "unavailable" {
		t.Fatalf("controller diagnostics did not round trip: %#v", node)
	}
	if err := repository.SetTelemetry(context.Background(), id, 0, 0, 0, 0, 0, "invalid"); err == nil {
		t.Fatal("invalid controller diagnostics were accepted")
	}
}

func TestRejectsInvalidLegacyPortRange(t *testing.T) {
	_, err := normalizeRequest(CreateRequest{ServerIP: "edge.example.com", Port: "2000-1000"})
	if err != nil {
		t.Fatal("normalization should leave range ordering to validation")
	}
	request, _ := normalizeRequest(CreateRequest{Name: "edge", ServerIP: "edge.example.com", Port: "2000-1000"})
	if err := validate(request); err == nil {
		t.Fatal("invalid port ordering was accepted")
	}
	if _, err := normalizeRequest(CreateRequest{ServerIP: "edge.example.com", Port: "invalid"}); err == nil {
		t.Fatal("invalid legacy port was accepted")
	}
}
