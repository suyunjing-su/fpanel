package database

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

func TestCheckpointWorkerStopsAfterCancellation(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "checkpoint-worker.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	worker := NewCheckpointWorker(db, slog.New(slog.NewTextHandler(io.Discard, nil)))
	worker.Start(ctx)
	shutdownCtx, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	if err := worker.Shutdown(shutdownCtx); err != nil {
		t.Fatal(err)
	}
}

func TestCheckpointFlushesWALAndRejectsUnknownMode(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "checkpoint.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `CREATE TABLE checkpoint_fixture(id INTEGER PRIMARY KEY, value TEXT)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO checkpoint_fixture(value) VALUES('persisted')`); err != nil {
		t.Fatal(err)
	}

	passive, err := Checkpoint(ctx, db, CheckpointPassive)
	if err != nil {
		t.Fatal(err)
	}
	if passive.Busy || passive.WALFrames < 1 || passive.CheckpointedFrames < 1 || passive.CheckpointedFrames > passive.WALFrames {
		t.Fatalf("unexpected passive checkpoint result: %+v", passive)
	}
	truncated, err := Checkpoint(ctx, db, CheckpointTruncate)
	if err != nil {
		t.Fatal(err)
	}
	if truncated.Busy || truncated.WALFrames != 0 || truncated.CheckpointedFrames != 0 {
		t.Fatalf("unexpected truncate checkpoint result: %+v", truncated)
	}
	if _, err := Checkpoint(ctx, db, CheckpointMode("INVALID")); err == nil {
		t.Fatal("invalid checkpoint mode was accepted")
	}
}
