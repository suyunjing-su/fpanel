package maintenance

import (
	"context"
	"database/sql"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestWorkerResetsShortMonthAndRecordsHourlyIncrementOnce(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "maintenance.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if _, err := db.Exec(`INSERT INTO users(id,username,password_hash,role,expires_at,flow_reset_day,ingress_bytes,egress_bytes,status,created_at,updated_at) VALUES
		(1,'member','hash','user',0,31,100,50,1,1,1),
		(2,'other','hash','user',0,15,70,30,1,1,1),
		(3,'admin','hash','admin',0,31,90,10,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO tunnels(id,name,type,flow,status,created_at,updated_at) VALUES(1,'primary',1,1,1,1,1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO user_tunnels(id,user_id,tunnel_id,flow_reset_day,ingress_bytes,egress_bytes,status,created_at,updated_at) VALUES(1,1,1,31,40,20,1,1,1)`); err != nil {
		t.Fatal(err)
	}

	worker := NewWorker(db, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	now := time.Date(2026, time.February, 28, 10, 0, 5, 0, time.Local)
	changed, err := worker.processAt(ctx, now)
	if err != nil || !changed {
		t.Fatalf("maintenance changed=%v err=%v", changed, err)
	}
	assertCounters(t, db, "users", 1, 0, 0)
	assertCounters(t, db, "user_tunnels", 1, 0, 0)
	assertCounters(t, db, "users", 2, 70, 30)
	assertCounters(t, db, "users", 3, 90, 10)

	var flow, total int64
	if err := db.QueryRow(`SELECT flow,total_flow FROM statistics_flows WHERE user_id=1`).Scan(&flow, &total); err != nil {
		t.Fatal(err)
	}
	if flow != 0 || total != 0 {
		t.Fatalf("post-reset statistics flow=%d total=%d", flow, total)
	}
	if _, err := db.Exec(`UPDATE users SET ingress_bytes=120,egress_bytes=30 WHERE id=1`); err != nil {
		t.Fatal(err)
	}
	if err := worker.recordHourlyStatistics(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT flow,total_flow FROM statistics_flows WHERE user_id=1 ORDER BY recorded_at DESC LIMIT 1`).Scan(&flow, &total); err != nil {
		t.Fatal(err)
	}
	if flow != 150 || total != 150 {
		t.Fatalf("hourly statistics flow=%d total=%d", flow, total)
	}
	if err := worker.recordHourlyStatistics(ctx, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow(`SELECT COUNT(*) FROM statistics_flows WHERE user_id=1`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("hourly statistics duplicated: count=%d", count)
	}
	var succeededEvents int
	if err := db.QueryRow(`SELECT COUNT(*) FROM maintenance_run_events WHERE status='succeeded'`).Scan(&succeededEvents); err != nil {
		t.Fatal(err)
	}
	if succeededEvents != 3 {
		t.Fatalf("maintenance events=%d want=3", succeededEvents)
	}

	changed, err = worker.resetMonthlyTraffic(ctx, now)
	if err != nil || changed {
		t.Fatalf("daily reset was not idempotent: changed=%v err=%v", changed, err)
	}
}

func assertCounters(t *testing.T, db interface{ QueryRow(string, ...any) *sql.Row }, table string, id, ingress, egress int64) {
	t.Helper()
	var gotIngress, gotEgress int64
	if err := db.QueryRow(`SELECT ingress_bytes,egress_bytes FROM `+table+` WHERE id=?`, id).Scan(&gotIngress, &gotEgress); err != nil {
		t.Fatal(err)
	}
	if gotIngress != ingress || gotEgress != egress {
		t.Fatalf("%s %d counters=%d/%d want=%d/%d", table, id, gotIngress, gotEgress, ingress, egress)
	}
}
