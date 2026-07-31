CREATE TABLE IF NOT EXISTS forwards (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    tunnel_id INTEGER NOT NULL REFERENCES tunnels(id) ON DELETE RESTRICT,
    remote_addr TEXT NOT NULL,
    interface_name TEXT NOT NULL DEFAULT '',
    strategy TEXT NOT NULL DEFAULT 'fifo' CHECK (strategy IN ('fifo', 'round', 'rand')),
    ingress_bytes INTEGER NOT NULL DEFAULT 0,
    egress_bytes INTEGER NOT NULL DEFAULT 0,
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (-1, 0, 1, 2)),
    sort_index INTEGER NOT NULL DEFAULT 0,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_forwards_user ON forwards(user_id, status, sort_index);
CREATE INDEX IF NOT EXISTS idx_forwards_tunnel ON forwards(tunnel_id, status);
CREATE TABLE IF NOT EXISTS forward_ports (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    forward_id INTEGER NOT NULL REFERENCES forwards(id) ON DELETE CASCADE,
    node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    UNIQUE(node_id, port)
);
CREATE INDEX IF NOT EXISTS idx_forward_ports_forward ON forward_ports(forward_id);
CREATE TABLE IF NOT EXISTS statistics_flows (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    flow INTEGER NOT NULL,
    total_flow INTEGER NOT NULL,
    recorded_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_statistics_flows_user_time ON statistics_flows(user_id, recorded_at DESC);
