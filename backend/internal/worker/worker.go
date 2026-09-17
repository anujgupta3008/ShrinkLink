package worker

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	ua "github.com/mileusna/useragent"
	"url-shortener/internal/geoip"
	"url-shortener/internal/model"
	"url-shortener/internal/redis"
	"url-shortener/internal/repository"
)

type AnalyticsWorker struct {
	analyticsRepo repository.AnalyticsRepository
	urlRepo       repository.URLRepository
	redisClient   *redis.Client
	geoResolver   *geoip.Resolver
	queueName     string
	batchSize     int
	flushInt      time.Duration
}

func NewAnalyticsWorker(
	analyticsRepo repository.AnalyticsRepository,
	urlRepo repository.URLRepository,
	rdb *redis.Client,
	geo *geoip.Resolver,
	queueName string,
	batchSize int,
	flushInterval time.Duration,
) *AnalyticsWorker {
	return &AnalyticsWorker{
		analyticsRepo: analyticsRepo,
		urlRepo:       urlRepo,
		redisClient:   rdb,
		geoResolver:   geo,
		queueName:     queueName,
		batchSize:     batchSize,
		flushInt:      flushInterval,
	}
}

func (w *AnalyticsWorker) Start(ctx context.Context) {
	slog.Info("Starting background analytics worker...")
	ticker := time.NewTicker(w.flushInt)
	defer ticker.Stop()

	var batch []model.ClickEvent

	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping analytics worker, flushing remaining events...")
			if len(batch) > 0 {
				w.flush(batch)
			}
			return
		case <-ticker.C:
			if len(batch) > 0 {
				w.flush(batch)
				batch = nil
			}
		default:
			// Fetch from Redis queue (block for up to 1 second)
			results, err := w.redisClient.BRPop(ctx, 1*time.Second, w.queueName).Result()
			if err != nil {
				// Nil error represents a timeout when no items are available
				if err.Error() != "redis: nil" {
					slog.Error("Error popping from queue", "err", err)
				}
				continue
			}

			// results[0] is the queue name, results[1] is the value
			if len(results) < 2 {
				continue
			}

			var event model.ClickEvent
			if err := json.Unmarshal([]byte(results[1]), &event); err != nil {
				slog.Error("Failed to unmarshal click event", "err", err)
				continue
			}

			batch = append(batch, event)

			if len(batch) >= w.batchSize {
				w.flush(batch)
				batch = nil
			}
		}
	}
}

func (w *AnalyticsWorker) flush(events []model.ClickEvent) {
	if len(events) == 0 {
		return
	}

	slog.Info("Flushing analytics batch", "count", len(events))

	var records []model.AnalyticsRecord
	clickCounts := make(map[string]int)

	ctx := context.Background()

	for _, event := range events {
		// Level 9: Real GeoIP lookup (replaces mock random country)
		country := w.geoResolver.Country(event.IPAddress)

		// Level 9: Maintained UA parsing library (replaces hand-rolled parser)
		browser, os, device := ParseUserAgent(event.UserAgent)

		records = append(records, model.AnalyticsRecord{
			ShortCode: event.ShortCode,
			ClickTime: event.ClickTime,
			IPAddress: event.IPAddress,
			UserAgent: event.UserAgent,
			Referrer:  event.Referrer,
			Country:   country,
			Browser:   browser,
			OS:        os,
			Device:    device,
		})
		clickCounts[event.ShortCode]++

		// Level 9: HyperLogLog for unique visitors per short code.
		// PFADD is O(1) and uses ~12KB per key regardless of cardinality.
		w.redisClient.PFAdd(ctx, "uv:"+event.ShortCode, event.IPAddress)
	}

	// 1. Insert analytics records
	if err := w.analyticsRepo.BulkInsert(ctx, records); err != nil {
		slog.Error("Failed to insert analytics events", "err", err)
	}

	// 2. Batch update URL click_counts
	if err := w.urlRepo.BulkIncrementClicks(ctx, clickCounts); err != nil {
		slog.Error("Failed to bulk increment click counts", "err", err)
	}

	// 3. Decrement the Redis live click counters for the flushed amount
	for code, count := range clickCounts {
		w.redisClient.DecrBy(ctx, "clicks:"+code, int64(count))
	}
}

// ParseUserAgent extracts Browser, OS, and Device type from a user agent string
// using the maintained github.com/mileusna/useragent library (Level 9).
func ParseUserAgent(uaStr string) (browser, os, device string) {
	parsed := ua.Parse(uaStr)

	// Browser
	browser = parsed.Name
	if browser == "" {
		browser = "Other"
	}

	// OS
	os = parsed.OS
	if os == "" {
		os = "Other"
	}

	// Device type
	if parsed.Bot {
		device = "Bot"
	} else if parsed.Mobile {
		device = "Mobile"
	} else if parsed.Tablet {
		device = "Tablet"
	} else if parsed.Desktop {
		device = "Desktop"
	} else {
		device = "Other"
	}

	return browser, os, device
}
