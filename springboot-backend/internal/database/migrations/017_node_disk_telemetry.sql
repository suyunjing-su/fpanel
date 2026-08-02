ALTER TABLE nodes ADD COLUMN disk_usage REAL NOT NULL DEFAULT 0 CHECK (disk_usage >= 0 AND disk_usage <= 100);
