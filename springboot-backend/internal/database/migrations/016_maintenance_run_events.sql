CREATE TABLE IF NOT EXISTS maintenance_run_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    job TEXT NOT NULL,
    period_key TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('succeeded', 'failed')),
    detail TEXT NOT NULL DEFAULT '',
    started_at INTEGER NOT NULL,
    completed_at INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_maintenance_run_events_completed ON maintenance_run_events(completed_at DESC, id DESC);
