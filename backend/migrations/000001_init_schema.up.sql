-- 000001_init_schema.up.sql
-- Creates the initial urls and analytics tables.

CREATE TABLE IF NOT EXISTS urls (
    id          BIGINT PRIMARY KEY,
    short_code  VARCHAR(10) UNIQUE NOT NULL,
    long_url    TEXT NOT NULL,
    is_custom   BOOLEAN DEFAULT FALSE,
    created_at  TIMESTAMPTZ DEFAULT NOW(),
    expires_at  TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS analytics (
    id          BIGSERIAL PRIMARY KEY,
    short_code  VARCHAR(10) NOT NULL,
    click_time  TIMESTAMPTZ DEFAULT NOW(),
    ip_address  VARCHAR(45),
    user_agent  TEXT,
    referrer    TEXT,
    country     VARCHAR(100),
    browser     VARCHAR(50),
    os          VARCHAR(50)
);

CREATE INDEX IF NOT EXISTS idx_urls_short_code      ON urls(short_code);
CREATE INDEX IF NOT EXISTS idx_analytics_short_code ON analytics(short_code);
