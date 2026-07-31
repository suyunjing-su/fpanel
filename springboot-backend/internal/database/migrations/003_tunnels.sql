CREATE TABLE IF NOT EXISTS tunnels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    type INTEGER NOT NULL CHECK (type IN (1, 2)),
    flow INTEGER NOT NULL CHECK (flow IN (1, 2)),
    traffic_ratio REAL NOT NULL DEFAULT 1.0 CHECK (traffic_ratio >= 0 AND traffic_ratio <= 100),
    in_ip TEXT NOT NULL DEFAULT '',
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS tunnel_nodes (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tunnel_id INTEGER NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
    chain_type INTEGER NOT NULL CHECK (chain_type IN (1, 2, 3)),
    node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    port INTEGER,
    strategy TEXT,
    hop_index INTEGER,
    protocol TEXT,
    flow_quota_bytes INTEGER,
    speed_limit_mbps INTEGER,
    ingress_bytes INTEGER NOT NULL DEFAULT 0,
    egress_bytes INTEGER NOT NULL DEFAULT 0,
    health_status INTEGER NOT NULL DEFAULT 1 CHECK (health_status IN (0, 1)),
    bandwidth_overloaded INTEGER NOT NULL DEFAULT 0 CHECK (bandwidth_overloaded IN (0, 1)),
    last_latency_ms INTEGER,
    health_checked_at INTEGER
);
CREATE INDEX IF NOT EXISTS idx_tunnel_nodes_tunnel ON tunnel_nodes(tunnel_id, chain_type, hop_index, id);
CREATE INDEX IF NOT EXISTS idx_tunnel_nodes_node ON tunnel_nodes(node_id);
CREATE TABLE IF NOT EXISTS user_tunnels (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    tunnel_id INTEGER NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
    speed_limit_id INTEGER,
    forward_quota INTEGER NOT NULL DEFAULT 0,
    flow_quota_bytes INTEGER NOT NULL DEFAULT 0,
    ingress_bytes INTEGER NOT NULL DEFAULT 0,
    egress_bytes INTEGER NOT NULL DEFAULT 0,
    flow_reset_day INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0,
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(user_id, tunnel_id)
);
CREATE INDEX IF NOT EXISTS idx_user_tunnels_user ON user_tunnels(user_id, status);
CREATE INDEX IF NOT EXISTS idx_user_tunnels_tunnel ON user_tunnels(tunnel_id, status);
