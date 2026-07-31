package users

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/auth"
	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestCreateUserPersistsQuotaAndInitialTunnelPermissions(t *testing.T) {
	db := openTestDatabase(t)
	insertTestTunnels(t, db)
	repository := NewRepository(db)

	userID, err := repository.Create(context.Background(), CreateRequest{
		Username:      "member",
		Password:      "secret-password",
		Status:        1,
		Flow:          100,
		Num:           10,
		ExpTime:       4102444800000,
		FlowResetTime: 15,
		TunnelIDs:     []int64{1, 2, 1},
	})
	if err != nil {
		t.Fatal(err)
	}

	var quotaBytes int64
	if err := db.QueryRow("SELECT flow_quota_bytes FROM users WHERE id=?", userID).Scan(&quotaBytes); err != nil {
		t.Fatal(err)
	}
	if quotaBytes != 100*bytesPerGB {
		t.Fatalf("stored user quota = %d, want %d", quotaBytes, 100*bytesPerGB)
	}

	rows, err := db.Query(`SELECT tunnel_id,flow_quota_bytes,forward_quota,flow_reset_day,expires_at,status FROM user_tunnels WHERE user_id=? ORDER BY tunnel_id`, userID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var tunnelID, permissionQuota, expiresAt int64
		var forwardQuota, resetDay, status int
		if err := rows.Scan(&tunnelID, &permissionQuota, &forwardQuota, &resetDay, &expiresAt, &status); err != nil {
			t.Fatal(err)
		}
		count++
		if tunnelID != int64(count) || permissionQuota != 100*bytesPerGB || forwardQuota != 10 || resetDay != 15 || expiresAt != 4102444800000 || status != 1 {
			t.Fatalf("unexpected tunnel permission: tunnel=%d quota=%d forwards=%d reset=%d expires=%d status=%d", tunnelID, permissionQuota, forwardQuota, resetDay, expiresAt, status)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("permission count = %d, want 2", count)
	}

	users, err := repository.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Flow != 100 {
		t.Fatalf("unexpected listed users: %#v", users)
	}
}

func TestCreateUserRollsBackWhenTunnelDoesNotExist(t *testing.T) {
	db := openTestDatabase(t)
	insertTestTunnels(t, db)
	repository := NewRepository(db)

	_, err := repository.Create(context.Background(), CreateRequest{
		Username:  "member",
		Password:  "secret-password",
		Flow:      100,
		Num:       10,
		TunnelIDs: []int64{1, 999},
	})
	if err == nil {
		t.Fatal("missing tunnel was accepted")
	}

	var userCount, permissionCount int
	if err := db.QueryRow("SELECT COUNT(1) FROM users WHERE username='member'").Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow("SELECT COUNT(1) FROM user_tunnels").Scan(&permissionCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 0 || permissionCount != 0 {
		t.Fatalf("failed creation persisted user=%d permissions=%d", userCount, permissionCount)
	}
}

func TestUpdateUserWithoutPasswordPreservesHashAndConvertsQuota(t *testing.T) {
	db := openTestDatabase(t)
	repository := NewRepository(db)
	hash, err := auth.HashPassword("original-password")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,token_version,expires_at,flow_quota_bytes,forward_quota,status,created_at,updated_at) VALUES(1,'member',?,'user',1,1,0,0,1,1,1)`, hash); err != nil {
		t.Fatal(err)
	}

	err = repository.Update(context.Background(), UpdateRequest{
		ID: 1,
		CreateRequest: CreateRequest{
			Username:      "renamed",
			Status:        1,
			Flow:          200,
			Num:           20,
			ExpTime:       4102444800000,
			FlowResetTime: 20,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	var storedHash, username string
	var quotaBytes int64
	if err := db.QueryRow("SELECT username,password_hash,flow_quota_bytes FROM users WHERE id=1").Scan(&username, &storedHash, &quotaBytes); err != nil {
		t.Fatal(err)
	}
	if username != "renamed" || storedHash != hash || quotaBytes != 200*bytesPerGB {
		t.Fatalf("unexpected update: username=%q hashPreserved=%t quota=%d", username, storedHash == hash, quotaBytes)
	}

	err = repository.Update(context.Background(), UpdateRequest{ID: 999, CreateRequest: CreateRequest{Username: "missing"}})
	if err == nil || err != sql.ErrNoRows {
		t.Fatalf("missing user update error = %v, want sql.ErrNoRows", err)
	}
}

func TestUserQuotaRejectsOverflow(t *testing.T) {
	request := CreateRequest{Username: "member", Password: "secret-password", Flow: maxQuotaGB + 1}
	if err := validateCreate(request); err == nil {
		t.Fatal("overflowing quota was accepted")
	}
	if err := validateUpdate(UpdateRequest{ID: 1, CreateRequest: request}); err == nil {
		t.Fatal("overflowing update quota was accepted")
	}
}

func openTestDatabase(t *testing.T) *sql.DB {
	t.Helper()
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "users.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func insertTestTunnels(t *testing.T, db *sql.DB) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO tunnels(id,name,type,flow,traffic_ratio,status,created_at,updated_at) VALUES
		(1,'first',1,1,1,1,1,1),
		(2,'second',1,1,1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
}
