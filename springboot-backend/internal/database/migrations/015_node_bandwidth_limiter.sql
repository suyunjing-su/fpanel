DROP TRIGGER IF EXISTS trg_node_runtime_refresh_update;

CREATE TRIGGER trg_node_runtime_refresh_update
AFTER UPDATE OF server_ip, status, interface_name, tcp_listen_addr, udp_listen_addr, max_bandwidth_mbps ON nodes
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM tunnel_nodes
    WHERE tunnel_id IN (SELECT tunnel_id FROM tunnel_nodes WHERE node_id=NEW.id)
    UNION SELECT NEW.id,CAST(strftime('%s','now') AS INTEGER)*1000
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
