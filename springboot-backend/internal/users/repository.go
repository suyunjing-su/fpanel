package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/bqlpfy/flux-panel/backend/internal/auth"
)

type User struct {
	ID            int64  `json:"id"`
	Name          string `json:"name,omitempty"`
	Username      string `json:"user"`
	Status        int    `json:"status"`
	Flow          int64  `json:"flow"`
	Num           int    `json:"num"`
	ExpTime       int64  `json:"expTime"`
	FlowResetTime int    `json:"flowResetTime"`
	CreatedTime   int64  `json:"createdTime"`
	InFlow        int64  `json:"inFlow"`
	OutFlow       int64  `json:"outFlow"`
}

type CreateRequest struct {
	Name          string `json:"name"`
	Username      string `json:"user"`
	Password      string `json:"pwd"`
	Status        int    `json:"status"`
	Flow          int64  `json:"flow"`
	Num           int    `json:"num"`
	ExpTime       int64  `json:"expTime"`
	FlowResetTime int    `json:"flowResetTime"`
}
type UpdateRequest struct {
	CreateRequest
	ID int64 `json:"id"`
}
type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(ctx context.Context) ([]User, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id, username, status, flow_quota_bytes, forward_quota, expires_at, flow_reset_day, created_at, ingress_bytes, egress_bytes FROM users WHERE role <> 'admin' ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()
	result := make([]User, 0)
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.ID, &u.Username, &u.Status, &u.Flow, &u.Num, &u.ExpTime, &u.FlowResetTime, &u.CreatedTime, &u.InFlow, &u.OutFlow); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		result = append(result, u)
	}
	return result, rows.Err()
}

func (r *Repository) Create(ctx context.Context, req CreateRequest) (int64, error) {
	if err := validateCreate(req); err != nil {
		return 0, err
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	res, err := r.db.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,token_version,expires_at,flow_quota_bytes,forward_quota,flow_reset_day,status,created_at,updated_at) VALUES(?,?,'user',1,?,?,?,?,?,?,?)`, strings.TrimSpace(req.Username), hash, req.ExpTime, req.Flow, req.Num, req.FlowResetTime, normalizeStatus(req.Status), now, now)
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	return res.LastInsertId()
}
func (r *Repository) Update(ctx context.Context, req UpdateRequest) error {
	if req.ID <= 0 {
		return errors.New("user id must be positive")
	}
	if err := validateCreate(req.CreateRequest); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	var err error
	if strings.TrimSpace(req.Password) != "" {
		hash, e := auth.HashPassword(req.Password)
		if e != nil {
			return e
		}
		_, err = r.db.ExecContext(ctx, `UPDATE users SET username=?,password_hash=?,status=?,expires_at=?,flow_quota_bytes=?,forward_quota=?,flow_reset_day=?,updated_at=? WHERE id=? AND role <> 'admin'`, req.Username, hash, normalizeStatus(req.Status), req.ExpTime, req.Flow, req.Num, req.FlowResetTime, now, req.ID)
	} else {
		_, err = r.db.ExecContext(ctx, `UPDATE users SET username=?,status=?,expires_at=?,flow_quota_bytes=?,forward_quota=?,flow_reset_day=?,updated_at=? WHERE id=? AND role <> 'admin'`, req.Username, normalizeStatus(req.Status), req.ExpTime, req.Flow, req.Num, req.FlowResetTime, now, req.ID)
	}
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	return nil
}
func (r *Repository) Delete(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx, "DELETE FROM users WHERE id=? AND role <> 'admin'", id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return sql.ErrNoRows
	}
	return nil
}
func validateCreate(r CreateRequest) error {
	if strings.TrimSpace(r.Username) == "" {
		return errors.New("username is required")
	}
	if r.Password == "" {
		return errors.New("password is required")
	}
	if r.Flow < 0 || r.Num < 0 {
		return errors.New("quota values cannot be negative")
	}
	return nil
}
func normalizeStatus(v int) int {
	if v == 0 {
		return 0
	}
	return 1
}
