ALTER TABLE tunnels ADD COLUMN tot_paths TEXT NOT NULL DEFAULT '[]';

DROP TRIGGER IF EXISTS trg_tunnel_tot_refresh_update;
CREATE TRIGGER trg_tunnel_tot_refresh_update
AFTER UPDATE OF tot_enabled, tot_secret, tot_path_count, tot_paths, tot_max_payload, tot_window, tot_retransmit_interval_ms, tot_max_retries, tot_recovery_period_ms, tot_handshake_timeout_ms, tot_max_clock_skew_ms, tot_idle_ttl_ms, tot_mptcp ON tunnels
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
