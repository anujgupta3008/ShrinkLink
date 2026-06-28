# Premium Distributed URL Shortener

A high-performance, fault-tolerant, and horizontally scalable URL Shortener and click analytics engine built from scratch using **Go, PostgreSQL, Redis, Docker, and Nginx**.

---

## 🚀 Key Architectural Features

### 1. Stateless Horizontally Scalable Backends
The Go application instances (`app-1`, `app-2`) are entirely stateless. They handle API requests, redirects, and analytics compilations. You can spin up as many backend instances as required and load balance traffic across them without any session synchronization issues.

### 2. Distributed Unique ID Allocation (Range Allocator)
To avoid standard database auto-increment locks and prevent collisions in a multi-instance system:
- Each stateless instance claims a block of unique numeric IDs (e.g. 1000 IDs at a time) from Redis using `INCRBY`.
- The instance manages this range in-memory using atomic Compare-And-Swap (`sync/atomic`) operations.
- This results in O(1) in-memory ID generation for new URLs, requiring a Redis roundtrip only once per 1,000 shorten requests.
- The numeric ID is then encoded using custom **Base62 encoding** (e.g. `981240` -> `4aD2`).

### 3. Redis Cache-Aside & Protection Strategies
- **Cache-Aside Redirection**: Redirect operations (`GET /:code`) first query Redis. On a hit, they redirect instantly. On a miss, they read from PostgreSQL and write to Redis with a 24-hour TTL (or up to the URL's specific expiration).
- **Cache Penetration Protection**: If a code doesn't exist, we cache a special `__NOT_FOUND__` marker in Redis for 5 minutes. This prevents malicious actors from spamming invalid URLs to overwhelm PostgreSQL.

### 4. Async Analytics Pipeline
Writing visitor metadata (IP, OS, browser, referrer, country) to PostgreSQL during redirection introduces latency. To ensure redirections remain sub-millisecond:
- The redirect handler pushes a JSON click event into a Redis list (`queue:analytics`) and immediately returns a `302 Found` response.
- A background **Analytics Worker** runs in a separate loop, polling the Redis queue, batching incoming events, parsing browser/OS, resolving countries, and flushing them to PostgreSQL using bulk insert transactions.

### 5. Nginx Entrypoint & Load Balancing
Nginx acts as the single entrypoint on port `80`:
- Performs round-robin HTTP load balancing across backend application containers.
- Directly serves the static single-page administration dashboard for maximum speed.
- Automatically handles routing, routing `/api/*` and redirection codes (`/*`) to the backend.

---

## 🛠️ Technology Stack
- **Backend Language**: Go (Golang)
- **Routing Framework**: Gin Gonic
- **Caching & Queue**: Redis 7
- **Database Persistence**: PostgreSQL 15
- **Load Balancer**: Nginx
- **Containerization**: Docker & Docker Compose
- **Frontend Dashboard**: HTML5, Vanilla CSS (Glassmorphism), Vanilla JS, Chart.js

---

## 📂 Project Structure

```text
URL-Shortener/
├── docker-compose.yml       # Orchestrates all service containers
├── README.md                # System documentation
├── nginx/
│   └── nginx.conf           # Load balancer & static serving configuration
├── web/                     # Premium frontend dashboard
│   ├── index.html           # Structure & Modal
│   ├── styles.css           # Glassmorphism dark mode styles
│   └── app.js               # Dashboard & Chart.js logic
└── backend/                 # Go code
    ├── cmd/
    │   └── api/
    │       └── main.go      # Application entrypoint
    ├── internal/
    │   ├── config/          # Configurations
    │   ├── database/        # PostgreSQL client & migrations
    │   ├── redis/           # Redis client
    │   ├── idgen/           # Range allocator & Base62
    │   ├── model/           # API request/response structures
    │   ├── handler/         # HTTP handlers
    │   ├── middleware/      # Rate limiter middleware
    │   └── worker/          # Async analytics pipeline worker
    ├── Dockerfile           # Multi-stage production build
    ├── go.mod
    └── go.sum
```

---

## ⚙️ How to Run the Project

### Prerequisites
Make sure you have [Docker](https://www.docker.com/) and [Docker Compose](https://docs.docker.com/compose/) installed on your machine.

### Step 1: Clone and Start
Simply navigate to the project root folder and boot the environment:
```bash
docker-compose up --build
```
Docker Compose will automatically compile the Go binary, download dependencies, run PostgreSQL database tables, set up Redis, and boot up Nginx on port 80.

### Step 2: Open the Dashboard
Open your browser and visit:
```text
http://localhost
```
You will be greeted by the premium glassmorphism dashboard where you can:
- Paste a URL to shorten.
- Specify a custom alias.
- Add an expiration timeframe.
- Click the analytics icon of any generated shortcode to view real-time click metrics, country distribution, and OS/browser breakdowns!

---

## 🧪 Running Unit Tests
If you have Go installed on your host machine, you can run the test suite:
```bash
cd backend
go test ./...
```
This runs the unit tests, verifying the correctness of the Base62 encoding and decoding boundaries.

---

## 📈 System Scaling and Future Extensions

To support billions of redirection requests, the following improvements can be integrated:
1. **Message Broker (Kafka/RabbitMQ)**: Replace the Redis list queue with Apache Kafka for highly reliable, partitions-based analytics logging.
2. **Sonyflake ID Generator**: Use Sonyflake (distributed coordinate-free ID generator) instead of the Range Allocator if Redis becomes a single point of range failure.
3. **GeoIP MaxMind Reader**: Integrate an offline MaxMind GeoLite2 country database instead of standard simulated mapping for robust real-world visitor locations.

