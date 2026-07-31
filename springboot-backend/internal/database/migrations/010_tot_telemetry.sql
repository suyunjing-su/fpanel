ALTER TABLE nodes ADD COLUMN tot_sessions INTEGER NOT NULL DEFAULT 0 CHECK (tot_sessions >= 0);
ALTER TABLE nodes ADD COLUMN tot_active_paths INTEGER NOT NULL DEFAULT 0 CHECK (tot_active_paths >= 0);
ALTER TABLE nodes ADD COLUMN tot_pending_frames INTEGER NOT NULL DEFAULT 0 CHECK (tot_pending_frames >= 0);
ALTER TABLE nodes ADD COLUMN tot_sent_frames INTEGER NOT NULL DEFAULT 0 CHECK (tot_sent_frames >= 0);
ALTER TABLE nodes ADD COLUMN tot_received_frames INTEGER NOT NULL DEFAULT 0 CHECK (tot_received_frames >= 0);
ALTER TABLE nodes ADD COLUMN tot_retransmits INTEGER NOT NULL DEFAULT 0 CHECK (tot_retransmits >= 0);
ALTER TABLE nodes ADD COLUMN tot_duplicate_frames INTEGER NOT NULL DEFAULT 0 CHECK (tot_duplicate_frames >= 0);
ALTER TABLE nodes ADD COLUMN tot_path_failures INTEGER NOT NULL DEFAULT 0 CHECK (tot_path_failures >= 0);
