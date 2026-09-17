-- Revert Level 9: Remove device column from analytics table
DROP INDEX IF EXISTS idx_analytics_device;
ALTER TABLE analytics DROP COLUMN IF EXISTS device;
