package runtimecontrols

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"
)

type Endpoint struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Address   string `json:"address"`
	Priority  int    `json:"priority"`
	Backup    int    `json:"backup"`
	Status    int    `json:"status"`
	SortIndex int    `json:"sortIndex"`
}

type EndpointGroup struct {
	ID              int64      `json:"id"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Strategy        string     `json:"strategy"`
	MaxFails        int        `json:"maxFails"`
	FailTimeoutMS   int64      `json:"failTimeoutMs"`
	ProbeIntervalMS int64      `json:"probeIntervalMs"`
	ProbeTimeoutMS  int64      `json:"probeTimeoutMs"`
	Status          int        `json:"status"`
	CreatedTime     int64      `json:"createdTime"`
	Endpoints       []Endpoint `json:"endpoints"`
}

type EndpointGroupRequest struct {
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	Strategy        string     `json:"strategy"`
	MaxFails        int        `json:"maxFails"`
	FailTimeoutMS   int64      `json:"failTimeoutMs"`
	ProbeIntervalMS int64      `json:"probeIntervalMs"`
	ProbeTimeoutMS  int64      `json:"probeTimeoutMs"`
	Status          int        `json:"status"`
	Endpoints       []Endpoint `json:"endpoints"`
}

type UpdateEndpointGroupRequest struct {
	EndpointGroupRequest
	ID int64 `json:"id"`
}

type RouteRule struct {
	ID             int64   `json:"id"`
	Name           string  `json:"name"`
	MatchType      string  `json:"matchType"`
	Value          string  `json:"value"`
	SecondaryValue string  `json:"secondaryValue"`
	Negate         int     `json:"negate"`
	Priority       int     `json:"priority"`
	Status         int     `json:"status"`
	SortIndex      int     `json:"sortIndex"`
	EndpointIDs    []int64 `json:"endpointIds"`
}

type RouteRuleSet struct {
	ID          int64       `json:"id"`
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Status      int         `json:"status"`
	CreatedTime int64       `json:"createdTime"`
	Rules       []RouteRule `json:"rules"`
}

type RouteRuleSetRequest struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	Status      int         `json:"status"`
	Rules       []RouteRule `json:"rules"`
}

type UpdateRouteRuleSetRequest struct {
	RouteRuleSetRequest
	ID int64 `json:"id"`
}

type NodeGroupMember struct {
	NodeID    int64 `json:"nodeId"`
	Priority  int   `json:"priority"`
	Backup    int   `json:"backup"`
	SortIndex int   `json:"sortIndex"`
}

type NodeGroup struct {
	ID          int64             `json:"id"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Strategy    string            `json:"strategy"`
	MaxFails    int               `json:"maxFails"`
	FailTimeout int64             `json:"failTimeoutMs"`
	Status      int               `json:"status"`
	CreatedTime int64             `json:"createdTime"`
	Members     []NodeGroupMember `json:"members"`
}

type NodeGroupRequest struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Strategy    string            `json:"strategy"`
	MaxFails    int               `json:"maxFails"`
	FailTimeout int64             `json:"failTimeoutMs"`
	Status      int               `json:"status"`
	Members     []NodeGroupMember `json:"members"`
}

type UpdateNodeGroupRequest struct {
	NodeGroupRequest
	ID int64 `json:"id"`
}

type TunnelNodeGroupBinding struct {
	ID             int64  `json:"id"`
	TunnelID       int64  `json:"tunnelId"`
	GroupID        int64  `json:"groupId"`
	ChainType      int    `json:"chainType"`
	Port           int    `json:"port"`
	Strategy       string `json:"strategy"`
	HopIndex       int    `json:"hopIndex"`
	Protocol       string `json:"protocol"`
	FlowQuotaBytes int64  `json:"flowQuotaBytes"`
	SpeedLimitMbps int    `json:"speedLimitMbps"`
}

type TunnelNodeGroupBindingRequest struct {
	TunnelID       int64  `json:"tunnelId"`
	GroupID        int64  `json:"groupId"`
	ChainType      int    `json:"chainType"`
	Port           int    `json:"port"`
	Strategy       string `json:"strategy"`
	HopIndex       int    `json:"hopIndex"`
	Protocol       string `json:"protocol"`
	FlowQuotaBytes int64  `json:"flowQuotaBytes"`
	SpeedLimitMbps int    `json:"speedLimitMbps"`
}

type UpdateTunnelNodeGroupBindingRequest struct {
	TunnelNodeGroupBindingRequest
	ID int64 `json:"id"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ListNodeGroups(ctx context.Context) ([]NodeGroup, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,description,strategy,max_fails,fail_timeout_ms,status,created_at FROM node_groups ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list node groups: %w", err)
	}
	defer rows.Close()
	groups := make([]NodeGroup, 0)
	for rows.Next() {
		var group NodeGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &group.Strategy, &group.MaxFails, &group.FailTimeout, &group.Status, &group.CreatedTime); err != nil {
			return nil, err
		}
		group.Members, err = loadNodeGroupMembers(ctx, r.db, group.ID)
		if err != nil {
			return nil, err
		}
		groups = append(groups, group)
	}
	return groups, rows.Err()
}

func (r *Repository) CreateNodeGroup(ctx context.Context, request NodeGroupRequest) (int64, error) {
	request = normalizeNodeGroup(request)
	if err := validateNodeGroup(request); err != nil {
		return 0, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := validateNodeGroupMembers(ctx, tx, request.Members); err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `INSERT INTO node_groups(name,description,strategy,max_fails,fail_timeout_ms,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?)`, request.Name, request.Description, request.Strategy, request.MaxFails, request.FailTimeout, request.Status, now, now)
	if err != nil {
		return 0, fmt.Errorf("create node group: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := replaceNodeGroupMembers(ctx, tx, id, request.Members); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (r *Repository) UpdateNodeGroup(ctx context.Context, request UpdateNodeGroupRequest) error {
	if request.ID <= 0 {
		return errors.New("node group id must be positive")
	}
	request.NodeGroupRequest = normalizeNodeGroup(request.NodeGroupRequest)
	if err := validateNodeGroup(request.NodeGroupRequest); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := validateNodeGroupMembers(ctx, tx, request.Members); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE node_groups SET name=?,description=?,strategy=?,max_fails=?,fail_timeout_ms=?,status=?,updated_at=? WHERE id=?`, request.Name, request.Description, request.Strategy, request.MaxFails, request.FailTimeout, request.Status, time.Now().UnixMilli(), request.ID)
	if err != nil {
		return fmt.Errorf("update node group: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	if err := replaceNodeGroupMembers(ctx, tx, request.ID, request.Members); err != nil {
		return err
	}
	if err := syncBindingsForGroup(ctx, tx, request.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) DeleteNodeGroup(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("node group id must be positive")
	}
	var references int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM tunnel_node_group_bindings WHERE group_id=?`, id).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return errors.New("node group is assigned to tunnels")
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM node_groups WHERE id=?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) ListTunnelNodeGroupBindings(ctx context.Context) ([]TunnelNodeGroupBinding, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,tunnel_id,group_id,chain_type,port,strategy,hop_index,protocol,flow_quota_bytes,speed_limit_mbps FROM tunnel_node_group_bindings ORDER BY tunnel_id,chain_type,hop_index,id`)
	if err != nil {
		return nil, fmt.Errorf("list tunnel node group bindings: %w", err)
	}
	defer rows.Close()
	bindings := make([]TunnelNodeGroupBinding, 0)
	for rows.Next() {
		var binding TunnelNodeGroupBinding
		if err := rows.Scan(&binding.ID, &binding.TunnelID, &binding.GroupID, &binding.ChainType, &binding.Port, &binding.Strategy, &binding.HopIndex, &binding.Protocol, &binding.FlowQuotaBytes, &binding.SpeedLimitMbps); err != nil {
			return nil, err
		}
		bindings = append(bindings, binding)
	}
	return bindings, rows.Err()
}

func (r *Repository) CreateTunnelNodeGroupBinding(ctx context.Context, request TunnelNodeGroupBindingRequest) (int64, error) {
	request = normalizeBinding(request)
	if err := validateBinding(request); err != nil {
		return 0, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if err := validateBindingReferences(ctx, tx, request, 0); err != nil {
		return 0, err
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO tunnel_node_group_bindings(tunnel_id,group_id,chain_type,port,strategy,hop_index,protocol,flow_quota_bytes,speed_limit_mbps) VALUES(?,?,?,?,?,?,?,?,?)`, request.TunnelID, request.GroupID, request.ChainType, request.Port, request.Strategy, request.HopIndex, request.Protocol, request.FlowQuotaBytes, request.SpeedLimitMbps)
	if err != nil {
		return 0, fmt.Errorf("create tunnel node group binding: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := syncBinding(ctx, tx, id); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (r *Repository) UpdateTunnelNodeGroupBinding(ctx context.Context, request UpdateTunnelNodeGroupBindingRequest) error {
	if request.ID <= 0 {
		return errors.New("binding id must be positive")
	}
	request.TunnelNodeGroupBindingRequest = normalizeBinding(request.TunnelNodeGroupBindingRequest)
	if err := validateBinding(request.TunnelNodeGroupBindingRequest); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var oldTunnelID int64
	if err := tx.QueryRowContext(ctx, `SELECT tunnel_id FROM tunnel_node_group_bindings WHERE id=?`, request.ID).Scan(&oldTunnelID); err != nil {
		return err
	}
	if err := validateBindingReferences(ctx, tx, request.TunnelNodeGroupBindingRequest, request.ID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tunnel_nodes WHERE group_binding_id=?`, request.ID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `UPDATE tunnel_node_group_bindings SET tunnel_id=?,group_id=?,chain_type=?,port=?,strategy=?,hop_index=?,protocol=?,flow_quota_bytes=?,speed_limit_mbps=? WHERE id=?`, request.TunnelID, request.GroupID, request.ChainType, request.Port, request.Strategy, request.HopIndex, request.Protocol, request.FlowQuotaBytes, request.SpeedLimitMbps, request.ID)
	if err != nil {
		return fmt.Errorf("update tunnel node group binding: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	if err := syncBinding(ctx, tx, request.ID); err != nil {
		return err
	}
	if oldTunnelID != request.TunnelID {
		if err := syncForwardPorts(ctx, tx, oldTunnelID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) DeleteTunnelNodeGroupBinding(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("binding id must be positive")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var tunnelID int64
	if err := tx.QueryRowContext(ctx, `SELECT tunnel_id FROM tunnel_node_group_bindings WHERE id=?`, id).Scan(&tunnelID); err != nil {
		return err
	}
	result, err := tx.ExecContext(ctx, `DELETE FROM tunnel_node_group_bindings WHERE id=?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	if err := syncForwardPorts(ctx, tx, tunnelID); err != nil {
		return err
	}
	return tx.Commit()
}

func loadNodeGroupMembers(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, groupID int64) ([]NodeGroupMember, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT node_id,priority,backup,sort_index FROM node_group_members WHERE group_id=? ORDER BY sort_index,node_id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	members := make([]NodeGroupMember, 0)
	for rows.Next() {
		var member NodeGroupMember
		if err := rows.Scan(&member.NodeID, &member.Priority, &member.Backup, &member.SortIndex); err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return members, rows.Err()
}

func replaceNodeGroupMembers(ctx context.Context, tx *sql.Tx, groupID int64, members []NodeGroupMember) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM node_group_members WHERE group_id=?`, groupID); err != nil {
		return err
	}
	for _, member := range members {
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_group_members(group_id,node_id,priority,backup,sort_index) VALUES(?,?,?,?,?)`, groupID, member.NodeID, member.Priority, member.Backup, member.SortIndex); err != nil {
			return fmt.Errorf("save node group member: %w", err)
		}
	}
	return nil
}

func syncBindingsForGroup(ctx context.Context, tx *sql.Tx, groupID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM tunnel_node_group_bindings WHERE group_id=? ORDER BY id`, groupID)
	if err != nil {
		return err
	}
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, id := range ids {
		if err := syncBinding(ctx, tx, id); err != nil {
			return err
		}
	}
	return nil
}

func syncBinding(ctx context.Context, tx *sql.Tx, bindingID int64) error {
	var binding TunnelNodeGroupBinding
	var groupStrategy string
	var groupStatus, groupMaxFails int
	var groupFailTimeout int64
	if err := tx.QueryRowContext(ctx, `SELECT b.id,b.tunnel_id,b.group_id,b.chain_type,b.port,b.strategy,b.hop_index,b.protocol,b.flow_quota_bytes,b.speed_limit_mbps,g.strategy,g.status,g.max_fails,g.fail_timeout_ms FROM tunnel_node_group_bindings b JOIN node_groups g ON g.id=b.group_id WHERE b.id=?`, bindingID).Scan(
		&binding.ID, &binding.TunnelID, &binding.GroupID, &binding.ChainType, &binding.Port, &binding.Strategy, &binding.HopIndex, &binding.Protocol, &binding.FlowQuotaBytes, &binding.SpeedLimitMbps, &groupStrategy, &groupStatus, &groupMaxFails, &groupFailTimeout,
	); err != nil {
		return err
	}
	if groupStatus != 1 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM tunnel_nodes WHERE group_binding_id=?`, bindingID); err != nil {
			return err
		}
		return syncForwardPorts(ctx, tx, binding.TunnelID)
	}
	members, err := loadNodeGroupMembers(ctx, tx, binding.GroupID)
	if err != nil {
		return err
	}
	keep := make([]int64, 0, len(members))
	for _, member := range members {
		var conflict int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tunnel_nodes WHERE tunnel_id=? AND node_id=? AND (group_binding_id IS NULL OR group_binding_id<>?)`, binding.TunnelID, member.NodeID, bindingID).Scan(&conflict); err != nil {
			return err
		}
		if conflict > 0 {
			return fmt.Errorf("node %d already belongs to the tunnel topology", member.NodeID)
		}
		strategy := binding.Strategy
		if strategy == "" {
			strategy = groupStrategy
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO tunnel_nodes(tunnel_id,chain_type,node_id,port,strategy,hop_index,protocol,flow_quota_bytes,speed_limit_mbps,group_binding_id,group_priority,group_backup,group_max_fails,group_fail_timeout_ms) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(tunnel_id,node_id) DO UPDATE SET chain_type=excluded.chain_type,port=excluded.port,strategy=excluded.strategy,hop_index=excluded.hop_index,protocol=excluded.protocol,flow_quota_bytes=excluded.flow_quota_bytes,speed_limit_mbps=excluded.speed_limit_mbps,group_binding_id=excluded.group_binding_id,group_priority=excluded.group_priority,group_backup=excluded.group_backup,group_max_fails=excluded.group_max_fails,group_fail_timeout_ms=excluded.group_fail_timeout_ms`, binding.TunnelID, binding.ChainType, member.NodeID, binding.Port, strategy, binding.HopIndex, binding.Protocol, binding.FlowQuotaBytes, binding.SpeedLimitMbps, bindingID, member.Priority, member.Backup, groupMaxFails, groupFailTimeout)
		if err != nil {
			return fmt.Errorf("expand node group binding: %w", err)
		}
		keep = append(keep, member.NodeID)
	}
	query := `DELETE FROM tunnel_nodes WHERE group_binding_id=?`
	args := []any{bindingID}
	if len(keep) > 0 {
		query += ` AND node_id NOT IN (` + strings.TrimRight(strings.Repeat("?,", len(keep)), ",") + `)`
		for _, nodeID := range keep {
			args = append(args, nodeID)
		}
	}
	_, err = tx.ExecContext(ctx, query, args...)
	if err != nil {
		return err
	}
	return syncForwardPorts(ctx, tx, binding.TunnelID)
}

func syncForwardPorts(ctx context.Context, tx *sql.Tx, tunnelID int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM forwards WHERE tunnel_id=?`, tunnelID)
	if err != nil {
		return err
	}
	forwardIDs := make([]int64, 0)
	for rows.Next() {
		var forwardID int64
		if err := rows.Scan(&forwardID); err != nil {
			rows.Close()
			return err
		}
		forwardIDs = append(forwardIDs, forwardID)
	}
	if err := rows.Close(); err != nil {
		return err
	}
	for _, forwardID := range forwardIDs {
		var port int
		if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MIN(port),0) FROM forward_ports WHERE forward_id=?`, forwardID).Scan(&port); err != nil {
			return err
		}
		if port == 0 {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM forward_ports WHERE forward_id=? AND node_id NOT IN (SELECT node_id FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=1)`, forwardID, tunnelID); err != nil {
			return err
		}
		result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO forward_ports(forward_id,node_id,port) SELECT ?,node_id,? FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=1`, forwardID, port, tunnelID)
		if err != nil {
			return err
		}
		var expected, actual int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tunnel_nodes WHERE tunnel_id=? AND chain_type=1`, tunnelID).Scan(&expected); err != nil {
			return err
		}
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM forward_ports WHERE forward_id=?`, forwardID).Scan(&actual); err != nil {
			return err
		}
		if expected != actual {
			return fmt.Errorf("port %d is unavailable on a node added by the entry group", port)
		}
		_, _ = result.RowsAffected()
	}
	return nil
}

func validateNodeGroupMembers(ctx context.Context, tx *sql.Tx, members []NodeGroupMember) error {
	seen := make(map[int64]struct{}, len(members))
	for _, member := range members {
		if member.NodeID <= 0 || member.Priority < 0 || member.SortIndex < 0 || member.Backup < 0 || member.Backup > 1 {
			return errors.New("invalid node group member")
		}
		if _, duplicate := seen[member.NodeID]; duplicate {
			return fmt.Errorf("duplicate node group member %d", member.NodeID)
		}
		seen[member.NodeID] = struct{}{}
		var exists int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM nodes WHERE id=?`, member.NodeID).Scan(&exists); err != nil {
			return err
		}
		if exists == 0 {
			return fmt.Errorf("node %d does not exist", member.NodeID)
		}
	}
	return nil
}

func validateBindingReferences(ctx context.Context, tx *sql.Tx, request TunnelNodeGroupBindingRequest, bindingID int64) error {
	var tunnelType, groupStatus int
	if err := tx.QueryRowContext(ctx, `SELECT type FROM tunnels WHERE id=?`, request.TunnelID).Scan(&tunnelType); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("tunnel does not exist")
		}
		return err
	}
	if err := tx.QueryRowContext(ctx, `SELECT status FROM node_groups WHERE id=?`, request.GroupID).Scan(&groupStatus); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("node group does not exist")
		}
		return err
	}
	if groupStatus != 1 {
		return errors.New("node group is disabled")
	}
	if tunnelType == 1 && request.ChainType != 1 {
		return errors.New("direct tunnels only support entry node groups")
	}
	var members int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM node_group_members WHERE group_id=?`, request.GroupID).Scan(&members); err != nil {
		return err
	}
	if members == 0 {
		return errors.New("node group has no members")
	}
	var duplicate int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM tunnel_node_group_bindings WHERE tunnel_id=? AND group_id=? AND chain_type=? AND hop_index=? AND id<>?`, request.TunnelID, request.GroupID, request.ChainType, request.HopIndex, bindingID).Scan(&duplicate); err != nil {
		return err
	}
	if duplicate > 0 {
		return errors.New("equivalent tunnel node group binding already exists")
	}
	return nil
}

func normalizeNodeGroup(request NodeGroupRequest) NodeGroupRequest {
	request.Name = strings.TrimSpace(request.Name)
	request.Description = strings.TrimSpace(request.Description)
	request.Strategy = normalizeStrategy(request.Strategy)
	return request
}

func validateNodeGroup(request NodeGroupRequest) error {
	if request.Name == "" || len([]rune(request.Name)) > 100 {
		return errors.New("node group name is required and must be at most 100 characters")
	}
	if request.MaxFails <= 0 || request.FailTimeout < 1000 || request.Status < 0 || request.Status > 1 {
		return errors.New("invalid node group settings")
	}
	return nil
}

func normalizeBinding(request TunnelNodeGroupBindingRequest) TunnelNodeGroupBindingRequest {
	request.Strategy = normalizeStrategy(request.Strategy)
	request.Protocol = strings.ToLower(strings.TrimSpace(request.Protocol))
	if request.Protocol == "mtcp" {
		request.Protocol = "mptcp"
	}
	if request.Protocol == "" {
		request.Protocol = "tcp"
	}
	return request
}

func validateBinding(request TunnelNodeGroupBindingRequest) error {
	if request.TunnelID <= 0 || request.GroupID <= 0 || request.ChainType < 1 || request.ChainType > 3 || request.Port < 1 || request.Port > 65535 || request.HopIndex < 0 || request.FlowQuotaBytes < 0 || request.SpeedLimitMbps < 0 {
		return errors.New("invalid tunnel node group binding")
	}
	if request.ChainType == 2 && request.HopIndex == 0 || request.ChainType != 2 && request.HopIndex != 0 {
		return errors.New("invalid binding hop index")
	}
	switch request.Protocol {
	case "tcp", "udp+quic", "udp+kcp", "mptcp":
	default:
		return errors.New("unsupported binding protocol")
	}
	return nil
}

func (r *Repository) ListEndpointGroups(ctx context.Context) ([]EndpointGroup, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,description,strategy,max_fails,fail_timeout_ms,probe_interval_ms,probe_timeout_ms,status,created_at FROM endpoint_groups ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list endpoint groups: %w", err)
	}
	defer rows.Close()
	result := make([]EndpointGroup, 0)
	for rows.Next() {
		var group EndpointGroup
		if err := rows.Scan(&group.ID, &group.Name, &group.Description, &group.Strategy, &group.MaxFails, &group.FailTimeoutMS, &group.ProbeIntervalMS, &group.ProbeTimeoutMS, &group.Status, &group.CreatedTime); err != nil {
			return nil, err
		}
		group.Endpoints, err = r.loadEndpoints(ctx, r.db, group.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, group)
	}
	return result, rows.Err()
}

func (r *Repository) CreateEndpointGroup(ctx context.Context, request EndpointGroupRequest) (int64, error) {
	request = normalizeEndpointGroup(request)
	if err := validateEndpointGroup(request); err != nil {
		return 0, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `INSERT INTO endpoint_groups(name,description,strategy,max_fails,fail_timeout_ms,probe_interval_ms,probe_timeout_ms,status,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, request.Name, request.Description, request.Strategy, request.MaxFails, request.FailTimeoutMS, request.ProbeIntervalMS, request.ProbeTimeoutMS, request.Status, now, now)
	if err != nil {
		return 0, fmt.Errorf("create endpoint group: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := replaceEndpoints(ctx, tx, id, request.Endpoints, now); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (r *Repository) UpdateEndpointGroup(ctx context.Context, request UpdateEndpointGroupRequest) error {
	if request.ID <= 0 {
		return errors.New("endpoint group id must be positive")
	}
	request.EndpointGroupRequest = normalizeEndpointGroup(request.EndpointGroupRequest)
	if err := validateEndpointGroup(request.EndpointGroupRequest); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `UPDATE endpoint_groups SET name=?,description=?,strategy=?,max_fails=?,fail_timeout_ms=?,probe_interval_ms=?,probe_timeout_ms=?,status=?,updated_at=? WHERE id=?`, request.Name, request.Description, request.Strategy, request.MaxFails, request.FailTimeoutMS, request.ProbeIntervalMS, request.ProbeTimeoutMS, request.Status, now, request.ID)
	if err != nil {
		return fmt.Errorf("update endpoint group: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	if err := replaceEndpoints(ctx, tx, request.ID, request.Endpoints, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) DeleteEndpointGroup(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("endpoint group id must be positive")
	}
	var references int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM forwards WHERE endpoint_group_id=?`, id).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return errors.New("endpoint group is assigned to forwards")
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM endpoint_groups WHERE id=?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) loadEndpoints(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, groupID int64) ([]Endpoint, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT id,name,address,priority,backup,status,sort_index FROM endpoints WHERE group_id=? ORDER BY sort_index,id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Endpoint, 0)
	for rows.Next() {
		var endpoint Endpoint
		if err := rows.Scan(&endpoint.ID, &endpoint.Name, &endpoint.Address, &endpoint.Priority, &endpoint.Backup, &endpoint.Status, &endpoint.SortIndex); err != nil {
			return nil, err
		}
		result = append(result, endpoint)
	}
	return result, rows.Err()
}

func replaceEndpoints(ctx context.Context, tx *sql.Tx, groupID int64, endpoints []Endpoint, now int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT id FROM endpoints WHERE group_id=?`, groupID)
	if err != nil {
		return err
	}
	existing := make(map[int64]struct{})
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		existing[id] = struct{}{}
	}
	if err := rows.Close(); err != nil {
		return err
	}
	if err := rows.Err(); err != nil {
		return err
	}
	kept := make(map[int64]struct{}, len(endpoints))
	for _, endpoint := range endpoints {
		if endpoint.ID > 0 {
			if _, exists := existing[endpoint.ID]; !exists {
				return fmt.Errorf("endpoint %d does not belong to group", endpoint.ID)
			}
			if _, duplicate := kept[endpoint.ID]; duplicate {
				return fmt.Errorf("duplicate endpoint id %d", endpoint.ID)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE endpoints SET name=?,address=?,priority=?,backup=?,status=?,sort_index=?,updated_at=? WHERE id=? AND group_id=?`, strings.TrimSpace(endpoint.Name), strings.TrimSpace(endpoint.Address), endpoint.Priority, endpoint.Backup, endpoint.Status, endpoint.SortIndex, now, endpoint.ID, groupID); err != nil {
				return fmt.Errorf("update endpoint: %w", err)
			}
			kept[endpoint.ID] = struct{}{}
			continue
		}
		result, err := tx.ExecContext(ctx, `INSERT INTO endpoints(group_id,name,address,priority,backup,status,sort_index,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?)`, groupID, strings.TrimSpace(endpoint.Name), strings.TrimSpace(endpoint.Address), endpoint.Priority, endpoint.Backup, endpoint.Status, endpoint.SortIndex, now, now)
		if err != nil {
			return fmt.Errorf("save endpoint: %w", err)
		}
		id, err := result.LastInsertId()
		if err != nil {
			return err
		}
		kept[id] = struct{}{}
	}
	for id := range existing {
		if _, keep := kept[id]; keep {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM endpoints WHERE id=? AND group_id=?`, id, groupID); err != nil {
			return err
		}
	}
	return nil
}

func (r *Repository) ListRouteRuleSets(ctx context.Context) ([]RouteRuleSet, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,description,status,created_at FROM route_rule_sets ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list route rule sets: %w", err)
	}
	defer rows.Close()
	result := make([]RouteRuleSet, 0)
	for rows.Next() {
		var set RouteRuleSet
		if err := rows.Scan(&set.ID, &set.Name, &set.Description, &set.Status, &set.CreatedTime); err != nil {
			return nil, err
		}
		set.Rules, err = r.loadRules(ctx, r.db, set.ID)
		if err != nil {
			return nil, err
		}
		result = append(result, set)
	}
	return result, rows.Err()
}

func (r *Repository) CreateRouteRuleSet(ctx context.Context, request RouteRuleSetRequest) (int64, error) {
	request = normalizeRuleSet(request)
	if err := r.validateRuleSet(ctx, r.db, request); err != nil {
		return 0, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `INSERT INTO route_rule_sets(name,description,status,created_at,updated_at) VALUES(?,?,?,?,?)`, request.Name, request.Description, request.Status, now, now)
	if err != nil {
		return 0, fmt.Errorf("create route rule set: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := replaceRules(ctx, tx, id, request.Rules, now); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (r *Repository) UpdateRouteRuleSet(ctx context.Context, request UpdateRouteRuleSetRequest) error {
	if request.ID <= 0 {
		return errors.New("route rule set id must be positive")
	}
	request.RouteRuleSetRequest = normalizeRuleSet(request.RouteRuleSetRequest)
	if err := r.validateRuleSet(ctx, r.db, request.RouteRuleSetRequest); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	result, err := tx.ExecContext(ctx, `UPDATE route_rule_sets SET name=?,description=?,status=?,updated_at=? WHERE id=?`, request.Name, request.Description, request.Status, now, request.ID)
	if err != nil {
		return fmt.Errorf("update route rule set: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	if err := replaceRules(ctx, tx, request.ID, request.Rules, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *Repository) DeleteRouteRuleSet(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("route rule set id must be positive")
	}
	var references int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM forwards WHERE route_rule_set_id=?`, id).Scan(&references); err != nil {
		return err
	}
	if references > 0 {
		return errors.New("route rule set is assigned to forwards")
	}
	result, err := r.db.ExecContext(ctx, `DELETE FROM route_rule_sets WHERE id=?`, id)
	if err != nil {
		return err
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) loadRules(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, setID int64) ([]RouteRule, error) {
	rows, err := queryer.QueryContext(ctx, `SELECT id,name,match_type,value,secondary_value,negate,priority,status,sort_index FROM route_rules WHERE rule_set_id=? ORDER BY priority DESC,sort_index,id`, setID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RouteRule, 0)
	for rows.Next() {
		var rule RouteRule
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.MatchType, &rule.Value, &rule.SecondaryValue, &rule.Negate, &rule.Priority, &rule.Status, &rule.SortIndex); err != nil {
			return nil, err
		}
		endpointRows, err := queryer.QueryContext(ctx, `SELECT endpoint_id FROM route_rule_endpoints WHERE rule_id=? ORDER BY endpoint_id`, rule.ID)
		if err != nil {
			return nil, err
		}
		for endpointRows.Next() {
			var id int64
			if err := endpointRows.Scan(&id); err != nil {
				endpointRows.Close()
				return nil, err
			}
			rule.EndpointIDs = append(rule.EndpointIDs, id)
		}
		if err := endpointRows.Close(); err != nil {
			return nil, err
		}
		if rule.EndpointIDs == nil {
			rule.EndpointIDs = []int64{}
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}

func replaceRules(ctx context.Context, tx *sql.Tx, setID int64, rules []RouteRule, now int64) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM route_rules WHERE rule_set_id=?`, setID); err != nil {
		return err
	}
	for _, rule := range rules {
		result, err := tx.ExecContext(ctx, `INSERT INTO route_rules(rule_set_id,name,match_type,value,secondary_value,negate,priority,status,sort_index,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, setID, strings.TrimSpace(rule.Name), rule.MatchType, strings.TrimSpace(rule.Value), strings.TrimSpace(rule.SecondaryValue), rule.Negate, rule.Priority, rule.Status, rule.SortIndex, now, now)
		if err != nil {
			return fmt.Errorf("save route rule: %w", err)
		}
		ruleID, err := result.LastInsertId()
		if err != nil {
			return err
		}
		for _, endpointID := range rule.EndpointIDs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO route_rule_endpoints(rule_id,endpoint_id) VALUES(?,?)`, ruleID, endpointID); err != nil {
				return fmt.Errorf("bind route rule endpoint: %w", err)
			}
		}
	}
	return nil
}

func (r *Repository) validateRuleSet(ctx context.Context, queryer interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, request RouteRuleSetRequest) error {
	if request.Name == "" || len([]rune(request.Name)) > 100 {
		return errors.New("route rule set name is required and must be at most 100 characters")
	}
	if request.Status != 0 && request.Status != 1 {
		return errors.New("invalid route rule set status")
	}
	seenNames := make(map[string]struct{}, len(request.Rules))
	endpointIDs := make(map[int64]struct{})
	for index, rule := range request.Rules {
		if err := validateRouteRule(rule); err != nil {
			return fmt.Errorf("rule %d: %w", index+1, err)
		}
		key := strings.ToLower(strings.TrimSpace(rule.Name))
		if _, exists := seenNames[key]; exists {
			return fmt.Errorf("duplicate route rule name %q", rule.Name)
		}
		seenNames[key] = struct{}{}
		for _, id := range rule.EndpointIDs {
			if id <= 0 {
				return errors.New("route rule endpoint id must be positive")
			}
			endpointIDs[id] = struct{}{}
		}
	}
	if len(endpointIDs) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(endpointIDs))
	for id := range endpointIDs {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	rows, err := queryer.QueryContext(ctx, `SELECT id FROM endpoints WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	found := make(map[int64]struct{}, len(ids))
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return err
		}
		found[id] = struct{}{}
	}
	for _, id := range ids {
		if _, exists := found[id]; !exists {
			return fmt.Errorf("endpoint %d does not exist", id)
		}
	}
	return rows.Err()
}

func normalizeEndpointGroup(request EndpointGroupRequest) EndpointGroupRequest {
	request.Name = strings.TrimSpace(request.Name)
	request.Description = strings.TrimSpace(request.Description)
	request.Strategy = normalizeStrategy(request.Strategy)
	if request.MaxFails == 0 {
		request.MaxFails = 1
	}
	if request.FailTimeoutMS == 0 {
		request.FailTimeoutMS = 600000
	}
	if request.ProbeIntervalMS == 0 {
		request.ProbeIntervalMS = 10000
	}
	if request.ProbeTimeoutMS == 0 {
		request.ProbeTimeoutMS = 3000
	}
	return request
}

func validateEndpointGroup(request EndpointGroupRequest) error {
	if request.Name == "" || len([]rune(request.Name)) > 100 {
		return errors.New("endpoint group name is required and must be at most 100 characters")
	}
	if request.MaxFails <= 0 || request.FailTimeoutMS < 1000 || request.ProbeIntervalMS < 1000 || request.ProbeTimeoutMS < 100 {
		return errors.New("invalid endpoint health settings")
	}
	if request.Status != 0 && request.Status != 1 {
		return errors.New("invalid endpoint group status")
	}
	if len(request.Endpoints) == 0 {
		return errors.New("endpoint group requires at least one endpoint")
	}
	seenNames := make(map[string]struct{}, len(request.Endpoints))
	seenAddresses := make(map[string]struct{}, len(request.Endpoints))
	for index, endpoint := range request.Endpoints {
		name := strings.TrimSpace(endpoint.Name)
		address := strings.TrimSpace(endpoint.Address)
		if name == "" || len([]rune(name)) > 100 {
			return fmt.Errorf("endpoint %d name is required", index+1)
		}
		if _, _, err := net.SplitHostPort(address); err != nil {
			return fmt.Errorf("endpoint %d address is invalid", index+1)
		}
		if endpoint.Priority < 0 || endpoint.SortIndex < 0 || endpoint.Backup < 0 || endpoint.Backup > 1 || endpoint.Status < 0 || endpoint.Status > 1 {
			return fmt.Errorf("endpoint %d settings are invalid", index+1)
		}
		nameKey := strings.ToLower(name)
		if _, exists := seenNames[nameKey]; exists {
			return fmt.Errorf("duplicate endpoint name %q", name)
		}
		if _, exists := seenAddresses[address]; exists {
			return fmt.Errorf("duplicate endpoint address %q", address)
		}
		seenNames[nameKey] = struct{}{}
		seenAddresses[address] = struct{}{}
	}
	return nil
}

func normalizeRuleSet(request RouteRuleSetRequest) RouteRuleSetRequest {
	request.Name = strings.TrimSpace(request.Name)
	request.Description = strings.TrimSpace(request.Description)
	for index := range request.Rules {
		request.Rules[index].Name = strings.TrimSpace(request.Rules[index].Name)
		request.Rules[index].MatchType = strings.ToLower(strings.TrimSpace(request.Rules[index].MatchType))
		request.Rules[index].Value = strings.TrimSpace(request.Rules[index].Value)
		request.Rules[index].SecondaryValue = strings.TrimSpace(request.Rules[index].SecondaryValue)
	}
	return request
}

func validateRouteRule(rule RouteRule) error {
	if strings.TrimSpace(rule.Name) == "" || len([]rune(rule.Name)) > 100 {
		return errors.New("name is required and must be at most 100 characters")
	}
	if rule.Value == "" {
		return errors.New("match value is required")
	}
	if strings.Contains(rule.Value, "`") || strings.Contains(rule.SecondaryValue, "`") {
		return errors.New("match values cannot contain backticks")
	}
	if rule.Priority <= 0 || rule.SortIndex < 0 || rule.Negate < 0 || rule.Negate > 1 || rule.Status < 0 || rule.Status > 1 {
		return errors.New("invalid rule settings")
	}
	switch rule.MatchType {
	case "client_ip":
		if net.ParseIP(rule.Value) == nil {
			if _, _, err := net.ParseCIDR(rule.Value); err != nil {
				return errors.New("client IP must be an IP address or CIDR")
			}
		}
	case "protocol":
		if rule.Value != "tcp" && rule.Value != "udp" && rule.Value != "http" && rule.Value != "tls" {
			return errors.New("unsupported protocol matcher")
		}
	case "host":
		if strings.ContainsAny(rule.Value, " /\\") {
			return errors.New("invalid host matcher")
		}
	case "host_regexp", "path_regexp":
		if _, err := regexp.Compile(rule.Value); err != nil {
			return fmt.Errorf("invalid regular expression: %w", err)
		}
	case "method":
		if strings.ContainsAny(rule.Value, " \t/\\") {
			return errors.New("invalid method matcher")
		}
	case "path", "path_prefix":
		if !strings.HasPrefix(rule.Value, "/") {
			return errors.New("path matcher must start with /")
		}
	case "header", "header_regexp", "query", "query_regexp":
		if strings.ContainsAny(rule.Value, " \t") {
			return errors.New("header or query name is invalid")
		}
		if strings.HasSuffix(rule.MatchType, "_regexp") && rule.SecondaryValue != "" {
			if _, err := regexp.Compile(rule.SecondaryValue); err != nil {
				return fmt.Errorf("invalid secondary regular expression: %w", err)
			}
		}
	default:
		return errors.New("unsupported route match type")
	}
	return nil
}

func normalizeStrategy(strategy string) string {
	strategy = strings.ToLower(strings.TrimSpace(strategy))
	if strategy == "round" || strategy == "rand" {
		return strategy
	}
	return "fifo"
}
