-- Level 9: Add device column to analytics table for device type tracking (Desktop/Mobile/Tablet/Bot)
ALTER TABLE analytics ADD COLUMN IF NOT EXISTS device VARCHAR(20) NOT NULL DEFAULT 'Unknown';

-- Index for device breakdown queries
CREATE INDEX IF NOT EXISTS idx_analytics_device ON analytics (short_code, device);
