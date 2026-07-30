package socket

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/go-gost/x/config"
)

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
