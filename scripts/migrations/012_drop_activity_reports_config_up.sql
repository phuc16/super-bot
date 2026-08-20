-- Drop the never-shipped activity-tracking / weekly-reports feature and its
-- config table. No command, service, or scheduler task ever used these
-- tables; /config only ever configured the reports/active-role/inactivity
-- settings, and that command has been removed.
DROP TABLE IF EXISTS weekly_reports;
DROP TABLE IF EXISTS weekly_activities;
DROP TABLE IF EXISTS user_activity_records;
DROP TABLE IF EXISTS server_configs;
