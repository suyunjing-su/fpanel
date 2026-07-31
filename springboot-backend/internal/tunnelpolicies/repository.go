package tunnelpolicies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	bytesPerGB int64 = 1024 * 1024 * 1024
	maxQuotaGB int64 = 8_589_934_591
)

type EntryPolicy struct {
	ID             int64  `json:"id"`
	UserTunnelID   int64  `json:"userTunnelId"`
	TunnelID       int64  `json:"tunnelId"`
	EntryNodeID    int64  `json:"entryNodeId"`
	EntryNodeName  string `json:"entryNodeName"`
	SpeedLimitMbps int    `json:"speedLimitMbps"`
	FlowQuotaGB    int64  `json:"flowQuotaGb"`
	UsedFlow       int64  `json:"usedFlow"`
	Status         int    `json:"status"`
}

type ExitPolicy struct {
	ID                  int64  `json:"id"`
	UserTunnelID        int64  `json:"userTunnelId"`
	TunnelID            int64  `json:"tunnelId"`
	ExitNodeID          int64  `json:"exitNodeId"`
	ExitNodeName        string `json:"exitNodeName"`
	FlowQuotaGB         int64  `json:"flowQuotaGb"`
	UsedFlow            int64  `json:"usedFlow"`
	Status              int    `json:"status"`
	HealthStatus        int    `json:"healthStatus"`
	BandwidthOverloaded int    `json:"bandwidthOverloaded"`
	LastLatencyMS       *int64 `json:"lastLatencyMs"`
	HealthCheckedAt     *int64 `json:"healthCheckedTime"`
}

type UpdateEntryRequest struct {
	ID             int64 `json:"id"`
	SpeedLimitMbps int   `json:"speedLimitMbps"`
	FlowQuotaGB    int64 `json:"flowQuotaGb"`
	Status         int   `json:"status"`
}

type UpdateExitRequest struct {
	ID          int64 `json:"id"`
	FlowQuotaGB int64 `json:"flowQuotaGb"`
	Status      int   `json:"status"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ListEntry(ctx context.Context, userTunnelID int64) ([]EntryPolicy, error) {
	if err := r.sync(ctx, userTunnelID, 1); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.user_tunnel_id,p.tunnel_id,p.entry_node_id,n.name,p.speed_limit_mbps,p.flow_quota_bytes,p.used_bytes,p.status FROM user_tunnel_entry_policies p JOIN nodes n ON n.id=p.entry_node_id WHERE p.user_tunnel_id=? ORDER BY p.entry_node_id`, userTunnelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]EntryPolicy, 0)
	for rows.Next() {
		var policy EntryPolicy
		var quota int64
		if err := rows.Scan(&policy.ID, &policy.UserTunnelID, &policy.TunnelID, &policy.EntryNodeID, &policy.EntryNodeName, &policy.SpeedLimitMbps, &quota, &policy.UsedFlow, &policy.Status); err != nil {
			return nil, err
		}
		policy.FlowQuotaGB = quota / bytesPerGB
		result = append(result, policy)
	}
	return result, rows.Err()
}

func (r *Repository) ListExit(ctx context.Context, userTunnelID int64) ([]ExitPolicy, error) {
	if err := r.sync(ctx, userTunnelID, 3); err != nil {
		return nil, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,p.user_tunnel_id,p.tunnel_id,p.exit_node_id,n.name,p.flow_quota_bytes,p.used_bytes,p.status,tn.health_status,tn.bandwidth_overloaded,tn.last_latency_ms,tn.health_checked_at FROM user_tunnel_exit_policies p JOIN nodes n ON n.id=p.exit_node_id JOIN tunnel_nodes tn ON tn.tunnel_id=p.tunnel_id AND tn.node_id=p.exit_node_id AND tn.chain_type=3 WHERE p.user_tunnel_id=? ORDER BY p.exit_node_id`, userTunnelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]ExitPolicy, 0)
	for rows.Next() {
		var policy ExitPolicy
		var quota int64
		var latency, checked sql.NullInt64
		if err := rows.Scan(&policy.ID, &policy.UserTunnelID, &policy.TunnelID, &policy.ExitNodeID, &policy.ExitNodeName, &quota, &policy.UsedFlow, &policy.Status, &policy.HealthStatus, &policy.BandwidthOverloaded, &latency, &checked); err != nil {
			return nil, err
		}
		policy.FlowQuotaGB = quota / bytesPerGB
		if latency.Valid {
			policy.LastLatencyMS = &latency.Int64
		}
		if checked.Valid {
			policy.HealthCheckedAt = &checked.Int64
		}
		result = append(result, policy)
	}
	return result, rows.Err()
}

func (r *Repository) UpdateEntry(ctx context.Context, request UpdateEntryRequest) (int64, error) {
	if request.ID <= 0 || request.SpeedLimitMbps < 0 || request.FlowQuotaGB < 0 || request.FlowQuotaGB > maxQuotaGB || (request.Status != 0 && request.Status != 1) {
		return 0, errors.New("invalid entry policy")
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer transaction.Rollback()
	var tunnelID, usedBytes int64
	if err := transaction.QueryRowContext(ctx, "SELECT tunnel_id,used_bytes FROM user_tunnel_entry_policies WHERE id=?", request.ID).Scan(&tunnelID, &usedBytes); err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	quotaBytes := request.FlowQuotaGB * bytesPerGB
	if _, err := transaction.ExecContext(ctx, `UPDATE user_tunnel_entry_policies SET speed_limit_mbps=?,flow_quota_bytes=?,status=?,updated_at=? WHERE id=?`, request.SpeedLimitMbps, quotaBytes, request.Status, now, request.ID); err != nil {
		return 0, err
	}
	if request.Status == 0 || quotaBytes == 0 || usedBytes < quotaBytes {
		if _, err := transaction.ExecContext(ctx, `UPDATE tunnel_failure_events SET resolved_at=? WHERE policy_type='entry' AND policy_id=? AND event_type='quota' AND resolved_at IS NULL`, now, request.ID); err != nil {
			return 0, err
		}
	} else {
		detail := fmt.Sprintf("entry policy quota exceeded: %d/%d bytes", usedBytes, quotaBytes)
		if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO tunnel_failure_events(tunnel_id,node_id,tunnel_node_id,user_tunnel_id,policy_type,policy_id,event_type,from_status,to_status,detail,started_at,created_at) SELECT p.tunnel_id,p.entry_node_id,tn.id,p.user_tunnel_id,'entry',p.id,'quota',1,0,?,?,? FROM user_tunnel_entry_policies p LEFT JOIN tunnel_nodes tn ON tn.tunnel_id=p.tunnel_id AND tn.node_id=p.entry_node_id WHERE p.id=?`, detail, now, now, request.ID); err != nil {
			return 0, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return 0, err
	}
	return tunnelID, nil
}

func (r *Repository) UpdateExit(ctx context.Context, request UpdateExitRequest) (int64, error) {
	if request.ID <= 0 || request.FlowQuotaGB < 0 || request.FlowQuotaGB > maxQuotaGB || (request.Status != 0 && request.Status != 1) {
		return 0, errors.New("invalid exit policy")
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer transaction.Rollback()
	var tunnelID, usedBytes int64
	if err := transaction.QueryRowContext(ctx, "SELECT tunnel_id,used_bytes FROM user_tunnel_exit_policies WHERE id=?", request.ID).Scan(&tunnelID, &usedBytes); err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	quotaBytes := request.FlowQuotaGB * bytesPerGB
	if _, err := transaction.ExecContext(ctx, `UPDATE user_tunnel_exit_policies SET flow_quota_bytes=?,status=?,updated_at=? WHERE id=?`, quotaBytes, request.Status, now, request.ID); err != nil {
		return 0, err
	}
	if request.Status == 0 || quotaBytes == 0 || usedBytes < quotaBytes {
		if _, err := transaction.ExecContext(ctx, `UPDATE tunnel_failure_events SET resolved_at=? WHERE policy_type='exit' AND policy_id=? AND event_type='quota' AND resolved_at IS NULL`, now, request.ID); err != nil {
			return 0, err
		}
	} else {
		detail := fmt.Sprintf("exit policy quota exceeded: %d/%d bytes", usedBytes, quotaBytes)
		if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO tunnel_failure_events(tunnel_id,node_id,tunnel_node_id,user_tunnel_id,policy_type,policy_id,event_type,from_status,to_status,detail,started_at,created_at) SELECT p.tunnel_id,p.exit_node_id,tn.id,p.user_tunnel_id,'exit',p.id,'quota',1,0,?,?,? FROM user_tunnel_exit_policies p LEFT JOIN tunnel_nodes tn ON tn.tunnel_id=p.tunnel_id AND tn.node_id=p.exit_node_id WHERE p.id=?`, detail, now, now, request.ID); err != nil {
			return 0, err
		}
	}
	if err := transaction.Commit(); err != nil {
		return 0, err
	}
	return tunnelID, nil
}

func (r *Repository) EntryNodeIDs(ctx context.Context, tunnelID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT node_id FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=1 ORDER BY node_id", tunnelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]int64, 0)
	for rows.Next() {
		var nodeID int64
		if err := rows.Scan(&nodeID); err != nil {
			return nil, err
		}
		result = append(result, nodeID)
	}
	return result, rows.Err()
}

func (r *Repository) sync(ctx context.Context, userTunnelID int64, chainType int) error {
	if userTunnelID <= 0 || (chainType != 1 && chainType != 3) {
		return errors.New("invalid user tunnel policy request")
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var tunnelID int64
	if err := transaction.QueryRowContext(ctx, "SELECT tunnel_id FROM user_tunnels WHERE id=?", userTunnelID).Scan(&tunnelID); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	if chainType == 1 {
		if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO user_tunnel_entry_policies(user_tunnel_id,tunnel_id,entry_node_id,created_at,updated_at) SELECT ?,?,node_id,?,? FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=1`, userTunnelID, tunnelID, now, now, tunnelID); err != nil {
			return fmt.Errorf("sync entry policies: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE tunnel_failure_events SET resolved_at=? WHERE event_type='quota' AND policy_type='entry' AND resolved_at IS NULL AND policy_id IN (SELECT id FROM user_tunnel_entry_policies WHERE user_tunnel_id=? AND entry_node_id NOT IN (SELECT node_id FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=1))`, now, userTunnelID, tunnelID); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `DELETE FROM user_tunnel_entry_policies WHERE user_tunnel_id=? AND entry_node_id NOT IN (SELECT node_id FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=1)`, userTunnelID, tunnelID); err != nil {
			return err
		}
	} else {
		if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO user_tunnel_exit_policies(user_tunnel_id,tunnel_id,exit_node_id,created_at,updated_at) SELECT ?,?,node_id,?,? FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=3`, userTunnelID, tunnelID, now, now, tunnelID); err != nil {
			return fmt.Errorf("sync exit policies: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE tunnel_failure_events SET resolved_at=? WHERE event_type='quota' AND policy_type='exit' AND resolved_at IS NULL AND policy_id IN (SELECT id FROM user_tunnel_exit_policies WHERE user_tunnel_id=? AND exit_node_id NOT IN (SELECT node_id FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=3))`, now, userTunnelID, tunnelID); err != nil {
			return err
		}
		if _, err := transaction.ExecContext(ctx, `DELETE FROM user_tunnel_exit_policies WHERE user_tunnel_id=? AND exit_node_id NOT IN (SELECT node_id FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=3)`, userTunnelID, tunnelID); err != nil {
			return err
		}
	}
	return transaction.Commit()
}
