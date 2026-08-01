package audit

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/suyunjing-su/fpanel/backend/internal/database"
)

func TestRepositoryRecordsAndListsNewestEvents(t *testing.T) {
	db, err := database.Open(context.Background(), filepath.Join(t.TempDir(), "audit.db"), slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	repository := NewRepository(db)
	actorID := int64(7)
	if err := repository.Record(context.Background(), Event{ActorID: &actorID, Action: "post", ResourceType: "node/create", Outcome: "success", RequestID: "request-1", RemoteAddr: "192.0.2.1", Detail: "status=200", CreatedAt: 100}); err != nil {
		t.Fatal(err)
	}
	if err := repository.Record(context.Background(), Event{Action: "post", ResourceType: "forward/delete", Outcome: "failure", Detail: "status=400", CreatedAt: 200}); err != nil {
		t.Fatal(err)
	}

	page, err := repository.List(context.Background(), 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 1 {
		t.Fatalf("page total=%d items=%d", page.Total, len(page.Items))
	}
	if page.Items[0].ResourceType != "forward/delete" || page.Items[0].Outcome != "failure" || page.Items[0].ActorID != nil {
		t.Fatalf("unexpected newest event: %+v", page.Items[0])
	}

	page, err = repository.List(context.Background(), 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ActorID == nil || *page.Items[0].ActorID != actorID || page.Items[0].RequestID != "request-1" {
		t.Fatalf("unexpected older event: %+v", page.Items)
	}
	if _, err := repository.List(context.Background(), 0, 201); err == nil {
		t.Fatal("oversized page was accepted")
	}
}
