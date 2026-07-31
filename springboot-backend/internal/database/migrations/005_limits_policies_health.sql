CREATE UNIQUE INDEX IF NOT EXISTS idx_tunnel_nodes_identity ON tunnel_nodes(tunnel_id, node_id);

CREATE TABLE IF NOT EXISTS speed_limits (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL,
    speed_mbps INTEGER NOT NULL CHECK (speed_mbps > 0),
    tunnel_id INTEGER NOT NULL REFERENCES tunnels(id) ON DELETE RESTRICT,
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(tunnel_id, name)
);
CREATE INDEX IF NOT EXISTS idx_speed_limits_tunnel ON speed_limits(tunnel_id, status);

UPDATE user_tunnels
SET flow_quota_bytes = CASE
    WHEN flow_quota_bytes > 8589934591 THEN 9223372036854775807
    ELSE flow_quota_bytes * 1073741824
END
WHERE flow_quota_bytes > 0;

UPDATE user_tunnels
SET speed_limit_id = NULL
WHERE speed_limit_id IS NOT NULL
  AND NOT EXISTS (SELECT 1 FROM speed_limits WHERE id=user_tunnels.speed_limit_id);

CREATE TRIGGER IF NOT EXISTS trg_user_tunnel_speed_limit_insert
BEFORE INSERT ON user_tunnels
WHEN NEW.speed_limit_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM speed_limits
    WHERE id=NEW.speed_limit_id AND tunnel_id=NEW.tunnel_id AND status=1
)
BEGIN
    SELECT RAISE(ABORT, 'invalid user tunnel speed limit');
END;

CREATE TRIGGER IF NOT EXISTS trg_user_tunnel_speed_limit_update
BEFORE UPDATE OF speed_limit_id, tunnel_id ON user_tunnels
WHEN NEW.speed_limit_id IS NOT NULL AND NOT EXISTS (
    SELECT 1 FROM speed_limits
    WHERE id=NEW.speed_limit_id AND tunnel_id=NEW.tunnel_id AND status=1
)
BEGIN
    SELECT RAISE(ABORT, 'invalid user tunnel speed limit');
END;

CREATE TRIGGER IF NOT EXISTS trg_speed_limit_referenced_update
BEFORE UPDATE OF tunnel_id, status ON speed_limits
WHEN (NEW.tunnel_id<>OLD.tunnel_id OR NEW.status<>1) AND EXISTS (
    SELECT 1 FROM user_tunnels WHERE speed_limit_id=OLD.id
)
BEGIN
    SELECT RAISE(ABORT, 'assigned speed limit cannot be moved or disabled');
END;

CREATE TRIGGER IF NOT EXISTS trg_speed_limit_referenced_delete
BEFORE DELETE ON speed_limits
WHEN EXISTS (SELECT 1 FROM user_tunnels WHERE speed_limit_id=OLD.id)
BEGIN
    SELECT RAISE(ABORT, 'assigned speed limit cannot be deleted');
END;

CREATE TABLE IF NOT EXISTS user_tunnel_entry_policies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_tunnel_id INTEGER NOT NULL REFERENCES user_tunnels(id) ON DELETE CASCADE,
    tunnel_id INTEGER NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
    entry_node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    speed_limit_mbps INTEGER NOT NULL DEFAULT 0 CHECK (speed_limit_mbps >= 0),
    flow_quota_bytes INTEGER NOT NULL DEFAULT 0 CHECK (flow_quota_bytes >= 0),
    used_bytes INTEGER NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(user_tunnel_id, entry_node_id)
);
CREATE INDEX IF NOT EXISTS idx_entry_policies_tunnel ON user_tunnel_entry_policies(tunnel_id, entry_node_id, status);

CREATE TABLE IF NOT EXISTS user_tunnel_exit_policies (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    user_tunnel_id INTEGER NOT NULL REFERENCES user_tunnels(id) ON DELETE CASCADE,
    tunnel_id INTEGER NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
    exit_node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    flow_quota_bytes INTEGER NOT NULL DEFAULT 0 CHECK (flow_quota_bytes >= 0),
    used_bytes INTEGER NOT NULL DEFAULT 0 CHECK (used_bytes >= 0),
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(user_tunnel_id, exit_node_id)
);
CREATE INDEX IF NOT EXISTS idx_exit_policies_tunnel ON user_tunnel_exit_policies(tunnel_id, exit_node_id, status);

CREATE TABLE IF NOT EXISTS tunnel_failure_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tunnel_id INTEGER NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
    node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    tunnel_node_id INTEGER REFERENCES tunnel_nodes(id) ON DELETE SET NULL,
    user_tunnel_id INTEGER REFERENCES user_tunnels(id) ON DELETE CASCADE,
    policy_type TEXT CHECK (policy_type IN ('entry', 'exit', 'node')),
    policy_id INTEGER,
    event_type TEXT NOT NULL CHECK (event_type IN ('health', 'bandwidth_overload', 'quota')),
    from_status INTEGER NOT NULL,
    to_status INTEGER NOT NULL,
    latency_ms INTEGER,
    detail TEXT NOT NULL DEFAULT '',
    started_at INTEGER NOT NULL,
    resolved_at INTEGER,
    created_at INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_failure_events_tunnel_time ON tunnel_failure_events(tunnel_id, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_failure_events_active ON tunnel_failure_events(event_type, resolved_at, node_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_failure_events_active_policy_quota ON tunnel_failure_events(policy_type, policy_id) WHERE event_type='quota' AND policy_id IS NOT NULL AND resolved_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_failure_events_active_node_quota ON tunnel_failure_events(tunnel_node_id) WHERE event_type='quota' AND policy_type='node' AND resolved_at IS NULL;

CREATE TABLE IF NOT EXISTS node_config_refreshes (
    node_id INTEGER PRIMARY KEY REFERENCES nodes(id) ON DELETE CASCADE,
    generation INTEGER NOT NULL DEFAULT 1 CHECK (generation > 0),
    requested_at INTEGER NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    last_attempt_at INTEGER,
    last_error TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_node_config_refreshes_requested ON node_config_refreshes(requested_at, node_id);

CREATE TRIGGER IF NOT EXISTS trg_speed_limit_refresh_insert
AFTER INSERT ON speed_limits
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.tunnel_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_speed_limit_refresh_update
AFTER UPDATE ON speed_limits
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id IN (OLD.tunnel_id,NEW.tunnel_id)
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_speed_limit_refresh_delete
AFTER DELETE ON speed_limits
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=OLD.tunnel_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_user_tunnel_refresh_insert
AFTER INSERT ON user_tunnels
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.tunnel_id AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_user_tunnel_refresh_update
AFTER UPDATE OF flow_quota_bytes, forward_quota, flow_reset_day, expires_at, speed_limit_id, status ON user_tunnels
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id IN (OLD.tunnel_id,NEW.tunnel_id) AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_user_tunnel_refresh_delete
AFTER DELETE ON user_tunnels
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=OLD.tunnel_id AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_entry_policy_refresh_update
AFTER UPDATE OF speed_limit_mbps, flow_quota_bytes, status ON user_tunnel_entry_policies
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.tunnel_id AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_exit_policy_refresh_update
AFTER UPDATE OF flow_quota_bytes, status ON user_tunnel_exit_policies
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.tunnel_id AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_forward_refresh_insert
AFTER INSERT ON forwards
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.tunnel_id AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_forward_refresh_update
AFTER UPDATE OF user_id, name, tunnel_id, remote_addr, interface_name, strategy, status, sort_index ON forwards
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id IN (OLD.tunnel_id,NEW.tunnel_id) AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_forward_refresh_delete
AFTER DELETE ON forwards
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=OLD.tunnel_id AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_tunnel_node_refresh_insert
AFTER INSERT ON tunnel_nodes
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.tunnel_id AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_tunnel_node_refresh_update
AFTER UPDATE OF tunnel_id, chain_type, node_id, port, strategy, hop_index, protocol, flow_quota_bytes, speed_limit_mbps, health_status, bandwidth_overloaded ON tunnel_nodes
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id IN (OLD.tunnel_id,NEW.tunnel_id) AND chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_tunnel_node_refresh_delete
BEFORE DELETE ON tunnel_nodes
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=OLD.tunnel_id AND chain_type=1
    UNION SELECT OLD.node_id,CAST(strftime('%s','now') AS INTEGER)*1000 WHERE OLD.chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

ALTER TABLE nodes ADD COLUMN max_bandwidth_mbps INTEGER NOT NULL DEFAULT 0 CHECK (max_bandwidth_mbps >= 0);
