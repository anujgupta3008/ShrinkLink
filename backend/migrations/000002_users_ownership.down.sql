-- 000002_users_ownership.down.sql

DROP INDEX IF EXISTS idx_analytics_code_time;
DROP INDEX IF EXISTS idx_urls_user_id;
ALTER TABLE urls DROP COLUMN IF EXISTS click_count;
ALTER TABLE urls DROP COLUMN IF EXISTS user_id;
DROP TABLE IF EXISTS users;
