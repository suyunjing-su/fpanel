package siteconfig

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type Repository struct{ db *sql.DB }

func NewRepository(db *sql.DB) *Repository { return &Repository{db: db} }

func (r *Repository) Get(ctx context.Context, key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("config name is required")
	}
	var value string
	if err := r.db.QueryRowContext(ctx, "SELECT value FROM site_config WHERE key = ?", key).Scan(&value); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("get site config: %w", err)
	}
	return value, nil
}

func (r *Repository) List(ctx context.Context) (map[string]string, error) {
	rows, err := r.db.QueryContext(ctx, "SELECT key, value FROM site_config ORDER BY key")
	if err != nil {
		return nil, fmt.Errorf("list site config: %w", err)
	}
	defer rows.Close()
	result := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan site config: %w", err)
		}
		result[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate site config: %w", err)
	}
	return result, nil
}

func (r *Repository) Update(ctx context.Context, values map[string]string) error {
	if len(values) == 0 {
		return errors.New("config values are required")
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin config update: %w", err)
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	for key, value := range values {
		key = strings.TrimSpace(key)
		if key == "" {
			return errors.New("config name is required")
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO site_config(key, value, updated_at) VALUES(?, ?, ?)
			ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, value, now); err != nil {
			return fmt.Errorf("update site config %q: %w", key, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit config update: %w", err)
	}
	return nil
}
