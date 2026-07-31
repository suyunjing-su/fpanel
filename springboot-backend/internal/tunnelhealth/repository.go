package tunnelhealth

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type FailureEvent struct {
	ID           int64  `json:"id"`
	TunnelID     int64  `json:"tunnelId"`
	NodeID       int64  `json:"nodeId"`
	TunnelNodeID *int64 `json:"tunnelNodeId,omitempty"`
	UserTunnelID *int64 `json:"userTunnelId,omitempty"`
	PolicyType   string `json:"policyType,omitempty"`
	PolicyID     *int64 `json:"policyId,omitempty"`
	EventType    string `json:"eventType"`
	FromStatus   int    `json:"fromStatus"`
	ToStatus     int    `json:"toStatus"`
	LatencyMS    *int64 `json:"latencyMs,omitempty"`
	Detail       string `json:"detail"`
	StartedAt    int64  `json:"startedAt"`
	ResolvedAt   *int64 `json:"resolvedAt,omitempty"`
	CreatedAt    int64  `json:"createdAt"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) UpdateHealth(ctx context.Context, tunnelID, nodeID int64, healthy bool, latency *int64, detail string) (bool, error) {
	status := 0
	if healthy {
		status = 1
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer transaction.Rollback()
	var previous int
	if err := transaction.QueryRowContext(ctx, "SELECT health_status FROM tunnel_nodes WHERE tunnel_id=? AND node_id=?", tunnelID, nodeID).Scan(&previous); err != nil {
		return false, err
	}
	now := time.Now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, "UPDATE tunnel_nodes SET health_status=?,last_latency_ms=?,health_checked_at=? WHERE tunnel_id=? AND node_id=?", status, latency, now, tunnelID, nodeID); err != nil {
		return false, err
	}
	changed := previous != status
	if changed {
		if status == 0 {
			if _, err := transaction.ExecContext(ctx, `INSERT INTO tunnel_failure_events(tunnel_id,node_id,event_type,from_status,to_status,latency_ms,detail,started_at,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, tunnelID, nodeID, "health", previous, status, latency, detail, now, now); err != nil {
				return false, err
			}
		} else if _, err := transaction.ExecContext(ctx, `UPDATE tunnel_failure_events SET resolved_at=? WHERE tunnel_id=? AND node_id=? AND event_type='health' AND resolved_at IS NULL`, now, tunnelID, nodeID); err != nil {
			return false, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, err
	}
	return changed, nil
}

func (r *Repository) UpdateBandwidth(ctx context.Context, tunnelID, nodeID int64, overloaded bool, detail string) (bool, error) {
	status := 0
	if overloaded {
		status = 1
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer transaction.Rollback()
	var previous int
	if err := transaction.QueryRowContext(ctx, "SELECT bandwidth_overloaded FROM tunnel_nodes WHERE tunnel_id=? AND node_id=?", tunnelID, nodeID).Scan(&previous); err != nil {
		return false, err
	}
	now := time.Now().UnixMilli()
	if _, err := transaction.ExecContext(ctx, "UPDATE tunnel_nodes SET bandwidth_overloaded=? WHERE tunnel_id=? AND node_id=?", status, tunnelID, nodeID); err != nil {
		return false, err
	}
	changed := previous != status
	if changed {
		if status == 1 {
			if _, err := transaction.ExecContext(ctx, `INSERT INTO tunnel_failure_events(tunnel_id,node_id,event_type,from_status,to_status,detail,started_at,created_at) VALUES(?,?,?,?,?,?,?,?)`, tunnelID, nodeID, "bandwidth_overload", previous, status, detail, now, now); err != nil {
				return false, err
			}
		} else if _, err := transaction.ExecContext(ctx, `UPDATE tunnel_failure_events SET resolved_at=? WHERE tunnel_id=? AND node_id=? AND event_type='bandwidth_overload' AND resolved_at IS NULL`, now, tunnelID, nodeID); err != nil {
			return false, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return false, err
	}
	return changed, nil
}

func (r *Repository) ListEvents(ctx context.Context, tunnelID, nodeID int64, eventType string, activeOnly bool) ([]FailureEvent, error) {
	query := `SELECT id,tunnel_id,node_id,tunnel_node_id,user_tunnel_id,policy_type,policy_id,event_type,from_status,to_status,latency_ms,detail,started_at,resolved_at,created_at FROM tunnel_failure_events WHERE 1=1`
	args := []any{}
	if tunnelID > 0 {
		query += " AND tunnel_id=?"
		args = append(args, tunnelID)
	}
	if nodeID > 0 {
		query += " AND node_id=?"
		args = append(args, nodeID)
	}
	if eventType != "" {
		query += " AND event_type=?"
		args = append(args, eventType)
	}
	if activeOnly {
		query += " AND resolved_at IS NULL"
	}
	query += " ORDER BY created_at DESC LIMIT 500"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list tunnel failure events: %w", err)
	}
	defer rows.Close()
	result := make([]FailureEvent, 0)
	for rows.Next() {
		var event FailureEvent
		var nodeRecord, userTunnel, policyID, latency, resolved sql.NullInt64
		var policyType sql.NullString
		if err := rows.Scan(&event.ID, &event.TunnelID, &event.NodeID, &nodeRecord, &userTunnel, &policyType, &policyID, &event.EventType, &event.FromStatus, &event.ToStatus, &latency, &event.Detail, &event.StartedAt, &resolved, &event.CreatedAt); err != nil {
			return nil, err
		}
		if nodeRecord.Valid {
			event.TunnelNodeID = &nodeRecord.Int64
		}
		if userTunnel.Valid {
			event.UserTunnelID = &userTunnel.Int64
		}
		if policyType.Valid {
			event.PolicyType = policyType.String
		}
		if policyID.Valid {
			event.PolicyID = &policyID.Int64
		}
		if latency.Valid {
			event.LatencyMS = &latency.Int64
		}
		if resolved.Valid {
			event.ResolvedAt = &resolved.Int64
		}
		result = append(result, event)
	}
	return result, rows.Err()
}
