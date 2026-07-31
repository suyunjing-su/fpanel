package socket

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/go-gost/x/config"
	"github.com/go-gost/x/controller"
	"github.com/gorilla/websocket"
)

func TestWebSocketConnectFallsBackAndPromotesController(t *testing.T) {
	primary := httptest.NewServer(nil)
	primary.Close()
	upgrader := websocket.Upgrader{}
	connected := make(chan struct{}, 1)
	backup := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		connected <- struct{}{}
		defer connection.Close()
		<-r.Context().Done()
	}))
	defer backup.Close()
	pool, err := controller.New([]string{primary.URL, backup.URL})
	if err != nil {
		t.Fatal(err)
	}
	reporter := NewWebSocketReporter("", "secret")
	reporter.controllers = pool
	reporter.addr = primary.URL
	reporter.version = "test"
	defer reporter.Stop()
	if err := reporter.connect(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-connected:
	case <-time.After(time.Second):
		t.Fatal("backup WebSocket was not connected")
	}
	if got := pool.Candidates(); !reflect.DeepEqual(got, []string{backup.URL, primary.URL}) {
		t.Fatalf("controllers were not promoted: %#v", got)
	}
	statuses := pool.Status()
	if statuses[0].ConsecutiveFailures != 1 || !statuses[1].Active {
		t.Fatalf("unexpected controller statuses: %#v", statuses)
	}
}

func TestSystemInfoIncludesControllerDiagnostics(t *testing.T) {
	pool, err := controller.New([]string{"https://primary.example.com", "https://backup.example.com"})
	if err != nil {
		t.Fatal(err)
	}
	pool.Fail("https://primary.example.com", errors.New("unavailable"))
	pool.Succeed("https://backup.example.com")
	reporter := NewWebSocketReporter("", "")
	defer reporter.Stop()
	reporter.controllers = pool
	info := reporter.collectSystemInfo()
	if !reflect.DeepEqual(info.ControllerStatuses, pool.Status()) {
		t.Fatalf("controller diagnostics missing from system info: %#v", info.ControllerStatuses)
	}
}

func TestUpdateLocalConfigPreservesControllers(t *testing.T) {
	oldDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDirectory)
	content := `{"addr":"https://primary.example.com","controllers":["https://primary.example.com","https://backup.example.com"],"secret":"secret"}`
	if err := os.WriteFile(filepath.Join("config.json"), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	httpValue, tlsValue, socksValue := 1, 0, 1
	if _, _, _, err := updateLocalConfigJSON(&httpValue, &tlsValue, &socksValue); err != nil {
		t.Fatal(err)
	}
	updated, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Controllers []string `json:"controllers"`
		HTTP        int      `json:"http"`
		SOCKS       int      `json:"socks"`
	}
	if err := json.Unmarshal(updated, &config); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(config.Controllers, []string{"https://primary.example.com", "https://backup.example.com"}) || config.HTTP != 1 || config.SOCKS != 1 {
		t.Fatalf("local configuration was corrupted: %s", updated)
	}
}

func TestUpdateLocalConfigPreservesOmittedProtocols(t *testing.T) {
	oldDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDirectory)
	content := `{"addr":"https://primary.example.com","controllers":["https://primary.example.com","https://backup.example.com"],"secret":"secret","http":1,"tls":1,"socks":1}`
	if err := os.WriteFile("config.json", []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	httpValue := 0
	httpFlag, tlsFlag, socksFlag, err := updateLocalConfigJSON(&httpValue, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if httpFlag != 0 || tlsFlag != 1 || socksFlag != 1 {
		t.Fatalf("protocol values were not preserved: %d %d %d", httpFlag, tlsFlag, socksFlag)
	}
	updated, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), `"secret": "secret"`) || !strings.Contains(string(updated), `"tls": 1`) {
		t.Fatalf("identity or protocol settings were lost: %s", updated)
	}
}

func TestUpdateLocalConfigRejectsCorruptIdentityFile(t *testing.T) {
	oldDirectory, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(oldDirectory)
	original := []byte(`{"secret":`)
	if err := os.WriteFile("config.json", original, 0600); err != nil {
		t.Fatal(err)
	}
	httpValue := 1
	if _, _, _, err := updateLocalConfigJSON(&httpValue, nil, nil); err == nil {
		t.Fatal("corrupt identity file was overwritten")
	}
	current, err := os.ReadFile("config.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(current, original) {
		t.Fatalf("corrupt file changed: %s", current)
	}
}

func TestPreprocessDurationFields(t *testing.T) {
	reporter := &WebSocketReporter{}
	payload := []byte(`[
		{
			"name": "forward-service",
			"forwarder": {
				"nodes": [{"name": "node_1", "addr": "192.0.2.1:443"}],
				"probePeriod": "10s",
				"probeTimeout": "3s",
				"selector": {
					"strategy": "fifo",
					"maxFails": 1,
					"failTimeout": "600s"
				}
			}
		},
		{
			"name": "numeric-service",
			"forwarder": {
				"nodes": [{"name": "node_1", "addr": "192.0.2.2:443"}],
				"probePeriod": 5000000000,
				"probeTimeout": 2000000000
			}
		}
	]`)

	processed, err := reporter.preprocessDurationFields(payload)
	if err != nil {
		t.Fatalf("preprocess duration fields: %v", err)
	}

	var services []config.ServiceConfig
	if err := json.Unmarshal(processed, &services); err != nil {
		t.Fatalf("unmarshal processed services: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("service count = %d, want 2", len(services))
	}

	forwarder := services[0].Forwarder
	if forwarder == nil {
		t.Fatal("forwarder is nil")
	}
	if forwarder.ProbePeriod != 10*time.Second {
		t.Errorf("probe period = %v, want %v", forwarder.ProbePeriod, 10*time.Second)
	}
	if forwarder.ProbeTimeout != 3*time.Second {
		t.Errorf("probe timeout = %v, want %v", forwarder.ProbeTimeout, 3*time.Second)
	}
	if forwarder.Selector == nil {
		t.Fatal("selector is nil")
	}
	if forwarder.Selector.FailTimeout != 10*time.Minute {
		t.Errorf("fail timeout = %v, want %v", forwarder.Selector.FailTimeout, 10*time.Minute)
	}

	numericForwarder := services[1].Forwarder
	if numericForwarder == nil {
		t.Fatal("numeric forwarder is nil")
	}
	if numericForwarder.ProbePeriod != 5*time.Second {
		t.Errorf("numeric probe period = %v, want %v", numericForwarder.ProbePeriod, 5*time.Second)
	}
	if numericForwarder.ProbeTimeout != 2*time.Second {
		t.Errorf("numeric probe timeout = %v, want %v", numericForwarder.ProbeTimeout, 2*time.Second)
	}
}

func TestTcpPingHostUsesOverallTimeoutAndNextEndpointGetsFreshContext(t *testing.T) {
	lookup := func(context.Context, string) ([]string, error) {
		return nil, errors.New("lookup should not be called for IP addresses")
	}

	var failedAttempts int
	blockingDial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		failedAttempts++
		<-ctx.Done()
		return nil, ctx.Err()
	}

	start := time.Now()
	_, loss, err := tcpPingHostWithDialer(
		"192.0.2.1",
		443,
		4,
		100*time.Millisecond,
		lookup,
		blockingDial,
	)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("failed endpoint error is nil")
	}
	if loss != 100 {
		t.Errorf("failed endpoint loss = %v, want 100", loss)
	}
	if failedAttempts != 1 {
		t.Errorf("failed endpoint attempts = %d, want 1", failedAttempts)
	}
	if elapsed >= 250*time.Millisecond {
		t.Errorf("failed endpoint took %v, want one overall timeout budget", elapsed)
	}

	var healthyContextErr error
	healthyDial := func(ctx context.Context, _, _ string) (net.Conn, error) {
		healthyContextErr = ctx.Err()
		client, server := net.Pipe()
		go server.Close()
		return client, nil
	}

	avg, loss, err := tcpPingHostWithDialer(
		"192.0.2.2",
		443,
		1,
		100*time.Millisecond,
		lookup,
		healthyDial,
	)
	if err != nil {
		t.Fatalf("healthy endpoint failed after timed-out endpoint: %v", err)
	}
	if healthyContextErr != nil {
		t.Fatalf("healthy endpoint received expired context: %v", healthyContextErr)
	}
	if loss != 0 {
		t.Errorf("healthy endpoint loss = %v, want 0", loss)
	}
	if avg < 0 {
		t.Errorf("healthy endpoint average = %v, want non-negative", avg)
	}
}
