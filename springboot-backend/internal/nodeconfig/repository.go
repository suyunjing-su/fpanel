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
	Services   []map[string]any `json:"services"`
	Chains     []map[string]any `json:"chains"`
	Limiters   []map[string]any `json:"limiters"`
	CLimiters  []map[string]any `json:"climiters"`
	Admissions []map[string]any `json:"admissions"`
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
	ID              int64
	Type            int
	Status          int
	TOTEnabled      bool
	TOTSecret       string
	TOTPathCount    int
	TOTMaxPayload   int
	TOTWindow       int
	TOTRetransmitMS int
	TOTMaxRetries   int
	TOTRecoveryMS   int
	TOTHandshakeMS  int
	TOTClockSkewMS  int
	TOTIdleTTLMS    int
	TOTMPTCP        bool
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
	GroupPriority       int
	GroupBackup         int
	GroupMaxFails       int
	GroupFailTimeoutMS  int64
	Node                nodeRecord
}

type forwardRecord struct {
	ID                     int64
	UserID                 int64
	TunnelID               int64
	RemoteAddr             string
	InterfaceName          string
	Strategy               string
	EndpointGroupID        *int64
	RouteRuleSetID         *int64
	MaxConnections         int
	MaxConnectionsPerIP    int
	SourceRanges           string
	SourceWhitelist        int
	ProxyProtocolReceive   int
	ProxyProtocolSend      int
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

type routeEndpoint struct {
	ID       int64
	Name     string
	Address  string
	Priority int
	Backup   int
	Rule     string
}

type endpointPlan struct {
	Strategy        string
	MaxFails        int
	FailTimeoutMS   int64
	ProbeIntervalMS int64
	ProbeTimeoutMS  int64
	Endpoints       []routeEndpoint
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
		Services:   make([]map[string]any, 0),
		Chains:     make([]map[string]any, 0),
		Limiters:   make([]map[string]any, 0),
		CLimiters:  make([]map[string]any, 0),
		Admissions: make([]map[string]any, 0),
	}
	chainNames := make(map[string]struct{})
	serviceNames := make(map[string]struct{})
	limiterSpeeds := make(map[string]int)
	connectionLimiters := make(map[string][]string)
	admissions := make(map[string]map[string]any)

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
				service := buildRelayService(current, currentNode, tunnel, topology, trafficProtocol)
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
				chain := buildPathChain(current, tunnel, nameSuffix, trafficProtocol, paths)
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

		endpointPlan, err := r.loadEndpointPlan(ctx, forward)
		if err != nil {
			return Document{}, err
		}
		if len(endpointPlan.Endpoints) == 0 && strings.TrimSpace(forward.RemoteAddr) == "" {
			continue
		}

		connectionLimiterName := ""
		connectionLimits := make([]string, 0, 2)
		if forward.MaxConnections > 0 {
			connectionLimits = append(connectionLimits, fmt.Sprintf("$ %d", forward.MaxConnections))
		}
		if forward.MaxConnectionsPerIP > 0 {
			connectionLimits = append(connectionLimits, fmt.Sprintf("$$ %d", forward.MaxConnectionsPerIP))
		}
		if len(connectionLimits) > 0 {
			connectionLimiterName = fmt.Sprintf("forward_conn_%d", forward.ID)
			connectionLimiters[connectionLimiterName] = connectionLimits
		}

		admissionName := ""
		if sourceRanges := splitValues(forward.SourceRanges); len(sourceRanges) > 0 {
			admissionName = fmt.Sprintf("forward_source_%d", forward.ID)
			admissions[admissionName] = map[string]any{
				"name":      admissionName,
				"whitelist": forward.SourceWhitelist == 1,
				"matchers":  sourceRanges,
			}
		}

		baseName := fmt.Sprintf("%d_%d_%d", forward.ID, forward.UserID, forward.UserTunnelID)
		for _, protocol := range []string{"tcp", "udp"} {
			service := buildForwardService(current, forward, tunnel, baseName, protocol, limiterName, connectionLimiterName, admissionName, chainNamesByTraffic[protocol], endpointPlan)
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
	connectionLimiterNames := sortedKeys(connectionLimiters)
	for _, name := range connectionLimiterNames {
		document.CLimiters = append(document.CLimiters, map[string]any{
			"name":   name,
			"limits": connectionLimiters[name],
		})
	}
	admissionNames := sortedKeys(admissions)
	for _, name := range admissionNames {
		document.Admissions = append(document.Admissions, admissions[name])
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
		SELECT t.id,t.type,t.status,t.tot_enabled,t.tot_secret,t.tot_path_count,t.tot_max_payload,t.tot_window,t.tot_retransmit_interval_ms,t.tot_max_retries,t.tot_recovery_period_ms,t.tot_handshake_timeout_ms,t.tot_max_clock_skew_ms,t.tot_idle_ttl_ms,t.tot_mptcp,tn.id,tn.chain_type,tn.node_id,COALESCE(tn.port,0),COALESCE(tn.strategy,'fifo'),COALESCE(tn.hop_index,0),COALESCE(tn.protocol,'tcp'),COALESCE(tn.flow_quota_bytes,0),COALESCE(tn.speed_limit_mbps,0),tn.ingress_bytes,tn.egress_bytes,tn.health_status,tn.bandwidth_overloaded,tn.last_latency_ms,tn.group_priority,tn.group_backup,tn.group_max_fails,tn.group_fail_timeout_ms,n.server_ip,n.status,n.interface_name,n.tcp_listen_addr,n.udp_listen_addr
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
		if err := rows.Scan(&tunnel.ID, &tunnel.Type, &tunnel.Status, &tunnel.TOTEnabled, &tunnel.TOTSecret, &tunnel.TOTPathCount, &tunnel.TOTMaxPayload, &tunnel.TOTWindow, &tunnel.TOTRetransmitMS, &tunnel.TOTMaxRetries, &tunnel.TOTRecoveryMS, &tunnel.TOTHandshakeMS, &tunnel.TOTClockSkewMS, &tunnel.TOTIdleTTLMS, &tunnel.TOTMPTCP, &item.ID, &item.ChainType, &item.NodeID, &item.Port, &item.Strategy, &item.HopIndex, &item.Protocol, &item.FlowQuotaBytes, &item.SpeedLimitMbps, &item.IngressBytes, &item.EgressBytes, &item.HealthStatus, &item.BandwidthOverloaded, &latency, &item.GroupPriority, &item.GroupBackup, &item.GroupMaxFails, &item.GroupFailTimeoutMS, &item.Node.ServerIP, &item.Node.Status, &item.Node.InterfaceName, &item.Node.TCPListenAddr, &item.Node.UDPListenAddr); err != nil {
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
		SELECT f.id,f.user_id,f.tunnel_id,f.remote_addr,f.interface_name,f.strategy,
		       f.endpoint_group_id,f.route_rule_set_id,f.max_connections,f.max_connections_per_ip,
		       f.source_ranges,f.source_whitelist,f.proxy_protocol_receive,f.proxy_protocol_send,f.status,fp.port,
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
		var endpointGroupID, routeRuleSetID, speedLimitID sql.NullInt64
		if err := rows.Scan(&item.ID, &item.UserID, &item.TunnelID, &item.RemoteAddr, &item.InterfaceName, &item.Strategy,
			&endpointGroupID, &routeRuleSetID, &item.MaxConnections, &item.MaxConnectionsPerIP,
			&item.SourceRanges, &item.SourceWhitelist, &item.ProxyProtocolReceive, &item.ProxyProtocolSend, &item.Status, &item.Port,
			&item.UserRole, &item.UserStatus, &item.UserExpiresAt, &item.UserFlowQuota, &item.UserIngress, &item.UserEgress, &item.UserForwardQuota, &item.UserForwardRank,
			&item.UserTunnelID, &item.UserTunnelStatus, &item.UserTunnelExpiresAt, &item.UserTunnelFlowQuota, &item.UserTunnelIngress, &item.UserTunnelEgress, &item.UserTunnelForwardQuota, &item.UserTunnelForwardRank,
			&speedLimitID, &item.SpeedLimitMbps, &item.SpeedLimitStatus); err != nil {
			return nil, fmt.Errorf("scan node forward: %w", err)
		}
		if endpointGroupID.Valid {
			item.EndpointGroupID = &endpointGroupID.Int64
		}
		if routeRuleSetID.Valid {
			item.RouteRuleSetID = &routeRuleSetID.Int64
		}
		if speedLimitID.Valid {
			item.SpeedLimitID = &speedLimitID.Int64
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (r *Repository) loadEndpointPlan(ctx context.Context, forward forwardRecord) (endpointPlan, error) {
	plan := endpointPlan{
		Strategy:        forward.Strategy,
		MaxFails:        1,
		FailTimeoutMS:   600000,
		ProbeIntervalMS: 10000,
		ProbeTimeoutMS:  3000,
		Endpoints:       make([]routeEndpoint, 0),
	}
	if forward.EndpointGroupID == nil {
		return plan, nil
	}
	var status int
	if err := r.db.QueryRowContext(ctx, `SELECT strategy,max_fails,fail_timeout_ms,probe_interval_ms,probe_timeout_ms,status FROM endpoint_groups WHERE id=?`, *forward.EndpointGroupID).Scan(
		&plan.Strategy, &plan.MaxFails, &plan.FailTimeoutMS, &plan.ProbeIntervalMS, &plan.ProbeTimeoutMS, &status,
	); err != nil {
		return endpointPlan{}, fmt.Errorf("load endpoint group %d: %w", *forward.EndpointGroupID, err)
	}
	if status != 1 {
		return plan, nil
	}

	rows, err := r.db.QueryContext(ctx, `SELECT id,name,address,priority,backup FROM endpoints WHERE group_id=? AND status=1 ORDER BY sort_index,id`, *forward.EndpointGroupID)
	if err != nil {
		return endpointPlan{}, fmt.Errorf("load endpoints: %w", err)
	}
	defer rows.Close()
	endpoints := make([]routeEndpoint, 0)
	byID := make(map[int64]routeEndpoint)
	for rows.Next() {
		var endpoint routeEndpoint
		if err := rows.Scan(&endpoint.ID, &endpoint.Name, &endpoint.Address, &endpoint.Priority, &endpoint.Backup); err != nil {
			return endpointPlan{}, fmt.Errorf("scan endpoint: %w", err)
		}
		endpoints = append(endpoints, endpoint)
		byID[endpoint.ID] = endpoint
	}
	if err := rows.Err(); err != nil {
		return endpointPlan{}, err
	}

	if forward.RouteRuleSetID != nil {
		ruleRows, err := r.db.QueryContext(ctx, `
			SELECT rr.id,rr.match_type,rr.value,rr.secondary_value,rr.negate,rr.priority,rre.endpoint_id
			FROM route_rule_sets rrs
			JOIN route_rules rr ON rr.rule_set_id=rrs.id AND rr.status=1
			JOIN route_rule_endpoints rre ON rre.rule_id=rr.id
			WHERE rrs.id=? AND rrs.status=1
			ORDER BY rr.priority DESC,rr.sort_index,rr.id,rre.endpoint_id`, *forward.RouteRuleSetID)
		if err != nil {
			return endpointPlan{}, fmt.Errorf("load route rules: %w", err)
		}
		defer ruleRows.Close()
		for ruleRows.Next() {
			var ruleID, endpointID int64
			var matchType, value, secondaryValue string
			var negate, priority int
			if err := ruleRows.Scan(&ruleID, &matchType, &value, &secondaryValue, &negate, &priority, &endpointID); err != nil {
				return endpointPlan{}, fmt.Errorf("scan route rule: %w", err)
			}
			endpoint, exists := byID[endpointID]
			if !exists {
				continue
			}
			endpoint.Name = fmt.Sprintf("rule_%d_%s", ruleID, endpoint.Name)
			endpoint.Rule = matcherExpression(matchType, value, secondaryValue, negate == 1)
			endpoint.Priority = 1_000_000 + priority
			plan.Endpoints = append(plan.Endpoints, endpoint)
		}
		if err := ruleRows.Err(); err != nil {
			return endpointPlan{}, err
		}
	}
	plan.Endpoints = append(plan.Endpoints, endpoints...)
	return plan, nil
}

func matcherExpression(matchType, value, secondaryValue string, negate bool) string {
	function := map[string]string{
		"client_ip":     "ClientIP",
		"protocol":      "Proto",
		"host":          "Host",
		"host_regexp":   "HostRegexp",
		"method":        "Method",
		"path":          "Path",
		"path_regexp":   "PathRegexp",
		"path_prefix":   "PathPrefix",
		"header":        "Header",
		"header_regexp": "HeaderRegexp",
		"query":         "Query",
		"query_regexp":  "QueryRegexp",
	}[matchType]
	if function == "" {
		return ""
	}
	arguments := "`" + value + "`"
	if secondaryValue != "" {
		arguments += ",`" + secondaryValue + "`"
	}
	expression := function + "(" + arguments + ")"
	if negate {
		return "!" + expression
	}
	return expression
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

func buildPathChain(current nodeRecord, tunnel tunnelRecord, suffix, trafficProtocol string, path [][]*tunnelNode) map[string]any {
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
			metadata := make(map[string]any)
			if item.GroupBackup == 1 {
				metadata["backup"] = true
			}
			if item.GroupMaxFails > 0 {
				metadata["maxFails"] = item.GroupMaxFails
			}
			if item.GroupFailTimeoutMS > 0 {
				metadata["failTimeout"] = item.GroupFailTimeoutMS * int64(time.Millisecond)
			}
			node := map[string]any{
				"name":      fmt.Sprintf("node_%d", index+1),
				"addr":      joinHostPort(item.Node.ServerIP, item.Port, item.Protocol, trafficProtocol),
				"connector": map[string]any{"type": "relay"},
				"dialer":    transportConfig(item.Protocol, trafficProtocol, false, tunnel),
			}
			if item.GroupPriority > 0 {
				node["matcher"] = map[string]any{"priority": item.GroupPriority}
			}
			if len(metadata) > 0 {
				node["metadata"] = metadata
			}
			if strings.TrimSpace(current.InterfaceName) != "" {
				node["interface"] = current.InterfaceName
			}
			nodes = append(nodes, node)
		}
		hops = append(hops, map[string]any{
			"name": fmt.Sprintf("hop_%d_%d", tunnel.ID, hopIndex+1),
			"selector": map[string]any{
				"strategy":    normalizeStrategy(candidates[0].Strategy),
				"maxFails":    1,
				"failTimeout": int64(600_000_000_000),
			},
			"nodes": nodes,
		})
	}
	return map[string]any{
		"name": chainName(tunnel.ID, suffix, trafficProtocol, pathHasHybrid(path)),
		"hops": hops,
	}
}

func buildRelayService(current nodeRecord, item *tunnelNode, tunnel tunnelRecord, _ []*tunnelNode, trafficProtocol string) map[string]any {
	service := map[string]any{
		"name":     relayServiceName(item.TunnelID, item.Protocol, trafficProtocol),
		"addr":     joinListenAddr(current, item.Port, item.Protocol, trafficProtocol),
		"handler":  map[string]any{"type": "relay"},
		"listener": transportConfig(item.Protocol, trafficProtocol, true, tunnel),
	}
	if item.ChainType == 3 && strings.TrimSpace(current.InterfaceName) != "" {
		service["metadata"] = map[string]any{"interface": current.InterfaceName}
	}
	return service
}

func buildForwardService(current nodeRecord, forward forwardRecord, tunnel tunnelRecord, baseName, protocol, limiterName, connectionLimiterName, admissionName, chain string, plan endpointPlan) map[string]any {
	handler := map[string]any{"type": protocol}
	listener := map[string]any{"type": protocol}
	service := map[string]any{
		"name":      baseName + "_" + protocol,
		"addr":      joinListenHost(protocolListenAddr(current, protocol), forward.Port),
		"handler":   handler,
		"listener":  listener,
		"forwarder": buildForwarder(forward.RemoteAddr, forward.Strategy, plan),
	}
	if protocol == "udp" {
		listener["metadata"] = map[string]any{"keepAlive": true}
	}
	metadata := make(map[string]any)
	if tunnel.Type == 1 && strings.TrimSpace(forward.InterfaceName) != "" {
		metadata["interface"] = forward.InterfaceName
	}
	if protocol == "tcp" && forward.ProxyProtocolReceive > 0 {
		metadata["proxyProtocol"] = forward.ProxyProtocolReceive
	}
	if len(metadata) > 0 {
		service["metadata"] = metadata
	}
	if protocol == "tcp" && forward.ProxyProtocolSend > 0 {
		handlerMetadata := map[string]any{"proxyProtocol": forward.ProxyProtocolSend}
		if planNeedsSniffing(plan) {
			handlerMetadata["sniffing"] = true
			handlerMetadata["sniffing.timeout"] = int64(5 * time.Second)
		}
		handler["metadata"] = handlerMetadata
	} else if protocol == "tcp" && planNeedsSniffing(plan) {
		handler["metadata"] = map[string]any{
			"sniffing":         true,
			"sniffing.timeout": int64(5 * time.Second),
		}
	}
	if limiterName != "" {
		service["limiter"] = limiterName
	}
	if connectionLimiterName != "" {
		service["climiter"] = connectionLimiterName
	}
	if admissionName != "" {
		service["admission"] = admissionName
	}
	if tunnel.Type == 2 {
		handler["retries"] = 1
		handler["chain"] = chain
	}
	return service
}

func buildForwarder(remoteAddr, strategy string, plan endpointPlan) map[string]any {
	nodes := make([]map[string]any, 0, len(plan.Endpoints)+1)
	for _, endpoint := range plan.Endpoints {
		node := map[string]any{
			"name": endpoint.Name,
			"addr": endpoint.Address,
		}
		metadata := make(map[string]any)
		if endpoint.Backup == 1 {
			metadata["backup"] = true
		}
		if len(metadata) > 0 {
			node["metadata"] = metadata
		}
		if endpoint.Rule != "" || endpoint.Priority > 0 {
			node["matcher"] = map[string]any{
				"rule":     endpoint.Rule,
				"priority": endpoint.Priority,
			}
		}
		nodes = append(nodes, node)
	}
	for _, address := range splitValues(remoteAddr) {
		nodes = append(nodes, map[string]any{
			"name": fmt.Sprintf("legacy_%d", len(nodes)+1),
			"addr": address,
		})
	}
	if plan.Strategy == "" {
		plan.Strategy = strategy
	}
	if plan.MaxFails <= 0 {
		plan.MaxFails = 1
	}
	if plan.FailTimeoutMS <= 0 {
		plan.FailTimeoutMS = 600000
	}
	if plan.ProbeIntervalMS <= 0 {
		plan.ProbeIntervalMS = 10000
	}
	if plan.ProbeTimeoutMS <= 0 {
		plan.ProbeTimeoutMS = 3000
	}
	return map[string]any{
		"nodes":        nodes,
		"probePeriod":  plan.ProbeIntervalMS * int64(time.Millisecond),
		"probeTimeout": plan.ProbeTimeoutMS * int64(time.Millisecond),
		"selector": map[string]any{
			"strategy":    normalizeStrategy(plan.Strategy),
			"maxFails":    plan.MaxFails,
			"failTimeout": plan.FailTimeoutMS * int64(time.Millisecond),
		},
	}
}

func planNeedsSniffing(plan endpointPlan) bool {
	for _, endpoint := range plan.Endpoints {
		if endpoint.Rule == "" {
			continue
		}
		if !strings.HasPrefix(endpoint.Rule, "ClientIP(") &&
			!strings.HasPrefix(endpoint.Rule, "!ClientIP(") &&
			!strings.HasPrefix(endpoint.Rule, "Proto(") &&
			!strings.HasPrefix(endpoint.Rule, "!Proto(") {
			return true
		}
	}
	return false
}

func transportConfig(protocol, trafficProtocol string, listener bool, tunnel tunnelRecord) map[string]any {
	if tunnel.TOTEnabled && strings.TrimSpace(tunnel.TOTSecret) != "" {
		metadata := map[string]any{
			"secret":             tunnel.TOTSecret,
			"maxPayload":         tunnel.TOTMaxPayload,
			"window":             tunnel.TOTWindow,
			"retransmitInterval": time.Duration(tunnel.TOTRetransmitMS) * time.Millisecond,
			"maxRetries":         tunnel.TOTMaxRetries,
			"handshakeTimeout":   time.Duration(tunnel.TOTHandshakeMS) * time.Millisecond,
			"maxClockSkew":       time.Duration(tunnel.TOTClockSkewMS) * time.Millisecond,
		}
		if listener {
			metadata["backlog"] = 128
			metadata["idleTTL"] = time.Duration(tunnel.TOTIdleTTLMS) * time.Millisecond
			metadata["mptcp"] = tunnel.TOTMPTCP
		} else {
			metadata["pathCount"] = tunnel.TOTPathCount
			metadata["recoveryPeriod"] = time.Duration(tunnel.TOTRecoveryMS) * time.Millisecond
		}
		return map[string]any{"type": "tot", "metadata": metadata}
	}
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

func splitValues(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			result = append(result, part)
		}
	}
	return result
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
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
	if document.CLimiters == nil {
		document.CLimiters = []map[string]any{}
	}
	if document.Admissions == nil {
		document.Admissions = []map[string]any{}
	}
	return document, nil
}

func Equivalent(expected, actual Document) bool {
	return namedItemsEqual(expected.Services, actual.Services, true) &&
		namedItemsEqual(expected.Chains, actual.Chains, false) &&
		namedItemsEqual(expected.Limiters, actual.Limiters, false) &&
		namedItemsEqual(expected.CLimiters, actual.CLimiters, false) &&
		namedItemsEqual(expected.Admissions, actual.Admissions, false)
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
