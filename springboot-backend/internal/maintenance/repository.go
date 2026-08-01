package maintenance

import (
	"context"
	"database/sql"
	"fmt"
)

type RunEvent struct {
	ID          int64  `json:"id"`
	Job         string `json:"job"`
	PeriodKey   string `json:"periodKey"`
	Status      string `json:"status"`
	Detail      string `json:"detail"`
	StartedAt   int64  `json:"startedAt"`
	CompletedAt int64  `json:"completedAt"`
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) ListEvents(ctx context.Context, limit int) ([]RunEvent, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,job,period_key,status,detail,started_at,completed_at
		FROM maintenance_run_events ORDER BY completed_at DESC,id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("list maintenance run events: %w", err)
	}
	defer rows.Close()
	events := make([]RunEvent, 0)
	for rows.Next() {
		var event RunEvent
		if err := rows.Scan(&event.ID, &event.Job, &event.PeriodKey, &event.Status, &event.Detail, &event.StartedAt, &event.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan maintenance run event: %w", err)
		}
		events = append(events, event)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate maintenance run events: %w", err)
	}
	return events, nil
}

func recordEvent(ctx context.Context, tx *sql.Tx, job, period, status, detail string, startedAt, completedAt int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO maintenance_run_events(job,period_key,status,detail,started_at,completed_at)
		VALUES(?,?,?,?,?,?)`, job, period, status, detail, startedAt, completedAt)
	if err != nil {
		return fmt.Errorf("record maintenance run event: %w", err)
	}
	return nil
}

func recordFailedEvent(ctx context.Context, db *sql.DB, job, period, detail string, startedAt, completedAt int64) error {
	_, err := db.ExecContext(ctx, `INSERT INTO maintenance_run_events(job,period_key,status,detail,started_at,completed_at)
		VALUES(?,?,?,?,?,?)`, job, period, "failed", detail, startedAt, completedAt)
	if err != nil {
		return fmt.Errorf("record failed maintenance run event: %w", err)
	}
	return nil
}
