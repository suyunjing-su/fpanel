package nodes

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

type Node struct {
	ID               int64   `json:"id"`
	Name             string  `json:"name"`
	IP               string  `json:"ip"`
	ServerIP         string  `json:"serverIp"`
	PortStart        int     `json:"portSta"`
	PortEnd          int     `json:"portEnd"`
	Version          string  `json:"version"`
	HTTP             int     `json:"http"`
	TLS              int     `json:"tls"`
	Socks            int     `json:"socks"`
	Status           int     `json:"status"`
	Uptime           uint64  `json:"uptime,omitempty"`
	BytesReceived    uint64  `json:"bytes_received,omitempty"`
	BytesTransmitted uint64  `json:"bytes_transmitted,omitempty"`
	CPUUsage         float64 `json:"cpu_usage,omitempty"`
	MemoryUsage      float64 `json:"memory_usage,omitempty"`
}

type CreateRequest struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	ServerIP  string `json:"serverIp"`
	PortStart int    `json:"portSta"`
	PortEnd   int    `json:"portEnd"`
	HTTP      int    `json:"http"`
	TLS       int    `json:"tls"`
	Socks     int    `json:"socks"`
}

type UpdateRequest struct {
	CreateRequest
	ID int64 `json:"id"`
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context) ([]Node, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, ip, server_ip, port_start, port_end, version, http, tls, socks, status, uptime, bytes_received, bytes_transmitted, cpu_usage, memory_usage FROM nodes ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list nodes: %w", err)
	}
	defer rows.Close()
	result := make([]Node, 0)
	for rows.Next() {
		var node Node
		if err := rows.Scan(&node.ID, &node.Name, &node.IP, &node.ServerIP, &node.PortStart, &node.PortEnd, &node.Version, &node.HTTP, &node.TLS, &node.Socks, &node.Status, &node.Uptime, &node.BytesReceived, &node.BytesTransmitted, &node.CPUUsage, &node.MemoryUsage); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		result = append(result, node)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate nodes: %w", err)
	}
	return result, nil
}

func (r *Repository) Create(ctx context.Context, request CreateRequest) (int64, error) {
	if err := validate(request); err != nil {
		return 0, err
	}
	secret, err := newSecret()
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	result, err := r.db.ExecContext(ctx, `INSERT INTO nodes(name, ip, server_ip, port_start, port_end, secret, http, tls, socks, created_at, updated_at) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, request.Name, request.IP, request.ServerIP, request.PortStart, request.PortEnd, secret, request.HTTP, request.TLS, request.Socks, now, now)
	if err != nil {
		return 0, fmt.Errorf("create node: %w", err)
	}
	return result.LastInsertId()
}

func (r *Repository) Update(ctx context.Context, request UpdateRequest) error {
	if request.ID <= 0 {
		return errors.New("node id must be positive")
	}
	if err := validate(request.CreateRequest); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE nodes SET name = ?, ip = ?, server_ip = ?, port_start = ?, port_end = ?, http = ?, tls = ?, socks = ?, updated_at = ? WHERE id = ?`, request.Name, request.IP, request.ServerIP, request.PortStart, request.PortEnd, request.HTTP, request.TLS, request.Socks, time.Now().UnixMilli(), request.ID)
	if err != nil {
		return fmt.Errorf("update node: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) LookupSecret(ctx context.Context, secret string, id *int64) error {
	if strings.TrimSpace(secret) == "" {
		return errors.New("node secret is required")
	}
	if err := r.db.QueryRowContext(ctx, "SELECT id FROM nodes WHERE secret = ?", secret).Scan(id); err != nil {
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
	_, err := r.db.ExecContext(ctx, "UPDATE nodes SET status = ?, version = ?, updated_at = ? WHERE id = ?", status, version, time.Now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("update node status: %w", err)
	}
	return nil
}

func (r *Repository) SetConnectionState(ctx context.Context, id int64, status int, version string, httpFlag, tlsFlag, socksFlag int) error {
	if status != 0 && status != 1 {
		return errors.New("invalid node status")
	}
	for _, value := range []int{httpFlag, tlsFlag, socksFlag} {
		if value != 0 && value != 1 {
			return errors.New("invalid node protocol flag")
		}
	}
	_, err := r.db.ExecContext(ctx, `UPDATE nodes SET status = ?, version = ?, http = ?, tls = ?, socks = ?, updated_at = ? WHERE id = ?`, status, version, httpFlag, tlsFlag, socksFlag, time.Now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("update node connection state: %w", err)
	}
	return nil
}

func (r *Repository) SetTelemetry(ctx context.Context, id int64, uptime, received, transmitted uint64, cpu, memory float64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE nodes SET uptime = ?, bytes_received = ?, bytes_transmitted = ?, cpu_usage = ?, memory_usage = ?, updated_at = ? WHERE id = ?`, uptime, received, transmitted, cpu, memory, time.Now().UnixMilli(), id)
	if err != nil {
		return fmt.Errorf("update node telemetry: %w", err)
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	result, err := r.db.ExecContext(ctx, "DELETE FROM nodes WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete node: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func validate(request CreateRequest) error {
	if strings.TrimSpace(request.Name) == "" || strings.TrimSpace(request.IP) == "" || strings.TrimSpace(request.ServerIP) == "" {
		return errors.New("node name and addresses are required")
	}
	if request.PortStart < 1 || request.PortStart > 65535 || request.PortEnd < request.PortStart || request.PortEnd > 65535 {
		return errors.New("invalid node port range")
	}
	for _, value := range []int{request.HTTP, request.TLS, request.Socks} {
		if value != 0 && value != 1 {
			return errors.New("node protocol flags must be 0 or 1")
		}
	}
	for _, address := range strings.Split(request.IP, ",") {
		if strings.TrimSpace(address) == "" {
			return errors.New("node address cannot be empty")
		}
		if net.ParseIP(strings.TrimSpace(address)) == nil {
			return fmt.Errorf("invalid node address: %s", address)
		}
	}
	if net.ParseIP(strings.TrimSpace(request.ServerIP)) == nil {
		return errors.New("invalid server address")
	}
	return nil
}

func newSecret() (string, error) {
	var value [24]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate node secret: %w", err)
	}
	return hex.EncodeToString(value[:]), nil
}
