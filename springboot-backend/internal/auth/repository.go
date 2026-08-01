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

func (r *Repository) UpdatePassword(ctx context.Context, id int64, newUsername, currentPassword, newPassword string) error {
	if id <= 0 {
		return errors.New("user id must be positive")
	}
	newUsername = NormalizeUsername(newUsername)
	if newUsername == "" || currentPassword == "" || newPassword == "" {
		return errors.New("username and passwords are required")
	}
	if len([]rune(newUsername)) < 3 || len([]rune(newUsername)) > 20 {
		return errors.New("username must be between 3 and 20 characters")
	}
	if len([]rune(newPassword)) < 6 || len([]rune(newPassword)) > 128 {
		return errors.New("password must be between 6 and 128 characters")
	}
	var encoded string
	if err := r.db.QueryRowContext(ctx, "SELECT password_hash FROM users WHERE id=? AND status=1", id).Scan(&encoded); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return errors.New("invalid credentials")
		}
		return fmt.Errorf("load current password: %w", err)
	}
	if !VerifyPassword(encoded, currentPassword) {
		return errors.New("invalid credentials")
	}
	hash, err := HashPassword(newPassword)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE users SET username=?,password_hash=?,token_version=token_version+1,updated_at=? WHERE id=? AND status=1`, newUsername, hash, nowMillis(), id)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return errors.New("username is already in use")
		}
		return fmt.Errorf("update password: %w", err)
	}
	if count, err := result.RowsAffected(); err != nil {
		return fmt.Errorf("read password update count: %w", err)
	} else if count == 0 {
		return sql.ErrNoRows
	}
	return nil
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
