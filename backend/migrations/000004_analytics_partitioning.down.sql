-- 000004_analytics_partitioning.down.sql
-- Reverts the analytics table back to a non-partitioned table.

-- Step 1: Create a temporary non-partitioned table
CREATE TABLE analytics_flat (
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

-- Step 2: Migrate all data from partitioned table
INSERT INTO analytics_flat (id, short_code, click_time, ip_address, user_agent, referrer, country, browser, os)
SELECT id, short_code, click_time, ip_address, user_agent, referrer, country, browser, os
FROM analytics;

-- Step 3: Drop the partitioned table (cascades to all partitions)
DROP TABLE analytics CASCADE;

-- Step 4: Rename the flat table back to analytics
ALTER TABLE analytics_flat RENAME TO analytics;

-- Step 5: Recreate original index
CREATE INDEX IF NOT EXISTS idx_analytics_short_code ON analytics(short_code);
