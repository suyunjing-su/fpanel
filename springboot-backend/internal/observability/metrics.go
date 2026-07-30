package observability

import (
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
	active    atomic.Int64
	mu        sync.Mutex
	requests  map[string]uint64
	durations map[string]float64
}

func NewMetrics() *Metrics {
	return &Metrics{
		startedAt: time.Now(),
		requests:  make(map[string]uint64),
		durations: make(map[string]float64),
	}
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

func (m *Metrics) ServeHTTP(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintf(w, "# HELP flux_uptime_seconds Process uptime in seconds.\n")
	fmt.Fprintf(w, "# TYPE flux_uptime_seconds gauge\nflux_uptime_seconds %.3f\n", time.Since(m.startedAt).Seconds())
	fmt.Fprintf(w, "# HELP flux_http_active_requests Current HTTP requests.\n")
	fmt.Fprintf(w, "# TYPE flux_http_active_requests gauge\nflux_http_active_requests %d\n", m.active.Load())

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
