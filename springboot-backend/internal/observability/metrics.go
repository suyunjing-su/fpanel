package observability

import (
	"database/sql"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Metrics struct {
	startedAt time.Time
	db        *sql.DB
	active    atomic.Int64
	mu        sync.Mutex
	requests  map[string]uint64
	durations map[string]float64
}

func NewMetrics(database ...*sql.DB) *Metrics {
	metrics := &Metrics{
		startedAt: time.Now(),
		requests:  make(map[string]uint64),
		durations: make(map[string]float64),
	}
	if len(database) > 0 {
		metrics.db = database[0]
	}
	return metrics
}

func (m *Metrics) Observe(method string, status int, duration time.Duration) {
	key := method + "\x00" + strconv.Itoa(status)
	m.mu.Lock()
	m.requests[key]++
	m.durations[key] += duration.Seconds()
	m.mu.Unlock()
}

func (m *Metrics) TrackActive(delta int64) {
	m.active.Add(delta)
}

func (m *Metrics) writeDatabaseMetrics(w http.ResponseWriter) {
	if m.db == nil {
		return
	}
	stats := m.db.Stats()
	fmt.Fprint(w, "# HELP flux_db_open_connections Open database connections.\n# TYPE flux_db_open_connections gauge\n")
	fmt.Fprintf(w, "flux_db_open_connections %d\n", stats.OpenConnections)
	fmt.Fprint(w, "# HELP flux_db_in_use_connections Database connections currently in use.\n# TYPE flux_db_in_use_connections gauge\n")
	fmt.Fprintf(w, "flux_db_in_use_connections %d\n", stats.InUse)
	for _, metric := range []struct {
		name  string
		help  string
		query string
	}{
		{name: "flux_nodes_online", help: "Nodes currently marked online.", query: "SELECT COUNT(1) FROM nodes WHERE status=1"},
		{name: "flux_tunnels_enabled", help: "Tunnels currently enabled.", query: "SELECT COUNT(1) FROM tunnels WHERE status=1"},
		{name: "flux_config_refresh_pending", help: "Pending node configuration refreshes.", query: "SELECT COUNT(1) FROM node_config_refreshes"},
		{name: "flux_failure_events_active", help: "Active tunnel failure events.", query: "SELECT COUNT(1) FROM tunnel_failure_events WHERE resolved_at IS NULL"},
	} {
		var value int64
		if err := m.db.QueryRow(metric.query).Scan(&value); err != nil {
			continue
		}
		fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", metric.name, metric.help, metric.name, metric.name, value)
	}
	m.writeTOTMetrics(w)
}

func (m *Metrics) writeTOTMetrics(w http.ResponseWriter) {
	rows, err := m.db.Query(`SELECT id,name,tot_sessions,tot_active_paths,tot_pending_frames,tot_sent_frames,tot_received_frames,tot_retransmits,tot_duplicate_frames,tot_path_failures FROM nodes ORDER BY id`)
	if err != nil {
		return
	}
	defer rows.Close()
	fmt.Fprint(w, "# HELP flux_node_tot_sessions TOT sessions currently tracked by node.\n# TYPE flux_node_tot_sessions gauge\n")
	fmt.Fprint(w, "# HELP flux_node_tot_active_paths TOT active paths currently tracked by node.\n# TYPE flux_node_tot_active_paths gauge\n")
	fmt.Fprint(w, "# HELP flux_node_tot_pending_frames TOT pending frames currently tracked by node.\n# TYPE flux_node_tot_pending_frames gauge\n")
	fmt.Fprint(w, "# HELP flux_node_tot_sent_frames_total TOT frames sent by node.\n# TYPE flux_node_tot_sent_frames_total counter\n")
	fmt.Fprint(w, "# HELP flux_node_tot_received_frames_total TOT frames received by node.\n# TYPE flux_node_tot_received_frames_total counter\n")
	fmt.Fprint(w, "# HELP flux_node_tot_retransmits_total TOT retransmitted frames by node.\n# TYPE flux_node_tot_retransmits_total counter\n")
	fmt.Fprint(w, "# HELP flux_node_tot_duplicate_frames_total TOT duplicate frames received by node.\n# TYPE flux_node_tot_duplicate_frames_total counter\n")
	fmt.Fprint(w, "# HELP flux_node_tot_path_failures_total TOT path failures by node.\n# TYPE flux_node_tot_path_failures_total counter\n")
	for rows.Next() {
		var id int64
		var name string
		var sessions, activePaths, pendingFrames int64
		var sentFrames, receivedFrames, retransmits, duplicateFrames, pathFailures uint64
		if err := rows.Scan(&id, &name, &sessions, &activePaths, &pendingFrames, &sentFrames, &receivedFrames, &retransmits, &duplicateFrames, &pathFailures); err != nil {
			continue
		}
		labels := fmt.Sprintf("{node_id=%q,node=%q}", strconv.FormatInt(id, 10), name)
		fmt.Fprintf(w, "flux_node_tot_sessions%s %d\n", labels, sessions)
		fmt.Fprintf(w, "flux_node_tot_active_paths%s %d\n", labels, activePaths)
		fmt.Fprintf(w, "flux_node_tot_pending_frames%s %d\n", labels, pendingFrames)
		fmt.Fprintf(w, "flux_node_tot_sent_frames_total%s %d\n", labels, sentFrames)
		fmt.Fprintf(w, "flux_node_tot_received_frames_total%s %d\n", labels, receivedFrames)
		fmt.Fprintf(w, "flux_node_tot_retransmits_total%s %d\n", labels, retransmits)
		fmt.Fprintf(w, "flux_node_tot_duplicate_frames_total%s %d\n", labels, duplicateFrames)
		fmt.Fprintf(w, "flux_node_tot_path_failures_total%s %d\n", labels, pathFailures)
	}
}

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP flux_uptime_seconds Process uptime in seconds.\n")
	fmt.Fprintf(w, "# TYPE flux_uptime_seconds gauge\nflux_uptime_seconds %.3f\n", time.Since(m.startedAt).Seconds())
	fmt.Fprintf(w, "# HELP flux_http_active_requests Current HTTP requests.\n")
	fmt.Fprintf(w, "# TYPE flux_http_active_requests gauge\nflux_http_active_requests %d\n", m.active.Load())
	m.writeDatabaseMetrics(w)

	m.mu.Lock()
	defer m.mu.Unlock()
	fmt.Fprint(w, "# HELP flux_http_requests_total HTTP requests by method and status.\n# TYPE flux_http_requests_total counter\n")
	for key, count := range m.requests {
		method, status, _ := strings.Cut(key, "\x00")
		fmt.Fprintf(w, "flux_http_requests_total{method=%q,status=%q} %d\n", method, status, count)
	}
	fmt.Fprint(w, "# HELP flux_http_request_duration_seconds_sum Total HTTP request duration.\n# TYPE flux_http_request_duration_seconds_sum counter\n")
	for key, duration := range m.durations {
		method, status, _ := strings.Cut(key, "\x00")
		fmt.Fprintf(w, "flux_http_request_duration_seconds_sum{method=%q,status=%q} %.6f\n", method, status, duration)
	}
}
