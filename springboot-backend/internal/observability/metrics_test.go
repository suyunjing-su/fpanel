package observability

import (
	"context"
	"io"
	"log/slog"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestMetricsExposeControlPlaneState(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "metrics.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`INSERT INTO nodes(id,name,ip,server_ip,port_start,port_end,secret,status,created_at,updated_at) VALUES(1,'node','127.0.0.1','127.0.0.1',1000,2000,'secret',1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnels(id,name,type,flow,status,created_at,updated_at) VALUES(1,'tunnel',1,1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO node_config_refreshes(node_id,requested_at) VALUES(1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnel_failure_events(tunnel_id,node_id,event_type,from_status,to_status,started_at,created_at) VALUES(1,1,'health',1,0,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE nodes SET tot_sessions=2,tot_active_paths=3,tot_pending_frames=4,tot_sent_frames=5,tot_received_frames=6,tot_retransmits=7,tot_duplicate_frames=8,tot_path_failures=9 WHERE id=1`); err != nil {
		t.Fatal(err)
	}

	response := httptest.NewRecorder()
	NewMetrics(db).ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	body := response.Body.String()
	for _, metric := range []string{
		"flux_db_open_connections",
		"flux_nodes_online 1",
		"flux_tunnels_enabled 1",
		"flux_config_refresh_pending 1",
		"flux_failure_events_active 1",
		"flux_node_tot_sessions{node_id=\"1\",node=\"node\"} 2",
		"flux_node_tot_sent_frames_total{node_id=\"1\",node=\"node\"} 5",
		"flux_node_tot_path_failures_total{node_id=\"1\",node=\"node\"} 9",
	} {
		if !strings.Contains(body, metric) {
			t.Fatalf("metric %q missing from output:\n%s", metric, body)
		}
	}
}
