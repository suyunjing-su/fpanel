package traffic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/nodehub/crypto"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
)

type ReportItem struct {
	Name string `json:"n"`
	Up   int64  `json:"u"`
	Down int64  `json:"d"`
}

type encryptedMessage struct {
	Encrypted bool   `json:"encrypted"`
	Data      string `json:"data"`
	Timestamp int64  `json:"timestamp"`
}

type Repository struct {
	db    *sql.DB
	nodes *nodes.Repository
}

func NewRepository(db *sql.DB, nodeRepo *nodes.Repository) *Repository {
	return &Repository{db: db, nodes: nodeRepo}
}

func DecodeReport(raw []byte, secret string) ([]ReportItem, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, errors.New("traffic report is empty")
	}
	var wrapper encryptedMessage
	if err := json.Unmarshal(raw, &wrapper); err == nil && wrapper.Encrypted {
		if wrapper.Data == "" {
			return nil, errors.New("encrypted traffic report has no data")
		}
		if wrapper.Timestamp > 0 && abs(time.Now().Unix()-wrapper.Timestamp) > 300 {
			return nil, errors.New("encrypted traffic report has expired")
		}
		cipher, err := crypto.New(secret)
		if err != nil {
			return nil, err
		}
		plain, err := cipher.Decrypt(wrapper.Data)
		if err != nil {
			return nil, fmt.Errorf("decrypt traffic report: %w", err)
		}
		raw = plain
	}
	var items []ReportItem
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("decode traffic report: %w", err)
	}
	for _, item := range items {
		if strings.TrimSpace(item.Name) == "" {
			return nil, errors.New("traffic report contains an empty service name")
		}
		if item.Up < 0 || item.Down < 0 {
			return nil, errors.New("traffic report contains negative bytes")
		}
	}
	return items, nil
}

func (r *Repository) Record(ctx context.Context, nodeID int64, items []ReportItem) ([]int64, error) {
	if nodeID <= 0 {
		return nil, errors.New("node id must be positive")
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin traffic transaction: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().UnixMilli()
	refreshTunnels := make(map[int64]struct{})
	refreshUsers := make(map[int64]struct{})
	for _, item := range items {
		tunnelID, refresh, refreshUserID, err := r.recordItem(ctx, transaction, nodeID, item, now)
		if err != nil {
			return nil, err
		}
		if refresh {
			refreshTunnels[tunnelID] = struct{}{}
		}
		if refreshUserID > 0 {
			refreshUsers[refreshUserID] = struct{}{}
		}
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM statistics_flows WHERE recorded_at<?", now-48*60*60*1000); err != nil {
		return nil, fmt.Errorf("cleanup traffic statistics: %w", err)
	}
	for userID := range refreshUsers {
		rows, err := transaction.QueryContext(ctx, "SELECT DISTINCT tunnel_id FROM forwards WHERE user_id=?", userID)
		if err != nil {
			return nil, fmt.Errorf("list user tunnels after quota transition: %w", err)
		}
		for rows.Next() {
			var tunnelID int64
			if err := rows.Scan(&tunnelID); err != nil {
				rows.Close()
				return nil, err
			}
			refreshTunnels[tunnelID] = struct{}{}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	entryNodes := make([]int64, 0)
	seenNodes := make(map[int64]struct{})
	tunnelIDs := make([]int64, 0, len(refreshTunnels))
	for tunnelID := range refreshTunnels {
		tunnelIDs = append(tunnelIDs, tunnelID)
	}
	sort.Slice(tunnelIDs, func(i, j int) bool { return tunnelIDs[i] < tunnelIDs[j] })
	for _, tunnelID := range tunnelIDs {
		rows, err := transaction.QueryContext(ctx, "SELECT node_id FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=1 ORDER BY node_id", tunnelID)
		if err != nil {
			return nil, fmt.Errorf("list entry nodes after quota transition: %w", err)
		}
		for rows.Next() {
			var entryNodeID int64
			if err := rows.Scan(&entryNodeID); err != nil {
				rows.Close()
				return nil, err
			}
			if _, exists := seenNodes[entryNodeID]; !exists {
				seenNodes[entryNodeID] = struct{}{}
				entryNodes = append(entryNodes, entryNodeID)
			}
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	for _, entryNodeID := range entryNodes {
		if _, err := transaction.ExecContext(ctx, `INSERT INTO node_config_refreshes(node_id,requested_at) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at`, entryNodeID, now); err != nil {
			return nil, fmt.Errorf("queue entry configuration refresh: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return nil, fmt.Errorf("commit traffic transaction: %w", err)
	}
	return entryNodes, nil
}

func (r *Repository) recordItem(ctx context.Context, transaction *sql.Tx, nodeID int64, item ReportItem, now int64) (int64, bool, int64, error) {
	parts := strings.Split(item.Name, "_")
	if len(parts) >= 2 && parts[1] == "relay" {
		tunnelID, err := parseID(parts[0])
		if err != nil {
			return 0, false, 0, nil
		}
		refresh, err := r.recordTunnelNodeTraffic(ctx, transaction, tunnelID, nodeID, 3, item.Down, item.Up, now)
		if err != nil {
			return 0, false, 0, fmt.Errorf("update relay traffic: %w", err)
		}
		return tunnelID, refresh, 0, nil
	}
	if len(parts) < 3 {
		return 0, false, 0, nil
	}
	forwardID, err := parseID(parts[0])
	if err != nil {
		return 0, false, 0, nil
	}
	userID, err := parseID(parts[1])
	if err != nil {
		return 0, false, 0, nil
	}
	userTunnelID, err := parseID(parts[2])
	if err != nil {
		return 0, false, 0, nil
	}

	var tunnelID int64
	var ratio float64
	var flow int
	if err := transaction.QueryRowContext(ctx, `SELECT tunnel_id,traffic_ratio,flow FROM forwards JOIN tunnels ON tunnels.id=forwards.tunnel_id WHERE forwards.id=? AND forwards.user_id=?`, forwardID, userID).Scan(&tunnelID, &ratio, &flow); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, false, 0, nil
		}
		return 0, false, 0, fmt.Errorf("load forward %d: %w", forwardID, err)
	}
	if flow != 1 && flow != 2 {
		return 0, false, 0, fmt.Errorf("invalid tunnel flow mode %d", flow)
	}
	up, err := scale(item.Up, ratio, flow)
	if err != nil {
		return 0, false, 0, err
	}
	down, err := scale(item.Down, ratio, flow)
	if err != nil {
		return 0, false, 0, err
	}

	if userTunnelID != 0 {
		var permissionStatus int
		if err := transaction.QueryRowContext(ctx, "SELECT status FROM user_tunnels WHERE id=? AND user_id=? AND tunnel_id=?", userTunnelID, userID, tunnelID).Scan(&permissionStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return 0, false, 0, nil
			}
			return 0, false, 0, fmt.Errorf("load user tunnel permission: %w", err)
		}
	}
	result, err := transaction.ExecContext(ctx, "UPDATE forwards SET ingress_bytes=ingress_bytes+?,egress_bytes=egress_bytes+?,updated_at=? WHERE id=? AND ingress_bytes<=? AND egress_bytes<=?", down, up, now, forwardID, math.MaxInt64-down, math.MaxInt64-up)
	if err != nil {
		return 0, false, 0, fmt.Errorf("update forward traffic: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return 0, false, 0, errors.New("forward traffic counter overflow")
	}
	result, err = transaction.ExecContext(ctx, "UPDATE users SET ingress_bytes=ingress_bytes+?,egress_bytes=egress_bytes+?,updated_at=? WHERE id=? AND ingress_bytes<=? AND egress_bytes<=?", down, up, now, userID, math.MaxInt64-down, math.MaxInt64-up)
	if err != nil {
		return 0, false, 0, fmt.Errorf("update user traffic: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return 0, false, 0, errors.New("user traffic counter overflow")
	}
	if userTunnelID != 0 {
		result, err = transaction.ExecContext(ctx, "UPDATE user_tunnels SET ingress_bytes=ingress_bytes+?,egress_bytes=egress_bytes+?,updated_at=? WHERE id=? AND user_id=? AND tunnel_id=? AND ingress_bytes<=? AND egress_bytes<=?", down, up, now, userTunnelID, userID, tunnelID, math.MaxInt64-down, math.MaxInt64-up)
		if err != nil {
			return 0, false, 0, fmt.Errorf("update user tunnel traffic: %w", err)
		}
		if count, _ := result.RowsAffected(); count == 0 {
			return 0, false, 0, errors.New("user tunnel traffic counter overflow")
		}
	}
	total, err := addBytes(up, down)
	if err != nil {
		return 0, false, 0, err
	}
	entryRefresh, err := r.recordTunnelNodeTraffic(ctx, transaction, tunnelID, nodeID, 1, down, up, now)
	if err != nil {
		return 0, false, 0, fmt.Errorf("update entry traffic: %w", err)
	}

	refresh := entryRefresh
	if userTunnelID != 0 {
		policyRefresh, err := r.recordPolicyTraffic(ctx, transaction, userTunnelID, tunnelID, nodeID, total, now)
		if err != nil {
			return 0, false, 0, err
		}
		refresh = policyRefresh
	}
	if _, err := transaction.ExecContext(ctx, "INSERT INTO statistics_flows(user_id,flow,total_flow,recorded_at) VALUES(?,?,?,?)", userID, flow, total, now); err != nil {
		return 0, false, 0, fmt.Errorf("record traffic statistics: %w", err)
	}
	tunnelRefresh, userRefresh, err := r.enforceQuotas(ctx, transaction, forwardID, userID, userTunnelID, tunnelID, now)
	if err != nil {
		return 0, false, 0, err
	}
	refreshUserID := int64(0)
	if userRefresh {
		refreshUserID = userID
	}
	return tunnelID, refresh || tunnelRefresh, refreshUserID, nil
}

func (r *Repository) recordTunnelNodeTraffic(ctx context.Context, transaction *sql.Tx, tunnelID, nodeID int64, chainType int, ingress, egress, now int64) (bool, error) {
	var tunnelNodeID, quota, oldIngress, oldEgress int64
	if err := transaction.QueryRowContext(ctx, `SELECT id,COALESCE(flow_quota_bytes,0),ingress_bytes,egress_bytes FROM tunnel_nodes WHERE tunnel_id=? AND node_id=? AND chain_type=?`, tunnelID, nodeID, chainType).Scan(&tunnelNodeID, &quota, &oldIngress, &oldEgress); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	newIngress, err := addBytes(oldIngress, ingress)
	if err != nil {
		return false, err
	}
	newEgress, err := addBytes(oldEgress, egress)
	if err != nil {
		return false, err
	}
	oldTotal, err := addBytes(oldIngress, oldEgress)
	if err != nil {
		return false, err
	}
	newTotal, err := addBytes(newIngress, newEgress)
	if err != nil {
		return false, err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE tunnel_nodes SET ingress_bytes=?,egress_bytes=? WHERE id=?`, newIngress, newEgress, tunnelNodeID); err != nil {
		return false, err
	}
	crossed := quota > 0 && oldTotal < quota && newTotal >= quota
	if crossed {
		detail := fmt.Sprintf("tunnel node quota exceeded: %d/%d bytes", newTotal, quota)
		if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO tunnel_failure_events(tunnel_id,node_id,tunnel_node_id,policy_type,event_type,from_status,to_status,detail,started_at,created_at) VALUES(?,?,?,'node','quota',1,0,?,?,?)`, tunnelID, nodeID, tunnelNodeID, detail, now, now); err != nil {
			return false, err
		}
	}
	return crossed, nil
}

func (r *Repository) recordPolicyTraffic(ctx context.Context, transaction *sql.Tx, userTunnelID, tunnelID, entryNodeID, total, now int64) (bool, error) {
	if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO user_tunnel_entry_policies(user_tunnel_id,tunnel_id,entry_node_id,created_at,updated_at) SELECT ?,?,node_id,?,? FROM tunnel_nodes WHERE tunnel_id=? AND node_id=? AND chain_type=1`, userTunnelID, tunnelID, now, now, tunnelID, entryNodeID); err != nil {
		return false, fmt.Errorf("initialize entry traffic policy: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO user_tunnel_exit_policies(user_tunnel_id,tunnel_id,exit_node_id,created_at,updated_at) SELECT ?,?,node_id,?,? FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=3`, userTunnelID, tunnelID, now, now, tunnelID); err != nil {
		return false, fmt.Errorf("initialize exit traffic policies: %w", err)
	}

	refresh := false
	var entryPolicyID, entryQuota, entryUsed int64
	var entryStatus int
	if err := transaction.QueryRowContext(ctx, `SELECT id,flow_quota_bytes,used_bytes,status FROM user_tunnel_entry_policies WHERE user_tunnel_id=? AND tunnel_id=? AND entry_node_id=?`, userTunnelID, tunnelID, entryNodeID).Scan(&entryPolicyID, &entryQuota, &entryUsed, &entryStatus); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	if entryPolicyID > 0 {
		newEntryUsed, err := addBytes(entryUsed, total)
		if err != nil {
			return false, err
		}
		if _, err := transaction.ExecContext(ctx, `UPDATE user_tunnel_entry_policies SET used_bytes=?,updated_at=? WHERE id=?`, newEntryUsed, now, entryPolicyID); err != nil {
			return false, fmt.Errorf("update entry policy traffic: %w", err)
		}
		if entryStatus == 1 && entryQuota > 0 && entryUsed < entryQuota && newEntryUsed >= entryQuota {
			detail := fmt.Sprintf("entry policy quota exceeded: %d/%d bytes", newEntryUsed, entryQuota)
			if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO tunnel_failure_events(tunnel_id,node_id,tunnel_node_id,user_tunnel_id,policy_type,policy_id,event_type,from_status,to_status,detail,started_at,created_at) SELECT ?,?,id,?,'entry',?,'quota',1,0,?,?,? FROM tunnel_nodes WHERE tunnel_id=? AND node_id=? AND chain_type=1`, tunnelID, entryNodeID, userTunnelID, entryPolicyID, detail, now, now, tunnelID, entryNodeID); err != nil {
				return false, fmt.Errorf("record entry policy quota event: %w", err)
			}
			refresh = true
		}
	}

	var chainCount int
	if err := transaction.QueryRowContext(ctx, "SELECT COUNT(1) FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=2", tunnelID).Scan(&chainCount); err != nil {
		return false, err
	}
	if chainCount != 0 {
		return refresh, nil
	}
	var exitPolicyID, exitNodeID, exitQuota, exitUsed int64
	err := transaction.QueryRowContext(ctx, `SELECT p.id,p.exit_node_id,p.flow_quota_bytes,p.used_bytes FROM user_tunnel_exit_policies p JOIN tunnel_nodes tn ON tn.tunnel_id=p.tunnel_id AND tn.node_id=p.exit_node_id AND tn.chain_type=3 WHERE p.user_tunnel_id=? AND p.tunnel_id=? AND p.status=1 AND (p.flow_quota_bytes=0 OR p.used_bytes<p.flow_quota_bytes) AND tn.health_status=1 AND tn.bandwidth_overloaded=0 AND (tn.flow_quota_bytes=0 OR tn.ingress_bytes+tn.egress_bytes<tn.flow_quota_bytes) ORDER BY p.id LIMIT 1`, userTunnelID, tunnelID).Scan(&exitPolicyID, &exitNodeID, &exitQuota, &exitUsed)
	if errors.Is(err, sql.ErrNoRows) {
		return refresh, nil
	}
	if err != nil {
		return false, err
	}
	newExitUsed, err := addBytes(exitUsed, total)
	if err != nil {
		return false, err
	}
	if _, err := transaction.ExecContext(ctx, `UPDATE user_tunnel_exit_policies SET used_bytes=?,updated_at=? WHERE id=?`, newExitUsed, now, exitPolicyID); err != nil {
		return false, fmt.Errorf("update exit policy traffic: %w", err)
	}
	if exitQuota > 0 && exitUsed < exitQuota && newExitUsed >= exitQuota {
		detail := fmt.Sprintf("exit policy quota exceeded: %d/%d bytes", newExitUsed, exitQuota)
		if _, err := transaction.ExecContext(ctx, `INSERT OR IGNORE INTO tunnel_failure_events(tunnel_id,node_id,tunnel_node_id,user_tunnel_id,policy_type,policy_id,event_type,from_status,to_status,detail,started_at,created_at) SELECT ?,?,id,?,'exit',?,'quota',1,0,?,?,? FROM tunnel_nodes WHERE tunnel_id=? AND node_id=? AND chain_type=3`, tunnelID, exitNodeID, userTunnelID, exitPolicyID, detail, now, now, tunnelID, exitNodeID); err != nil {
			return false, fmt.Errorf("record exit policy quota event: %w", err)
		}
		refresh = true
	}
	return refresh, nil
}

func (r *Repository) enforceQuotas(ctx context.Context, transaction *sql.Tx, forwardID, userID, userTunnelID, tunnelID, now int64) (bool, bool, error) {
	userRefresh := false
	var userQuota, userIn, userOut int64
	var userStatus int
	if err := transaction.QueryRowContext(ctx, "SELECT flow_quota_bytes,ingress_bytes,egress_bytes,status FROM users WHERE id=?", userID).Scan(&userQuota, &userIn, &userOut, &userStatus); err != nil {
		return false, false, err
	}
	userTotal, err := addBytes(userIn, userOut)
	if err != nil {
		return false, false, err
	}
	if userStatus == 1 && userQuota > 0 && userTotal >= userQuota {
		if _, err := transaction.ExecContext(ctx, "UPDATE users SET status=0,updated_at=? WHERE id=?", now, userID); err != nil {
			return false, false, fmt.Errorf("pause user after quota exceeded: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, "UPDATE forwards SET status=0,updated_at=? WHERE user_id=? AND status=1", now, userID); err != nil {
			return false, false, fmt.Errorf("pause user forwards after quota exceeded: %w", err)
		}
		userRefresh = true
	}
	if userTunnelID == 0 {
		return false, userRefresh, nil
	}
	var tunnelQuota, tunnelIn, tunnelOut int64
	var tunnelStatus int
	if err := transaction.QueryRowContext(ctx, "SELECT flow_quota_bytes,ingress_bytes,egress_bytes,status FROM user_tunnels WHERE id=? AND user_id=? AND tunnel_id=?", userTunnelID, userID, tunnelID).Scan(&tunnelQuota, &tunnelIn, &tunnelOut, &tunnelStatus); err != nil {
		return false, false, err
	}
	tunnelRefresh := false
	tunnelTotal, err := addBytes(tunnelIn, tunnelOut)
	if err != nil {
		return false, false, err
	}
	if tunnelStatus == 1 && tunnelQuota > 0 && tunnelTotal >= tunnelQuota {
		if _, err := transaction.ExecContext(ctx, "UPDATE user_tunnels SET status=0,updated_at=? WHERE id=?", now, userTunnelID); err != nil {
			return false, false, fmt.Errorf("pause user tunnel after quota exceeded: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, "UPDATE forwards SET status=0,updated_at=? WHERE id=?", now, forwardID); err != nil {
			return false, false, fmt.Errorf("pause forward after tunnel quota exceeded: %w", err)
		}
		tunnelRefresh = true
	}
	return tunnelRefresh, userRefresh, nil
}

func addBytes(left, right int64) (int64, error) {
	if left < 0 || right < 0 || left > math.MaxInt64-right {
		return 0, errors.New("traffic counter exceeds integer range")
	}
	return left + right, nil
}

func scale(value int64, ratio float64, flow int) (int64, error) {
	if value == 0 || ratio == 0 {
		return 0, nil
	}
	result := float64(value) * ratio * float64(flow)
	if result > math.MaxInt64 {
		return 0, errors.New("scaled traffic exceeds integer range")
	}
	return int64(result), nil
}

func parseID(value string) (int64, error) {
	var id int64
	if _, err := fmt.Sscan(value, &id); err != nil || id <= 0 {
		return 0, errors.New("invalid service identifier")
	}
	return id, nil
}

func abs(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
