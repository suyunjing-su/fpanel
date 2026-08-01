package audit

import (
	"context"
	"database/sql"
	"fmt"
)

type Event struct {
	ActorID      *int64
	Action       string
	ResourceType string
	ResourceID   string
	Outcome      string
	RequestID    string
	RemoteAddr   string
	Detail       string
	CreatedAt    int64
}

type Recorder interface {
	Record(context.Context, Event) error
}

type StoredEvent struct {
	ID           int64  `json:"id"`
	ActorID      *int64 `json:"actorId"`
	Action       string `json:"action"`
	ResourceType string `json:"resourceType"`
	ResourceID   string `json:"resourceId"`
	Outcome      string `json:"outcome"`
	RequestID    string `json:"requestId"`
	RemoteAddr   string `json:"remoteAddr"`
	Detail       string `json:"detail"`
	CreatedAt    int64  `json:"createdAt"`
}

type Page struct {
	Items []StoredEvent `json:"items"`
	Total int64         `json:"total"`
}

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Record(ctx context.Context, event Event) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO audit_events(actor_id,action,resource_type,resource_id,outcome,request_id,remote_addr,detail,created_at) VALUES(?,?,?,?,?,?,?,?,?)`, event.ActorID, event.Action, event.ResourceType, event.ResourceID, event.Outcome, event.RequestID, event.RemoteAddr, event.Detail, event.CreatedAt)
	return err
}

func (r *Repository) List(ctx context.Context, offset, limit int) (Page, error) {
	if offset < 0 || limit < 1 || limit > 200 {
		return Page{}, fmt.Errorf("invalid audit pagination")
	}
	var page Page
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM audit_events`).Scan(&page.Total); err != nil {
		return Page{}, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id,actor_id,action,resource_type,COALESCE(resource_id,''),outcome,COALESCE(request_id,''),COALESCE(remote_addr,''),COALESCE(detail,''),created_at FROM audit_events ORDER BY id DESC LIMIT ? OFFSET ?`, limit, offset)
	if err != nil {
		return Page{}, err
	}
	defer rows.Close()
	page.Items = make([]StoredEvent, 0)
	for rows.Next() {
		var item StoredEvent
		var actorID sql.NullInt64
		if err := rows.Scan(&item.ID, &actorID, &item.Action, &item.ResourceType, &item.ResourceID, &item.Outcome, &item.RequestID, &item.RemoteAddr, &item.Detail, &item.CreatedAt); err != nil {
			return Page{}, err
		}
		if actorID.Valid {
			item.ActorID = &actorID.Int64
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}
