ALTER TABLE endpoints ADD COLUMN weight INTEGER NOT NULL DEFAULT 1 CHECK (weight BETWEEN 1 AND 100);

DROP TRIGGER IF EXISTS trg_endpoint_refresh_update;
CREATE TRIGGER trg_endpoint_refresh_update
AFTER UPDATE OF group_id, address, priority, weight, backup, status, sort_index ON endpoints
BEGIN
    INSERT INTO node_config_refreshes(node_id,requested_at)
    SELECT DISTINCT tn.node_id,CAST(strftime('%s','now') AS INTEGER)*1000
    FROM forwards f
    JOIN tunnel_nodes tn ON tn.tunnel_id=f.tunnel_id AND tn.chain_type=1
    WHERE f.endpoint_group_id IN (OLD.group_id,NEW.group_id)
    ON CONFLICT(node_id) DO UPDATE SET generation=generation+1,requested_at=excluded.requested_at;
END;
