ALTER TABLE server_configs ADD COLUMN deleted_at TIMESTAMP NULL;
ALTER TABLE weekly_activities ADD COLUMN deleted_at TIMESTAMP NULL;
