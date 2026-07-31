package tunnelhealth

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

type RefreshTask struct {
	NodeID     int64
	Generation int64
}

type FullConfigCommander interface {
	ForcePullFullConfig(context.Context, int64) error
}

type RefreshQueue struct {
	db        *sql.DB
	commander FullConfigCommander
	log       *slog.Logger
	wake      chan struct{}
}

func NewRefreshQueue(db *sql.DB, commander FullConfigCommander, log *slog.Logger) *RefreshQueue {
	return &RefreshQueue{db: db, commander: commander, log: log, wake: make(chan struct{}, 1)}
}

func (q *RefreshQueue) Start(ctx context.Context) {
	go q.loop(ctx)
}

func (q *RefreshQueue) Wake() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *RefreshQueue) RequestNodes(ctx context.Context, nodeIDs []int64) error {
	if len(nodeIDs) == 0 {
		return nil
	}
	tx, err := q.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UnixMilli()
	for _, nodeID := range nodeIDs {
		if nodeID <= 0 {
			return fmt.Errorf("invalid refresh node id %d", nodeID)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO node_config_refreshes(node_id,requested_at) VALUES(?,?) ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at`, nodeID, now); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	q.Wake()
	return nil
}

func (q *RefreshQueue) loop(ctx context.Context) {
	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	q.process(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			q.process(ctx)
		case <-q.wake:
			q.process(ctx)
		}
	}
}

func (q *RefreshQueue) process(ctx context.Context) {
	tasks, err := q.tasks(ctx)
	if err != nil {
		q.log.Warn("failed to load node configuration refresh queue", "error", err)
		return
	}
	for _, task := range tasks {
		commandCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 12*time.Second)
		err := q.commander.ForcePullFullConfig(commandCtx, task.NodeID)
		cancel()
		now := time.Now().UnixMilli()
		if err == nil {
			if _, deleteErr := q.db.ExecContext(ctx, `DELETE FROM node_config_refreshes WHERE node_id=? AND generation=?`, task.NodeID, task.Generation); deleteErr != nil {
				q.log.Warn("failed to complete node configuration refresh", "node_id", task.NodeID, "error", deleteErr)
			}
			continue
		}
		if _, updateErr := q.db.ExecContext(ctx, `UPDATE node_config_refreshes SET attempts=attempts+1,last_attempt_at=?,last_error=? WHERE node_id=? AND generation=?`, now, err.Error(), task.NodeID, task.Generation); updateErr != nil {
			q.log.Warn("failed to record node configuration refresh failure", "node_id", task.NodeID, "error", updateErr)
		}
	}
}

func (q *RefreshQueue) tasks(ctx context.Context) ([]RefreshTask, error) {
	rows, err := q.db.QueryContext(ctx, `SELECT node_id,generation FROM node_config_refreshes ORDER BY requested_at,node_id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]RefreshTask, 0)
	for rows.Next() {
		var task RefreshTask
		if err := rows.Scan(&task.NodeID, &task.Generation); err != nil {
			return nil, err
		}
		result = append(result, task)
	}
	return result, rows.Err()
}
