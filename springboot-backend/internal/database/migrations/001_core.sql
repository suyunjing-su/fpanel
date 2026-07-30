CREATE TABLE IF NOT EXISTS users (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    username TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    role TEXT NOT NULL CHECK (role IN ('admin', 'user')),
    token_version INTEGER NOT NULL DEFAULT 1,
    expires_at INTEGER NOT NULL,
    flow_quota_bytes INTEGER NOT NULL DEFAULT 0,
    ingress_bytes INTEGER NOT NULL DEFAULT 0,
    egress_bytes INTEGER NOT NULL DEFAULT 0,
    flow_reset_day INTEGER NOT NULL DEFAULT 0,
    forward_quota INTEGER NOT NULL DEFAULT 0,
    status INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS site_config (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    secret INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    actor_id INTEGER,
    action TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT,
    outcome TEXT NOT NULL,
    request_id TEXT,
    remote_addr TEXT,
    detail TEXT,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_audit_events_created_at ON audit_events(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_audit_events_actor_id ON audit_events(actor_id, created_at DESC);

CREATE TABLE IF NOT EXISTS health_profiles (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    method TEXT NOT NULL CHECK (method IN ('tcp', 'icmp', 'udp', 'quic', 'kcp')),
    target_host TEXT NOT NULL,
    target_port INTEGER,
    interval_ms INTEGER NOT NULL,
    timeout_ms INTEGER NOT NULL,
    success_threshold INTEGER NOT NULL,
    failure_threshold INTEGER NOT NULL,
    max_latency_ms INTEGER,
    recovery_stable_ms INTEGER NOT NULL,
    switch_cooldown_ms INTEGER NOT NULL,
    auto_failback INTEGER NOT NULL DEFAULT 1,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS metrics_samples (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    metric TEXT NOT NULL,
    labels TEXT NOT NULL DEFAULT '{}',
    value REAL NOT NULL,
    observed_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_metrics_samples_metric_time ON metrics_samples(metric, observed_at DESC);
