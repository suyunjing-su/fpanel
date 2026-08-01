package tunnelhealth

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

type ExpiryWorker struct {
	db        *sql.DB
	refreshes RefreshNotifier
	log       *slog.Logger
}

func NewExpiryWorker(db *sql.DB, refreshes RefreshNotifier, log *slog.Logger) *ExpiryWorker {
	return &ExpiryWorker{db: db, refreshes: refreshes, log: log}
}

func (w *ExpiryWorker) Start(ctx context.Context) {
	go w.loop(ctx)
}

func (w *ExpiryWorker) loop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	w.process(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.process(ctx)
		}
	}
}

func (w *ExpiryWorker) process(ctx context.Context) {
	changed, err := w.processAt(ctx, time.Now().UnixMilli())
	if err != nil {
		w.log.Warn("failed to process runtime expirations", "error", err)
		return
	}
	if changed && w.refreshes != nil {
		w.refreshes.Wake()
	}
}

func (w *ExpiryWorker) processAt(ctx context.Context, now int64) (bool, error) {
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM runtime_expiry_states WHERE (subject_type='user' AND NOT EXISTS (SELECT 1 FROM users WHERE id=subject_id)) OR (subject_type='user_tunnel' AND NOT EXISTS (SELECT 1 FROM user_tunnels WHERE id=subject_id))`); err != nil {
		return false, err
	}

	users, err := loadExpirations(ctx, tx, now, "user", `
		SELECT u.id,u.expires_at
		FROM users u
		LEFT JOIN runtime_expiry_states s ON s.subject_type='user' AND s.subject_id=u.id AND s.expires_at=u.expires_at
		WHERE u.role<>'admin' AND u.expires_at>0 AND u.expires_at<=? AND s.subject_id IS NULL`)
	if err != nil {
		return false, err
	}
	permissions, err := loadExpirations(ctx, tx, now, "user_tunnel", `
		SELECT ut.id,ut.expires_at
		FROM user_tunnels ut
		LEFT JOIN runtime_expiry_states s ON s.subject_type='user_tunnel' AND s.subject_id=ut.id AND s.expires_at=ut.expires_at
		WHERE ut.expires_at>0 AND ut.expires_at<=? AND s.subject_id IS NULL`)
	if err != nil {
		return false, err
	}

	for _, item := range users {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET status=0,token_version=token_version+1,updated_at=? WHERE id=? AND status<>0`, now, item.subjectID); err != nil {
			return false, fmt.Errorf("disable expired user %d: %w", item.subjectID, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE forwards SET status=0,updated_at=? WHERE user_id=? AND status<>0`, now, item.subjectID); err != nil {
			return false, fmt.Errorf("disable forwards for expired user %d: %w", item.subjectID, err)
		}
		if err := recordExpiration(ctx, tx, now, "user", item); err != nil {
			return false, err
		}
	}
	for _, item := range permissions {
		var userID, tunnelID int64
		if err := tx.QueryRowContext(ctx, `SELECT user_id,tunnel_id FROM user_tunnels WHERE id=?`, item.subjectID).Scan(&userID, &tunnelID); err != nil {
			return false, fmt.Errorf("load expired user tunnel %d: %w", item.subjectID, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE user_tunnels SET status=0,updated_at=? WHERE id=? AND status<>0`, now, item.subjectID); err != nil {
			return false, fmt.Errorf("disable expired user tunnel %d: %w", item.subjectID, err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE forwards SET status=0,updated_at=? WHERE user_id=? AND tunnel_id=? AND status<>0`, now, userID, tunnelID); err != nil {
			return false, fmt.Errorf("disable forwards for expired user tunnel %d: %w", item.subjectID, err)
		}
		if err := recordExpiration(ctx, tx, now, "user_tunnel", item); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return len(users) > 0 || len(permissions) > 0, nil
}

type expiration struct {
	subjectID int64
	expiresAt int64
}

func loadExpirations(ctx context.Context, tx *sql.Tx, now int64, subjectType, query string) ([]expiration, error) {
	rows, err := tx.QueryContext(ctx, query, now)
	if err != nil {
		return nil, fmt.Errorf("load expired %s: %w", subjectType, err)
	}
	defer rows.Close()
	items := make([]expiration, 0)
	for rows.Next() {
		var item expiration
		if err := rows.Scan(&item.subjectID, &item.expiresAt); err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

func recordExpiration(ctx context.Context, tx *sql.Tx, now int64, subjectType string, item expiration) error {
	if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_expiry_states(subject_type,subject_id,expires_at,processed_at) VALUES(?,?,?,?) ON CONFLICT(subject_type,subject_id) DO UPDATE SET expires_at=excluded.expires_at,processed_at=excluded.processed_at`, subjectType, item.subjectID, item.expiresAt, now); err != nil {
		return fmt.Errorf("record expired %s: %w", subjectType, err)
	}
	return nil
}
