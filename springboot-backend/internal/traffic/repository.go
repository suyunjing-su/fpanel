package traffic

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

func (r *Repository) Record(ctx context.Context, nodeID int64, items []ReportItem) error {
	if nodeID <= 0 {
		return errors.New("node id must be positive")
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin traffic transaction: %w", err)
	}
	defer transaction.Rollback()
	now := time.Now().UnixMilli()
	for _, item := range items {
		if err := r.recordItem(ctx, transaction, nodeID, item, now); err != nil {
			return err
		}
	}
	if _, err := transaction.ExecContext(ctx, "DELETE FROM statistics_flows WHERE recorded_at<?", now-48*60*60*1000); err != nil {
		return fmt.Errorf("cleanup traffic statistics: %w", err)
	}
	if err := transaction.Commit(); err != nil {
		return fmt.Errorf("commit traffic transaction: %w", err)
	}
	return nil
}

func (r *Repository) recordItem(ctx context.Context, transaction *sql.Tx, nodeID int64, item ReportItem, now int64) error {
	parts := strings.Split(item.Name, "_")
	if len(parts) >= 2 && parts[1] == "relay" {
		tunnelID, err := parseID(parts[0])
		if err != nil {
			return nil
		}
		up, down := item.Up, item.Down
		_, err = transaction.ExecContext(ctx, `UPDATE tunnel_nodes SET ingress_bytes=ingress_bytes+?,egress_bytes=egress_bytes+? WHERE tunnel_id=? AND node_id=? AND chain_type=3`, down, up, tunnelID, nodeID)
		if err != nil {
			return fmt.Errorf("update relay traffic: %w", err)
		}
		return nil
	}
	if len(parts) < 3 {
		return nil
	}
	forwardID, err := parseID(parts[0])
	if err != nil {
		return nil
	}
	userID, err := parseID(parts[1])
	if err != nil {
		return nil
	}
	userTunnelID, err := parseID(parts[2])
	if err != nil {
		return nil
	}

	var tunnelID int64
	var ratio float64
	var flow int
	if err := transaction.QueryRowContext(ctx, `SELECT tunnel_id,traffic_ratio,flow FROM forwards JOIN tunnels ON tunnels.id=forwards.tunnel_id WHERE forwards.id=? AND forwards.user_id=?`, forwardID, userID).Scan(&tunnelID, &ratio, &flow); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		return fmt.Errorf("load forward %d: %w", forwardID, err)
	}
	if flow != 1 && flow != 2 {
		return fmt.Errorf("invalid tunnel flow mode %d", flow)
	}
	up, err := scale(item.Up, ratio, flow)
	if err != nil {
		return err
	}
	down, err := scale(item.Down, ratio, flow)
	if err != nil {
		return err
	}

	if userTunnelID != 0 {
		var permissionStatus int
		if err := transaction.QueryRowContext(ctx, "SELECT status FROM user_tunnels WHERE id=? AND user_id=? AND tunnel_id=?", userTunnelID, userID, tunnelID).Scan(&permissionStatus); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil
			}
			return fmt.Errorf("load user tunnel permission: %w", err)
		}
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE forwards SET ingress_bytes=ingress_bytes+?,egress_bytes=egress_bytes+?,updated_at=? WHERE id=?", down, up, now, forwardID); err != nil {
		return fmt.Errorf("update forward traffic: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE users SET ingress_bytes=ingress_bytes+?,egress_bytes=egress_bytes+?,updated_at=? WHERE id=?", down, up, now, userID); err != nil {
		return fmt.Errorf("update user traffic: %w", err)
	}
	if userTunnelID != 0 {
		result, err := transaction.ExecContext(ctx, "UPDATE user_tunnels SET ingress_bytes=ingress_bytes+?,egress_bytes=egress_bytes+?,updated_at=? WHERE id=? AND user_id=? AND tunnel_id=?", down, up, now, userTunnelID, userID, tunnelID)
		if err != nil {
			return fmt.Errorf("update user tunnel traffic: %w", err)
		}
		if count, _ := result.RowsAffected(); count == 0 {
			return nil
		}
	}
	if _, err := transaction.ExecContext(ctx, "UPDATE tunnel_nodes SET ingress_bytes=ingress_bytes+?,egress_bytes=egress_bytes+? WHERE tunnel_id=? AND node_id=? AND chain_type=1", down, up, tunnelID, nodeID); err != nil {
		return fmt.Errorf("update entry traffic: %w", err)
	}
	if _, err := transaction.ExecContext(ctx, "INSERT INTO statistics_flows(user_id,flow,total_flow,recorded_at) VALUES(?,?,?,?)", userID, flow, down+up, now); err != nil {
		return fmt.Errorf("record traffic statistics: %w", err)
	}
	if err := r.enforceQuotas(ctx, transaction, forwardID, userID, userTunnelID, tunnelID, now); err != nil {
		return err
	}
	return nil
}

func (r *Repository) enforceQuotas(ctx context.Context, transaction *sql.Tx, forwardID, userID, userTunnelID, tunnelID, now int64) error {
	var userQuota, userIn, userOut int64
	if err := transaction.QueryRowContext(ctx, "SELECT flow_quota_bytes,ingress_bytes,egress_bytes FROM users WHERE id=?", userID).Scan(&userQuota, &userIn, &userOut); err != nil {
		return err
	}
	if userQuota > 0 && userIn+userOut >= userQuota {
		if _, err := transaction.ExecContext(ctx, "UPDATE users SET status=0,updated_at=? WHERE id=?", now, userID); err != nil {
			return fmt.Errorf("pause user after quota exceeded: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, "UPDATE forwards SET status=0,updated_at=? WHERE user_id=? AND status=1", now, userID); err != nil {
			return fmt.Errorf("pause user forwards after quota exceeded: %w", err)
		}
	}
	if userTunnelID == 0 {
		return nil
	}
	var tunnelQuota, tunnelIn, tunnelOut int64
	if err := transaction.QueryRowContext(ctx, "SELECT flow_quota_bytes,ingress_bytes,egress_bytes FROM user_tunnels WHERE id=? AND user_id=? AND tunnel_id=?", userTunnelID, userID, tunnelID).Scan(&tunnelQuota, &tunnelIn, &tunnelOut); err != nil {
		return err
	}
	if tunnelQuota > 0 && tunnelIn+tunnelOut >= tunnelQuota {
		if _, err := transaction.ExecContext(ctx, "UPDATE user_tunnels SET status=0,updated_at=? WHERE id=?", now, userTunnelID); err != nil {
			return fmt.Errorf("pause user tunnel after quota exceeded: %w", err)
		}
		if _, err := transaction.ExecContext(ctx, "UPDATE forwards SET status=0,updated_at=? WHERE id=?", now, forwardID); err != nil {
			return fmt.Errorf("pause forward after tunnel quota exceeded: %w", err)
		}
	}
	return nil
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
