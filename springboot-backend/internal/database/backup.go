package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"modernc.org/sqlite"
)

const pendingRestoreSuffix = ".restore-pending"
const previousDatabaseSuffix = ".pre-restore"

var databaseFileSuffixes = []string{"", "-wal", "-shm"}

type backupDriver interface {
	NewBackup(string) (*sqlite.Backup, error)
}

func Backup(ctx context.Context, db *sql.DB, destination io.Writer) error {
	directory, err := os.MkdirTemp("", "flux-backup-")
	if err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	defer os.RemoveAll(directory)
	path := filepath.Join(directory, "backup.db")
	if err := BackupToPath(ctx, db, path); err != nil {
		return err
	}
	input, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open backup file: %w", err)
	}
	defer input.Close()
	if _, err := io.Copy(destination, input); err != nil {
		return fmt.Errorf("read backup file: %w", err)
	}
	return nil
}

func BackupToPath(ctx context.Context, db *sql.DB, path string) error {
	if path == "" {
		return errors.New("backup destination is required")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("create backup directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".flux-backup-*.db")
	if err != nil {
		return fmt.Errorf("create backup file: %w", err)
	}
	temporaryPath := temporary.Name()
	if err := temporary.Close(); err != nil {
		os.Remove(temporaryPath)
		return fmt.Errorf("close backup file: %w", err)
	}
	defer os.Remove(temporaryPath)
	if err := onlineBackup(ctx, db, temporaryPath); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("publish backup file: %w", err)
	}
	return nil
}

func onlineBackup(ctx context.Context, db *sql.DB, path string) error {
	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("acquire database connection: %w", err)
	}
	defer conn.Close()
	return conn.Raw(func(driverConn any) error {
		backuper, ok := driverConn.(backupDriver)
		if !ok {
			return errors.New("sqlite driver does not support online backup")
		}
		operation, err := backuper.NewBackup(filepath.Clean(path))
		if err != nil {
			return fmt.Errorf("start online backup: %w", err)
		}
		finished := false
		defer func() {
			if !finished {
				_ = operation.Finish()
			}
		}()
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			more, err := operation.Step(-1)
			if err != nil {
				return fmt.Errorf("copy database pages: %w", err)
			}
			if !more {
				if err := operation.Finish(); err != nil {
					return fmt.Errorf("finish online backup: %w", err)
				}
				finished = true
				return nil
			}
		}
	})
}

func StageRestore(ctx context.Context, databasePath string, source io.Reader, maxBytes int64) error {
	if maxBytes <= 0 {
		return errors.New("restore size limit must be positive")
	}
	directory := filepath.Dir(databasePath)
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return fmt.Errorf("create database directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".flux-restore-*.db")
	if err != nil {
		return fmt.Errorf("create restore file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	written, copyErr := io.Copy(temporary, io.LimitReader(source, maxBytes+1))
	closeErr := temporary.Close()
	if copyErr != nil {
		return fmt.Errorf("write restore file: %w", copyErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close restore file: %w", closeErr)
	}
	if written == 0 {
		return errors.New("restore file is empty")
	}
	if written > maxBytes {
		return fmt.Errorf("restore file exceeds %d bytes", maxBytes)
	}
	if err := ValidateDatabase(ctx, temporaryPath); err != nil {
		return err
	}
	if err := replaceFile(temporaryPath, databasePath+pendingRestoreSuffix); err != nil {
		return fmt.Errorf("stage restore file: %w", err)
	}
	return nil
}

func ActivatePendingRestore(ctx context.Context, databasePath string) (bool, error) {
	pendingPath := databasePath + pendingRestoreSuffix
	if _, err := os.Stat(pendingPath); errors.Is(err, os.ErrNotExist) {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("inspect pending restore: %w", err)
	}
	if err := ValidateDatabase(ctx, pendingPath); err != nil {
		return false, fmt.Errorf("validate pending restore: %w", err)
	}
	previousPath := databasePath + previousDatabaseSuffix
	for _, suffix := range databaseFileSuffixes {
		if err := os.Remove(previousPath + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return false, fmt.Errorf("remove previous restore backup %s: %w", suffix, err)
		}
	}
	moved := make([]string, 0, len(databaseFileSuffixes))
	for _, suffix := range databaseFileSuffixes {
		if _, err := os.Stat(databasePath + suffix); err == nil {
			if err := os.Rename(databasePath+suffix, previousPath+suffix); err != nil {
				if rollbackErr := restoreMovedDatabaseFiles(databasePath, previousPath, moved); rollbackErr != nil {
					return false, fmt.Errorf("preserve current database %s: %v; rollback failed: %w", suffix, err, rollbackErr)
				}
				return false, fmt.Errorf("preserve current database %s: %w", suffix, err)
			}
			moved = append(moved, suffix)
		} else if !errors.Is(err, os.ErrNotExist) {
			if rollbackErr := restoreMovedDatabaseFiles(databasePath, previousPath, moved); rollbackErr != nil {
				return false, fmt.Errorf("inspect current database %s: %v; rollback failed: %w", suffix, err, rollbackErr)
			}
			return false, fmt.Errorf("inspect current database %s: %w", suffix, err)
		}
	}
	if err := os.Rename(pendingPath, databasePath); err != nil {
		if rollbackErr := restoreMovedDatabaseFiles(databasePath, previousPath, moved); rollbackErr != nil {
			return false, fmt.Errorf("activate restored database: %v; rollback failed: %w", err, rollbackErr)
		}
		return false, fmt.Errorf("activate restored database: %w", err)
	}
	return true, nil
}

func restoreMovedDatabaseFiles(databasePath, previousPath string, suffixes []string) error {
	for index := len(suffixes) - 1; index >= 0; index-- {
		suffix := suffixes[index]
		if err := os.Rename(previousPath+suffix, databasePath+suffix); err != nil {
			return fmt.Errorf("restore current database %s: %w", suffix, err)
		}
	}
	return nil
}

func RollbackRestore(databasePath string) error {
	previousPath := databasePath + previousDatabaseSuffix
	if _, err := os.Stat(previousPath); err != nil {
		return fmt.Errorf("restore rollback database unavailable: %w", err)
	}
	failedPath := databasePath + ".restore-failed"
	for _, suffix := range databaseFileSuffixes {
		_ = os.Remove(failedPath + suffix)
		if err := os.Rename(databasePath+suffix, failedPath+suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("preserve failed restored database %s: %w", suffix, err)
		}
		if err := os.Rename(previousPath+suffix, databasePath+suffix); err != nil && !(suffix != "" && errors.Is(err, os.ErrNotExist)) {
			return fmt.Errorf("rollback restored database %s: %w", suffix, err)
		}
	}
	return nil
}

func ValidateDatabase(ctx context.Context, path string) error {
	dsn := fmt.Sprintf("file:%s?mode=ro&_pragma=busy_timeout(5000)", filepath.ToSlash(path))
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return fmt.Errorf("open restore database: %w", err)
	}
	defer db.Close()
	var integrity string
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&integrity); err != nil {
		return fmt.Errorf("check restore database integrity: %w", err)
	}
	if integrity != "ok" {
		return fmt.Errorf("restore database integrity check failed: %s", integrity)
	}
	var migrationTable int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='schema_migrations'`).Scan(&migrationTable); err != nil {
		return fmt.Errorf("check restore schema: %w", err)
	}
	if migrationTable != 1 {
		return errors.New("restore database does not contain flux schema metadata")
	}
	return nil
}

func replaceFile(source, destination string) error {
	if err := os.Remove(destination); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return os.Rename(source, destination)
}

type SupportInfo struct {
	GeneratedAt       int64            `json:"generatedAt"`
	Integrity         string           `json:"integrity"`
	MigrationVersions []string         `json:"migrationVersions"`
	TableCounts       map[string]int64 `json:"tableCounts"`
}

func CollectSupportInfo(ctx context.Context, db *sql.DB) (SupportInfo, error) {
	info := SupportInfo{GeneratedAt: time.Now().UnixMilli(), TableCounts: map[string]int64{}}
	if err := db.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&info.Integrity); err != nil {
		return SupportInfo{}, fmt.Errorf("check database integrity: %w", err)
	}
	rows, err := db.QueryContext(ctx, "SELECT version FROM schema_migrations ORDER BY version")
	if err != nil {
		return SupportInfo{}, fmt.Errorf("list migrations: %w", err)
	}
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			rows.Close()
			return SupportInfo{}, err
		}
		info.MigrationVersions = append(info.MigrationVersions, version)
	}
	if err := rows.Close(); err != nil {
		return SupportInfo{}, err
	}
	if err := rows.Err(); err != nil {
		return SupportInfo{}, err
	}
	for _, table := range []string{"users", "nodes", "tunnels", "forwards", "audit_events"} {
		var count int64
		if err := db.QueryRowContext(ctx, "SELECT COUNT(*) FROM \""+table+"\"").Scan(&count); err != nil {
			return SupportInfo{}, fmt.Errorf("count %s: %w", table, err)
		}
		info.TableCounts[table] = count
	}
	return info, nil
}
