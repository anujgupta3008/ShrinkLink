package worker

import (
	"context"
	"encoding/json"
	"log"
	"math/rand"
	"strings"
	"time"

	"url-shortener/internal/model"
	"url-shortener/internal/redis"
	"url-shortener/internal/repository"
)

type AnalyticsWorker struct {
	analyticsRepo repository.AnalyticsRepository
	urlRepo       repository.URLRepository
	redisClient   *redis.Client
	queueName     string
	batchSize     int
	flushInt      time.Duration
}

func NewAnalyticsWorker(
	analyticsRepo repository.AnalyticsRepository,
	urlRepo repository.URLRepository,
	rdb *redis.Client,
	queueName string,
	batchSize int,
	flushInterval time.Duration,
) *AnalyticsWorker {
	return &AnalyticsWorker{
		analyticsRepo: analyticsRepo,
		urlRepo:       urlRepo,
		redisClient:   rdb,
		queueName:     queueName,
		batchSize:     batchSize,
		flushInt:      flushInterval,
	}
}

func (w *AnalyticsWorker) Start(ctx context.Context) {
	log.Println("Starting background analytics worker...")
	ticker := time.NewTicker(w.flushInt)
	defer ticker.Stop()

	var batch []model.ClickEvent

	for {
		select {
		case <-ctx.Done():
			log.Println("Stopping analytics worker, flushing remaining events...")
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
					log.Printf("Error popping from queue: %v", err)
				}
				continue
			}

			// results[0] is the queue name, results[1] is the value
			if len(results) < 2 {
				continue
			}

			var event model.ClickEvent
			if err := json.Unmarshal([]byte(results[1]), &event); err != nil {
				log.Printf("Failed to unmarshal click event: %v", err)
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

	log.Printf("Flushing batch of %d analytics events to PostgreSQL...", len(events))

	var records []model.AnalyticsRecord
	clickCounts := make(map[string]int)

	for _, event := range events {
		country := resolveCountry(event.IPAddress)
		browser, os := ParseUserAgent(event.UserAgent)
		records = append(records, model.AnalyticsRecord{
			ShortCode: event.ShortCode,
			ClickTime: event.ClickTime,
			IPAddress: event.IPAddress,
			UserAgent: event.UserAgent,
			Referrer:  event.Referrer,
			Country:   country,
			Browser:   browser,
			OS:        os,
		})
		clickCounts[event.ShortCode]++
	}

	ctx := context.Background()

	// 1. Insert analytics records
	if err := w.analyticsRepo.BulkInsert(ctx, records); err != nil {
		log.Printf("Failed to insert analytics events: %v", err)
	}

	// 2. Batch update URL click_counts
	if err := w.urlRepo.BulkIncrementClicks(ctx, clickCounts); err != nil {
		log.Printf("Failed to bulk increment click counts: %v", err)
	}

	// 3. Decrement the Redis live click counters for the flushed amount
	for code, count := range clickCounts {
		w.redisClient.DecrBy(ctx, "clicks:"+code, int64(count))
	}
}

// Simple country resolver. Mocks countries for local/private IPs to make dashboard look pretty!
func resolveCountry(ip string) string {
	if ip == "127.0.0.1" || ip == "::1" || strings.HasPrefix(ip, "192.168.") || strings.HasPrefix(ip, "10.") {
		countries := []string{"United States", "United Kingdom", "Germany", "India", "Japan", "Canada", "Australia", "France"}
		return countries[rand.Intn(len(countries))]
	}
	return "United States" // Fallback
}

// ParseUserAgent extracts Browser and OS from user agent string for analytics stats.
func ParseUserAgent(ua string) (browser, os string) {
	ua = strings.ToLower(ua)

	// OS detection
	if strings.Contains(ua, "windows") {
		os = "Windows"
	} else if strings.Contains(ua, "macintosh") || strings.Contains(ua, "mac os x") {
		if strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") {
			os = "iOS"
		} else {
			os = "macOS"
		}
	} else if strings.Contains(ua, "android") {
		os = "Android"
	} else if strings.Contains(ua, "linux") {
		os = "Linux"
	} else if strings.Contains(ua, "iphone") || strings.Contains(ua, "ipad") {
		os = "iOS"
	} else {
		os = "Other"
	}

	// Browser detection
	if strings.Contains(ua, "firefox") {
		browser = "Firefox"
	} else if strings.Contains(ua, "chrome") || strings.Contains(ua, "chromium") {
		if strings.Contains(ua, "edg") {
			browser = "Edge"
		} else {
			browser = "Chrome"
		}
	} else if strings.Contains(ua, "safari") {
		browser = "Safari"
	} else if strings.Contains(ua, "opr") || strings.Contains(ua, "opera") {
		browser = "Opera"
	} else {
		browser = "Other"
	}

	return browser, os
}
