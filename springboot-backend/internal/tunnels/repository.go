package tunnels

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
)

const (
	bytesPerGB int64 = 1024 * 1024 * 1024
	maxQuotaGB int64 = 8_589_934_591

	defaultTOTPathCount            = 2
	defaultTOTMaxPayload           = 32 * 1024
	defaultTOTWindow               = 256
	defaultTOTRetransmitIntervalMS = 300
	defaultTOTMaxRetries           = 20
	defaultTOTRecoveryPeriodMS     = 1000
	defaultTOTHandshakeTimeoutMS   = 5000
	defaultTOTMaxClockSkewMS       = 30000
	defaultTOTIdleTTLMS            = 300000
)

type NodeSpec struct {
	NodeID            int64  `json:"nodeId"`
	Protocol          string `json:"protocol,omitempty"`
	Strategy          string `json:"strategy,omitempty"`
	ChainType         int    `json:"chainType,omitempty"`
	Inx               int    `json:"inx,omitempty"`
	FlowQuotaGB       *int64 `json:"flowQuotaGb,omitempty"`
	SpeedLimitMbps    *int   `json:"speedLimitMbps,omitempty"`
	Port              int    `json:"port,omitempty"`
	HealthStatus      int    `json:"healthStatus,omitempty"`
	BandwidthOverload int    `json:"bandwidthOverloaded,omitempty"`
	LastLatencyMS     *int64 `json:"lastLatencyMs,omitempty"`
	HealthCheckedAt   *int64 `json:"healthCheckedTime,omitempty"`
}

type Tunnel struct {
	ID           int64        `json:"id"`
	Name         string       `json:"name"`
	Type         int          `json:"type"`
	InNodeID     []NodeSpec   `json:"inNodeId"`
	OutNodeID    []NodeSpec   `json:"outNodeId,omitempty"`
	ChainNodes   [][]NodeSpec `json:"chainNodes,omitempty"`
	InIP         string       `json:"inIp"`
	Flow         int          `json:"flow"`
	TrafficRatio float64      `json:"trafficRatio"`
	Status       int          `json:"status"`
	CreatedTime  int64        `json:"createdTime"`
	TOT          TOTConfig    `json:"tot"`
}

type TOTConfig struct {
	Enabled              bool     `json:"enabled"`
	SecretConfigured     bool     `json:"secretConfigured"`
	PathCount            int      `json:"pathCount"`
	Paths                []string `json:"paths"`
	MaxPayload           int      `json:"maxPayload"`
	Window               int      `json:"window"`
	RetransmitIntervalMS int      `json:"retransmitIntervalMs"`
	MaxRetries           int      `json:"maxRetries"`
	RecoveryPeriodMS     int      `json:"recoveryPeriodMs"`
	HandshakeTimeoutMS   int      `json:"handshakeTimeoutMs"`
	MaxClockSkewMS       int      `json:"maxClockSkewMs"`
	IdleTTLMS            int      `json:"idleTtlMs"`
	MPTCP                bool     `json:"mptcp"`
}

type totRuntimeConfig struct {
	Enabled              bool
	Secret               string
	PathCount            int
	PathsJSON            string
	MaxPayload           int
	Window               int
	RetransmitIntervalMS int
	MaxRetries           int
	RecoveryPeriodMS     int
	HandshakeTimeoutMS   int
	MaxClockSkewMS       int
	IdleTTLMS            int
	MPTCP                bool
}

type CreateRequest struct {
	Name         string       `json:"name"`
	Type         int          `json:"type"`
	InNodeID     []NodeSpec   `json:"inNodeId"`
	OutNodeID    []NodeSpec   `json:"outNodeId"`
	ChainNodes   [][]NodeSpec `json:"chainNodes"`
	Flow         int          `json:"flow"`
	TrafficRatio float64      `json:"trafficRatio"`
	InIP         string       `json:"inIp"`
	Status       int          `json:"status"`
	TOT          TOTConfig    `json:"tot"`
}

type UpdateRequest struct {
	CreateRequest
	ID int64 `json:"id"`
}

type UserTunnel struct {
	ID             int64  `json:"id"`
	UserID         int64  `json:"userId"`
	TunnelID       int64  `json:"tunnelId"`
	TunnelName     string `json:"tunnelName"`
	Status         int    `json:"status"`
	Flow           int64  `json:"flow"`
	InFlow         int64  `json:"inFlow"`
	OutFlow        int64  `json:"outFlow"`
	Num            int    `json:"num"`
	ExpTime        int64  `json:"expTime"`
	FlowResetTime  int    `json:"flowResetTime"`
	SpeedID        *int64 `json:"speedId,omitempty"`
	SpeedLimitName string `json:"speedLimitName,omitempty"`
	TunnelFlow     int    `json:"tunnelFlow"`
}

type AssignRequest struct {
	UserID        int64  `json:"userId"`
	TunnelID      int64  `json:"tunnelId"`
	Flow          int64  `json:"flow"`
	Num           int    `json:"num"`
	ExpTime       int64  `json:"expTime"`
	FlowResetTime int    `json:"flowResetTime"`
	SpeedID       *int64 `json:"speedId"`
}

type UpdateUserTunnelRequest struct {
	ID            int64  `json:"id"`
	Flow          int64  `json:"flow"`
	Num           int    `json:"num"`
	ExpTime       *int64 `json:"expTime"`
	FlowResetTime *int   `json:"flowResetTime"`
	SpeedID       *int64 `json:"speedId"`
	Status        *int   `json:"status"`
}

type Repository struct {
	db    *sql.DB
	nodes *nodes.Repository
}

func NewRepository(db *sql.DB, nodeRepo *nodes.Repository) *Repository {
	return &Repository{db: db, nodes: nodeRepo}
}

func (r *Repository) List(ctx context.Context) ([]Tunnel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,type,flow,traffic_ratio,in_ip,status,created_at,tot_enabled,tot_secret,tot_path_count,tot_paths,tot_max_payload,tot_window,tot_retransmit_interval_ms,tot_max_retries,tot_recovery_period_ms,tot_handshake_timeout_ms,tot_max_clock_skew_ms,tot_idle_ttl_ms,tot_mptcp FROM tunnels ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list tunnels: %w", err)
	}
	defer rows.Close()
	result := make([]Tunnel, 0)
	for rows.Next() {
		var tunnel Tunnel
		var raw totRuntimeConfig
		if err := rows.Scan(&tunnel.ID, &tunnel.Name, &tunnel.Type, &tunnel.Flow, &tunnel.TrafficRatio, &tunnel.InIP, &tunnel.Status, &tunnel.CreatedTime,
			&raw.Enabled, &raw.Secret, &raw.PathCount, &raw.PathsJSON, &raw.MaxPayload, &raw.Window, &raw.RetransmitIntervalMS, &raw.MaxRetries, &raw.RecoveryPeriodMS, &raw.HandshakeTimeoutMS, &raw.MaxClockSkewMS, &raw.IdleTTLMS, &raw.MPTCP); err != nil {
			return nil, fmt.Errorf("scan tunnel: %w", err)
		}
		tunnel.TOT, err = raw.public()
		if err != nil {
			return nil, fmt.Errorf("decode tunnel %d TOT paths: %w", tunnel.ID, err)
		}
		if err := r.loadNodes(ctx, &tunnel); err != nil {
			return nil, err
		}
		result = append(result, tunnel)
	}
	return result, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id int64) (Tunnel, error) {
	var tunnel Tunnel
	var raw totRuntimeConfig
	err := r.db.QueryRowContext(ctx, `SELECT id,name,type,flow,traffic_ratio,in_ip,status,created_at,tot_enabled,tot_secret,tot_path_count,tot_paths,tot_max_payload,tot_window,tot_retransmit_interval_ms,tot_max_retries,tot_recovery_period_ms,tot_handshake_timeout_ms,tot_max_clock_skew_ms,tot_idle_ttl_ms,tot_mptcp FROM tunnels WHERE id=?`, id).Scan(
		&tunnel.ID, &tunnel.Name, &tunnel.Type, &tunnel.Flow, &tunnel.TrafficRatio, &tunnel.InIP, &tunnel.Status, &tunnel.CreatedTime,
		&raw.Enabled, &raw.Secret, &raw.PathCount, &raw.PathsJSON, &raw.MaxPayload, &raw.Window, &raw.RetransmitIntervalMS, &raw.MaxRetries, &raw.RecoveryPeriodMS, &raw.HandshakeTimeoutMS, &raw.MaxClockSkewMS, &raw.IdleTTLMS, &raw.MPTCP,
	)
	if err != nil {
		return tunnel, err
	}
	tunnel.TOT, err = raw.public()
	if err != nil {
		return tunnel, fmt.Errorf("decode tunnel %d TOT paths: %w", tunnel.ID, err)
	}
	return tunnel, r.loadNodes(ctx, &tunnel)
}

func (r *Repository) Create(ctx context.Context, request CreateRequest) (int64, error) {
	if err := validate(request); err != nil {
		return 0, err
	}
	if err := r.validateNodes(ctx, request); err != nil {
		return 0, err
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin tunnel create: %w", err)
	}
	defer transaction.Rollback()
	inIP := strings.TrimSpace(request.InIP)
	if inIP == "" {
		inIP, err = r.buildNodeAddresses(ctx, request.InNodeID)
		if err != nil {
			return 0, err
		}
	}
	tot := normalizeTOT(request.TOT)
	pathsJSON, err := json.Marshal(tot.Paths)
	if err != nil {
		return 0, fmt.Errorf("encode TOT paths: %w", err)
	}
	secret, err := newTOTSecret()
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	result, err := transaction.ExecContext(ctx, `INSERT INTO tunnels(name,type,flow,traffic_ratio,in_ip,status,tot_enabled,tot_secret,tot_path_count,tot_paths,tot_max_payload,tot_window,tot_retransmit_interval_ms,tot_max_retries,tot_recovery_period_ms,tot_handshake_timeout_ms,tot_max_clock_skew_ms,tot_idle_ttl_ms,tot_mptcp,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		strings.TrimSpace(request.Name), request.Type, request.Flow, request.TrafficRatio, inIP, normalizeStatus(request.Status), boolInt(tot.Enabled), secret, tot.PathCount, string(pathsJSON), tot.MaxPayload, tot.Window, tot.RetransmitIntervalMS, tot.MaxRetries, tot.RecoveryPeriodMS, tot.HandshakeTimeoutMS, tot.MaxClockSkewMS, tot.IdleTTLMS, boolInt(tot.MPTCP), now, now)
	if err != nil {
		return 0, fmt.Errorf("create tunnel: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := insertNodes(ctx, transaction, id, request); err != nil {
		return 0, err
	}
	if err := transaction.Commit(); err != nil {
		return 0, fmt.Errorf("commit tunnel create: %w", err)
	}
	return id, nil
}

func (r *Repository) Update(ctx context.Context, request UpdateRequest) error {
	if request.ID <= 0 {
		return errors.New("tunnel id must be positive")
	}
	if err := validate(request.CreateRequest); err != nil {
		return err
	}
	if err := r.validateNodes(ctx, request.CreateRequest); err != nil {
		return err
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	inIP := strings.TrimSpace(request.InIP)
	if inIP == "" {
		inIP, err = r.buildNodeAddresses(ctx, request.InNodeID)
		if err != nil {
			return err
		}
	}
	tot := normalizeTOT(request.TOT)
	pathsJSON, err := json.Marshal(tot.Paths)
	if err != nil {
		return fmt.Errorf("encode TOT paths: %w", err)
	}
	result, err := transaction.ExecContext(ctx, `UPDATE tunnels SET name=?,type=?,flow=?,traffic_ratio=?,in_ip=?,status=?,tot_enabled=?,tot_path_count=?,tot_paths=?,tot_max_payload=?,tot_window=?,tot_retransmit_interval_ms=?,tot_max_retries=?,tot_recovery_period_ms=?,tot_handshake_timeout_ms=?,tot_max_clock_skew_ms=?,tot_idle_ttl_ms=?,tot_mptcp=?,updated_at=? WHERE id=?`,
		strings.TrimSpace(request.Name), request.Type, request.Flow, request.TrafficRatio, inIP, normalizeStatus(request.Status), boolInt(tot.Enabled), tot.PathCount, string(pathsJSON), tot.MaxPayload, tot.Window, tot.RetransmitIntervalMS, tot.MaxRetries, tot.RecoveryPeriodMS, tot.HandshakeTimeoutMS, tot.MaxClockSkewMS, tot.IdleTTLMS, boolInt(tot.MPTCP), time.Now().UnixMilli(), request.ID)
	if err != nil {
		return fmt.Errorf("update tunnel: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	if err := syncNodes(ctx, transaction, request.ID, request.CreateRequest); err != nil {
		return err
	}
	return transaction.Commit()
}

func (r *Repository) RotateTOTSecret(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("tunnel id must be positive")
	}
	secret, err := newTOTSecret()
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE tunnels SET tot_secret=?,updated_at=? WHERE id=?`, secret, time.Now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("rotate TOT secret: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	_, err := r.DeleteWithName(ctx, id)
	return err
}

func (r *Repository) DeleteWithName(ctx context.Context, id int64) (string, error) {
	if id <= 0 {
		return "", errors.New("tunnel id must be positive")
	}
	var name string
	if err := r.db.QueryRowContext(ctx, "SELECT name FROM tunnels WHERE id=?", id).Scan(&name); err != nil {
		return "", err
	}
	result, err := r.db.ExecContext(ctx, "DELETE FROM tunnels WHERE id=?", id)
	if err != nil {
		return name, fmt.Errorf("delete tunnel: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil {
		return name, err
	} else if count == 0 {
		return name, sql.ErrNoRows
	}
	return name, nil
}

func (r *Repository) UserTunnelList(ctx context.Context, userID int64) ([]UserTunnel, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT ut.id,ut.user_id,ut.tunnel_id,t.name,ut.status,ut.flow_quota_bytes,ut.ingress_bytes,ut.egress_bytes,ut.forward_quota,ut.expires_at,ut.flow_reset_day,ut.speed_limit_id,COALESCE(sl.name,''),t.flow FROM user_tunnels ut JOIN tunnels t ON t.id=ut.tunnel_id LEFT JOIN speed_limits sl ON sl.id=ut.speed_limit_id WHERE ut.user_id=? ORDER BY ut.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]UserTunnel, 0)
	for rows.Next() {
		var permission UserTunnel
		if err := rows.Scan(&permission.ID, &permission.UserID, &permission.TunnelID, &permission.TunnelName, &permission.Status, &permission.Flow, &permission.InFlow, &permission.OutFlow, &permission.Num, &permission.ExpTime, &permission.FlowResetTime, &permission.SpeedID, &permission.SpeedLimitName, &permission.TunnelFlow); err != nil {
			return nil, err
		}
		permission.Flow /= bytesPerGB
		result = append(result, permission)
	}
	return result, rows.Err()
}

func (r *Repository) Assign(ctx context.Context, request AssignRequest) (int64, error) {
	if request.UserID <= 0 || request.TunnelID <= 0 || request.Flow < 0 || request.Flow > maxQuotaGB || request.Num < 0 {
		return 0, errors.New("invalid user tunnel assignment")
	}
	if err := r.validateSpeedLimit(ctx, request.SpeedID, request.TunnelID); err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	result, err := r.db.ExecContext(ctx, `INSERT INTO user_tunnels(user_id,tunnel_id,flow_quota_bytes,forward_quota,flow_reset_day,expires_at,speed_limit_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		request.UserID, request.TunnelID, request.Flow*bytesPerGB, request.Num, request.FlowResetTime, request.ExpTime, request.SpeedID, 1, now, now)
	if err != nil {
		return 0, fmt.Errorf("assign tunnel: %w", err)
	}
	return result.LastInsertId()
}

func (r *Repository) UpdateUserTunnel(ctx context.Context, request UpdateUserTunnelRequest) (int64, error) {
	if request.ID <= 0 || request.Flow < 0 || request.Flow > maxQuotaGB || request.Num < 0 {
		return 0, errors.New("invalid user tunnel update")
	}
	if request.Status != nil && *request.Status != 0 && *request.Status != 1 {
		return 0, errors.New("invalid user tunnel status")
	}
	var tunnelID int64
	err := r.db.QueryRowContext(ctx, `UPDATE user_tunnels SET flow_quota_bytes=?,forward_quota=?,expires_at=COALESCE(?,expires_at),flow_reset_day=COALESCE(?,flow_reset_day),speed_limit_id=?,status=COALESCE(?,status),updated_at=? WHERE id=? AND (CAST(? AS INTEGER) IS NULL OR EXISTS (SELECT 1 FROM speed_limits WHERE id=CAST(? AS INTEGER) AND tunnel_id=user_tunnels.tunnel_id AND status=1)) RETURNING tunnel_id`,
		request.Flow*bytesPerGB, request.Num, request.ExpTime, request.FlowResetTime, request.SpeedID, request.Status, time.Now().UnixMilli(), request.ID, request.SpeedID, request.SpeedID).Scan(&tunnelID)
	if errors.Is(err, sql.ErrNoRows) {
		var exists int
		if checkErr := r.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM user_tunnels WHERE id=?", request.ID).Scan(&exists); checkErr != nil {
			return 0, checkErr
		}
		if exists == 0 {
			return 0, sql.ErrNoRows
		}
		return 0, errors.New("speed limit does not belong to tunnel or is disabled")
	}
	if err != nil {
		return 0, err
	}
	return tunnelID, nil
}

func (r *Repository) Remove(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM user_tunnels WHERE id=?", id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) UserChoices(ctx context.Context, userID int64) ([]map[string]any, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT t.id,t.name,t.status FROM tunnels t JOIN user_tunnels ut ON ut.tunnel_id=t.id WHERE ut.user_id=? AND ut.status=1 AND t.status=1 ORDER BY t.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]map[string]any, 0)
	for rows.Next() {
		var id int64
		var name string
		var status int
		if err := rows.Scan(&id, &name, &status); err != nil {
			return nil, err
		}
		result = append(result, map[string]any{"id": id, "name": name, "status": status})
	}
	return result, rows.Err()
}

func (r *Repository) validateSpeedLimit(ctx context.Context, speedID *int64, tunnelID int64) error {
	if speedID == nil {
		return nil
	}
	if *speedID <= 0 {
		return errors.New("invalid speed limit")
	}
	var status int
	if err := r.db.QueryRowContext(ctx, "SELECT status FROM speed_limits WHERE id=? AND tunnel_id=?", *speedID, tunnelID).Scan(&status); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("speed limit does not belong to tunnel")
		}
		return err
	}
	if status != 1 {
		return errors.New("speed limit is disabled")
	}
	return nil
}

func (r *Repository) validateNodes(ctx context.Context, request CreateRequest) error {
	if r.nodes == nil {
		return errors.New("node repository is unavailable")
	}
	all := append([]NodeSpec{}, request.InNodeID...)
	for _, group := range request.ChainNodes {
		all = append(all, group...)
	}
	all = append(all, request.OutNodeID...)
	for _, spec := range all {
		node, err := r.nodes.Get(ctx, spec.NodeID)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("node %d does not exist", spec.NodeID)
			}
			return fmt.Errorf("load node %d: %w", spec.NodeID, err)
		}
		if node.Status != 1 {
			return fmt.Errorf("node %d is offline", spec.NodeID)
		}
	}
	return nil
}

func (r *Repository) buildNodeAddresses(ctx context.Context, specs []NodeSpec) (string, error) {
	addresses := make([]string, 0, len(specs))
	for _, spec := range specs {
		node, err := r.nodes.Get(ctx, spec.NodeID)
		if err != nil {
			return "", fmt.Errorf("load node address %d: %w", spec.NodeID, err)
		}
		addresses = append(addresses, node.ServerIP)
	}
	return strings.Join(addresses, ","), nil
}

func validate(request CreateRequest) error {
	if strings.TrimSpace(request.Name) == "" || len([]rune(request.Name)) > 100 {
		return errors.New("tunnel name is required and must be at most 100 characters")
	}
	if request.Type != 1 && request.Type != 2 {
		return errors.New("invalid tunnel type")
	}
	if request.Flow != 1 && request.Flow != 2 {
		return errors.New("invalid flow mode")
	}
	if request.TrafficRatio < 0 || request.TrafficRatio > 100 {
		return errors.New("traffic ratio must be between 0 and 100")
	}
	if len(request.InNodeID) == 0 {
		return errors.New("at least one entry node is required")
	}
	if request.Type == 2 && len(request.OutNodeID) == 0 {
		return errors.New("at least one exit node is required")
	}
	if request.TOT.Enabled {
		if request.Type != 2 {
			return errors.New("TOT is only supported by chained tunnels")
		}
		if len(request.TOT.Paths) > 16 {
			return errors.New("TOT supports at most 16 explicit paths")
		}
		tot := normalizeTOT(request.TOT)
		if tot.PathCount < 1 || tot.PathCount > 16 {
			return errors.New("TOT path count must be between 1 and 16")
		}
		if len(tot.Paths) > 16 {
			return errors.New("TOT supports at most 16 explicit paths")
		}
		if (len(tot.Paths) > 0 || tot.MPTCP) && (len(request.ChainNodes) > 0 || len(request.OutNodeID) != 1) {
			return errors.New("explicit TOT paths and MPTCP require one exit node and no relay hops")
		}
		for _, address := range tot.Paths {
			host, portValue, err := net.SplitHostPort(address)
			if err != nil || strings.TrimSpace(host) == "" {
				return fmt.Errorf("invalid TOT path %q: expected host:port", address)
			}
			port, err := strconv.Atoi(portValue)
			if err != nil || port < 1 || port > 65535 {
				return fmt.Errorf("invalid TOT path %q: port must be between 1 and 65535", address)
			}
		}
		if tot.MaxPayload < 1024 || tot.MaxPayload > 1024*1024 {
			return errors.New("TOT maximum payload must be between 1024 and 1048576 bytes")
		}
		if tot.Window < 1 || tot.Window > 65536 {
			return errors.New("TOT window must be between 1 and 65536 frames")
		}
		if tot.RetransmitIntervalMS < 10 || tot.RetransmitIntervalMS > 60000 || tot.MaxRetries < 1 || tot.MaxRetries > 1000 {
			return errors.New("invalid TOT retransmission settings")
		}
		if tot.RecoveryPeriodMS < 100 || tot.RecoveryPeriodMS > 300000 || tot.HandshakeTimeoutMS < 100 || tot.HandshakeTimeoutMS > 60000 {
			return errors.New("invalid TOT recovery or handshake timeout")
		}
		if tot.MaxClockSkewMS < 1000 || tot.MaxClockSkewMS > 300000 || tot.IdleTTLMS < 1000 || tot.IdleTTLMS > 86400000 {
			return errors.New("invalid TOT clock skew or idle timeout")
		}
	}
	seen := map[int64]bool{}
	all := append([]NodeSpec{}, request.InNodeID...)
	for _, group := range request.ChainNodes {
		if len(group) == 0 {
			return errors.New("empty chain hop")
		}
		all = append(all, group...)
	}
	all = append(all, request.OutNodeID...)
	for _, spec := range all {
		if spec.NodeID <= 0 || seen[spec.NodeID] {
			return errors.New("tunnel nodes must be unique and positive")
		}
		seen[spec.NodeID] = true
		if spec.FlowQuotaGB != nil && (*spec.FlowQuotaGB < 0 || *spec.FlowQuotaGB > maxQuotaGB) {
			return errors.New("flow quota is outside the supported range")
		}
		if spec.SpeedLimitMbps != nil && *spec.SpeedLimitMbps < 0 {
			return errors.New("speed limit cannot be negative")
		}
	}
	return nil
}

func syncNodes(ctx context.Context, transaction *sql.Tx, tunnelID int64, request CreateRequest) error {
	keep := make([]int64, 0, len(request.InNodeID)+len(request.OutNodeID))
	all := []struct {
		kind int
		hop  int
		spec NodeSpec
	}{}
	for _, spec := range request.InNodeID {
		all = append(all, struct {
			kind int
			hop  int
			spec NodeSpec
		}{1, 0, spec})
	}
	for index, group := range request.ChainNodes {
		for _, spec := range group {
			all = append(all, struct {
				kind int
				hop  int
				spec NodeSpec
			}{2, index + 1, spec})
		}
	}
	for _, spec := range request.OutNodeID {
		all = append(all, struct {
			kind int
			hop  int
			spec NodeSpec
		}{3, 0, spec})
	}
	for _, item := range all {
		quota := int64(0)
		if item.spec.FlowQuotaGB != nil {
			quota = *item.spec.FlowQuotaGB * bytesPerGB
		}
		speed := 0
		if item.spec.SpeedLimitMbps != nil {
			speed = *item.spec.SpeedLimitMbps
		}
		result, err := transaction.ExecContext(ctx, `INSERT INTO tunnel_nodes(tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol,flow_quota_bytes,speed_limit_mbps) VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(tunnel_id,node_id) DO UPDATE SET chain_type=excluded.chain_type,port=excluded.port,strategy=excluded.strategy,hop_index=excluded.hop_index,protocol=excluded.protocol,flow_quota_bytes=excluded.flow_quota_bytes,speed_limit_mbps=excluded.speed_limit_mbps WHERE tunnel_nodes.group_binding_id IS NULL`, tunnelID, item.kind, item.spec.NodeID, item.spec.Port, item.spec.Strategy, item.hop, normalizeProtocol(item.spec.Protocol), quota, speed)
		if err != nil {
			return err
		}
		if count, _ := result.RowsAffected(); count == 0 {
			return fmt.Errorf("node %d is managed by a node group binding", item.spec.NodeID)
		}
		keep = append(keep, item.spec.NodeID)
	}
	query := "DELETE FROM tunnel_nodes WHERE tunnel_id=? AND group_binding_id IS NULL"
	args := []any{tunnelID}
	if len(keep) > 0 {
		query += " AND node_id NOT IN (" + strings.TrimRight(strings.Repeat("?,", len(keep)), ",") + ")"
		for _, id := range keep {
			args = append(args, id)
		}
	}
	_, err := transaction.ExecContext(ctx, query, args...)
	return err
}

func insertNodes(ctx context.Context, transaction *sql.Tx, tunnelID int64, request CreateRequest) error {
	for _, spec := range request.InNodeID {
		if err := insertNode(ctx, transaction, tunnelID, 1, 0, spec); err != nil {
			return err
		}
	}
	for index, group := range request.ChainNodes {
		for _, spec := range group {
			if err := insertNode(ctx, transaction, tunnelID, 2, index+1, spec); err != nil {
				return err
			}
		}
	}
	for _, spec := range request.OutNodeID {
		if err := insertNode(ctx, transaction, tunnelID, 3, 0, spec); err != nil {
			return err
		}
	}
	return nil
}

func insertNode(ctx context.Context, transaction *sql.Tx, tunnelID int64, kind, hop int, spec NodeSpec) error {
	quota := int64(0)
	if spec.FlowQuotaGB != nil {
		quota = *spec.FlowQuotaGB * bytesPerGB
	}
	speed := 0
	if spec.SpeedLimitMbps != nil {
		speed = *spec.SpeedLimitMbps
	}
	_, err := transaction.ExecContext(ctx, `INSERT INTO tunnel_nodes(tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol,flow_quota_bytes,speed_limit_mbps) VALUES(?,?,?,?,?,?,?,?,?)`,
		tunnelID, kind, spec.NodeID, spec.Port, spec.Strategy, hop, normalizeProtocol(spec.Protocol), quota, speed)
	return err
}

func (r *Repository) loadNodes(ctx context.Context, tunnel *Tunnel) error {
	rows, err := r.db.QueryContext(ctx, `SELECT chain_type,node_id,port,strategy,hop_index,protocol,flow_quota_bytes,speed_limit_mbps,health_status,bandwidth_overloaded,last_latency_ms,health_checked_at FROM tunnel_nodes WHERE tunnel_id=? ORDER BY chain_type,hop_index,id`, tunnel.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	tunnel.InNodeID = []NodeSpec{}
	tunnel.OutNodeID = []NodeSpec{}
	tunnel.ChainNodes = [][]NodeSpec{}
	for rows.Next() {
		var kind, nodeID, port, hop, speed, health, overloaded int
		var strategy, protocol string
		var quota, latency, checked sql.NullInt64
		if err := rows.Scan(&kind, &nodeID, &port, &strategy, &hop, &protocol, &quota, &speed, &health, &overloaded, &latency, &checked); err != nil {
			return err
		}
		spec := NodeSpec{NodeID: int64(nodeID), Port: port, Strategy: strategy, ChainType: kind, Inx: hop, Protocol: protocol, HealthStatus: health, BandwidthOverload: overloaded}
		if quota.Valid && quota.Int64 > 0 {
			value := quota.Int64 / bytesPerGB
			spec.FlowQuotaGB = &value
		}
		if speed > 0 {
			spec.SpeedLimitMbps = &speed
		}
		if latency.Valid {
			spec.LastLatencyMS = &latency.Int64
		}
		if checked.Valid {
			spec.HealthCheckedAt = &checked.Int64
		}
		switch kind {
		case 1:
			tunnel.InNodeID = append(tunnel.InNodeID, spec)
		case 2:
			for len(tunnel.ChainNodes) < hop {
				tunnel.ChainNodes = append(tunnel.ChainNodes, []NodeSpec{})
			}
			tunnel.ChainNodes[hop-1] = append(tunnel.ChainNodes[hop-1], spec)
		case 3:
			tunnel.OutNodeID = append(tunnel.OutNodeID, spec)
		}
	}
	return rows.Err()
}

func (raw totRuntimeConfig) public() (TOTConfig, error) {
	paths := make([]string, 0)
	if strings.TrimSpace(raw.PathsJSON) != "" {
		if err := json.Unmarshal([]byte(raw.PathsJSON), &paths); err != nil {
			return TOTConfig{}, err
		}
	}
	return TOTConfig{
		Enabled:              raw.Enabled,
		SecretConfigured:     strings.TrimSpace(raw.Secret) != "",
		PathCount:            raw.PathCount,
		Paths:                paths,
		MaxPayload:           raw.MaxPayload,
		Window:               raw.Window,
		RetransmitIntervalMS: raw.RetransmitIntervalMS,
		MaxRetries:           raw.MaxRetries,
		RecoveryPeriodMS:     raw.RecoveryPeriodMS,
		HandshakeTimeoutMS:   raw.HandshakeTimeoutMS,
		MaxClockSkewMS:       raw.MaxClockSkewMS,
		IdleTTLMS:            raw.IdleTTLMS,
		MPTCP:                raw.MPTCP,
	}, nil
}

func normalizeTOT(value TOTConfig) TOTConfig {
	paths := make([]string, 0, len(value.Paths))
	seen := make(map[string]struct{}, len(value.Paths))
	for _, raw := range value.Paths {
		address := strings.TrimSpace(raw)
		if address == "" {
			continue
		}
		if host, port, err := net.SplitHostPort(address); err == nil {
			address = net.JoinHostPort(strings.TrimSpace(host), port)
		}
		if _, exists := seen[address]; exists {
			continue
		}
		seen[address] = struct{}{}
		paths = append(paths, address)
	}
	value.Paths = paths
	if len(paths) > 0 {
		value.PathCount = len(paths)
	} else if value.PathCount == 0 {
		value.PathCount = defaultTOTPathCount
	}
	if value.MaxPayload == 0 {
		value.MaxPayload = defaultTOTMaxPayload
	}
	if value.Window == 0 {
		value.Window = defaultTOTWindow
	}
	if value.RetransmitIntervalMS == 0 {
		value.RetransmitIntervalMS = defaultTOTRetransmitIntervalMS
	}
	if value.MaxRetries == 0 {
		value.MaxRetries = defaultTOTMaxRetries
	}
	if value.RecoveryPeriodMS == 0 {
		value.RecoveryPeriodMS = defaultTOTRecoveryPeriodMS
	}
	if value.HandshakeTimeoutMS == 0 {
		value.HandshakeTimeoutMS = defaultTOTHandshakeTimeoutMS
	}
	if value.MaxClockSkewMS == 0 {
		value.MaxClockSkewMS = defaultTOTMaxClockSkewMS
	}
	if value.IdleTTLMS == 0 {
		value.IdleTTLMS = defaultTOTIdleTTLMS
	}
	return value
}

func newTOTSecret() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate TOT secret: %w", err)
	}
	return hex.EncodeToString(value), nil
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func normalizeStatus(value int) int {
	if value == 0 {
		return 0
	}
	return 1
}

func normalizeProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return "tcp"
	}
	if value == "mtcp" {
		return "mptcp"
	}
	return value
}
