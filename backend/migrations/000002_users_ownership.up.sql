-- 000002_users_ownership.up.sql
-- Adds users table, ownership on urls, and better indexes.

-- Enable pgcrypto for gen_random_uuid() (Postgres 13+: also available as built-in)
CREATE EXTENSION IF NOT EXISTS pgcrypto;

CREATE TABLE IF NOT EXISTS users (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    firebase_uid VARCHAR(128) UNIQUE NOT NULL,
    email        TEXT NOT NULL,
    plan         VARCHAR(20) NOT NULL DEFAULT 'free',  -- 'free' | 'pro'
    created_at   TIMESTAMPTZ DEFAULT NOW()
);

-- Nullable user_id: NULL means anonymous link, non-NULL means owned link.
ALTER TABLE urls ADD COLUMN IF NOT EXISTS user_id UUID REFERENCES users(id) ON DELETE SET NULL;

-- Denormalised click counter for fast dashboard rendering (avoids COUNT(*) on analytics).
ALTER TABLE urls ADD COLUMN IF NOT EXISTS click_count INTEGER NOT NULL DEFAULT 0;

-- Index for listing a user's links.
CREATE INDEX IF NOT EXISTS idx_urls_user_id ON urls(user_id);

-- Composite index for analytics range queries per link.
CREATE INDEX IF NOT EXISTS idx_analytics_code_time ON analytics(short_code, click_time);
