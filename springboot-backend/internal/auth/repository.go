package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Authenticate(ctx context.Context, username, password string) (Identity, error) {
	var identity Identity
	var encoded string
	var status int
	var expiresAt int64
	if err := r.db.QueryRowContext(ctx, `SELECT id, password_hash, role, status, expires_at, token_version
		FROM users WHERE username = ?`, username).Scan(
		&identity.UserID, &encoded, &identity.Role, &status, &expiresAt, &identity.TokenVersion,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Identity{}, errors.New("invalid credentials")
		}
		return Identity{}, fmt.Errorf("query user: %w", err)
	}
	if status == 0 || (expiresAt > 0 && expiresAt <= nowMillis()) || !VerifyPassword(encoded, password) {
		return Identity{}, errors.New("invalid credentials")
	}
	identity.Username = username
	if identity.Role == "admin" {
		identity.RoleID = 0
	} else {
		identity.RoleID = 1
	}
	return identity, nil
}

func (r *Repository) FindIdentity(ctx context.Context, id int64) (Identity, error) {
	var identity Identity
	var username string
	if err := r.db.QueryRowContext(ctx, `SELECT username, role, status, expires_at, token_version
		FROM users WHERE id = ?`, id).Scan(&username, &identity.Role, new(int), new(int64), &identity.TokenVersion); err != nil {
		return Identity{}, fmt.Errorf("find user: %w", err)
	}
	identity.UserID = id
	identity.Username = username
	identity.RoleID = 1
	if identity.Role == "admin" {
		identity.RoleID = 0
	}
	return identity, nil
}

func NormalizeUsername(username string) string {
	return strings.TrimSpace(username)
}

func nowMillis() int64 {
	return timeNow().UnixMilli()
}

var timeNow = func() time.Time { return time.Now() }
