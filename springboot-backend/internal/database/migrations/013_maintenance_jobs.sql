CREATE TABLE IF NOT EXISTS maintenance_runs (
    job TEXT NOT NULL,
    period_key TEXT NOT NULL,
    completed_at INTEGER NOT NULL,
    PRIMARY KEY(job, period_key)
);

CREATE INDEX IF NOT EXISTS idx_maintenance_runs_completed ON maintenance_runs(completed_at);
