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
	changed := false
	if _, err := tx.ExecContext(ctx, `DELETE FROM runtime_expiry_states WHERE (subject_type='user' AND NOT EXISTS (SELECT 1 FROM users WHERE id=subject_id)) OR (subject_type='user_tunnel' AND NOT EXISTS (SELECT 1 FROM user_tunnels WHERE id=subject_id))`); err != nil {
		return false, err
	}
	userChanged, err := enqueueExpirations(ctx, tx, now, "user", `
		SELECT u.id,u.expires_at,tn.node_id
		FROM users u
		JOIN forwards f ON f.user_id=u.id
		JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
		LEFT JOIN runtime_expiry_states s ON s.subject_type='user' AND s.subject_id=u.id AND s.expires_at=u.expires_at
		WHERE u.expires_at>0 AND u.expires_at<=? AND s.subject_id IS NULL`)
	if err != nil {
		return false, err
	}
	changed = changed || userChanged
	permissionChanged, err := enqueueExpirations(ctx, tx, now, "user_tunnel", `
		SELECT ut.id,ut.expires_at,tn.node_id
		FROM user_tunnels ut
		JOIN tunnel_nodes tn ON tn.tunnel_id=ut.tunnel_id AND tn.chain_type=1
		LEFT JOIN runtime_expiry_states s ON s.subject_type='user_tunnel' AND s.subject_id=ut.id AND s.expires_at=ut.expires_at
		WHERE ut.expires_at>0 AND ut.expires_at<=? AND s.subject_id IS NULL`)
	if err != nil {
		return false, err
	}
	changed = changed || permissionChanged
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return changed, nil
}

func enqueueExpirations(ctx context.Context, tx *sql.Tx, now int64, subjectType, query string) (bool, error) {
	rows, err := tx.QueryContext(ctx, query, now)
	if err != nil {
		return false, err
	}
	type expiry struct {
		subjectID int64
		expiresAt int64
		nodeID    int64
	}
	items := make([]expiry, 0)
	for rows.Next() {
		var item expiry
		if err := rows.Scan(&item.subjectID, &item.expiresAt, &item.nodeID); err != nil {
			rows.Close()
			return false, err
		}
		items = append(items, item)
	}
	if err := rows.Close(); err != nil {
		return false, err
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(items) == 0 {
		return false, nil
	}
	seenSubjects := make(map[int64]int64)
	seenNodes := make(map[int64]struct{})
	for _, item := range items {
		seenSubjects[item.subjectID] = item.expiresAt
		seenNodes[item.nodeID] = struct{}{}
	}
	for nodeID := range seenNodes {
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_config_refreshes(node_id,requested_at) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at`, nodeID, now); err != nil {
			return false, fmt.Errorf("enqueue expired %s node: %w", subjectType, err)
		}
	}
	for subjectID, expiresAt := range seenSubjects {
		if _, err := tx.ExecContext(ctx, `INSERT INTO runtime_expiry_states(subject_type,subject_id,expires_at,processed_at) VALUES(?,?,?,?) ON CONFLICT(subject_type,subject_id) DO UPDATE SET expires_at=excluded.expires_at,processed_at=excluded.processed_at`, subjectType, subjectID, expiresAt, now); err != nil {
			return false, fmt.Errorf("record expired %s: %w", subjectType, err)
		}
	}
	return true, nil
}
