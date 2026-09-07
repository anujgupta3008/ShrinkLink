# ShrinkLink: Distributed URL Shortener & Analytics Engine
## Technical Interview Preparation Guide

---

## Table of Contents
1. [What Problem Does This Project Solve?](#1-what-problem-does-this-project-solve)
2. [Project Structure](#2-project-structure)
3. [Technology Stack: What We Use and Why](#3-technology-stack-what-we-use-and-why)

---

## 1. What Problem Does This Project Solve?

### The Core Real-World Problems

**Problem 1 — Long URLs are unusable in the real world.**
Long URLs with verbose query parameters, UTM tracking tags, and deep path segments are ugly, hard to share in SMS/tweets/printed media, and prone to copy-paste truncation. ShrinkLink converts any URL into a short, human-friendly code like `localhost/a3D9`.

**Problem 2 — Unique short code generation at distributed scale.**
In a distributed system running multiple backend nodes simultaneously, how do you generate millions of unique, short codes without:
- Slowing everything down with a central lock (DB auto-increment bottleneck)?
- Creating codes that are too long (UUID approach defeats the purpose of a *short* URL)?

ShrinkLink solves this with a **Redis-backed Range Allocator** — each app node reserves a block of 1,000 IDs from a single atomic Redis counter (`INCRBY`). Within that block, ID generation is entirely in-memory, requiring zero network calls and taking nanoseconds. This is the core distributed systems design challenge at the heart of the project.

**Problem 3 — Sub-millisecond redirects vs. rich analytics tracking.**
Users expect the redirect (`GET /:code`) to be instantaneous. But product teams need rich click analytics: IP, country, browser, OS, referrer. Writing analytics to SQL synchronously on every redirect would add 10-50ms of disk-write latency to each request.

ShrinkLink decouples these two concerns:
- The redirect response completes immediately after a Redis cache lookup (~0.1ms).
- Analytics are pushed to a Redis list queue (`LPUSH`) — also sub-millisecond.
- A separate background goroutine (`AnalyticsWorker`) consumes the queue, batches up to 50 events, and bulk-inserts them into PostgreSQL every 2 seconds — so the database is never touched on the hot redirect path.

### What the System Delivers

| Capability | Implementation |
|---|---|
| Shorten any URL | `POST /api/shorten` with optional custom alias and expiry |
| Instant redirect | `GET /:code` with Redis cache-aside, falls back to PostgreSQL |
| Analytics dashboard | `GET /api/analytics/:code` — total clicks, browsers, OS, countries, referrers |
| Collision-free code generation | Redis range allocator + Base62 encoding |
| Cache penetration protection | `__NOT_FOUND__` sentinel cached for 5 min on 404 |
| Rate limiting | Redis-backed sliding window, 30 req/min per IP |
| Horizontal scaling | 2 stateless Go replicas behind Nginx load balancer |

---

## 2. Project Structure

```
URL-Shortener/
|
+-- docker-compose.yml          # Orchestrates all 5 services (postgres, redis, app-1, app-2, nginx)
|
+-- nginx/
|   +-- nginx.conf              # Reverse proxy: round-robin to app-1 and app-2, serves static UI
|
+-- web/                        # Vanilla HTML/CSS/JS frontend (no framework)
|   +-- index.html              # Single-page app: shorten form + active URLs table + analytics modal
|   +-- styles.css              # All UI styles (glassmorphism, dark mode, responsive layout)
|   +-- app.js                  # All client logic: API calls, Chart.js rendering, clipboard, history
|
+-- backend/                    # Go application (single binary, stateless, horizontally scalable)
    +-- Dockerfile              # Multi-stage build: golang:1.23-alpine -> alpine:3.19 (~15MB image)
    +-- go.mod / go.sum         # Module: url-shortener | Deps: gin, pq, go-redis, cors
    |
    +-- cmd/
    |   +-- api/
    |       +-- main.go         # Entry point: wires all components, starts HTTP server + analytics worker
    |
    +-- internal/               # Private application packages (Go convention)
        |
        +-- config/
        |   +-- config.go       # Reads all config from env vars: DBHost, DBPort, DBUser, DBPassword,
        |                       # DBName, DBSSLMode, RedisHost, RedisPort, Port, BaseURL
        |
        +-- database/
        |   +-- db.go           # PostgreSQL connection with retry logic (10 attempts, 3s apart).
        |                       # runMigrations(): CREATE TABLE IF NOT EXISTS for `urls` and
        |                       # `analytics` tables + B-Tree indices on startup (self-bootstrapping).
        |
        +-- redis/
        |   +-- redis.go        # Initializes go-redis v9 client from config
        |
        +-- model/
        |   +-- models.go       # All shared Go structs: URL, ClickEvent, AnalyticsRecord,
        |                       # ShortenRequest, ShortenResponse, URLAnalyticsResponse,
        |                       # ClickStats, StatBreakdown
        |
        +-- idgen/
        |   +-- allocator.go    # Range Allocator: atomic CompareAndSwap in-memory + Redis INCRBY
        |   |                   # for non-overlapping lock-free ID block allocation across instances
        |   +-- base62.go       # Encode(int64) -> compact string | Decode(string) -> int64
        |   |                   # Alphabet: "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
        |   +-- base62_test.go  # Unit tests for encode/decode round-trips
        |
        +-- handler/
        |   +-- handlers.go     # Shorten(): validates + generates ID + inserts to DB + caches
        |                       # Redirect(): Redis cache-aside + queues analytics event
        |                       # GetAnalytics(): aggregates 5 dimensions from analytics table
        |                       # queueAnalytics(): LPUSH click event JSON to Redis queue
        |
        +-- middleware/
        |   +-- ratelimit.go    # Redis sliding-window rate limiter. Key format: rate:<ip>:<minute>.
        |                       # Fail-open design: if Redis is down, request is allowed through.
        |
        +-- worker/
        |   +-- worker.go       # AnalyticsWorker: BRPOP from queue:analytics (blocks 1s per poll),
        |                       # batches up to 50 events in memory, flushes every 2s via SQL
        |                       # transaction with prepared statement. Parses UserAgent and IP.
        |
        +-- service/            # (Reserved, currently empty - future business logic layer)
```

### How the Layers Connect (Dependency Flow)

```
[main.go] wires together all components in this order:
    1. Load config from environment variables
    2. Connect to Redis   <-----------+
    3. Connect to PostgreSQL  <----+  |
    4. Create Allocator (Redis)    |  |
    5. Create Handler (DB+Redis+Allocator)
    6. Start AnalyticsWorker goroutine (DB+Redis)
    7. Start Gin HTTP server

HTTP Request Flow:

  POST /api/shorten  -->  Handler.Shorten()
      allocator.NextID()                      --> Redis: INCRBY global_url_id_counter 1000
                                                  (only 1 network call per 1000 URLs; rest are in-memory)
      idgen.Encode(id)                        --> in-memory Base62 conversion
      db.Exec(INSERT INTO urls ...)           --> PostgreSQL
      redis.Set("url:<code>", longURL, 24h)  --> Redis cache write

  GET /:code  -->  Handler.Redirect()
      redis.Get("url:<code>")                 --> Redis
          CACHE HIT:  redis.LPush(queue:analytics, event)  --> Redis
                      c.Redirect(302, longURL)              --> HTTP response
          CACHE MISS: db.QueryRow(SELECT long_url ...)      --> PostgreSQL
                      redis.Set("url:<code>", ...)          --> Redis (populate cache)
                      redis.LPush(queue:analytics, event)   --> Redis
                      c.Redirect(302, longURL)              --> HTTP response
          NOT FOUND:  redis.Set("url:<code>", "__NOT_FOUND__", 5min)
                      c.JSON(404)

  GET /api/analytics/:code  -->  Handler.GetAnalytics()
      db.Query(SELECT COUNT(*) ...)           --> PostgreSQL
      db.Query(GROUP BY period/browser/os/country/referrer ...)  --> PostgreSQL
      c.JSON(200, URLAnalyticsResponse{...})

[AnalyticsWorker goroutine] - runs in background, never touches the HTTP path:
      redis.BRPop("queue:analytics", 1s)      --> Redis (blocks until event available)
      ParseUserAgent(ua)                       --> extracts browser + OS from User-Agent string
      resolveCountry(ip)                       --> maps IP to country string
      tx := db.Begin()
      stmt := tx.Prepare(INSERT INTO analytics ...)
      for each event: stmt.Exec(...)           --> PostgreSQL (batch, up to 50 rows)
      tx.Commit()
```

---

## 3. Technology Stack: What We Use and Why

### 3.1 Go (Golang) — Backend Language

**What**: The entire backend is written in Go 1.26, compiled to a single static binary via `go build`.

**Why**:

- **Goroutines for concurrency**: Go goroutines are multiplexed over OS threads by the runtime scheduler. Each goroutine starts with only ~2KB of stack (vs ~1MB for a Java or C++ OS thread). One Go server handles tens of thousands of concurrent HTTP connections — like simultaneous redirects — with a fraction of the memory.

- **`sync/atomic` for lock-free ID allocation**: In `allocator.go`, `atomic.CompareAndSwapInt64` is a single CPU instruction that updates a memory address only if its current value matches the expected value. Multiple goroutines race to claim the next ID in-memory with no mutex — no thread is ever blocked or context-switched. This is measurably faster than `sync.Mutex` under high concurrency.

- **Single compiled binary**: `CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s"` produces a fully static binary with no external runtime dependencies. The final Docker image is ~15MB (built on `alpine:3.19`).

- **Standard library depth**: `net/http`, `database/sql`, `encoding/json`, `context`, `sync`, `os/signal` handle the HTTP server, database driver, JSON marshaling, graceful shutdown, and goroutine lifecycle — all without third-party frameworks.

---

### 3.2 Gin Gonic — HTTP Router and Middleware Framework

**What**: `github.com/gin-gonic/gin v1.12.0`

**Why**:

- **Radix tree routing**: Gin uses a compressed radix trie (Patricia tree) internally. Route matching is `O(k)` where `k` is path length — constant time regardless of how many routes are registered. Crucial for `GET /:code` which matches every short code.

- **Middleware chain**: `router.Use()` composes layers cleanly. We apply `gin.Logger()`, `gin.Recovery()` (panic -> 500), `cors.New()`, and per-group `middleware.RateLimiter()` without coupling to handler code.

- **Route groups for selective middleware**: `router.Group("/api").Use(RateLimiter(...))` applies rate limiting only to the shorten and analytics API endpoints. The `GET /:code` redirect route is intentionally left outside the group — the high-frequency redirect path should not be rate-limited.

- **`gin.Context`**: Wraps request/response with convenience methods: `c.Param("code")`, `c.ClientIP()`, `c.GetHeader("User-Agent")`, `c.ShouldBindJSON()`, `c.JSON()`, `c.Redirect()` — all without reflection.

---

### 3.3 PostgreSQL 15 — Relational Database (Persistence Tier)

**What**: `postgres:15-alpine` Docker container. Go driver: `github.com/lib/pq`.

**Why**:

- **ACID transactions**: Every URL insert and every analytics batch-flush is wrapped in a `BEGIN` / `COMMIT` / `ROLLBACK` transaction. No half-written analytics rows corrupt the analytics queries.

- **Self-bootstrapping schema (auto-migration)**: `db.go` runs `CREATE TABLE IF NOT EXISTS` for both tables on every startup. The app creates its own schema — no external migration tooling needed. This is critical for Docker Compose where the DB container starts fresh.

- **B-Tree index on `short_code`**: `CREATE INDEX IF NOT EXISTS idx_urls_short_code ON urls(short_code)` turns the redirect lookup (`SELECT long_url WHERE short_code = $1`) from a full sequential scan into an `O(log n)` index lookup.

- **`BIGINT` primary key from Range Allocator**: Sequential integer keys cause monotonically growing B-Tree pages — no random page splits. UUID primary keys cause B-Tree fragmentation and ~30% write amplification at scale.

- **Prepared statement bulk inserts**: In `worker.go`, a single `tx.Prepare(INSERT INTO analytics ...)` call is reused for every row in a batch. One `BEGIN`, N executions, one `COMMIT` — minimizes round trips and PostgreSQL write WAL amplification.

**Schema created at runtime**:
```sql
CREATE TABLE IF NOT EXISTS urls (
  id           BIGINT PRIMARY KEY,       -- From Range Allocator, converted via Base62
  short_code   VARCHAR(10) UNIQUE NOT NULL, -- B-Tree indexed; the lookup key
  long_url     TEXT NOT NULL,
  is_custom    BOOLEAN DEFAULT FALSE,
  created_at   TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  expires_at   TIMESTAMPTZ               -- NULL = never expires
);

CREATE TABLE IF NOT EXISTS analytics (
  id           BIGSERIAL PRIMARY KEY,
  short_code   VARCHAR(10) NOT NULL,    -- B-Tree indexed
  click_time   TIMESTAMPTZ DEFAULT CURRENT_TIMESTAMP,
  ip_address   VARCHAR(45),
  user_agent   TEXT,
  referrer     TEXT,
  country      VARCHAR(100),            -- Resolved by resolveCountry() in worker.go
  browser      VARCHAR(50),             -- Resolved by ParseUserAgent() in worker.go
  os           VARCHAR(50)              -- Resolved by ParseUserAgent() in worker.go
);
```

---

### 3.4 Redis 7 — Cache, Queue, ID Counter, and Rate Limiter

**What**: `redis:7-alpine` Docker container. Go driver: `github.com/redis/go-redis/v9`.

Redis is used in **four completely distinct roles** in this project:

| Role | Redis Commands | Where in Code | Why Redis (not something else) |
|---|---|---|---|
| **URL Cache (Cache-Aside)** | `SET url:<code> <url> EX <ttl>` / `GET url:<code>` | `handler/handlers.go` | Sub-millisecond reads; avoids PostgreSQL on every redirect |
| **Analytics Event Queue** | `LPUSH queue:analytics <json>` / `BRPOP queue:analytics 1` | handler pushes; `worker/worker.go` consumes | Decouples redirect latency from DB write latency |
| **Distributed ID Counter** | `INCRBY global_url_id_counter 1000` | `idgen/allocator.go` — `fetchNextBlock()` | Atomic counter shared across all app instances |
| **Rate Limiter Bucket** | `INCR rate:<ip>:<minute>` / `EXPIRE rate:<ip>:<minute>` | `middleware/ratelimit.go` | Per-IP per-minute counter with automatic TTL expiry |

**Cache penetration protection**:
When a `short_code` is not found in PostgreSQL, the sentinel value `"__NOT_FOUND__"` is stored in Redis with a 5-minute TTL. Subsequent requests for the same invalid code find the sentinel in Redis and immediately return `404` — without hitting PostgreSQL. This blocks cache penetration attacks where an attacker spams non-existent codes to overload the database.

**Rate limiter key format**: `rate:127.0.0.1:2026-09-06-23-27` (IP + minute-precision timestamp bucket). On the first request in a bucket, `EXPIRE` is set. If Redis is unavailable, the middleware fails open (`c.Next()`) — preserving availability over strict enforcement.

**TTL strategy**:
- Active URLs: 24-hour TTL (stale entries automatically evicted)
- Expiring URLs: TTL = `expiresAt - now` (cache invalidates exactly at expiry time)
- Not-found sentinel: 5-minute TTL

---

### 3.5 Nginx 1.25 — Load Balancer, Reverse Proxy, and Static File Server

**What**: `nginx:1.25-alpine` Docker container. Config in `nginx/nginx.conf`.

**Why**:

- **Single public entrypoint**: Port `80` is the only externally exposed port. All traffic enters through Nginx. Neither `app-1:8080` nor `app-2:8080` are directly reachable from outside Docker.

- **Round-robin load balancing**: `upstream backend_servers { server app-1:8080; server app-2:8080; }` distributes requests evenly across both Go instances by default.

- **Static file serving bypasses Go**: The `web/` directory is mounted as a Docker volume at `/usr/share/nginx/html`. Nginx serves `index.html`, `styles.css`, and `app.js` directly from disk — Go app servers are never involved. Saves backend CPU entirely for API requests and redirects.

- **Smart fallback routing**: `try_files $uri $uri/ @backend` — static files are served directly; unrecognized paths (any short code like `/3N9a`) fall through to the `@backend` named location, which proxies to the Go upstream.

- **Real IP forwarding**: `proxy_set_header X-Real-IP $remote_addr` and `X-Forwarded-For` ensure the Go app sees the actual client IP (not Nginx's internal Docker IP) for rate limiting and analytics country resolution.

- **Gzip compression**: `gzip on` for `text/plain text/css application/json application/javascript` — reduces payload transfer sizes.

---

### 3.6 Docker and Docker Compose — Containerization and Orchestration

**What**: `Dockerfile` (multi-stage build) + `docker-compose.yml` (5-service stack).

**Why**:

- **Multi-stage Dockerfile eliminates bloat**: Stage 1 (`golang:1.23-alpine`) downloads modules and compiles the binary. Stage 2 (`alpine:3.19`) copies only the binary and `ca-certificates`. Final image: ~15MB vs ~800MB for a single-stage full Go image.

- **Health checks enforce startup ordering**: `postgres` and `redis` define `healthcheck` blocks (`pg_isready`, `redis-cli ping`). `app-1` and `app-2` use `depends_on: condition: service_healthy` — Go servers don't start until dependencies are confirmed healthy. The Go `db.go` also has its own 10-retry loop with 3s delays as a second layer of protection.

- **Stateless app replicas**: `app-1` and `app-2` are identical service definitions. All shared state lives in Redis and PostgreSQL — the Go processes themselves hold no state. Adding a third replica requires only duplicating the service block and updating `nginx.conf`.

- **Named volume for data persistence**: `pgdata:/var/lib/postgresql/data` persists the database across `docker-compose down && docker-compose up` cycles.

- **Environment variable injection**: All config (DB credentials, Redis host/port, `BASE_URL`, `GIN_MODE`) is passed via `environment:` in `docker-compose.yml`. The Go app reads them via `os.Getenv()` in `config/config.go` — the 12-factor app pattern.

---

### 3.7 Vanilla HTML/CSS/JS — Frontend Dashboard

**What**: Three files in `web/` — `index.html`, `styles.css`, `app.js`. `Chart.js` loaded from `cdn.jsdelivr.net`.

**Why no React/Vue/Angular?**
The UI is a single page with two API endpoints (`/api/shorten`, `/api/analytics/:code`). A framework adds 100KB+ of JavaScript bundle, a build toolchain, and a Node.js dev server — none of which provide architectural value here. Nginx serves the three static files directly.

**What each file does**:
- **`index.html`**: Semantic HTML with a shorten form, an active URLs table (`#history-table`), and an analytics modal overlay (`#analytics-modal`) containing Chart.js canvas elements.
- **`styles.css`**: Glassmorphism dark-mode design using CSS custom properties, `backdrop-filter: blur()`, gradient glow orbs (`glow-orb`), responsive CSS grid for chart layout, and CSS transitions for interactive states.
- **`app.js`**: `fetch()` calls to backend API, `Chart.js` initialization for 5 chart types (line chart for clicks over time; doughnut charts for browsers, OS, referrers, countries), clipboard copy via `navigator.clipboard.writeText()`, and session-scoped URL history via `localStorage`.
- **`Chart.js`** (CDN): Renders all analytics visualizations. Zero frontend build step — `<script src="https://cdn.jsdelivr.net/npm/chart.js">` is sufficient.

---

### Complete Technology Stack at a Glance

| Layer | Technology | Version | Key Package / Tool |
|---|---|---|---|
| **Backend Language** | Go | 1.26 | `net/http`, `sync/atomic`, `database/sql`, `context` |
| **HTTP Router & Middleware** | Gin Gonic | v1.12.0 | `github.com/gin-gonic/gin` |
| **CORS** | gin-contrib/cors | v1.7.7 | `github.com/gin-contrib/cors` |
| **Relational Database** | PostgreSQL | 15-alpine | `github.com/lib/pq` |
| **Cache / Queue / ID Counter / Rate Limiter** | Redis | 7-alpine | `github.com/redis/go-redis/v9` |
| **Load Balancer & Static Server** | Nginx | 1.25-alpine | Custom `nginx/nginx.conf` |
| **Containerization** | Docker + Compose | v3.8 | Multi-stage `Dockerfile` |
| **Frontend** | Vanilla HTML/CSS/JS | — | `Chart.js` from CDN |
