CREATE TABLE IF NOT EXISTS runtime_expiry_states (
    subject_type TEXT NOT NULL CHECK (subject_type IN ('user', 'user_tunnel')),
    subject_id INTEGER NOT NULL,
    expires_at INTEGER NOT NULL,
    processed_at INTEGER NOT NULL,
    PRIMARY KEY(subject_type, subject_id)
);
CREATE INDEX IF NOT EXISTS idx_runtime_expiry_states_expiry ON runtime_expiry_states(subject_type, expires_at);

ALTER TABLE nodes ADD COLUMN interface_name TEXT NOT NULL DEFAULT '';
ALTER TABLE nodes ADD COLUMN tcp_listen_addr TEXT NOT NULL DEFAULT '[::]';
ALTER TABLE nodes ADD COLUMN udp_listen_addr TEXT NOT NULL DEFAULT '[::]';

DROP TRIGGER IF EXISTS trg_forward_refresh_insert;
DROP TRIGGER IF EXISTS trg_forward_refresh_update;
DROP TRIGGER IF EXISTS trg_forward_refresh_delete;

CREATE TRIGGER trg_forward_refresh_insert
AFTER INSERT ON forwards
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.user_id=NEW.user_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER trg_forward_refresh_update
AFTER UPDATE OF user_id, tunnel_id, remote_addr, interface_name, strategy, status, sort_index ON forwards
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

CREATE TRIGGER trg_forward_refresh_delete
AFTER DELETE ON forwards
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.user_id=OLD.user_id
    UNION SELECT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM tunnel_nodes tn
    WHERE tn.tunnel_id=OLD.tunnel_id AND tn.chain_type=1
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

DROP TRIGGER IF EXISTS trg_tunnel_node_refresh_insert;
DROP TRIGGER IF EXISTS trg_tunnel_node_refresh_update;
DROP TRIGGER IF EXISTS trg_tunnel_node_refresh_delete;

CREATE TRIGGER trg_tunnel_node_refresh_insert
AFTER INSERT ON tunnel_nodes
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.tunnel_id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER trg_tunnel_node_refresh_update
AFTER UPDATE OF tunnel_id, chain_type, node_id, port, strategy, hop_index, protocol, flow_quota_bytes, speed_limit_mbps, health_status, bandwidth_overloaded ON tunnel_nodes
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id IN (OLD.tunnel_id,NEW.tunnel_id)
    UNION SELECT OLD.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER trg_tunnel_node_refresh_delete
BEFORE DELETE ON tunnel_nodes
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=OLD.tunnel_id
    UNION SELECT OLD.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_tunnel_refresh_update
AFTER UPDATE OF type, status ON tunnels
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_tunnel_refresh_delete
BEFORE DELETE ON tunnels
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=OLD.id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_user_runtime_refresh_update
AFTER UPDATE OF status, expires_at, flow_quota_bytes, forward_quota ON users
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.user_id=NEW.id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_forward_port_refresh_insert
AFTER INSERT ON forward_ports
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at) VALUES(NEW.node_id,CAST(strftime('%s','now') AS INTEGER)*1000)
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_forward_port_refresh_update
AFTER UPDATE OF node_id, port ON forward_ports
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    VALUES(OLD.node_id,CAST(strftime('%s','now') AS INTEGER)*1000),(NEW.node_id,CAST(strftime('%s','now') AS INTEGER)*1000)
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_forward_port_refresh_delete
BEFORE DELETE ON forward_ports
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at) VALUES(OLD.node_id,CAST(strftime('%s','now') AS INTEGER)*1000)
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;

CREATE TRIGGER IF NOT EXISTS trg_node_runtime_refresh_update
AFTER UPDATE OF server_ip, status, interface_name, tcp_listen_addr, udp_listen_addr ON nodes
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM tunnel_nodes
    WHERE tunnel_id IN (SELECT tunnel_id FROM tunnel_nodes WHERE node_id=NEW.id)
    UNION SELECT NEW.id,CAST(strftime('%s','now') AS INTEGER)*1000
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
