ALTER TABLE nodes ADD COLUMN upload_speed REAL NOT NULL DEFAULT 0 CHECK (upload_speed >= 0);
ALTER TABLE nodes ADD COLUMN download_speed REAL NOT NULL DEFAULT 0 CHECK (download_speed >= 0);
ALTER TABLE nodes ADD COLUMN telemetry_at INTEGER NOT NULL DEFAULT 0;
