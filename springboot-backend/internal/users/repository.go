package users

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
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
	Name          string  `json:"name"`
	Username      string  `json:"user"`
	Password      string  `json:"pwd"`
	Status        int     `json:"status"`
	Flow          int64   `json:"flow"`
	Num           int     `json:"num"`
	ExpTime       int64   `json:"expTime"`
	FlowResetTime int     `json:"flowResetTime"`
	TunnelIDs     []int64 `json:"tunnelIds"`
}
type UpdateRequest struct {
	CreateRequest
	ID int64 `json:"id"`
}
type Repository struct{ db *sql.DB }

const (
	bytesPerGB int64 = 1024 * 1024 * 1024
	maxQuotaGB       = int64(^uint64(0)>>1) / bytesPerGB
)

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
		u.Flow /= bytesPerGB
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
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("begin user creation: %w", err)
	}
	defer tx.Rollback()

	tunnelIDs, err := validateTunnelIDs(ctx, tx, req.TunnelIDs)
	if err != nil {
		return 0, err
	}
	now := time.Now().UnixMilli()
	quotaBytes := req.Flow * bytesPerGB
	res, err := tx.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,token_version,expires_at,flow_quota_bytes,forward_quota,flow_reset_day,status,created_at,updated_at) VALUES(?,?,'user',1,?,?,?,?,?,?,?)`, strings.TrimSpace(req.Username), hash, req.ExpTime, quotaBytes, req.Num, req.FlowResetTime, normalizeStatus(req.Status), now, now)
	if err != nil {
		return 0, fmt.Errorf("create user: %w", err)
	}
	userID, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("read created user id: %w", err)
	}
	for _, tunnelID := range tunnelIDs {
		if _, err := tx.ExecContext(ctx, `INSERT INTO user_tunnels(user_id,tunnel_id,flow_quota_bytes,forward_quota,flow_reset_day,expires_at,status,created_at,updated_at) VALUES(?,?,?,?,?,?,1,?,?)`, userID, tunnelID, quotaBytes, req.Num, req.FlowResetTime, req.ExpTime, now, now); err != nil {
			return 0, fmt.Errorf("assign tunnel %d: %w", tunnelID, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("commit user creation: %w", err)
	}
	return userID, nil
}

func (r *Repository) Update(ctx context.Context, req UpdateRequest) error {
	if req.ID <= 0 {
		return errors.New("user id must be positive")
	}
	if err := validateUpdate(req); err != nil {
		return err
	}
	now := time.Now().UnixMilli()
	quotaBytes := req.Flow * bytesPerGB
	var result sql.Result
	var err error
	if strings.TrimSpace(req.Password) != "" {
		hash, hashErr := auth.HashPassword(req.Password)
		if hashErr != nil {
			return hashErr
		}
		result, err = r.db.ExecContext(ctx, `UPDATE users SET username=?,password_hash=?,status=?,expires_at=?,flow_quota_bytes=?,forward_quota=?,flow_reset_day=?,updated_at=? WHERE id=? AND role <> 'admin'`, strings.TrimSpace(req.Username), hash, normalizeStatus(req.Status), req.ExpTime, quotaBytes, req.Num, req.FlowResetTime, now, req.ID)
	} else {
		result, err = r.db.ExecContext(ctx, `UPDATE users SET username=?,status=?,expires_at=?,flow_quota_bytes=?,forward_quota=?,flow_reset_day=?,updated_at=? WHERE id=? AND role <> 'admin'`, strings.TrimSpace(req.Username), normalizeStatus(req.Status), req.ExpTime, quotaBytes, req.Num, req.FlowResetTime, now, req.ID)
	}
	if err != nil {
		return fmt.Errorf("update user: %w", err)
	}
	if count, countErr := result.RowsAffected(); countErr != nil {
		return fmt.Errorf("read updated user count: %w", countErr)
	} else if count == 0 {
		return sql.ErrNoRows
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
	if err := validateUserFields(r); err != nil {
		return err
	}
	if r.Password == "" {
		return errors.New("password is required")
	}
	return nil
}

func validateUpdate(r UpdateRequest) error {
	return validateUserFields(r.CreateRequest)
}

func validateUserFields(r CreateRequest) error {
	if strings.TrimSpace(r.Username) == "" {
		return errors.New("username is required")
	}
	if r.Flow < 0 || r.Flow > maxQuotaGB || r.Num < 0 {
		return errors.New("quota values are outside the supported range")
	}
	if r.FlowResetTime < 0 || r.FlowResetTime > 31 {
		return errors.New("flow reset day must be between 0 and 31")
	}
	return nil
}

func validateTunnelIDs(ctx context.Context, tx *sql.Tx, rawIDs []int64) ([]int64, error) {
	ids := make([]int64, 0, len(rawIDs))
	seen := make(map[int64]struct{}, len(rawIDs))
	for _, id := range rawIDs {
		if id <= 0 {
			return nil, errors.New("tunnel id must be positive")
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		var exists int
		if err := tx.QueryRowContext(ctx, "SELECT 1 FROM tunnels WHERE id=?", id).Scan(&exists); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, fmt.Errorf("tunnel %d does not exist", id)
			}
			return nil, fmt.Errorf("validate tunnel %d: %w", id, err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func normalizeStatus(v int) int {
	if v == 0 {
		return 0
	}
	return 1
}
