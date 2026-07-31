ALTER TABLE tunnels ADD COLUMN tot_enabled INTEGER NOT NULL DEFAULT 0 CHECK (tot_enabled IN (0, 1));
ALTER TABLE tunnels ADD COLUMN tot_secret TEXT NOT NULL DEFAULT '';
ALTER TABLE tunnels ADD COLUMN tot_path_count INTEGER NOT NULL DEFAULT 2 CHECK (tot_path_count > 0);
ALTER TABLE tunnels ADD COLUMN tot_max_payload INTEGER NOT NULL DEFAULT 32768 CHECK (tot_max_payload > 0);
ALTER TABLE tunnels ADD COLUMN tot_window INTEGER NOT NULL DEFAULT 256 CHECK (tot_window > 0);
ALTER TABLE tunnels ADD COLUMN tot_retransmit_interval_ms INTEGER NOT NULL DEFAULT 300 CHECK (tot_retransmit_interval_ms > 0);
ALTER TABLE tunnels ADD COLUMN tot_max_retries INTEGER NOT NULL DEFAULT 20 CHECK (tot_max_retries > 0);
ALTER TABLE tunnels ADD COLUMN tot_recovery_period_ms INTEGER NOT NULL DEFAULT 1000 CHECK (tot_recovery_period_ms > 0);
ALTER TABLE tunnels ADD COLUMN tot_handshake_timeout_ms INTEGER NOT NULL DEFAULT 5000 CHECK (tot_handshake_timeout_ms > 0);
ALTER TABLE tunnels ADD COLUMN tot_max_clock_skew_ms INTEGER NOT NULL DEFAULT 30000 CHECK (tot_max_clock_skew_ms > 0);
ALTER TABLE tunnels ADD COLUMN tot_idle_ttl_ms INTEGER NOT NULL DEFAULT 300000 CHECK (tot_idle_ttl_ms > 0);
ALTER TABLE tunnels ADD COLUMN tot_mptcp INTEGER NOT NULL DEFAULT 0 CHECK (tot_mptcp IN (0, 1));

DROP TRIGGER IF EXISTS trg_tunnel_tot_refresh_update;
CREATE TRIGGER trg_tunnel_tot_refresh_update
AFTER UPDATE OF tot_enabled, tot_secret, tot_path_count, tot_max_payload, tot_window, tot_retransmit_interval_ms, tot_max_retries, tot_recovery_period_ms, tot_handshake_timeout_ms, tot_max_clock_skew_ms, tot_idle_ttl_ms, tot_mptcp ON tunnels
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT node_id,CAST(strftime('%s','now') AS INTEGER)*1000 FROM tunnel_nodes WHERE tunnel_id=NEW.id
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
