package speedlimits

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Rule struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Speed       int    `json:"speed"`
	TunnelID    int64  `json:"tunnelId"`
	TunnelName  string `json:"tunnelName"`
	Status      int    `json:"status"`
	CreatedTime int64  `json:"createdTime"`
}

type CreateRequest struct {
	Name     string `json:"name"`
	Speed    int    `json:"speed"`
	TunnelID int64  `json:"tunnelId"`
	Status   int    `json:"status"`
}

type UpdateRequest struct {
	CreateRequest
	ID int64 `json:"id"`
}

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context) ([]Rule, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT sl.id,sl.name,sl.speed_mbps,sl.tunnel_id,t.name,sl.status,sl.created_at FROM speed_limits sl JOIN tunnels t ON t.id=sl.tunnel_id ORDER BY sl.id`)
	if err != nil {
		return nil, fmt.Errorf("list speed limits: %w", err)
	}
	defer rows.Close()
	result := make([]Rule, 0)
	for rows.Next() {
		var rule Rule
		if err := rows.Scan(&rule.ID, &rule.Name, &rule.Speed, &rule.TunnelID, &rule.TunnelName, &rule.Status, &rule.CreatedTime); err != nil {
			return nil, err
		}
		result = append(result, rule)
	}
	return result, rows.Err()
}

func (r *Repository) Get(ctx context.Context, id int64) (Rule, error) {
	var rule Rule
	err := r.db.QueryRowContext(ctx, `SELECT sl.id,sl.name,sl.speed_mbps,sl.tunnel_id,t.name,sl.status,sl.created_at FROM speed_limits sl JOIN tunnels t ON t.id=sl.tunnel_id WHERE sl.id=?`, id).Scan(&rule.ID, &rule.Name, &rule.Speed, &rule.TunnelID, &rule.TunnelName, &rule.Status, &rule.CreatedTime)
	return rule, err
}

func (r *Repository) NodeIDs(ctx context.Context, tunnelID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT DISTINCT node_id FROM tunnel_nodes WHERE tunnel_id=? ORDER BY node_id", tunnelID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

func (r *Repository) Create(ctx context.Context, request CreateRequest) (int64, error) {
	if err := validate(request); err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	result, err := r.db.ExecContext(ctx, `INSERT INTO speed_limits(name,speed_mbps,tunnel_id,status,created_at,updated_at) VALUES(?,?,?,?,?,?)`, strings.TrimSpace(request.Name), request.Speed, request.TunnelID, normalizeStatus(request.Status), now, now)
	if err != nil {
		return 0, fmt.Errorf("create speed limit: %w", err)
	}
	return result.LastInsertId()
}

func (r *Repository) Update(ctx context.Context, request UpdateRequest) error {
	if request.ID <= 0 {
		return errors.New("speed limit id must be positive")
	}
	if err := validate(request.CreateRequest); err != nil {
		return err
	}
	var currentTunnelID int64
	if err := r.db.QueryRowContext(ctx, "SELECT tunnel_id FROM speed_limits WHERE id=?", request.ID).Scan(&currentTunnelID); err != nil {
		return err
	}
	if currentTunnelID != request.TunnelID {
		var references int
		if err := r.db.QueryRowContext(ctx, "SELECT COUNT(1) FROM user_tunnels WHERE speed_limit_id=?", request.ID).Scan(&references); err != nil {
			return err
		}
		if references > 0 {
			return errors.New("cannot move an assigned speed limit to another tunnel")
		}
	}
	result, err := r.db.ExecContext(ctx, `UPDATE speed_limits SET name=?,speed_mbps=?,tunnel_id=?,status=?,updated_at=? WHERE id=?`, strings.TrimSpace(request.Name), request.Speed, request.TunnelID, normalizeStatus(request.Status), time.Now().UnixMilli(), request.ID)
	if err != nil {
		return fmt.Errorf("update speed limit: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) CanDelete(ctx context.Context, id int64) error {
	if id <= 0 {
		return errors.New("speed limit id must be positive")
	}
	var exists, references int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(1),COUNT(ut.id) FROM speed_limits sl LEFT JOIN user_tunnels ut ON ut.speed_limit_id=sl.id WHERE sl.id=?`, id).Scan(&exists, &references); err != nil {
		return err
	}
	if exists == 0 {
		return sql.ErrNoRows
	}
	if references > 0 {
		return errors.New("speed limit is assigned to user tunnel policies")
	}
	return nil
}

func (r *Repository) Delete(ctx context.Context, id int64) error {
	if err := r.CanDelete(ctx, id); err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, "DELETE FROM speed_limits WHERE id=?", id)
	if err != nil {
		return fmt.Errorf("delete speed limit: %w", err)
	}
	if count, _ := result.RowsAffected(); count == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func (r *Repository) BatchDelete(ctx context.Context, ids []int64) error {
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for _, id := range ids {
		if id <= 0 {
			return errors.New("speed limit id must be positive")
		}
		var references int
		if err := transaction.QueryRowContext(ctx, "SELECT COUNT(1) FROM user_tunnels WHERE speed_limit_id=?", id).Scan(&references); err != nil {
			return err
		}
		if references > 0 {
			return fmt.Errorf("speed limit %d is assigned to user tunnel policies", id)
		}
		if _, err := transaction.ExecContext(ctx, "DELETE FROM speed_limits WHERE id=?", id); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func validate(request CreateRequest) error {
	if strings.TrimSpace(request.Name) == "" || len([]rune(request.Name)) > 100 {
		return errors.New("speed limit name is required and must be at most 100 characters")
	}
	if request.Speed <= 0 {
		return errors.New("speed must be positive")
	}
	if request.TunnelID <= 0 {
		return errors.New("tunnel is required")
	}
	if request.Status != 0 && request.Status != 1 {
		return errors.New("invalid speed limit status")
	}
	return nil
}

func normalizeStatus(status int) int {
	if status == 0 {
		return 0
	}
	return 1
}
