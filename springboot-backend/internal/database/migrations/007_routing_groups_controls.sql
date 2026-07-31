CREATE TABLE IF NOT EXISTS node_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    strategy TEXT NOT NULL DEFAULT 'fifo' CHECK (strategy IN ('fifo', 'round', 'rand')),
    max_fails INTEGER NOT NULL DEFAULT 1 CHECK (max_fails > 0),
    fail_timeout_ms INTEGER NOT NULL DEFAULT 600000 CHECK (fail_timeout_ms >= 1000),
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS node_group_members (
    group_id INTEGER NOT NULL REFERENCES node_groups(id) ON DELETE CASCADE,
    node_id INTEGER NOT NULL REFERENCES nodes(id) ON DELETE RESTRICT,
    priority INTEGER NOT NULL DEFAULT 0 CHECK (priority >= 0),
    backup INTEGER NOT NULL DEFAULT 0 CHECK (backup IN (0, 1)),
    sort_index INTEGER NOT NULL DEFAULT 0 CHECK (sort_index >= 0),
    PRIMARY KEY(group_id, node_id)
);
CREATE INDEX IF NOT EXISTS idx_node_group_members_node ON node_group_members(node_id, group_id);

CREATE TABLE IF NOT EXISTS tunnel_node_group_bindings (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    tunnel_id INTEGER NOT NULL REFERENCES tunnels(id) ON DELETE CASCADE,
    group_id INTEGER NOT NULL REFERENCES node_groups(id) ON DELETE RESTRICT,
    chain_type INTEGER NOT NULL CHECK (chain_type IN (1, 2, 3)),
    port INTEGER NOT NULL CHECK (port BETWEEN 1 AND 65535),
    strategy TEXT NOT NULL DEFAULT 'fifo' CHECK (strategy IN ('fifo', 'round', 'rand')),
    hop_index INTEGER NOT NULL DEFAULT 0 CHECK (hop_index >= 0),
    protocol TEXT NOT NULL DEFAULT 'tcp',
    flow_quota_bytes INTEGER NOT NULL DEFAULT 0 CHECK (flow_quota_bytes >= 0),
    speed_limit_mbps INTEGER NOT NULL DEFAULT 0 CHECK (speed_limit_mbps >= 0),
    UNIQUE(tunnel_id, group_id, chain_type, hop_index)
);
CREATE INDEX IF NOT EXISTS idx_tunnel_node_group_bindings_group ON tunnel_node_group_bindings(group_id, tunnel_id);
ALTER TABLE tunnel_nodes ADD COLUMN group_binding_id INTEGER REFERENCES tunnel_node_group_bindings(id) ON DELETE CASCADE;
ALTER TABLE tunnel_nodes ADD COLUMN group_priority INTEGER NOT NULL DEFAULT 0 CHECK (group_priority >= 0);
ALTER TABLE tunnel_nodes ADD COLUMN group_backup INTEGER NOT NULL DEFAULT 0 CHECK (group_backup IN (0, 1));
ALTER TABLE tunnel_nodes ADD COLUMN group_max_fails INTEGER NOT NULL DEFAULT 1 CHECK (group_max_fails > 0);
ALTER TABLE tunnel_nodes ADD COLUMN group_fail_timeout_ms INTEGER NOT NULL DEFAULT 600000 CHECK (group_fail_timeout_ms >= 1000);
CREATE INDEX IF NOT EXISTS idx_tunnel_nodes_group_binding ON tunnel_nodes(group_binding_id);

CREATE TRIGGER IF NOT EXISTS trg_node_group_refresh_update
AFTER UPDATE OF strategy, max_fails, fail_timeout_ms, status ON node_groups
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM tunnel_node_group_bindings b
    JOIN tunnel_nodes tn ON tn.tunnel_id=b.tunnel_id
    WHERE b.group_id=NEW.id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_node_group_member_refresh_insert
AFTER INSERT ON node_group_members
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM tunnel_node_group_bindings b
    JOIN tunnel_nodes tn ON tn.tunnel_id=b.tunnel_id
    WHERE b.group_id=NEW.group_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_node_group_member_refresh_update
AFTER UPDATE OF group_id, node_id, priority, backup, sort_index ON node_group_members
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM tunnel_node_group_bindings b
    JOIN tunnel_nodes tn ON tn.tunnel_id=b.tunnel_id
    WHERE b.group_id IN (OLD.group_id,NEW.group_id)
    UNION SELECT OLD.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    UNION SELECT NEW.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_node_group_member_refresh_delete
BEFORE DELETE ON node_group_members
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM tunnel_node_group_bindings b
    JOIN tunnel_nodes tn ON tn.tunnel_id=b.tunnel_id
    WHERE b.group_id=OLD.group_id
    UNION SELECT OLD.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TABLE IF NOT EXISTS endpoint_groups (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    strategy TEXT NOT NULL DEFAULT 'fifo' CHECK (strategy IN ('fifo', 'round', 'rand')),
    max_fails INTEGER NOT NULL DEFAULT 1 CHECK (max_fails > 0),
    fail_timeout_ms INTEGER NOT NULL DEFAULT 600000 CHECK (fail_timeout_ms >= 1000),
    probe_interval_ms INTEGER NOT NULL DEFAULT 10000 CHECK (probe_interval_ms >= 1000),
    probe_timeout_ms INTEGER NOT NULL DEFAULT 3000 CHECK (probe_timeout_ms >= 100),
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS endpoints (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    group_id INTEGER NOT NULL REFERENCES endpoint_groups(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    address TEXT NOT NULL,
    priority INTEGER NOT NULL DEFAULT 0 CHECK (priority >= 0),
    backup INTEGER NOT NULL DEFAULT 0 CHECK (backup IN (0, 1)),
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    sort_index INTEGER NOT NULL DEFAULT 0 CHECK (sort_index >= 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(group_id, name),
    UNIQUE(group_id, address)
);
CREATE INDEX IF NOT EXISTS idx_endpoints_group ON endpoints(group_id, status, sort_index, id);

CREATE TABLE IF NOT EXISTS route_rule_sets (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT NOT NULL UNIQUE,
    description TEXT NOT NULL DEFAULT '',
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS route_rules (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    rule_set_id INTEGER NOT NULL REFERENCES route_rule_sets(id) ON DELETE CASCADE,
    name TEXT NOT NULL,
    match_type TEXT NOT NULL CHECK (match_type IN ('client_ip', 'protocol', 'host', 'host_regexp', 'method', 'path', 'path_regexp', 'path_prefix', 'header', 'header_regexp', 'query', 'query_regexp')),
    value TEXT NOT NULL,
    secondary_value TEXT NOT NULL DEFAULT '',
    negate INTEGER NOT NULL DEFAULT 0 CHECK (negate IN (0, 1)),
    priority INTEGER NOT NULL DEFAULT 100 CHECK (priority > 0),
    status INTEGER NOT NULL DEFAULT 1 CHECK (status IN (0, 1)),
    sort_index INTEGER NOT NULL DEFAULT 0 CHECK (sort_index >= 0),
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    UNIQUE(rule_set_id, name)
);
CREATE INDEX IF NOT EXISTS idx_route_rules_set ON route_rules(rule_set_id, status, priority DESC, sort_index, id);

CREATE TABLE IF NOT EXISTS route_rule_endpoints (
    rule_id INTEGER NOT NULL REFERENCES route_rules(id) ON DELETE CASCADE,
    endpoint_id INTEGER NOT NULL REFERENCES endpoints(id) ON DELETE CASCADE,
    PRIMARY KEY(rule_id, endpoint_id)
);
CREATE INDEX IF NOT EXISTS idx_route_rule_endpoints_endpoint ON route_rule_endpoints(endpoint_id, rule_id);

ALTER TABLE forwards ADD COLUMN endpoint_group_id INTEGER REFERENCES endpoint_groups(id) ON DELETE RESTRICT;
ALTER TABLE forwards ADD COLUMN route_rule_set_id INTEGER REFERENCES route_rule_sets(id) ON DELETE RESTRICT;
ALTER TABLE forwards ADD COLUMN max_connections INTEGER NOT NULL DEFAULT 0 CHECK (max_connections >= 0);
ALTER TABLE forwards ADD COLUMN max_connections_per_ip INTEGER NOT NULL DEFAULT 0 CHECK (max_connections_per_ip >= 0);
ALTER TABLE forwards ADD COLUMN source_ranges TEXT NOT NULL DEFAULT '';
ALTER TABLE forwards ADD COLUMN source_whitelist INTEGER NOT NULL DEFAULT 0 CHECK (source_whitelist IN (0, 1));
ALTER TABLE forwards ADD COLUMN proxy_protocol_receive INTEGER NOT NULL DEFAULT 0 CHECK (proxy_protocol_receive IN (0, 1, 2));
ALTER TABLE forwards ADD COLUMN proxy_protocol_send INTEGER NOT NULL DEFAULT 0 CHECK (proxy_protocol_send IN (0, 1, 2));

DROP TRIGGER IF EXISTS trg_forward_refresh_update;
CREATE TRIGGER trg_forward_refresh_update
AFTER UPDATE OF user_id, tunnel_id, remote_addr, interface_name, strategy, status, sort_index, endpoint_group_id, route_rule_set_id, max_connections, max_connections_per_ip, source_ranges, source_whitelist, proxy_protocol_receive, proxy_protocol_send ON forwards
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.user_id IN (OLD.user_id,NEW.user_id)
    UNION SELECT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM tunnel_nodes tn
    WHERE tn.tunnel_id IN (OLD.tunnel_id,NEW.tunnel_id) AND tn.chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_endpoint_group_refresh_update
AFTER UPDATE OF strategy, max_fails, fail_timeout_ms, probe_interval_ms, probe_timeout_ms, status ON endpoint_groups
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.endpoint_group_id=NEW.id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_endpoint_refresh_insert
AFTER INSERT ON endpoints
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.endpoint_group_id=NEW.group_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_endpoint_refresh_update
AFTER UPDATE OF group_id, address, priority, backup, status, sort_index ON endpoints
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.endpoint_group_id IN (OLD.group_id,NEW.group_id)
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_endpoint_refresh_delete
BEFORE DELETE ON endpoints
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.endpoint_group_id=OLD.group_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_route_rule_set_refresh_update
AFTER UPDATE OF status ON route_rule_sets
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.route_rule_set_id=NEW.id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_route_rule_refresh_insert
AFTER INSERT ON route_rules
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.route_rule_set_id=NEW.rule_set_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_route_rule_refresh_update
AFTER UPDATE OF rule_set_id, match_type, value, secondary_value, negate, priority, status, sort_index ON route_rules
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.route_rule_set_id IN (OLD.rule_set_id,NEW.rule_set_id)
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_route_rule_refresh_delete
BEFORE DELETE ON route_rules
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.route_rule_set_id=OLD.rule_set_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_route_rule_endpoint_refresh_insert
AFTER INSERT ON route_rule_endpoints
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM route_rules rr
    JOIN forwards f ON f.route_rule_set_id=rr.rule_set_id
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE rr.id=NEW.rule_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
CREATE TRIGGER IF NOT EXISTS trg_route_rule_endpoint_refresh_delete
BEFORE DELETE ON route_rule_endpoints
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM route_rules rr
    JOIN forwards f ON f.route_rule_set_id=rr.rule_set_id
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE rr.id=OLD.rule_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
