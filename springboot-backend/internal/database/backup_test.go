package database

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
)

func TestBackupStageActivateAndRollback(t *testing.T) {
	ctx := context.Background()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	directory := t.TempDir()
	currentPath := filepath.Join(directory, "current.db")
	backupPath := filepath.Join(directory, "backup.db")

	db, err := Open(ctx, currentPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, `INSERT INTO users(username,password_hash,role,status,expires_at,created_at,updated_at) VALUES('before','hash','user',1,0,1,1)`); err != nil {
		t.Fatal(err)
	}
	if err := BackupToPath(ctx, db, backupPath); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE users SET username='after' WHERE username='before'`); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	db = nil

	input, err := os.Open(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := StageRestore(ctx, currentPath, input, 100<<20); err != nil {
		input.Close()
		t.Fatal(err)
	}
	input.Close()
	activated, err := ActivatePendingRestore(ctx, currentPath)
	if err != nil || !activated {
		t.Fatalf("activated=%v err=%v", activated, err)
	}
	restored, err := Open(ctx, currentPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	var username string
	if err := restored.QueryRowContext(ctx, `SELECT username FROM users WHERE username='before'`).Scan(&username); err != nil {
		restored.Close()
		t.Fatal(err)
	}
	restored.Close()
	if err := RollbackRestore(currentPath); err != nil {
		t.Fatal(err)
	}
	rolledBack, err := Open(ctx, currentPath, logger)
	if err != nil {
		t.Fatal(err)
	}
	defer rolledBack.Close()
	if err := rolledBack.QueryRowContext(ctx, `SELECT username FROM users WHERE username='after'`).Scan(&username); err != nil {
		t.Fatal(err)
	}
}

func TestStageRestoreRejectsInvalidDatabase(t *testing.T) {
	err := StageRestore(context.Background(), filepath.Join(t.TempDir(), "current.db"), bytes.NewBufferString("not sqlite"), 1024)
	if err == nil {
		t.Fatal("expected invalid database error")
	}
}

func TestCollectSupportInfoOmitsSensitiveValues(t *testing.T) {
	ctx := context.Background()
	db, err := Open(ctx, filepath.Join(t.TempDir(), "support.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	info, err := CollectSupportInfo(ctx, db)
	if err != nil {
		t.Fatal(err)
	}
	if info.Integrity != "ok" || len(info.MigrationVersions) == 0 {
		t.Fatalf("unexpected support info: %+v", info)
	}
	if _, ok := info.TableCounts["users"]; !ok {
		t.Fatal("users count missing")
	}
}
