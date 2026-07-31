package nodes

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
)

type ControllerStatus struct {
	Address             string `json:"address"`
	Active              bool   `json:"active"`
	ConsecutiveFailures int    `json:"consecutiveFailures"`
	LastSuccessAt       int64  `json:"lastSuccessAt"`
	LastFailureAt       int64  `json:"lastFailureAt"`
	LastError           string `json:"lastError"`
}

type TOTTelemetry struct {
	Sessions        int    `json:"sessions"`
	ActivePaths     int    `json:"activePaths"`
	PendingFrames   int    `json:"pendingFrames"`
	SentFrames      uint64 `json:"sentFrames"`
	ReceivedFrames  uint64 `json:"receivedFrames"`
	Retransmits     uint64 `json:"retransmits"`
	DuplicateFrames uint64 `json:"duplicateFrames"`
	PathFailures    uint64 `json:"pathFailures"`
}

type Node struct {
	ID               int64              `json:"id"`
	Name             string             `json:"name"`
	IP               string             `json:"ip"`
	ServerIP         string             `json:"serverIp"`
	PortStart        int                `json:"portSta"`
	PortEnd          int                `json:"portEnd"`
	Port             string             `json:"port"`
	Version          string             `json:"version"`
	HTTP             int                `json:"http"`
	TLS              int                `json:"tls"`
	Socks            int                `json:"socks"`
	Status           int                `json:"status"`
	Uptime           uint64             `json:"uptime,omitempty"`
	BytesReceived    uint64             `json:"bytes_received,omitempty"`
	BytesTransmitted uint64             `json:"bytes_transmitted,omitempty"`
	CPUUsage         float64            `json:"cpu_usage,omitempty"`
	MemoryUsage      float64            `json:"memory_usage,omitempty"`
	MaxBandwidthMbps int                `json:"maxBandwidthMbps"`
	InterfaceName    string             `json:"interfaceName"`
	TCPListenAddr    string             `json:"tcpListenAddr"`
	UDPListenAddr    string             `json:"udpListenAddr"`
	Controllers      []ControllerStatus `json:"controllers"`
	TOT              TOTTelemetry       `json:"tot"`
}

type CreateRequest struct {
	Name             string `json:"name"`
	IP               string `json:"ip"`
	ServerIP         string `json:"serverIp"`
	PortStart        int    `json:"portSta"`
	PortEnd          int    `json:"portEnd"`
	Port             string `json:"port"`
	HTTP             int    `json:"http"`
	TLS              int    `json:"tls"`
	Socks            int    `json:"socks"`
	MaxBandwidthMbps int    `json:"maxBandwidthMbps"`
	InterfaceName    string `json:"interfaceName"`
	TCPListenAddr    string `json:"tcpListenAddr"`
	UDPListenAddr    string `json:"udpListenAddr"`
}
type UpdateRequest struct {
	CreateRequest
	ID int64 `json:"id"`
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context) ([]Node, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,ip,server_ip,port_start,port_end,version,http,tls,socks,status,uptime,bytes_received,bytes_transmitted,cpu_usage,memory_usage,max_bandwidth_mbps,interface_name,tcp_listen_addr,udp_listen_addr,controller_statuses,tot_sessions,tot_active_paths,tot_pending_frames,tot_sent_frames,tot_received_frames,tot_retransmits,tot_duplicate_frames,tot_path_failures FROM nodes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()
	result := make([]Node, 0)
	for rows.Next() {
		var node Node
		var controllerStatuses string
		if err := rows.Scan(&node.ID, &node.Name, &node.IP, &node.ServerIP, &node.PortStart, &node.PortEnd, &node.Version, &node.HTTP, &node.TLS, &node.Socks, &node.Status, &node.Uptime, &node.BytesReceived, &node.BytesTransmitted, &node.CPUUsage, &node.MemoryUsage, &node.MaxBandwidthMbps, &node.InterfaceName, &node.TCPListenAddr, &node.UDPListenAddr, &controllerStatuses, &node.TOT.Sessions, &node.TOT.ActivePaths, &node.TOT.PendingFrames, &node.TOT.SentFrames, &node.TOT.ReceivedFrames, &node.TOT.Retransmits, &node.TOT.DuplicateFrames, &node.TOT.PathFailures); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		if err := json.Unmarshal([]byte(controllerStatuses), &node.Controllers); err != nil {
			return nil, fmt.Errorf("decode node controller diagnostics: %w", err)
		}
		node.Port = fmt.Sprintf("%d-%d", node.PortStart, node.PortEnd)
		result = append(result, node)
	}
	return result, rows.Err()
}
func (r *Repository) Get(ctx context.Context, id int64) (Node, error) {
	var n Node
	var controllerStatuses string
	err := r.db.QueryRowContext(ctx, `SELECT id,name,ip,server_ip,port_start,port_end,version,http,tls,socks,status,uptime,bytes_received,bytes_transmitted,cpu_usage,memory_usage,max_bandwidth_mbps,interface_name,tcp_listen_addr,udp_listen_addr,controller_statuses,tot_sessions,tot_active_paths,tot_pending_frames,tot_sent_frames,tot_received_frames,tot_retransmits,tot_duplicate_frames,tot_path_failures FROM nodes WHERE id=?`, id).Scan(&n.ID, &n.Name, &n.IP, &n.ServerIP, &n.PortStart, &n.PortEnd, &n.Version, &n.HTTP, &n.TLS, &n.Socks, &n.Status, &n.Uptime, &n.BytesReceived, &n.BytesTransmitted, &n.CPUUsage, &n.MemoryUsage, &n.MaxBandwidthMbps, &n.InterfaceName, &n.TCPListenAddr, &n.UDPListenAddr, &controllerStatuses, &n.TOT.Sessions, &n.TOT.ActivePaths, &n.TOT.PendingFrames, &n.TOT.SentFrames, &n.TOT.ReceivedFrames, &n.TOT.Retransmits, &n.TOT.DuplicateFrames, &n.TOT.PathFailures)
	if err != nil {
		return n, err
	}
	if err := json.Unmarshal([]byte(controllerStatuses), &n.Controllers); err != nil {
		return n, fmt.Errorf("decode node controller diagnostics: %w", err)
	}
	n.Port = fmt.Sprintf("%d-%d", n.PortStart, n.PortEnd)
	return n, nil
}
func (r *Repository) Create(ctx context.Context, req CreateRequest) (int64, error) {
	var err error
	req, err = normalizeRequest(req)
	if err != nil {
		return 0, err
	}
	if err := validate(req); err != nil {
		return 0, err
	}
	secret, err := newSecret()
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `INSERT INTO nodes(name,ip,server_ip,port_start,port_end,secret,http,tls,socks,max_bandwidth_mbps,interface_name,tcp_listen_addr,udp_listen_addr,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, req.Name, req.IP, req.ServerIP, req.PortStart, req.PortEnd, secret, req.HTTP, req.TLS, req.Socks, req.MaxBandwidthMbps, strings.TrimSpace(req.InterfaceName), normalizeListenAddr(req.TCPListenAddr), normalizeListenAddr(req.UDPListenAddr), now, now)
	if err != nil {
		return 0, fmt.Errorf("create node: %w", err)
	}
	return res.LastInsertId()
}
func (r *Repository) Update(ctx context.Context, req UpdateRequest) error {
	if req.ID <= 0 {
		return errors.New("node id must be positive")
	}
	normalized, err := normalizeRequest(req.CreateRequest)
	if err != nil {
		return err
	}
	req.CreateRequest = normalized
	if err := validate(req.CreateRequest); err != nil {
		return err
	}
	res, err := r.db.ExecContext(ctx, `UPDATE nodes SET name=?,ip=?,server_ip=?,port_start=?,port_end=?,http=?,tls=?,socks=?,max_bandwidth_mbps=?,interface_name=?,tcp_listen_addr=?,udp_listen_addr=?,updated_at=? WHERE id=?`, req.Name, req.IP, req.ServerIP, req.PortStart, req.PortEnd, req.HTTP, req.TLS, req.Socks, req.MaxBandwidthMbps, strings.TrimSpace(req.InterfaceName), normalizeListenAddr(req.TCPListenAddr), normalizeListenAddr(req.UDPListenAddr), time.Now().UnixMilli(), req.ID)
	if err != nil {
		return fmt.Errorf("update node: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func (r *Repository) LookupSecret(ctx context.Context, secret string, id *int64) error {
	if strings.TrimSpace(secret) == "" {
		return errors.New("node secret is required")
	}
	if err := r.db.QueryRowContext(ctx, "SELECT id FROM nodes WHERE secret=?", secret).Scan(id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("invalid node secret")
		}
		return fmt.Errorf("lookup node secret: %w", err)
	}
	return nil
}
func (r *Repository) SetStatus(ctx context.Context, id int64, status int, version string) error {
	if status != 0 && status != 1 {
		return errors.New("invalid node status")
	}
	_, err := r.db.ExecContext(ctx, "UPDATE nodes SET status=?,version=?,updated_at=? WHERE id=?", status, version, time.Now().UnixMilli(), id)
	return err
}
func (r *Repository) SetConnectionState(ctx context.Context, id int64, status int, version string, httpFlag, tlsFlag, socksFlag int) error {
	if status != 0 && status != 1 {
		return errors.New("invalid node status")
	}
	for _, v := range []int{httpFlag, tlsFlag, socksFlag} {
		if v != 0 && v != 1 {
			return errors.New("invalid node protocol flag")
		}
	}
	_, err := r.db.ExecContext(ctx, `UPDATE nodes SET status=?,version=?,http=?,tls=?,socks=?,updated_at=? WHERE id=?`, status, version, httpFlag, tlsFlag, socksFlag, time.Now().UnixMilli(), id)
	return err
}
func (r *Repository) SetTelemetry(ctx context.Context, id int64, uptime, received, transmitted uint64, cpu, memory float64, controllerStatuses string, telemetry ...TOTTelemetry) error {
	if !json.Valid([]byte(controllerStatuses)) {
		return errors.New("invalid controller diagnostics")
	}
	var tot TOTTelemetry
	if len(telemetry) > 0 {
		tot = telemetry[0]
	}
	_, err := r.db.ExecContext(ctx, `UPDATE nodes SET uptime=?,bytes_received=?,bytes_transmitted=?,cpu_usage=?,memory_usage=?,controller_statuses=?,tot_sessions=?,tot_active_paths=?,tot_pending_frames=?,tot_sent_frames=?,tot_received_frames=?,tot_retransmits=?,tot_duplicate_frames=?,tot_path_failures=?,updated_at=? WHERE id=?`, uptime, received, transmitted, cpu, memory, controllerStatuses, tot.Sessions, tot.ActivePaths, tot.PendingFrames, tot.SentFrames, tot.ReceivedFrames, tot.Retransmits, tot.DuplicateFrames, tot.PathFailures, time.Now().UnixMilli(), id)
	return err
}
func (r *Repository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM nodes WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func normalizeRequest(req CreateRequest) (CreateRequest, error) {
	if strings.TrimSpace(req.IP) == "" {
		req.IP = strings.TrimSpace(req.ServerIP)
	}
	if req.PortStart == 0 && req.PortEnd == 0 && strings.TrimSpace(req.Port) != "" {
		parts := strings.Split(strings.TrimSpace(req.Port), "-")
		if len(parts) == 1 {
			port, err := strconv.Atoi(strings.TrimSpace(parts[0]))
			if err != nil {
				return req, errors.New("invalid node port range")
			}
			req.PortStart, req.PortEnd = port, port
		} else if len(parts) == 2 {
			start, startErr := strconv.Atoi(strings.TrimSpace(parts[0]))
			end, endErr := strconv.Atoi(strings.TrimSpace(parts[1]))
			if startErr != nil || endErr != nil {
				return req, errors.New("invalid node port range")
			}
			req.PortStart, req.PortEnd = start, end
		} else {
			return req, errors.New("invalid node port range")
		}
	}
	return req, nil
}

func validate(req CreateRequest) error {
	if strings.TrimSpace(req.Name) == "" || strings.TrimSpace(req.IP) == "" || strings.TrimSpace(req.ServerIP) == "" {
		return errors.New("node name and addresses are required")
	}
	if req.PortStart < 1 || req.PortStart > 65535 || req.PortEnd < req.PortStart || req.PortEnd > 65535 {
		return errors.New("invalid node port range")
	}
	if req.MaxBandwidthMbps < 0 {
		return errors.New("maximum bandwidth cannot be negative")
	}
	for _, v := range []int{req.HTTP, req.TLS, req.Socks} {
		if v != 0 && v != 1 {
			return errors.New("node protocol flags must be 0 or 1")
		}
	}
	for _, address := range strings.Split(req.IP, ",") {
		if !validHost(strings.TrimSpace(address)) {
			return fmt.Errorf("invalid node address: %s", address)
		}
	}
	if !validHost(strings.TrimSpace(req.ServerIP)) {
		return errors.New("invalid server address")
	}
	return nil
}

func validHost(value string) bool {
	if net.ParseIP(value) != nil {
		return true
	}
	if value == "" || len(value) > 253 || strings.HasPrefix(value, ".") || strings.HasSuffix(value, ".") {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if character != '-' && (character < '0' || character > '9') && (character < 'a' || character > 'z') && (character < 'A' || character > 'Z') {
				return false
			}
		}
	}
	return true
}
func normalizeListenAddr(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "[::]"
	}
	return value
}

func newSecret() (string, error) {
	var b [24]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate node secret: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}
