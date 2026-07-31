package nodeconfig

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/nodehub/crypto"
)

type Document struct {
	Services []map[string]any `json:"services"`
	Chains   []map[string]any `json:"chains"`
	Limiters []map[string]any `json:"limiters"`
}

type Repository struct {
	db *sql.DB
}

type nodeRecord struct {
	ID            int64
	ServerIP      string
	Status        int
	InterfaceName string
	TCPListenAddr string
	UDPListenAddr string
}

type tunnelRecord struct {
	ID     int64
	Type   int
	Status int
}

type tunnelNode struct {
	ID                  int64
	TunnelID            int64
	ChainType           int
	NodeID              int64
	Port                int
	Strategy            string
	HopIndex            int
	Protocol            string
	FlowQuotaBytes      int64
	SpeedLimitMbps      int
	IngressBytes        int64
	EgressBytes         int64
	HealthStatus        int
	BandwidthOverloaded int
	LastLatencyMS       *int64
	Node                nodeRecord
}

type forwardRecord struct {
	ID                     int64
	UserID                 int64
	TunnelID               int64
	RemoteAddr             string
	InterfaceName          string
	Strategy               string
	Status                 int
	Port                   int
	UserRole               string
	UserStatus             int
	UserExpiresAt          int64
	UserFlowQuota          int64
	UserIngress            int64
	UserEgress             int64
	UserForwardQuota       int
	UserForwardRank        int
	UserTunnelID           int64
	UserTunnelStatus       int
	UserTunnelExpiresAt    int64
	UserTunnelFlowQuota    int64
	UserTunnelIngress      int64
	UserTunnelEgress       int64
	UserTunnelForwardQuota int
	UserTunnelForwardRank  int
	SpeedLimitID           *int64
	SpeedLimitMbps         int
	SpeedLimitStatus       int
}

type entryPolicy struct {
	UserTunnelID int64
	SpeedMbps    int
	QuotaBytes   int64
	UsedBytes    int64
	Status       int
}

type exitPolicy struct {
	UserTunnelID int64
	ExitNodeID   int64
	QuotaBytes   int64
	UsedBytes    int64
	Status       int
}

type encryptedMessage struct {
	Encrypted bool   `json:"encrypted"`
	Data      string `json:"data"`
	Timestamp int64  `json:"timestamp"`
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Build(ctx context.Context, nodeID int64) (Document, error) {
	if nodeID <= 0 {
		return Document{}, errors.New("node id must be positive")
	}
	current, err := r.loadNode(ctx, nodeID)
	if err != nil {
		return Document{}, fmt.Errorf("load configuration node: %w", err)
	}
	tunnels, nodesByTunnel, err := r.loadTopology(ctx, nodeID)
	if err != nil {
		return Document{}, err
	}
	if err := r.syncPolicies(ctx, tunnels); err != nil {
		return Document{}, err
	}
	forwards, err := r.loadForwards(ctx, nodeID)
	if err != nil {
		return Document{}, err
	}
	entryPolicies, err := r.loadEntryPolicies(ctx, nodeID)
	if err != nil {
		return Document{}, err
	}
	exitPolicies, err := r.loadExitPolicies(ctx, tunnels)
	if err != nil {
		return Document{}, err
	}

	document := Document{
		Services: make([]map[string]any, 0),
		Chains:   make([]map[string]any, 0),
		Limiters: make([]map[string]any, 0),
	}
	chainNames := make(map[string]struct{})
	serviceNames := make(map[string]struct{})
	limiterSpeeds := make(map[string]int)

	for tunnelID, topology := range nodesByTunnel {
		tunnel := tunnels[tunnelID]
		if tunnel.Status != 1 || tunnel.Type != 2 {
			continue
		}
		currentNode := findTunnelNode(topology, nodeID)
		if currentNode == nil || !currentNodeSelectable(currentNode) {
			continue
		}
		if currentNode.ChainType == 2 || currentNode.ChainType == 3 {
			for _, trafficProtocol := range trafficProtocols([]*tunnelNode{currentNode}) {
				service := buildRelayService(current, currentNode, topology, trafficProtocol)
				addNamed(&document.Services, serviceNames, service)
			}
		}
	}

	customChains := make(map[string]struct{})
	for _, forward := range forwards {
		tunnel, exists := tunnels[forward.TunnelID]
		if !exists || !forwardSelectable(forward, tunnel, time.Now().UnixMilli()) {
			continue
		}
		topology := nodesByTunnel[forward.TunnelID]
		entry := findTunnelNode(topology, nodeID)
		if entry == nil || entry.ChainType != 1 || !currentNodeSelectable(entry) {
			continue
		}
		policy, hasEntryPolicy := entryPolicies[forward.UserTunnelID]
		if hasEntryPolicy && (policy.Status != 1 || quotaReached(policy.QuotaBytes, policy.UsedBytes, 0)) {
			continue
		}

		limiterName := ""
		if hasEntryPolicy && policy.SpeedMbps > 0 {
			limiterName = fmt.Sprintf("user_entry_%d_%d_%d", forward.UserTunnelID, forward.TunnelID, nodeID)
			limiterSpeeds[limiterName] = policy.SpeedMbps
		} else if entry.SpeedLimitMbps > 0 {
			limiterName = fmt.Sprintf("entry_%d_%d", forward.TunnelID, nodeID)
			limiterSpeeds[limiterName] = entry.SpeedLimitMbps
		} else if forward.SpeedLimitID != nil && forward.SpeedLimitStatus == 1 && forward.SpeedLimitMbps > 0 {
			limiterName = strconv.FormatInt(*forward.SpeedLimitID, 10)
			limiterSpeeds[limiterName] = forward.SpeedLimitMbps
		}

		chainNamesByTraffic := map[string]string{}
		if tunnel.Type == 2 {
			policies := exitPolicies[forward.UserTunnelID]
			paths := selectablePath(topology, policies)
			if len(paths) == 0 {
				continue
			}
			nameSuffix := ""
			if forward.UserTunnelID > 0 {
				nameSuffix = fmt.Sprintf("user_%d", forward.UserTunnelID)
			}
			for _, trafficProtocol := range pathTrafficProtocols(paths) {
				chain := buildPathChain(current, forward.TunnelID, nameSuffix, trafficProtocol, paths)
				name, _ := chain["name"].(string)
				if _, exists := customChains[name]; !exists {
					addNamed(&document.Chains, chainNames, chain)
					customChains[name] = struct{}{}
				}
				chainNamesByTraffic[trafficProtocol] = name
				if !pathHasHybrid(paths) {
					chainNamesByTraffic["udp"] = name
				}
			}
		}

		baseName := fmt.Sprintf("%d_%d_%d", forward.ID, forward.UserID, forward.UserTunnelID)
		for _, protocol := range []string{"tcp", "udp"} {
			service := buildForwardService(current, forward, tunnel, baseName, protocol, limiterName, chainNamesByTraffic[protocol])
			addNamed(&document.Services, serviceNames, service)
		}
	}

	limiterNames := make([]string, 0, len(limiterSpeeds))
	for name := range limiterSpeeds {
		limiterNames = append(limiterNames, name)
	}
	sort.Strings(limiterNames)
	for _, name := range limiterNames {
		speed := formatMegabytes(limiterSpeeds[name])
		document.Limiters = append(document.Limiters, map[string]any{
			"name":   name,
			"limits": []string{"$ " + speed + "MB " + speed + "MB"},
		})
	}
	sortNamed(document.Chains)
	sortNamed(document.Services)
	return document, nil
}

func (r *Repository) loadNode(ctx context.Context, nodeID int64) (nodeRecord, error) {
	var node nodeRecord
	err := r.db.QueryRowContext(ctx, `SELECT id,server_ip,status,interface_name,tcp_listen_addr,udp_listen_addr FROM nodes WHERE id=?`, nodeID).Scan(
		&node.ID, &node.ServerIP, &node.Status, &node.InterfaceName, &node.TCPListenAddr, &node.UDPListenAddr,
	)
	return node, err
}

func (r *Repository) loadTopology(ctx context.Context, nodeID int64) (map[int64]tunnelRecord, map[int64][]*tunnelNode, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.id,t.type,t.status,tn.id,tn.chain_type,tn.node_id,COALESCE(tn.port,0),COALESCE(tn.strategy,'fifo'),COALESCE(tn.hop_index,0),COALESCE(tn.protocol,'tcp'),COALESCE(tn.flow_quota_bytes,0),COALESCE(tn.speed_limit_mbps,0),tn.ingress_bytes,tn.egress_bytes,tn.health_status,tn.bandwidth_overloaded,tn.last_latency_ms,n.server_ip,n.status,n.interface_name,n.tcp_listen_addr,n.udp_listen_addr
		FROM tunnels t
		JOIN tunnel_nodes tn ON tn.tunnel_id=t.id
		JOIN nodes n ON n.id=tn.node_id
		WHERE t.id IN (SELECT tunnel_id FROM tunnel_nodes WHERE node_id=?)
		ORDER BY t.id,tn.chain_type,tn.hop_index,tn.id`, nodeID)
	if err != nil {
		return nil, nil, fmt.Errorf("load node topology: %w", err)
	}
	defer rows.Close()
	tunnels := make(map[int64]tunnelRecord)
	grouped := make(map[int64][]*tunnelNode)
	for rows.Next() {
		var tunnel tunnelRecord
		var item tunnelNode
		var latency sql.NullInt64
		if err := rows.Scan(&tunnel.ID, &tunnel.Type, &tunnel.Status, &item.ID, &item.ChainType, &item.NodeID, &item.Port, &item.Strategy, &item.HopIndex, &item.Protocol, &item.FlowQuotaBytes, &item.SpeedLimitMbps, &item.IngressBytes, &item.EgressBytes, &item.HealthStatus, &item.BandwidthOverloaded, &latency, &item.Node.ServerIP, &item.Node.Status, &item.Node.InterfaceName, &item.Node.TCPListenAddr, &item.Node.UDPListenAddr); err != nil {
			return nil, nil, fmt.Errorf("scan node topology: %w", err)
		}
		item.TunnelID = tunnel.ID
		item.Node.ID = item.NodeID
		if latency.Valid {
			item.LastLatencyMS = &latency.Int64
		}
		tunnels[tunnel.ID] = tunnel
		grouped[tunnel.ID] = append(grouped[tunnel.ID], &item)
	}
	return tunnels, grouped, rows.Err()
}

func (r *Repository) syncPolicies(ctx context.Context, tunnels map[int64]tunnelRecord) error {
	if len(tunnels) == 0 {
		return nil
	}
	ids := make([]int64, 0, len(tunnels))
	for id := range tunnels {
		ids = append(ids, id)
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	entryQuery := `INSERT OR IGNORE INTO user_tunnel_entry_policies(user_tunnel_id,tunnel_id,entry_node_id,created_at,updated_at) SELECT ut.id,ut.tunnel_id,tn.node_id,?,? FROM user_tunnels ut JOIN tunnel_nodes tn ON tn.tunnel_id=ut.tunnel_id AND tn.chain_type=1 WHERE ut.tunnel_id IN (` + placeholders + `)`
	exitQuery := `INSERT OR IGNORE INTO user_tunnel_exit_policies(user_tunnel_id,tunnel_id,exit_node_id,created_at,updated_at) SELECT ut.id,ut.tunnel_id,tn.node_id,?,? FROM user_tunnels ut JOIN tunnel_nodes tn ON tn.tunnel_id=ut.tunnel_id AND tn.chain_type=3 WHERE ut.tunnel_id IN (` + placeholders + `)`
	queryArgs := append([]any{now, now}, args...)
	if _, err := tx.ExecContext(ctx, entryQuery, queryArgs...); err != nil {
		return fmt.Errorf("sync entry policies for configuration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, exitQuery, queryArgs...); err != nil {
		return fmt.Errorf("sync exit policies for configuration: %w", err)
	}
	return tx.Commit()
}

func (r *Repository) loadForwards(ctx context.Context, nodeID int64) ([]forwardRecord, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT f.id,f.user_id,f.tunnel_id,f.remote_addr,f.interface_name,f.strategy,f.status,fp.port,
		       u.role,u.status,u.expires_at,u.flow_quota_bytes,u.ingress_bytes,u.egress_bytes,u.forward_quota,
		       (SELECT COUNT(*) FROM forwards f2 WHERE f2.user_id=f.user_id AND f2.status=1 AND (f2.sort_index<f.sort_index OR (f2.sort_index=f.sort_index AND f2.id<=f.id))),
		       COALESCE(ut.id,0),COALESCE(ut.status,0),COALESCE(ut.expires_at,0),COALESCE(ut.flow_quota_bytes,0),COALESCE(ut.ingress_bytes,0),COALESCE(ut.egress_bytes,0),COALESCE(ut.forward_quota,0),
		       (SELECT COUNT(*) FROM forwards f3 WHERE f3.user_id=f.user_id AND f3.tunnel_id=f.tunnel_id AND f3.status=1 AND (f3.sort_index<f.sort_index OR (f3.sort_index=f.sort_index AND f3.id<=f.id))),
		       ut.speed_limit_id,COALESCE(sl.speed_mbps,0),COALESCE(sl.status,0)
		FROM forward_ports fp
		JOIN forwards f ON f.id=fp.forward_id
		JOIN users u ON u.id=f.user_id
		LEFT JOIN user_tunnels ut ON ut.user_id=f.user_id AND ut.tunnel_id=f.tunnel_id
		LEFT JOIN speed_limits sl ON sl.id=ut.speed_limit_id
		WHERE fp.node_id=?
		ORDER BY f.sort_index,f.id`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("load node forwards: %w", err)
	}
	defer rows.Close()
	result := make([]forwardRecord, 0)
	for rows.Next() {
		var item forwardRecord
		var speedLimitID sql.NullInt64
		if err := rows.Scan(&item.ID, &item.UserID, &item.TunnelID, &item.RemoteAddr, &item.InterfaceName, &item.Strategy, &item.Status, &item.Port,
			&item.UserRole, &item.UserStatus, &item.UserExpiresAt, &item.UserFlowQuota, &item.UserIngress, &item.UserEgress, &item.UserForwardQuota, &item.UserForwardRank,
			&item.UserTunnelID, &item.UserTunnelStatus, &item.UserTunnelExpiresAt, &item.UserTunnelFlowQuota, &item.UserTunnelIngress, &item.UserTunnelEgress, &item.UserTunnelForwardQuota, &item.UserTunnelForwardRank,
			&speedLimitID, &item.SpeedLimitMbps, &item.SpeedLimitStatus); err != nil {
			return nil, fmt.Errorf("scan node forward: %w", err)
		}
		if speedLimitID.Valid {
			item.SpeedLimitID = &speedLimitID.Int64
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) loadEntryPolicies(ctx context.Context, nodeID int64) (map[int64]entryPolicy, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT user_tunnel_id,speed_limit_mbps,flow_quota_bytes,used_bytes,status FROM user_tunnel_entry_policies WHERE entry_node_id=?`, nodeID)
	if err != nil {
		return nil, fmt.Errorf("load entry policies: %w", err)
	}
	defer rows.Close()
	result := make(map[int64]entryPolicy)
	for rows.Next() {
		var item entryPolicy
		if err := rows.Scan(&item.UserTunnelID, &item.SpeedMbps, &item.QuotaBytes, &item.UsedBytes, &item.Status); err != nil {
			return nil, err
		}
		result[item.UserTunnelID] = item
	}
	return result, rows.Err()
}

func (r *Repository) loadExitPolicies(ctx context.Context, tunnels map[int64]tunnelRecord) (map[int64][]exitPolicy, error) {
	if len(tunnels) == 0 {
		return map[int64][]exitPolicy{}, nil
	}
	ids := make([]int64, 0, len(tunnels))
	for id := range tunnels {
		ids = append(ids, id)
	}
	placeholders := strings.TrimRight(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, len(ids))
	for index, id := range ids {
		args[index] = id
	}
	rows, err := r.db.QueryContext(ctx, `SELECT user_tunnel_id,exit_node_id,flow_quota_bytes,used_bytes,status FROM user_tunnel_exit_policies WHERE tunnel_id IN (`+placeholders+`) ORDER BY user_tunnel_id,exit_node_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("load exit policies: %w", err)
	}
	defer rows.Close()
	result := make(map[int64][]exitPolicy)
	for rows.Next() {
		var item exitPolicy
		if err := rows.Scan(&item.UserTunnelID, &item.ExitNodeID, &item.QuotaBytes, &item.UsedBytes, &item.Status); err != nil {
			return nil, err
		}
		result[item.UserTunnelID] = append(result[item.UserTunnelID], item)
	}
	return result, rows.Err()
}

func forwardSelectable(forward forwardRecord, tunnel tunnelRecord, now int64) bool {
	if forward.Status != 1 || tunnel.Status != 1 || forward.UserStatus != 1 {
		return false
	}
	if expired(forward.UserExpiresAt, now) || quotaReached(forward.UserFlowQuota, forward.UserIngress, forward.UserEgress) {
		return false
	}
	if forward.UserForwardQuota > 0 && forward.UserForwardRank > forward.UserForwardQuota {
		return false
	}
	if forward.UserTunnelID == 0 {
		return forward.UserRole == "admin"
	}
	if forward.UserTunnelStatus != 1 || expired(forward.UserTunnelExpiresAt, now) || quotaReached(forward.UserTunnelFlowQuota, forward.UserTunnelIngress, forward.UserTunnelEgress) {
		return false
	}
	return forward.UserTunnelForwardQuota <= 0 || forward.UserTunnelForwardRank <= forward.UserTunnelForwardQuota
}

func currentNodeSelectable(item *tunnelNode) bool {
	if item == nil || quotaReached(item.FlowQuotaBytes, item.IngressBytes, item.EgressBytes) {
		return false
	}
	if item.ChainType == 3 {
		return exitNodeSelectable(item)
	}
	return true
}

func nextNodeSelectable(item *tunnelNode) bool {
	if item == nil || item.Node.Status != 1 || quotaReached(item.FlowQuotaBytes, item.IngressBytes, item.EgressBytes) {
		return false
	}
	if item.ChainType == 3 {
		return exitNodeSelectable(item)
	}
	return true
}

func exitNodeSelectable(item *tunnelNode) bool {
	if item.HealthStatus != 1 || item.BandwidthOverloaded != 0 {
		return false
	}
	return item.LastLatencyMS == nil || *item.LastLatencyMS <= 20
}

func selectablePath(topology []*tunnelNode, exitPolicies []exitPolicy) [][]*tunnelNode {
	maxHop := 0
	for _, item := range topology {
		if item.ChainType == 2 && item.HopIndex > maxHop {
			maxHop = item.HopIndex
		}
	}
	path := make([][]*tunnelNode, 0, maxHop+1)
	for hopIndex := 1; hopIndex <= maxHop; hopIndex++ {
		configured := false
		items := make([]*tunnelNode, 0)
		for _, item := range topology {
			if item.ChainType == 2 && item.HopIndex == hopIndex {
				configured = true
				if nextNodeSelectable(item) {
					items = append(items, item)
				}
			}
		}
		if !configured || len(items) == 0 {
			return nil
		}
		path = append(path, items)
	}
	exits := make([]*tunnelNode, 0)
	for _, item := range topology {
		if item.ChainType == 3 && nextNodeSelectable(item) {
			exits = append(exits, item)
		}
	}
	if len(exitPolicies) > 0 {
		exits = filterExitPolicies(exits, exitPolicies)
	}
	if len(exits) == 0 {
		return nil
	}
	return append(path, exits)
}

func filterExitPolicies(items []*tunnelNode, policies []exitPolicy) []*tunnelNode {
	configured := make(map[int64]exitPolicy, len(policies))
	for _, policy := range policies {
		configured[policy.ExitNodeID] = policy
	}
	result := make([]*tunnelNode, 0, len(items))
	for _, item := range items {
		policy, exists := configured[item.NodeID]
		if !exists || policy.Status == 1 && !quotaReached(policy.QuotaBytes, policy.UsedBytes, 0) {
			result = append(result, item)
		}
	}
	return result
}

func buildPathChain(current nodeRecord, tunnelID int64, suffix, trafficProtocol string, path [][]*tunnelNode) map[string]any {
	if len(path) == 0 {
		return nil
	}
	hops := make([]map[string]any, 0, len(path))
	for hopIndex, candidates := range path {
		if len(candidates) == 0 {
			return nil
		}
		nodes := make([]map[string]any, 0, len(candidates))
		for index, item := range candidates {
			node := map[string]any{
				"name":      fmt.Sprintf("node_%d", index+1),
				"addr":      joinHostPort(item.Node.ServerIP, item.Port, item.Protocol, trafficProtocol),
				"connector": map[string]any{"type": "relay"},
				"dialer":    transportConfig(item.Protocol, trafficProtocol, false),
			}
			if strings.TrimSpace(current.InterfaceName) != "" {
				node["interface"] = current.InterfaceName
			}
			nodes = append(nodes, node)
		}
		hops = append(hops, map[string]any{
			"name": fmt.Sprintf("hop_%d_%d", tunnelID, hopIndex+1),
			"selector": map[string]any{
				"strategy":    normalizeStrategy(candidates[0].Strategy),
				"maxFails":    1,
				"failTimeout": int64(600_000_000_000),
			},
			"nodes": nodes,
		})
	}
	return map[string]any{
		"name": chainName(tunnelID, suffix, trafficProtocol, pathHasHybrid(path)),
		"hops": hops,
	}
}

func buildRelayService(current nodeRecord, item *tunnelNode, _ []*tunnelNode, trafficProtocol string) map[string]any {
	service := map[string]any{
		"name":     relayServiceName(item.TunnelID, item.Protocol, trafficProtocol),
		"addr":     joinListenAddr(current, item.Port, item.Protocol, trafficProtocol),
		"handler":  map[string]any{"type": "relay"},
		"listener": transportConfig(item.Protocol, trafficProtocol, true),
	}
	if item.ChainType == 3 && strings.TrimSpace(current.InterfaceName) != "" {
		service["metadata"] = map[string]any{"interface": current.InterfaceName}
	}
	return service
}

func buildForwardService(current nodeRecord, forward forwardRecord, tunnel tunnelRecord, baseName, protocol, limiterName, chain string) map[string]any {
	service := map[string]any{
		"name":      baseName + "_" + protocol,
		"addr":      joinListenHost(protocolListenAddr(current, protocol), forward.Port),
		"handler":   map[string]any{"type": protocol},
		"listener":  map[string]any{"type": protocol},
		"forwarder": buildForwarder(forward.RemoteAddr, forward.Strategy),
	}
	if protocol == "udp" {
		service["listener"] = map[string]any{"type": "udp", "metadata": map[string]any{"keepAlive": true}}
	}
	if tunnel.Type == 1 && strings.TrimSpace(forward.InterfaceName) != "" {
		service["metadata"] = map[string]any{"interface": forward.InterfaceName}
	}
	if limiterName != "" {
		service["limiter"] = limiterName
	}
	if tunnel.Type == 2 {
		service["handler"] = map[string]any{"type": protocol, "retries": 1, "chain": chain}
	}
	return service
}

func buildForwarder(remoteAddr, strategy string) map[string]any {
	addresses := strings.Split(remoteAddr, ",")
	nodes := make([]map[string]any, 0, len(addresses))
	for _, address := range addresses {
		address = strings.TrimSpace(address)
		if address == "" {
			continue
		}
		nodes = append(nodes, map[string]any{"name": fmt.Sprintf("node_%d", len(nodes)+1), "addr": address})
	}
	return map[string]any{
		"nodes":        nodes,
		"probePeriod":  int64(10_000_000_000),
		"probeTimeout": int64(3_000_000_000),
		"selector": map[string]any{
			"strategy":    normalizeStrategy(strategy),
			"maxFails":    1,
			"failTimeout": int64(600_000_000_000),
		},
	}
}

func transportConfig(protocol, trafficProtocol string, listener bool) map[string]any {
	normalized := normalizeProtocol(protocol)
	transport := transportType(normalized, trafficProtocol)
	value := map[string]any{"type": transport}
	if normalized == "mptcp" {
		value["metadata"] = map[string]any{"mptcp": true}
	} else if listener && transport == "udp" {
		value["metadata"] = map[string]any{"keepAlive": true}
	}
	return value
}

func normalizeProtocol(protocol string) string {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	switch protocol {
	case "tcp", "udp+quic", "udp+kcp", "mptcp":
		return protocol
	case "mtcp":
		return "mptcp"
	default:
		return "tcp"
	}
}

func transportType(protocol, trafficProtocol string) string {
	protocol = normalizeProtocol(protocol)
	if protocol == "udp+quic" || protocol == "udp+kcp" {
		if trafficProtocol == "udp" {
			return "udp"
		}
		if protocol == "udp+quic" {
			return "quic"
		}
		return "kcp"
	}
	if protocol == "mptcp" {
		return "mtcp"
	}
	return protocol
}

func relayServiceName(tunnelID int64, protocol, trafficProtocol string) string {
	normalized := strings.ReplaceAll(normalizeProtocol(protocol), "+", "_")
	if isHybrid(protocol) {
		return fmt.Sprintf("%d_relay_%s_%s", tunnelID, normalized, trafficProtocol)
	}
	return fmt.Sprintf("%d_relay_%s", tunnelID, normalized)
}

func chainName(tunnelID int64, suffix, trafficProtocol string, hybrid bool) string {
	name := "chains_" + strconv.FormatInt(tunnelID, 10)
	if suffix != "" {
		name += "_" + suffix
	}
	if hybrid {
		name += "_" + trafficProtocol
	}
	return name
}

func pathTrafficProtocols(path [][]*tunnelNode) []string {
	if len(path) == 0 {
		return nil
	}
	if pathHasHybrid(path) {
		return []string{"tcp", "udp"}
	}
	return []string{"tcp"}
}

func pathHasHybrid(path [][]*tunnelNode) bool {
	for _, items := range path {
		if hasHybrid(items) {
			return true
		}
	}
	return false
}

func trafficProtocols(items []*tunnelNode) []string {
	if len(items) == 0 {
		return nil
	}
	if hasHybrid(items) {
		return []string{"tcp", "udp"}
	}
	return []string{"tcp"}
}

func hasHybrid(items []*tunnelNode) bool {
	for _, item := range items {
		if isHybrid(item.Protocol) {
			return true
		}
	}
	return false
}

func isHybrid(protocol string) bool {
	protocol = normalizeProtocol(protocol)
	return protocol == "udp+quic" || protocol == "udp+kcp"
}

func joinHostPort(host string, basePort int, protocol, trafficProtocol string) string {
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(resolvePort(basePort, protocol, trafficProtocol)))
}

func joinListenAddr(node nodeRecord, basePort int, protocol, trafficProtocol string) string {
	transport := transportType(protocol, trafficProtocol)
	host := node.TCPListenAddr
	if transport == "udp" || transport == "quic" || transport == "kcp" {
		host = node.UDPListenAddr
	}
	return joinListenHost(host, resolvePort(basePort, protocol, trafficProtocol))
}

func joinListenHost(host string, port int) string {
	host = strings.TrimSpace(host)
	if host == "" {
		host = "[::]"
	}
	return net.JoinHostPort(strings.Trim(host, "[]"), strconv.Itoa(port))
}

func protocolListenAddr(node nodeRecord, protocol string) string {
	if protocol == "udp" {
		return node.UDPListenAddr
	}
	return node.TCPListenAddr
}

func resolvePort(basePort int, protocol, trafficProtocol string) int {
	if !isHybrid(protocol) || trafficProtocol != "udp" {
		return basePort
	}
	if basePort < 65535 {
		return basePort + 1
	}
	if basePort > 1 {
		return basePort - 1
	}
	return basePort
}

func findTunnelNode(items []*tunnelNode, nodeID int64) *tunnelNode {
	for _, item := range items {
		if item.NodeID == nodeID {
			return item
		}
	}
	return nil
}

func expired(expiresAt, now int64) bool {
	return expiresAt > 0 && expiresAt <= now
}

func quotaReached(quota, first, second int64) bool {
	if quota <= 0 {
		return false
	}
	if first >= quota || second >= quota {
		return true
	}
	return first >= quota-second
}

func normalizeStrategy(strategy string) string {
	strategy = strings.ToLower(strings.TrimSpace(strategy))
	if strategy == "round" || strategy == "rand" {
		return strategy
	}
	return "fifo"
}

func formatMegabytes(mbps int) string {
	value := math.Round((float64(mbps)/8)*10) / 10
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func addNamed(target *[]map[string]any, seen map[string]struct{}, item map[string]any) {
	if item == nil {
		return
	}
	name, _ := item["name"].(string)
	if name == "" {
		return
	}
	if _, exists := seen[name]; exists {
		return
	}
	seen[name] = struct{}{}
	*target = append(*target, item)
}

func sortNamed(items []map[string]any) {
	sort.Slice(items, func(left, right int) bool {
		leftName, _ := items[left]["name"].(string)
		rightName, _ := items[right]["name"].(string)
		return leftName < rightName
	})
}

func DecodeSnapshot(raw []byte, secret string) (Document, error) {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return Document{}, errors.New("configuration snapshot is empty")
	}
	var wrapper encryptedMessage
	if err := json.Unmarshal(raw, &wrapper); err == nil && wrapper.Encrypted {
		if wrapper.Data == "" {
			return Document{}, errors.New("encrypted configuration snapshot has no data")
		}
		if wrapper.Timestamp > 0 && absolute(time.Now().Unix()-wrapper.Timestamp) > 300 {
			return Document{}, errors.New("encrypted configuration snapshot has expired")
		}
		cipher, err := crypto.New(secret)
		if err != nil {
			return Document{}, err
		}
		plain, err := cipher.Decrypt(wrapper.Data)
		if err != nil {
			return Document{}, fmt.Errorf("decrypt configuration snapshot: %w", err)
		}
		raw = plain
	}
	var document Document
	if err := json.Unmarshal(raw, &document); err != nil {
		return Document{}, fmt.Errorf("decode configuration snapshot: %w", err)
	}
	if document.Services == nil {
		document.Services = []map[string]any{}
	}
	if document.Chains == nil {
		document.Chains = []map[string]any{}
	}
	if document.Limiters == nil {
		document.Limiters = []map[string]any{}
	}
	return document, nil
}

func Equivalent(expected, actual Document) bool {
	return namedItemsEqual(expected.Services, actual.Services, true) &&
		namedItemsEqual(expected.Chains, actual.Chains, false) &&
		namedItemsEqual(expected.Limiters, actual.Limiters, false)
}

func namedItemsEqual(expected, actual []map[string]any, stripStatus bool) bool {
	left, ok := canonicalItems(expected, stripStatus)
	if !ok {
		return false
	}
	right, ok := canonicalItems(actual, stripStatus)
	if !ok || len(left) != len(right) {
		return false
	}
	for name, value := range left {
		if right[name] != value {
			return false
		}
	}
	return true
}

func canonicalItems(items []map[string]any, stripStatus bool) (map[string]string, bool) {
	result := make(map[string]string, len(items))
	for _, source := range items {
		item := cloneMap(source)
		if stripStatus {
			delete(item, "status")
		} else {
			normalizeChainInterfaces(item)
		}
		name, _ := item["name"].(string)
		if name == "" {
			return nil, false
		}
		if _, exists := result[name]; exists {
			return nil, false
		}
		encoded, err := json.Marshal(item)
		if err != nil {
			return nil, false
		}
		result[name] = string(encoded)
	}
	return result, true
}

func cloneMap(source map[string]any) map[string]any {
	encoded, _ := json.Marshal(source)
	var result map[string]any
	_ = json.Unmarshal(encoded, &result)
	return result
}

func normalizeChainInterfaces(chain map[string]any) {
	hops, ok := chain["hops"].([]any)
	if !ok {
		return
	}
	for _, rawHop := range hops {
		hop, ok := rawHop.(map[string]any)
		if !ok {
			continue
		}
		inherited, _ := hop["interface"].(string)
		if inherited == "" {
			continue
		}
		nodes, _ := hop["nodes"].([]any)
		for _, rawNode := range nodes {
			node, ok := rawNode.(map[string]any)
			if ok && node["interface"] == inherited {
				delete(node, "interface")
			}
		}
	}
}

func absolute(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}
