-- 000001_init_schema.down.sql
-- Drops all objects created in the up migration.

DROP INDEX IF EXISTS idx_analytics_short_code;
DROP INDEX IF EXISTS idx_urls_short_code;
DROP TABLE IF EXISTS analytics;
DROP TABLE IF EXISTS urls;
