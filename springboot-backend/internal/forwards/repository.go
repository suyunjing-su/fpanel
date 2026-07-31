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

	"github.com/suyunjing-su/fpanel/backend/internal/nodes"
	"github.com/suyunjing-su/fpanel/backend/internal/tunnels"
)

type Forward struct {
	ID            int64  `json:"id"`
	UserID        int64  `json:"userId"`
	UserName      string `json:"userName,omitempty"`
	Name          string `json:"name"`
	TunnelID      int64  `json:"tunnelId"`
	TunnelName    string `json:"tunnelName"`
	InIP          string `json:"inIp"`
	InPort        int    `json:"inPort"`
	RemoteAddr    string `json:"remoteAddr"`
	InterfaceName string `json:"interfaceName,omitempty"`
	Strategy      string `json:"strategy"`
	Status        int    `json:"status"`
	InFlow        int64  `json:"inFlow"`
	OutFlow       int64  `json:"outFlow"`
	SortIndex     int    `json:"inx"`
	CreatedTime   int64  `json:"createdTime"`
}

type CreateRequest struct {
	UserID        int64  `json:"userId"`
	Name          string `json:"name"`
	TunnelID      int64  `json:"tunnelId"`
	InPort        *int   `json:"inPort"`
	RemoteAddr    string `json:"remoteAddr"`
	InterfaceName string `json:"interfaceName"`
	Strategy      string `json:"strategy"`
}

type UpdateRequest struct {
	CreateRequest
	ID int64 `json:"id"`
}

type Port struct {
	NodeID int64 `json:"nodeId"`
	Port   int   `json:"port"`
}

type Repository struct {
	db      *sql.DB
	nodes   *nodes.Repository
	tunnels *tunnels.Repository
}

func NewRepository(db *sql.DB, nodeRepo *nodes.Repository, tunnelRepo *tunnels.Repository) *Repository {
	return &Repository{db: db, nodes: nodeRepo, tunnels: tunnelRepo}
}

func (r *Repository) List(ctx context.Context, userID int64, admin bool) ([]Forward, error) {
	query := `SELECT f.id,f.user_id,COALESCE(u.username,''),f.name,f.tunnel_id,t.name,f.remote_addr,f.interface_name,f.strategy,f.ingress_bytes,f.egress_bytes,f.status,f.sort_index,f.created_at,COALESCE((SELECT MIN(fp.port) FROM forward_ports fp WHERE fp.forward_id=f.id),0),t.in_ip FROM forwards f JOIN users u ON u.id=f.user_id JOIN tunnels t ON t.id=f.tunnel_id`
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
		if err := rows.Scan(&f.ID, &f.UserID, &f.UserName, &f.Name, &f.TunnelID, &f.TunnelName, &f.RemoteAddr, &f.InterfaceName, &f.Strategy, &f.InFlow, &f.OutFlow, &f.Status, &f.SortIndex, &f.CreatedTime, &f.InPort, &f.InIP); err != nil {
			return nil, err
		}
		result = append(result, f)
	}
	return result, rows.Err()
}

func (r *Repository) Create(ctx context.Context, request CreateRequest, actorID int64, admin bool) (int64, error) {
	if request.UserID == 0 {
		request.UserID = actorID
	}
	if !admin && request.UserID != actorID {
		return 0, errors.New("cannot create forward for another user")
	}
	if err := validate(request); err != nil {
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
	ports, err := r.allocatePorts(ctx, transaction, entryNodes, request.InPort)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	res, err := transaction.ExecContext(ctx, `INSERT INTO forwards(user_id,name,tunnel_id,remote_addr,interface_name,strategy,status,sort_index,created_at,updated_at) VALUES(?,?,?,?,?,?,1,COALESCE((SELECT MAX(sort_index)+1 FROM forwards),0),?,?)`, request.UserID, strings.TrimSpace(request.Name), request.TunnelID, normalizeAddresses(request.RemoteAddr), strings.TrimSpace(request.InterfaceName), normalizeStrategy(request.Strategy), now, now)
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
	if err := validate(request.CreateRequest); err != nil {
		return err
	}
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	var owner int64
	var tunnelID int64
	if err := transaction.QueryRowContext(ctx, "SELECT user_id,tunnel_id FROM forwards WHERE id=?", request.ID).Scan(&owner, &tunnelID); err != nil {
		return err
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
	if _, err = transaction.ExecContext(ctx, "DELETE FROM forward_ports WHERE forward_id=?", request.ID); err != nil {
		return err
	}
	ports, err := r.allocatePorts(ctx, transaction, tunnel.InNodeID, request.InPort)
	if err != nil {
		return err
	}
	for _, port := range ports {
		if _, err = transaction.ExecContext(ctx, "INSERT INTO forward_ports(forward_id,node_id,port) VALUES(?,?,?)", request.ID, port.NodeID, port.Port); err != nil {
			return err
		}
	}
	_, err = transaction.ExecContext(ctx, "UPDATE forwards SET name=?,remote_addr=?,interface_name=?,strategy=?,updated_at=? WHERE id=?", strings.TrimSpace(request.Name), normalizeAddresses(request.RemoteAddr), strings.TrimSpace(request.InterfaceName), normalizeStrategy(request.Strategy), time.Now().UnixMilli(), request.ID)
	if err != nil {
		return err
	}
	return transaction.Commit()
}

func (r *Repository) Delete(ctx context.Context, id, userID int64, admin bool) error {
	var owner int64
	if err := r.db.QueryRowContext(ctx, "SELECT user_id FROM forwards WHERE id=?", id).Scan(&owner); err != nil {
		return err
	}
	if !admin && owner != userID {
		return errors.New("forward access denied")
	}
	res, err := r.db.ExecContext(ctx, "DELETE FROM forwards WHERE id=?", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
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

func (r *Repository) allocatePorts(ctx context.Context, transaction *sql.Tx, entries []tunnels.NodeSpec, requested *int) ([]Port, error) {
	if len(entries) == 0 {
		return nil, errors.New("tunnel has no entry nodes")
	}
	portStart := 1
	portEnd := 65535
	for _, entry := range entries {
		node, err := r.nodes.Get(ctx, entry.NodeID)
		if err != nil {
			return nil, fmt.Errorf("load entry node %d: %w", entry.NodeID, err)
		}
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
		out := make([]Port, 0, len(entries))
		for _, entry := range entries {
			var count int
			if err := transaction.QueryRowContext(ctx, "SELECT COUNT(1) FROM forward_ports WHERE node_id=? AND port=?", entry.NodeID, *requested).Scan(&count); err != nil {
				return nil, err
			}
			if count > 0 {
				return nil, fmt.Errorf("port %d is already in use on node %d", *requested, entry.NodeID)
			}
			out = append(out, Port{NodeID: entry.NodeID, Port: *requested})
		}
		return out, nil
	}
	for port := portStart; port <= portEnd; port++ {
		available := true
		for _, entry := range entries {
			var count int
			if err := transaction.QueryRowContext(ctx, "SELECT COUNT(1) FROM forward_ports WHERE node_id=? AND port=?", entry.NodeID, port).Scan(&count); err != nil {
				return nil, err
			}
			if count > 0 {
				available = false
				break
			}
		}
		if available {
			out := make([]Port, 0, len(entries))
			for _, entry := range entries {
				out = append(out, Port{NodeID: entry.NodeID, Port: port})
			}
			return out, nil
		}
	}
	return nil, errors.New("no common ingress port available")
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
	if strings.TrimSpace(r.RemoteAddr) == "" {
		return errors.New("remote address is required")
	}
	for _, value := range strings.Split(r.RemoteAddr, ",") {
		if _, _, err := net.SplitHostPort(strings.TrimSpace(value)); err != nil {
			return fmt.Errorf("invalid remote address: %s", value)
		}
	}
	return nil
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
