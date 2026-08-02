package nodes

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestNullableBandwidthRoundTrip(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "nodes.db"), logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	repository := NewRepository(db)
	var request CreateRequest
	decoder := json.NewDecoder(bytes.NewBufferString(`{"id":null,"name":"edge","serverIp":"edge.example.com","port":"1000-2000","maxBandwidthMbps":null}`))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&request); err != nil {
		t.Fatalf("decode create request: %v", err)
	}
	id, err := repository.Create(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}

	var storedBandwidth int
	if err := db.QueryRow("SELECT max_bandwidth_mbps FROM nodes WHERE id=?", id).Scan(&storedBandwidth); err != nil {
		t.Fatal(err)
	}
	if storedBandwidth != 0 {
		t.Fatalf("stored unlimited bandwidth = %d, want 0", storedBandwidth)
	}
	node, err := repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.MaxBandwidthMbps != nil {
		t.Fatalf("public unlimited bandwidth = %v, want nil", *node.MaxBandwidthMbps)
	}

	bandwidth := 500
	if err := repository.Update(context.Background(), UpdateRequest{ID: id, CreateRequest: CreateRequest{
		Name:             "edge",
		ServerIP:         "edge.example.com",
		PortStart:        1000,
		PortEnd:          2000,
		MaxBandwidthMbps: &bandwidth,
	}}); err != nil {
		t.Fatal(err)
	}
	node, err = repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.MaxBandwidthMbps == nil || *node.MaxBandwidthMbps != 500 {
		t.Fatalf("public limited bandwidth = %v, want 500", node.MaxBandwidthMbps)
	}

	zero := 0
	negative := -1
	tooLarge := 1000001
	for _, value := range []*int{&zero, &negative, &tooLarge} {
		invalid := request
		invalid.MaxBandwidthMbps = value
		if _, err := repository.Create(context.Background(), invalid); err == nil {
			t.Fatalf("invalid bandwidth %d was accepted", *value)
		}
	}

	nonNilID := int64(9)
	request.ID = &nonNilID
	if _, err := repository.Create(context.Background(), request); err == nil {
		t.Fatal("non-null create id was accepted")
	}
}

func intPointer(value int) *int { return &value }

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
		MaxBandwidthMbps: intPointer(1000),
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
	if err := repository.SetTelemetry(context.Background(), id, 10, 20, 30, 4.5, 6.7, 8.9, statuses, TOTTelemetry{
		Sessions:        2,
		ActivePaths:     4,
		PendingFrames:   3,
		SentFrames:      100,
		ReceivedFrames:  90,
		Retransmits:     7,
		DuplicateFrames: 2,
		PathFailures:    1,
	}); err != nil {
		t.Fatal(err)
	}
	node, err := repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.Uptime != 10 || node.DiskUsage != 8.9 || len(node.Controllers) != 1 || node.Controllers[0].ConsecutiveFailures != 2 || node.Controllers[0].LastError != "unavailable" {
		t.Fatalf("controller diagnostics did not round trip: %#v", node)
	}
	if node.TOT.Sessions != 2 || node.TOT.ActivePaths != 4 || node.TOT.Retransmits != 7 || node.TOT.PathFailures != 1 {
		t.Fatalf("TOT telemetry did not round trip: %#v", node.TOT)
	}
	if _, err := db.Exec(`UPDATE nodes SET bytes_received=100,bytes_transmitted=200,telemetry_at=? WHERE id=?`, time.Now().Add(-time.Second).UnixMilli(), id); err != nil {
		t.Fatal(err)
	}
	if err := repository.SetTelemetry(context.Background(), id, 11, 400, 800, 5, 7, 9, statuses); err != nil {
		t.Fatal(err)
	}
	node, err = repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.DownloadSpeed <= 0 || node.UploadSpeed <= 0 {
		t.Fatalf("telemetry rates were not calculated: download=%v upload=%v", node.DownloadSpeed, node.UploadSpeed)
	}
	if err := repository.SetTelemetry(context.Background(), id, 12, 1, 1, 5, 7, 9, statuses); err != nil {
		t.Fatal(err)
	}
	node, err = repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.DownloadSpeed != 0 || node.UploadSpeed != 0 {
		t.Fatalf("counter reset retained stale rates: download=%v upload=%v", node.DownloadSpeed, node.UploadSpeed)
	}
	if err := repository.SetTelemetry(context.Background(), id, 0, 0, 0, 0, 0, 0, "invalid"); err == nil {
		t.Fatal("invalid controller diagnostics were accepted")
	}
}

func TestSetStatusPreservesProtocolPolicy(t *testing.T) {
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
		HTTP:      1,
		TLS:       0,
		Socks:     1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.SetStatus(context.Background(), id, 1, "3.0.31"); err != nil {
		t.Fatal(err)
	}

	node, err := repository.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if node.Status != 1 || node.Version != "3.0.31" || node.HTTP != 1 || node.TLS != 0 || node.Socks != 1 {
		t.Fatalf("status update changed stored protocol policy: %#v", node)
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
