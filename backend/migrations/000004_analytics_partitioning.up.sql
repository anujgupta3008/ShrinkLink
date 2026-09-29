-- 000004_analytics_partitioning.up.sql
-- Level 17: Partition the analytics table by month for scalability.
-- This converts the existing analytics table to a range-partitioned table
-- based on click_time, and creates initial monthly partitions.

-- Step 1: Rename the existing analytics table
ALTER TABLE analytics RENAME TO analytics_old;

-- Step 2: Create the new partitioned analytics table
CREATE TABLE analytics (
    id          BIGSERIAL,
    short_code  VARCHAR(10) NOT NULL,
    click_time  TIMESTAMPTZ DEFAULT NOW(),
    ip_address  VARCHAR(45),
    user_agent  TEXT,
    referrer    TEXT,
    country     VARCHAR(100),
    browser     VARCHAR(50),
    os          VARCHAR(50),
    PRIMARY KEY (id, click_time)
) PARTITION BY RANGE (click_time);

-- Step 3: Create partitions for recent and upcoming months
CREATE TABLE analytics_2025_01 PARTITION OF analytics
    FOR VALUES FROM ('2025-01-01') TO ('2025-02-01');
CREATE TABLE analytics_2025_02 PARTITION OF analytics
    FOR VALUES FROM ('2025-02-01') TO ('2025-03-01');
CREATE TABLE analytics_2025_03 PARTITION OF analytics
    FOR VALUES FROM ('2025-03-01') TO ('2025-04-01');
CREATE TABLE analytics_2025_04 PARTITION OF analytics
    FOR VALUES FROM ('2025-04-01') TO ('2025-05-01');
CREATE TABLE analytics_2025_05 PARTITION OF analytics
    FOR VALUES FROM ('2025-05-01') TO ('2025-06-01');
CREATE TABLE analytics_2025_06 PARTITION OF analytics
    FOR VALUES FROM ('2025-06-01') TO ('2025-07-01');
CREATE TABLE analytics_2025_07 PARTITION OF analytics
    FOR VALUES FROM ('2025-07-01') TO ('2025-08-01');
CREATE TABLE analytics_2025_08 PARTITION OF analytics
    FOR VALUES FROM ('2025-08-01') TO ('2025-09-01');
CREATE TABLE analytics_2025_09 PARTITION OF analytics
    FOR VALUES FROM ('2025-09-01') TO ('2025-10-01');
CREATE TABLE analytics_2025_10 PARTITION OF analytics
    FOR VALUES FROM ('2025-10-01') TO ('2025-11-01');
CREATE TABLE analytics_2025_11 PARTITION OF analytics
    FOR VALUES FROM ('2025-11-01') TO ('2025-12-01');
CREATE TABLE analytics_2025_12 PARTITION OF analytics
    FOR VALUES FROM ('2025-12-01') TO ('2026-01-01');

-- 2026 partitions
CREATE TABLE analytics_2026_01 PARTITION OF analytics
    FOR VALUES FROM ('2026-01-01') TO ('2026-02-01');
CREATE TABLE analytics_2026_02 PARTITION OF analytics
    FOR VALUES FROM ('2026-02-01') TO ('2026-03-01');
CREATE TABLE analytics_2026_03 PARTITION OF analytics
    FOR VALUES FROM ('2026-03-01') TO ('2026-04-01');
CREATE TABLE analytics_2026_04 PARTITION OF analytics
    FOR VALUES FROM ('2026-04-01') TO ('2026-05-01');
CREATE TABLE analytics_2026_05 PARTITION OF analytics
    FOR VALUES FROM ('2026-05-01') TO ('2026-06-01');
CREATE TABLE analytics_2026_06 PARTITION OF analytics
    FOR VALUES FROM ('2026-06-01') TO ('2026-07-01');
CREATE TABLE analytics_2026_07 PARTITION OF analytics
    FOR VALUES FROM ('2026-07-01') TO ('2026-08-01');
CREATE TABLE analytics_2026_08 PARTITION OF analytics
    FOR VALUES FROM ('2026-08-01') TO ('2026-09-01');
CREATE TABLE analytics_2026_09 PARTITION OF analytics
    FOR VALUES FROM ('2026-09-01') TO ('2026-10-01');
CREATE TABLE analytics_2026_10 PARTITION OF analytics
    FOR VALUES FROM ('2026-10-01') TO ('2026-11-01');
CREATE TABLE analytics_2026_11 PARTITION OF analytics
    FOR VALUES FROM ('2026-11-01') TO ('2026-12-01');
CREATE TABLE analytics_2026_12 PARTITION OF analytics
    FOR VALUES FROM ('2026-12-01') TO ('2027-01-01');

-- Default partition for any data outside defined ranges
CREATE TABLE analytics_default PARTITION OF analytics DEFAULT;

-- Step 4: Migrate existing data
INSERT INTO analytics (id, short_code, click_time, ip_address, user_agent, referrer, country, browser, os)
SELECT id, short_code, click_time, ip_address, user_agent, referrer, country, browser, os
FROM analytics_old;

-- Step 5: Recreate indexes on the partitioned table
CREATE INDEX IF NOT EXISTS idx_analytics_short_code ON analytics(short_code);
CREATE INDEX IF NOT EXISTS idx_analytics_click_time ON analytics(click_time);

-- Step 6: Drop the old table
DROP TABLE analytics_old;
