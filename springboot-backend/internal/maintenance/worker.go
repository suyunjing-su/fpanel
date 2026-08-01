package maintenance

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"time"
)

type RefreshNotifier interface {
	Wake()
}

type Worker struct {
	db        *sql.DB
	refreshes RefreshNotifier
	log       *slog.Logger
	now       func() time.Time
}

func NewWorker(db *sql.DB, refreshes RefreshNotifier, log *slog.Logger) *Worker {
	return &Worker{db: db, refreshes: refreshes, log: log, now: time.Now}
}

func (w *Worker) Start(ctx context.Context) {
	go w.loop(ctx)
}

func (w *Worker) loop(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
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

func (w *Worker) process(ctx context.Context) {
	changed, err := w.processAt(ctx, w.now())
	if err != nil {
		w.log.Warn("failed to run scheduled maintenance", "error", err)
		return
	}
	if changed && w.refreshes != nil {
		w.refreshes.Wake()
	}
}

func (w *Worker) processAt(ctx context.Context, now time.Time) (bool, error) {
	resetChanged, err := w.resetMonthlyTraffic(ctx, now)
	if err != nil {
		return false, err
	}
	if now.Minute() <= 1 {
		if err := w.recordHourlyStatistics(ctx, now); err != nil {
			return false, err
		}
	}
	return resetChanged, nil
}

func (w *Worker) resetMonthlyTraffic(ctx context.Context, now time.Time) (bool, error) {
	const job = "monthly_traffic_reset"
	period := now.Format("2006-01-02")
	startedAt := now.UnixMilli()
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	claimed, err := claimRun(ctx, tx, job, period, startedAt)
	if err != nil || !claimed {
		return false, err
	}
	day := now.Day()
	lastDay := time.Date(now.Year(), now.Month()+1, 0, 0, 0, 0, 0, now.Location()).Day()
	result, err := tx.ExecContext(ctx, `UPDATE users SET ingress_bytes=0,egress_bytes=0,updated_at=? WHERE role<>'admin' AND flow_reset_day>0 AND (flow_reset_day=? OR (?=? AND flow_reset_day>?))`, startedAt, day, day, lastDay, lastDay)
	if err != nil {
		return false, w.abortRun(ctx, tx, job, period, startedAt, fmt.Errorf("reset monthly user traffic: %w", err))
	}
	userCount, err := result.RowsAffected()
	if err != nil {
		return false, w.abortRun(ctx, tx, job, period, startedAt, err)
	}
	result, err = tx.ExecContext(ctx, `UPDATE user_tunnels SET ingress_bytes=0,egress_bytes=0,updated_at=? WHERE flow_reset_day>0 AND (flow_reset_day=? OR (?=? AND flow_reset_day>?))`, startedAt, day, day, lastDay, lastDay)
	if err != nil {
		return false, w.abortRun(ctx, tx, job, period, startedAt, fmt.Errorf("reset monthly user tunnel traffic: %w", err))
	}
	permissionCount, err := result.RowsAffected()
	if err != nil {
		return false, w.abortRun(ctx, tx, job, period, startedAt, err)
	}
	if err := recordEvent(ctx, tx, job, period, "succeeded", fmt.Sprintf("reset %d users and %d tunnel permissions", userCount, permissionCount), startedAt, now.UnixMilli()); err != nil {
		return false, w.abortRun(ctx, tx, job, period, startedAt, err)
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return userCount > 0 || permissionCount > 0, nil
}

func (w *Worker) recordHourlyStatistics(ctx context.Context, now time.Time) error {
	const job = "hourly_traffic_statistics"
	hour := time.Date(now.Year(), now.Month(), now.Day(), now.Hour(), 0, 0, 0, now.Location())
	period := hour.Format("2006-01-02T15")
	startedAt := now.UnixMilli()
	tx, err := w.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	claimed, err := claimRun(ctx, tx, job, period, startedAt)
	if err != nil || !claimed {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO statistics_flows(user_id,flow,total_flow,recorded_at)
		SELECT u.id,
			CASE WHEN u.ingress_bytes+u.egress_bytes>=COALESCE(last.total_flow,0) THEN u.ingress_bytes+u.egress_bytes-COALESCE(last.total_flow,0) ELSE u.ingress_bytes+u.egress_bytes END,
			u.ingress_bytes+u.egress_bytes,?
		FROM users u
		LEFT JOIN statistics_flows last ON last.id=(SELECT id FROM statistics_flows WHERE user_id=u.id ORDER BY recorded_at DESC,id DESC LIMIT 1)
		WHERE u.role<>'admin'`, hour.UnixMilli()); err != nil {
		return w.abortRun(ctx, tx, job, period, startedAt, fmt.Errorf("record hourly traffic statistics: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM statistics_flows WHERE recorded_at<?`, hour.Add(-48*time.Hour).UnixMilli()); err != nil {
		return w.abortRun(ctx, tx, job, period, startedAt, fmt.Errorf("cleanup hourly traffic statistics: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM maintenance_runs WHERE completed_at<?`, now.AddDate(0, -3, 0).UnixMilli()); err != nil {
		return w.abortRun(ctx, tx, job, period, startedAt, fmt.Errorf("cleanup maintenance history: %w", err))
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM maintenance_run_events WHERE completed_at<?`, now.AddDate(0, -3, 0).UnixMilli()); err != nil {
		return w.abortRun(ctx, tx, job, period, startedAt, fmt.Errorf("cleanup maintenance event history: %w", err))
	}
	if err := recordEvent(ctx, tx, job, period, "succeeded", "recorded hourly traffic statistics", startedAt, now.UnixMilli()); err != nil {
		return w.abortRun(ctx, tx, job, period, startedAt, err)
	}
	return tx.Commit()
}

func (w *Worker) abortRun(ctx context.Context, tx *sql.Tx, job, period string, startedAt int64, cause error) error {
	if err := tx.Rollback(); err != nil && err != sql.ErrTxDone {
		return fmt.Errorf("%w; rollback maintenance run: %v", cause, err)
	}
	if err := recordFailedEvent(ctx, w.db, job, period, cause.Error(), startedAt, w.now().UnixMilli()); err != nil {
		return fmt.Errorf("%w; record maintenance failure: %v", cause, err)
	}
	return cause
}

func claimRun(ctx context.Context, tx *sql.Tx, job, period string, now int64) (bool, error) {
	result, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO maintenance_runs(job,period_key,completed_at) VALUES(?,?,?)`, job, period, now)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}
