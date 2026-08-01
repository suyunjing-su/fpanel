package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type CheckpointMode string

const (
	CheckpointPassive  CheckpointMode = "PASSIVE"
	CheckpointTruncate CheckpointMode = "TRUNCATE"
	checkpointInterval                = 5 * time.Minute
)

type CheckpointResult struct {
	Busy               bool
	WALFrames          int
	CheckpointedFrames int
}

type CheckpointWorker struct {
	db        *sql.DB
	log       *slog.Logger
	interval  time.Duration
	cancel    context.CancelFunc
	done      chan struct{}
	startOnce sync.Once
}

func NewCheckpointWorker(db *sql.DB, log *slog.Logger) *CheckpointWorker {
	return &CheckpointWorker{db: db, log: log, interval: checkpointInterval, done: make(chan struct{})}
}

func (w *CheckpointWorker) Start(ctx context.Context) {
	w.startOnce.Do(func() {
		workerCtx, cancel := context.WithCancel(ctx)
		w.cancel = cancel
		go w.loop(workerCtx)
	})
}

func (w *CheckpointWorker) Shutdown(ctx context.Context) error {
	if w.cancel == nil {
		return nil
	}
	w.cancel()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (w *CheckpointWorker) loop(ctx context.Context) {
	defer close(w.done)
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			result, err := Checkpoint(ctx, w.db, CheckpointPassive)
			if err != nil {
				if !errors.Is(err, context.Canceled) {
					w.log.Warn("sqlite WAL checkpoint failed", "error", err)
				}
				continue
			}
			level := slog.LevelDebug
			if result.Busy {
				level = slog.LevelWarn
			}
			w.log.Log(ctx, level, "sqlite WAL checkpoint completed", "mode", CheckpointPassive, "busy", result.Busy, "wal_frames", result.WALFrames, "checkpointed_frames", result.CheckpointedFrames)
		}
	}
}

func Checkpoint(ctx context.Context, db *sql.DB, mode CheckpointMode) (CheckpointResult, error) {
	if mode != CheckpointPassive && mode != CheckpointTruncate {
		return CheckpointResult{}, fmt.Errorf("unsupported SQLite checkpoint mode %q", mode)
	}
	var busy int
	var result CheckpointResult
	query := "PRAGMA wal_checkpoint(" + string(mode) + ")"
	if err := db.QueryRowContext(ctx, query).Scan(&busy, &result.WALFrames, &result.CheckpointedFrames); err != nil {
		return CheckpointResult{}, fmt.Errorf("checkpoint SQLite WAL: %w", err)
	}
	result.Busy = busy != 0
	return result, nil
}
