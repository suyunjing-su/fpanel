package tunnelhealth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/nodehub"
)

type Commander interface {
	TCPPing(context.Context, int64, nodehub.TCPPingRequest) (nodehub.TCPPingResponse, error)
}

type RefreshNotifier interface {
	Wake()
}

type Worker struct {
	db         *sql.DB
	repository *Repository
	commander  Commander
	refreshes  RefreshNotifier
	log        *slog.Logger
	mu         sync.Mutex
	snapshots  map[bandwidthKey]bandwidthSnapshot
}

type bandwidthKey struct {
	TunnelID int64
	NodeID   int64
}

type bandwidthSnapshot struct {
	NodeBytes   uint64
	TunnelBytes int64
	ObservedAt  time.Time
}

type healthCandidate struct {
	TunnelID int64
	NodeID   int64
	Online   bool
}

type exitCandidate struct {
	TunnelID         int64
	NodeID           int64
	Strategy         string
	MaxBandwidthMbps int
	NodeBytes        uint64
	TunnelBytes      int64
}

func NewWorker(db *sql.DB, repository *Repository, commander Commander, refreshes RefreshNotifier, log *slog.Logger) *Worker {
	return &Worker{db: db, repository: repository, commander: commander, refreshes: refreshes, log: log, snapshots: make(map[bandwidthKey]bandwidthSnapshot)}
}

func (w *Worker) Start(ctx context.Context) {
	go w.healthLoop(ctx)
	go w.bandwidthLoop(ctx)
}

func (w *Worker) healthLoop(ctx context.Context) {
	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			w.checkHealth(ctx)
			timer.Reset(60 * time.Second)
		}
	}
}

func (w *Worker) bandwidthLoop(ctx context.Context) {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			w.checkBandwidth(ctx)
			timer.Reset(15 * time.Second)
		}
	}
}

func (w *Worker) checkHealth(ctx context.Context) {
	candidates, err := w.healthCandidates(ctx)
	if err != nil {
		w.log.Warn("failed to list exit health candidates", "error", err)
		return
	}
	for _, candidate := range candidates {
		tunnelID, nodeID := candidate.TunnelID, candidate.NodeID
		result := nodehub.TCPPingResponse{}
		var commandErr error
		if candidate.Online {
			result, commandErr = w.commander.TCPPing(ctx, nodeID, nodehub.TCPPingRequest{IP: "www.google.com", Port: 443, Count: 1, Timeout: 2000})
		} else {
			commandErr = errors.New("node is offline")
		}
		healthy := commandErr == nil && result.Success && result.PacketLoss < 100 && result.AverageTime <= 20
		var latency *int64
		if commandErr == nil && result.AverageTime >= 0 {
			value := int64(result.AverageTime)
			latency = &value
		}
		detail := result.Error
		if commandErr != nil {
			detail = commandErr.Error()
		} else if !healthy && detail == "" {
			detail = fmt.Sprintf("latency %.2fms, packet loss %.2f%%", result.AverageTime, result.PacketLoss)
		}
		changed, err := w.repository.UpdateHealth(ctx, tunnelID, nodeID, healthy, latency, detail)
		if err != nil {
			w.log.Warn("failed to persist exit health", "tunnel_id", tunnelID, "node_id", nodeID, "error", err)
			continue
		}
		if changed && w.refreshes != nil {
			w.refreshes.Wake()
		}
	}
}

func (w *Worker) healthCandidates(ctx context.Context) ([]healthCandidate, error) {
	rows, err := w.db.QueryContext(ctx, `SELECT tn.tunnel_id,tn.node_id,n.status FROM tunnel_nodes tn JOIN nodes n ON n.id=tn.node_id WHERE tn.chain_type=3 AND tn.tunnel_id IN (SELECT tunnel_id FROM tunnel_nodes WHERE chain_type=3 GROUP BY tunnel_id HAVING COUNT(*)>1)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]healthCandidate, 0)
	for rows.Next() {
		var candidate healthCandidate
		if err := rows.Scan(&candidate.TunnelID, &candidate.NodeID, &candidate.Online); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}

func (w *Worker) checkBandwidth(ctx context.Context) {
	candidates, err := w.bandwidthCandidates(ctx)
	if err != nil {
		w.log.Warn("failed to list bandwidth candidates", "error", err)
		return
	}
	now := time.Now()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, candidate := range candidates {
		key := bandwidthKey{TunnelID: candidate.TunnelID, NodeID: candidate.NodeID}
		if candidate.Strategy != "fifo" {
			delete(w.snapshots, key)
			changed, err := w.repository.UpdateBandwidth(ctx, candidate.TunnelID, candidate.NodeID, false, "bandwidth monitoring disabled for non-fifo strategy")
			if err != nil {
				w.log.Warn("failed to clear non-fifo exit bandwidth state", "tunnel_id", candidate.TunnelID, "node_id", candidate.NodeID, "error", err)
				continue
			}
			if changed && w.refreshes != nil {
				w.refreshes.Wake()
			}
			continue
		}
		previous, ok := w.snapshots[key]
		current := bandwidthSnapshot{NodeBytes: candidate.NodeBytes, TunnelBytes: candidate.TunnelBytes, ObservedAt: now}
		w.snapshots[key] = current
		if !ok || !now.After(previous.ObservedAt) || candidate.NodeBytes < previous.NodeBytes || candidate.TunnelBytes < previous.TunnelBytes {
			continue
		}
		seconds := now.Sub(previous.ObservedAt).Seconds()
		nodeMbps := float64(candidate.NodeBytes-previous.NodeBytes) * 8 / seconds / 1_000_000
		tunnelMbps := float64(candidate.TunnelBytes-previous.TunnelBytes) * 8 / seconds / 1_000_000
		externalMbps := nodeMbps - tunnelMbps
		overloaded := candidate.MaxBandwidthMbps > 0 && nodeMbps >= float64(candidate.MaxBandwidthMbps)*0.8 && externalMbps > 0.1
		detail := fmt.Sprintf("node %.2fMbps, tunnel %.2fMbps, external %.2fMbps, limit %dMbps", nodeMbps, tunnelMbps, externalMbps, candidate.MaxBandwidthMbps)
		changed, err := w.repository.UpdateBandwidth(ctx, candidate.TunnelID, candidate.NodeID, overloaded, detail)
		if err != nil {
			w.log.Warn("failed to persist exit bandwidth", "tunnel_id", candidate.TunnelID, "node_id", candidate.NodeID, "error", err)
			continue
		}
		if changed && w.refreshes != nil {
			w.refreshes.Wake()
		}
	}
}

func (w *Worker) bandwidthCandidates(ctx context.Context) ([]exitCandidate, error) {
	rows, err := w.db.QueryContext(ctx, `SELECT tn.tunnel_id,tn.node_id,COALESCE(tn.strategy,'fifo'),n.max_bandwidth_mbps,n.bytes_received+n.bytes_transmitted,tn.ingress_bytes+tn.egress_bytes FROM tunnel_nodes tn JOIN nodes n ON n.id=tn.node_id WHERE tn.chain_type=3 AND n.status=1 AND tn.tunnel_id IN (SELECT tunnel_id FROM tunnel_nodes WHERE chain_type=3 GROUP BY tunnel_id HAVING COUNT(*)>1)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]exitCandidate, 0)
	for rows.Next() {
		var candidate exitCandidate
		if err := rows.Scan(&candidate.TunnelID, &candidate.NodeID, &candidate.Strategy, &candidate.MaxBandwidthMbps, &candidate.NodeBytes, &candidate.TunnelBytes); err != nil {
			return nil, err
		}
		result = append(result, candidate)
	}
	return result, rows.Err()
}
