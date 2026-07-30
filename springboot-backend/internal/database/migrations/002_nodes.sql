CREATE TABLE IF NOT EXISTS nodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    ip TEXT NOT NULL,
    server_ip TEXT NOT NULL,
    port_start INTEGER NOT NULL,
    port_end INTEGER NOT NULL,
    secret TEXT NOT NULL UNIQUE,
    version TEXT NOT NULL DEFAULT '',
    http INTEGER NOT NULL DEFAULT 0 CHECK (http IN (0, 1)),
    tls INTEGER NOT NULL DEFAULT 0 CHECK (tls IN (0, 1)),
    socks INTEGER NOT NULL DEFAULT 0 CHECK (socks IN (0, 1)),
    status INTEGER NOT NULL DEFAULT 0 CHECK (status IN (0, 1)),
    uptime INTEGER NOT NULL DEFAULT 0,
    bytes_received INTEGER NOT NULL DEFAULT 0,
    bytes_transmitted INTEGER NOT NULL DEFAULT 0,
    cpu_usage REAL NOT NULL DEFAULT 0,
    memory_usage REAL NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_nodes_status ON nodes(status);
