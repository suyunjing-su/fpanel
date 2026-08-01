package forwards

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/nodehub"
	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
)

type Forward struct {
	ID                   int64  `json:"id"`
	UserID               int64  `json:"userId"`
	UserName             string `json:"userName,omitempty"`
	Name                 string `json:"name"`
	TunnelID             int64  `json:"tunnelId"`
	TunnelName           string `json:"tunnelName"`
	InIP                 string `json:"inIp"`
	InPort               int    `json:"inPort"`
	RemoteAddr           string `json:"remoteAddr"`
	InterfaceName        string `json:"interfaceName,omitempty"`
	Strategy             string `json:"strategy"`
	EndpointGroupID      *int64 `json:"endpointGroupId,omitempty"`
	RouteRuleSetID       *int64 `json:"routeRuleSetId,omitempty"`
	MaxConnections       int    `json:"maxConnections"`
	MaxConnectionsPerIP  int    `json:"maxConnectionsPerIp"`
	SourceRanges         string `json:"sourceRanges"`
	SourceWhitelist      int    `json:"sourceWhitelist"`
	ProxyProtocolReceive int    `json:"proxyProtocolReceive"`
	ProxyProtocolSend    int    `json:"proxyProtocolSend"`
	Status               int    `json:"status"`
	InFlow               int64  `json:"inFlow"`
	OutFlow              int64  `json:"outFlow"`
	SortIndex            int    `json:"inx"`
	CreatedTime          int64  `json:"createdTime"`
}

type CreateRequest struct {
	UserID               int64  `json:"userId"`
	Name                 string `json:"name"`
	TunnelID             int64  `json:"tunnelId"`
	InPort               *int   `json:"inPort"`
	RemoteAddr           string `json:"remoteAddr"`
	InterfaceName        string `json:"interfaceName"`
	Strategy             string `json:"strategy"`
	EndpointGroupID      *int64 `json:"endpointGroupId"`
	RouteRuleSetID       *int64 `json:"routeRuleSetId"`
	MaxConnections       int    `json:"maxConnections"`
	MaxConnectionsPerIP  int    `json:"maxConnectionsPerIp"`
	SourceRanges         string `json:"sourceRanges"`
	SourceWhitelist      int    `json:"sourceWhitelist"`
	ProxyProtocolReceive int    `json:"proxyProtocolReceive"`
	ProxyProtocolSend    int    `json:"proxyProtocolSend"`
}

type UpdateRequest struct {
	CreateRequest
	ID int64 `json:"id"`
}

type Port struct {
	NodeID int64 `json:"nodeId"`
	Port   int   `json:"port"`
}

type PortProber interface {
	ProbePorts(context.Context, int64, nodehub.PortProbeRequest) (nodehub.PortProbeResponse, error)
}

type Repository struct {
	db      *sql.DB
	nodes   *nodes.Repository
	tunnels *tunnels.Repository
	prober  PortProber
}

func NewRepository(db *sql.DB, nodeRepo *nodes.Repository, tunnelRepo *tunnels.Repository, prober PortProber) *Repository {
	return &Repository{db: db, nodes: nodeRepo, tunnels: tunnelRepo, prober: prober}
}

func (r *Repository) List(ctx context.Context, userID int64, admin bool) ([]Forward, error) {
	query := `SELECT f.id,f.user_id,COALESCE(u.username,''),f.name,f.tunnel_id,t.name,f.remote_addr,f.interface_name,f.strategy,f.endpoint_group_id,f.route_rule_set_id,f.max_connections,f.max_connections_per_ip,f.source_ranges,f.source_whitelist,f.proxy_protocol_receive,f.proxy_protocol_send,f.ingress_bytes,f.egress_bytes,f.status,f.sort_index,f.created_at,COALESCE((SELECT MIN(fp.port) FROM forward_ports fp WHERE fp.forward_id=f.id),0),t.in_ip FROM forwards f JOIN users u ON u.id=f.user_id JOIN tunnels t ON t.id=f.tunnel_id`
	args := []any{}
	if !admin {
		query += " WHERE f.user_id=?"
		args = append(args, userID)
	}
	query += " ORDER BY f.sort_index,f.id"
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list forwards: %w", err)
	}
	defer rows.Close()
	result := make([]Forward, 0)
	for rows.Next() {
		var f Forward
		if err := rows.Scan(&f.ID, &f.UserID, &f.UserName, &f.Name, &f.TunnelID, &f.TunnelName, &f.RemoteAddr, &f.InterfaceName, &f.Strategy, &f.EndpointGroupID, &f.RouteRuleSetID, &f.MaxConnections, &f.MaxConnectionsPerIP, &f.SourceRanges, &f.SourceWhitelist, &f.ProxyProtocolReceive, &f.ProxyProtocolSend, &f.InFlow, &f.OutFlow, &f.Status, &f.SortIndex, &f.CreatedTime, &f.InPort, &f.InIP); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id, userID int64, admin bool) (Forward, error) {
	var f Forward
	if id <= 0 {
		return f, errors.New("forward id must be positive")
	}
	query := `SELECT f.id,f.user_id,COALESCE(u.username,''),f.name,f.tunnel_id,t.name,f.remote_addr,f.interface_name,f.strategy,f.endpoint_group_id,f.route_rule_set_id,f.max_connections,f.max_connections_per_ip,f.source_ranges,f.source_whitelist,f.proxy_protocol_receive,f.proxy_protocol_send,f.ingress_bytes,f.egress_bytes,f.status,f.sort_index,f.created_at,COALESCE((SELECT MIN(fp.port) FROM forward_ports fp WHERE fp.forward_id=f.id),0),t.in_ip FROM forwards f JOIN users u ON u.id=f.user_id JOIN tunnels t ON t.id=f.tunnel_id WHERE f.id=?`
	args := []any{id}
	if !admin {
		query += " AND f.user_id=?"
		args = append(args, userID)
	}
	if err := r.db.QueryRowContext(ctx, query, args...).Scan(&f.ID, &f.UserID, &f.UserName, &f.Name, &f.TunnelID, &f.TunnelName, &f.RemoteAddr, &f.InterfaceName, &f.Strategy, &f.EndpointGroupID, &f.RouteRuleSetID, &f.MaxConnections, &f.MaxConnectionsPerIP, &f.SourceRanges, &f.SourceWhitelist, &f.ProxyProtocolReceive, &f.ProxyProtocolSend, &f.InFlow, &f.OutFlow, &f.Status, &f.SortIndex, &f.CreatedTime, &f.InPort, &f.InIP); err != nil {
		return f, err
	}
	return f, nil
}

func (r *Repository) Create(ctx context.Context, request CreateRequest, actorID int64, admin bool) (int64, error) {
	if request.UserID == 0 {
		request.UserID = actorID
	}
	if !admin && request.UserID != actorID {
		return 0, errors.New("cannot create forward for another user")
	}
	if !admin && hasAdvancedControls(request) {
		return 0, errors.New("advanced forwarding controls require administrator privileges")
	}
	if err := validate(request); err != nil {
		return 0, err
	}
	if err := r.validateAdvancedReferences(ctx, request); err != nil {
		return 0, err
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer transaction.Rollback()
	if err := r.checkPermission(ctx, transaction, request.UserID, request.TunnelID); err != nil {
		return 0, err
	}
	tunnel, err := r.tunnels.Get(ctx, request.TunnelID)
	if err != nil {
		return 0, errors.New("tunnel does not exist")
	}
	entryNodes := tunnel.InNodeID
	if len(entryNodes) == 0 {
		return 0, errors.New("tunnel has no entry nodes")
	}
	if err := r.checkOnline(ctx, entryNodes); err != nil {
		return 0, err
	}
	ports, err := r.allocatePorts(ctx, transaction, entryNodes, request.InPort, 0)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	res, err := transaction.ExecContext(ctx, `INSERT INTO forwards(user_id,name,tunnel_id,remote_addr,interface_name,strategy,endpoint_group_id,route_rule_set_id,max_connections,max_connections_per_ip,source_ranges,source_whitelist,proxy_protocol_receive,proxy_protocol_send,status,sort_index,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,1,COALESCE((SELECT MAX(sort_index)+1 FROM forwards),0),?,?)`, request.UserID, strings.TrimSpace(request.Name), request.TunnelID, normalizeAddresses(request.RemoteAddr), strings.TrimSpace(request.InterfaceName), normalizeStrategy(request.Strategy), request.EndpointGroupID, request.RouteRuleSetID, request.MaxConnections, request.MaxConnectionsPerIP, normalizeSourceRanges(request.SourceRanges), request.SourceWhitelist, request.ProxyProtocolReceive, request.ProxyProtocolSend, now, now)
	if err != nil {
		return 0, fmt.Errorf("create forward: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	for _, port := range ports {
		if _, err := transaction.ExecContext(ctx, "INSERT INTO forward_ports(forward_id,node_id,port) VALUES(?,?,?)", id, port.NodeID, port.Port); err != nil {
			return 0, fmt.Errorf("reserve forward port: %w", err)
		}
	}
	if err := transaction.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

func (r *Repository) Update(ctx context.Context, request UpdateRequest, actorID int64, admin bool) error {
	if request.ID <= 0 {
		return errors.New("forward id must be positive")
	}
	if request.UserID == 0 {
		request.UserID = actorID
	}
	if !admin && hasAdvancedControls(request.CreateRequest) {
		return errors.New("advanced forwarding controls require administrator privileges")
	}
	if err := validate(request.CreateRequest); err != nil {
		return err
	}
	if err := r.validateAdvancedReferences(ctx, request.CreateRequest); err != nil {
		return err
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var owner int64
	var tunnelID int64
	var currentEndpointGroupID, currentRuleSetID sql.NullInt64
	var currentMaxConnections, currentMaxConnectionsPerIP, currentSourceWhitelist, currentProxyReceive, currentProxySend int
	var currentSourceRanges string
	if err := transaction.QueryRowContext(ctx, `SELECT user_id,tunnel_id,endpoint_group_id,route_rule_set_id,max_connections,max_connections_per_ip,source_ranges,source_whitelist,proxy_protocol_receive,proxy_protocol_send FROM forwards WHERE id=?`, request.ID).Scan(&owner, &tunnelID, &currentEndpointGroupID, &currentRuleSetID, &currentMaxConnections, &currentMaxConnectionsPerIP, &currentSourceRanges, &currentSourceWhitelist, &currentProxyReceive, &currentProxySend); err != nil {
		return err
	}
	if !admin && (currentEndpointGroupID.Valid || currentRuleSetID.Valid || currentMaxConnections > 0 || currentMaxConnectionsPerIP > 0 || strings.TrimSpace(currentSourceRanges) != "" || currentSourceWhitelist != 0 || currentProxyReceive != 0 || currentProxySend != 0) {
		return errors.New("advanced forwarding controls require administrator privileges")
	}
	if !admin && owner != actorID {
		return errors.New("forward access denied")
	}
	if !admin && request.UserID != actorID {
		return errors.New("forward access denied")
	}
	if tunnelID != request.TunnelID {
		return errors.New("changing tunnel requires recreation")
	}
	if err := r.checkPermission(ctx, transaction, request.UserID, request.TunnelID); err != nil {
		return err
	}
	tunnel, err := r.tunnels.Get(ctx, request.TunnelID)
	if err != nil {
		return err
	}
	if err := r.checkOnline(ctx, tunnel.InNodeID); err != nil {
		return err
	}
	ports, err := r.allocatePorts(ctx, transaction, tunnel.InNodeID, request.InPort, request.ID)
	if err != nil {
		return err
	}
	if _, err = transaction.ExecContext(ctx, "DELETE FROM forward_ports WHERE forward_id=?", request.ID); err != nil {
		return err
	}
	for _, port := range ports {
		if _, err = transaction.ExecContext(ctx, "INSERT INTO forward_ports(forward_id,node_id,port) VALUES(?,?,?)", request.ID, port.NodeID, port.Port); err != nil {
			return err
		}
	}
	_, err = transaction.ExecContext(ctx, `UPDATE forwards SET name=?,remote_addr=?,interface_name=?,strategy=?,endpoint_group_id=?,route_rule_set_id=?,max_connections=?,max_connections_per_ip=?,source_ranges=?,source_whitelist=?,proxy_protocol_receive=?,proxy_protocol_send=?,updated_at=? WHERE id=?`, strings.TrimSpace(request.Name), normalizeAddresses(request.RemoteAddr), strings.TrimSpace(request.InterfaceName), normalizeStrategy(request.Strategy), request.EndpointGroupID, request.RouteRuleSetID, request.MaxConnections, request.MaxConnectionsPerIP, normalizeSourceRanges(request.SourceRanges), request.SourceWhitelist, request.ProxyProtocolReceive, request.ProxyProtocolSend, time.Now().UnixMilli(), request.ID)
	if err != nil {
		return err
	}
	return transaction.Commit()
}

func (r *Repository) Delete(ctx context.Context, id, userID int64, admin bool) error {
	_, err := r.DeleteWithName(ctx, id, userID, admin)
	return err
}

func (r *Repository) DeleteWithName(ctx context.Context, id, userID int64, admin bool) (string, error) {
	if id <= 0 {
		return "", errors.New("forward id must be positive")
	}
	var owner int64
	var name string
	if err := r.db.QueryRowContext(ctx, "SELECT user_id,name FROM forwards WHERE id=?", id).Scan(&owner, &name); err != nil {
		return "", err
	}
	if !admin && owner != userID {
		return "", errors.New("forward access denied")
	}
	res, err := r.db.ExecContext(ctx, "DELETE FROM forwards WHERE id=?", id)
	if err != nil {
		return name, err
	}
	if count, err := res.RowsAffected(); err != nil {
		return name, err
	} else if count == 0 {
		return name, sql.ErrNoRows
	}
	return name, nil
}
func (r *Repository) SetStatus(ctx context.Context, id, userID int64, status int, admin bool) error {
	if status != 0 && status != 1 {
		return errors.New("invalid forward status")
	}
	var owner int64
	if err := r.db.QueryRowContext(ctx, "SELECT user_id FROM forwards WHERE id=?", id).Scan(&owner); err != nil {
		return err
	}
	if !admin && owner != userID {
		return errors.New("forward access denied")
	}
	_, err := r.db.ExecContext(ctx, "UPDATE forwards SET status=?,updated_at=? WHERE id=?", status, time.Now().UnixMilli(), id)
	return err
}
func (r *Repository) Reorder(ctx context.Context, userID int64, admin bool, items []struct {
	ID    int64 `json:"id"`
	Index int   `json:"inx"`
}) error {
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, item := range items {
		if item.Index < 0 {
			return errors.New("invalid forward order")
		}
		query := "UPDATE forwards SET sort_index=?,updated_at=? WHERE id=?"
		args := []any{item.Index, time.Now().UnixMilli(), item.ID}
		if !admin {
			query += " AND user_id=?"
			args = append(args, userID)
		}
		if _, err := transaction.ExecContext(ctx, query, args...); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (r *Repository) allocatePorts(ctx context.Context, transaction *sql.Tx, entries []tunnels.NodeSpec, requested *int, forwardID int64) ([]Port, error) {
	if len(entries) == 0 {
		return nil, errors.New("tunnel has no entry nodes")
	}
	portStart := 1
	portEnd := 65535
	nodeByID := make(map[int64]nodes.Node, len(entries))
	for _, entry := range entries {
		node, err := r.nodes.Get(ctx, entry.NodeID)
		if err != nil {
			return nil, fmt.Errorf("load entry node %d: %w", entry.NodeID, err)
		}
		nodeByID[entry.NodeID] = node
		if node.PortStart > portStart {
			portStart = node.PortStart
		}
		if node.PortEnd < portEnd {
			portEnd = node.PortEnd
		}
	}
	if portStart > portEnd {
		return nil, errors.New("entry nodes have no common port range")
	}
	if requested != nil {
		if *requested < portStart || *requested > portEnd {
			return nil, fmt.Errorf("ingress port %d is outside the common node port range", *requested)
		}
		return r.allocatePortBatch(ctx, transaction, entries, nodeByID, []int{*requested}, forwardID, true)
	}
	for start := portStart; start <= portEnd; start += 128 {
		end := min(start+127, portEnd)
		candidates := make([]int, 0, end-start+1)
		for port := start; port <= end; port++ {
			candidates = append(candidates, port)
		}
		ports, err := r.allocatePortBatch(ctx, transaction, entries, nodeByID, candidates, forwardID, false)
		if err != nil {
			return nil, err
		}
		if len(ports) > 0 {
			return ports, nil
		}
	}
	return nil, errors.New("no common ingress port available")
}

func (r *Repository) allocatePortBatch(ctx context.Context, transaction *sql.Tx, entries []tunnels.NodeSpec, nodeByID map[int64]nodes.Node, candidates []int, forwardID int64, required bool) ([]Port, error) {
	available := make(map[int]bool, len(candidates))
	for _, port := range candidates {
		available[port] = true
	}
	for _, entry := range entries {
		rows, err := transaction.QueryContext(ctx, `SELECT port FROM forward_ports WHERE node_id=? AND port BETWEEN ? AND ? AND forward_id<>?`, entry.NodeID, candidates[0], candidates[len(candidates)-1], forwardID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var port int
			if err := rows.Scan(&port); err != nil {
				rows.Close()
				return nil, err
			}
			delete(available, port)
		}
		if err := rows.Close(); err != nil {
			return nil, err
		}
	}
	if required && !available[candidates[0]] {
		return nil, fmt.Errorf("port %d is already reserved on an entry node", candidates[0])
	}
	if len(available) == 0 {
		return nil, nil
	}
	if r.prober != nil {
		owned, err := loadOwnedPorts(ctx, transaction, forwardID)
		if err != nil {
			return nil, err
		}
		type probeResult struct {
			nodeID   int64
			ports    []int
			response nodehub.PortProbeResponse
			err      error
		}
		results := make(chan probeResult, len(entries))
		pending := 0
		for _, entry := range entries {
			probePorts := make([]int, 0, len(available))
			for _, port := range candidates {
				if available[port] && !owned[Port{NodeID: entry.NodeID, Port: port}] {
					probePorts = append(probePorts, port)
				}
			}
			if len(probePorts) == 0 {
				continue
			}
			pending++
			node := nodeByID[entry.NodeID]
			go func(nodeID int64, ports []int, tcpListenAddr, udpListenAddr string) {
				response, err := r.prober.ProbePorts(ctx, nodeID, nodehub.PortProbeRequest{
					Ports:         ports,
					TCPListenAddr: tcpListenAddr,
					UDPListenAddr: udpListenAddr,
				})
				results <- probeResult{nodeID: nodeID, ports: ports, response: response, err: err}
			}(entry.NodeID, probePorts, node.TCPListenAddr, node.UDPListenAddr)
		}
		for range pending {
			probe := <-results
			if probe.err != nil {
				return nil, fmt.Errorf("probe ports on node %d: %w", probe.nodeID, probe.err)
			}
			byPort := make(map[int]nodehub.PortProbeResult, len(probe.response.Results))
			for _, result := range probe.response.Results {
				byPort[result.Port] = result
			}
			for _, port := range probe.ports {
				result, exists := byPort[port]
				if !exists {
					return nil, fmt.Errorf("node %d omitted port %d probe result", probe.nodeID, port)
				}
				if !result.Available {
					delete(available, port)
					if required {
						return nil, fmt.Errorf("port %d is unavailable on node %d: %s", port, probe.nodeID, result.Error)
					}
				}
			}
		}
	}
	for _, port := range candidates {
		if !available[port] {
			continue
		}
		result := make([]Port, 0, len(entries))
		for _, entry := range entries {
			result = append(result, Port{NodeID: entry.NodeID, Port: port})
		}
		return result, nil
	}
	return nil, nil
}

func loadOwnedPorts(ctx context.Context, transaction *sql.Tx, forwardID int64) (map[Port]bool, error) {
	result := make(map[Port]bool)
	if forwardID <= 0 {
		return result, nil
	}
	rows, err := transaction.QueryContext(ctx, `SELECT node_id,port FROM forward_ports WHERE forward_id=?`, forwardID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var port Port
		if err := rows.Scan(&port.NodeID, &port.Port); err != nil {
			return nil, err
		}
		result[port] = true
	}
	return result, rows.Err()
}
func (r *Repository) checkPermission(ctx context.Context, transaction *sql.Tx, userID, tunnelID int64) error {
	var status int
	var expires int64
	if err := transaction.QueryRowContext(ctx, "SELECT status,expires_at FROM user_tunnels WHERE user_id=? AND tunnel_id=?", userID, tunnelID).Scan(&status, &expires); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("user does not have tunnel permission")
		}
		return err
	}
	if status != 1 {
		return errors.New("user tunnel permission is disabled")
	}
	if expires > 0 && expires <= time.Now().UnixMilli() {
		return errors.New("user tunnel permission expired")
	}
	return nil
}
func (r *Repository) checkOnline(ctx context.Context, entries []tunnels.NodeSpec) error {
	for _, entry := range entries {
		node, err := r.nodes.Get(ctx, entry.NodeID)
		if err != nil {
			return err
		}
		if node.Status != 1 {
			return fmt.Errorf("entry node %d is offline", entry.NodeID)
		}
	}
	return nil
}
func validate(r CreateRequest) error {
	if strings.TrimSpace(r.Name) == "" || len([]rune(r.Name)) > 100 {
		return errors.New("forward name is required")
	}
	if r.TunnelID <= 0 {
		return errors.New("tunnel is required")
	}
	if strings.TrimSpace(r.RemoteAddr) == "" && r.EndpointGroupID == nil {
		return errors.New("remote address or endpoint group is required")
	}
	if strings.TrimSpace(r.RemoteAddr) != "" {
		for _, value := range strings.Split(r.RemoteAddr, ",") {
			if _, _, err := net.SplitHostPort(strings.TrimSpace(value)); err != nil {
				return fmt.Errorf("invalid remote address: %s", value)
			}
		}
	}
	if r.MaxConnections < 0 || r.MaxConnectionsPerIP < 0 {
		return errors.New("connection limits cannot be negative")
	}
	if r.SourceWhitelist != 0 && r.SourceWhitelist != 1 {
		return errors.New("invalid source range mode")
	}
	if r.ProxyProtocolReceive < 0 || r.ProxyProtocolReceive > 2 || r.ProxyProtocolSend < 0 || r.ProxyProtocolSend > 2 {
		return errors.New("Proxy Protocol version must be 0, 1, or 2")
	}
	if r.ProxyProtocolSend > 0 && r.TunnelID <= 0 {
		return errors.New("Proxy Protocol send requires a tunnel")
	}
	for _, value := range splitSourceRanges(r.SourceRanges) {
		if net.ParseIP(value) == nil {
			if _, _, err := net.ParseCIDR(value); err != nil {
				return fmt.Errorf("invalid source range: %s", value)
			}
		}
	}
	return nil
}

func (r *Repository) validateAdvancedReferences(ctx context.Context, request CreateRequest) error {
	if request.EndpointGroupID != nil {
		if *request.EndpointGroupID <= 0 {
			return errors.New("endpoint group id must be positive")
		}
		var status, endpoints int
		if err := r.db.QueryRowContext(ctx, `SELECT eg.status,COUNT(e.id) FROM endpoint_groups eg LEFT JOIN endpoints e ON e.group_id=eg.id AND e.status=1 WHERE eg.id=? GROUP BY eg.id`, *request.EndpointGroupID).Scan(&status, &endpoints); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("endpoint group does not exist")
			}
			return err
		}
		if status != 1 || endpoints == 0 {
			return errors.New("endpoint group is disabled or has no enabled endpoints")
		}
	}
	if request.RouteRuleSetID != nil {
		if request.EndpointGroupID == nil {
			return errors.New("route rule set requires an endpoint group")
		}
		if *request.RouteRuleSetID <= 0 {
			return errors.New("route rule set id must be positive")
		}
		var status int
		if err := r.db.QueryRowContext(ctx, `SELECT status FROM route_rule_sets WHERE id=?`, *request.RouteRuleSetID).Scan(&status); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return errors.New("route rule set does not exist")
			}
			return err
		}
		if status != 1 {
			return errors.New("route rule set is disabled")
		}
		var outside int
		if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM route_rule_endpoints rre JOIN route_rules rr ON rr.id=rre.rule_id JOIN endpoints e ON e.id=rre.endpoint_id WHERE rr.rule_set_id=? AND e.group_id<>?`, *request.RouteRuleSetID, *request.EndpointGroupID).Scan(&outside); err != nil {
			return err
		}
		if outside > 0 {
			return errors.New("route rule set references endpoints outside the selected group")
		}
	}
	return nil
}

func hasAdvancedControls(request CreateRequest) bool {
	return request.EndpointGroupID != nil || request.RouteRuleSetID != nil || request.MaxConnections > 0 || request.MaxConnectionsPerIP > 0 || strings.TrimSpace(request.SourceRanges) != "" || request.SourceWhitelist != 0 || request.ProxyProtocolReceive != 0 || request.ProxyProtocolSend != 0
}

func splitSourceRanges(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == ',' || r == '\n' || r == '\r' })
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func normalizeSourceRanges(value string) string {
	return strings.Join(splitSourceRanges(value), ",")
}
func normalizeAddresses(value string) string {
	parts := strings.FieldsFunc(value, func(r rune) bool { return r == '\n' || r == ',' })
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return strings.Join(out, ",")
}
func normalizeStrategy(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "round" || value == "rand" {
		return value
	}
	return "fifo"
}
func ParsePort(value string) (*int, error) {
	if strings.TrimSpace(value) == "" {
		return nil, nil
	}
	port, err := strconv.Atoi(value)
	if err != nil || port < 1 || port > 65535 {
		return nil, errors.New("invalid port")
	}
	return &port, nil
}
