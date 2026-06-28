package handler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"url-shortener/internal/database"
	"url-shortener/internal/idgen"
	"url-shortener/internal/model"
	"url-shortener/internal/redis"
)

var aliasRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type Handler struct {
	db          *database.DB
	redisClient *redis.Client
	allocator   *idgen.Allocator
	baseURL     string
}

func NewHandler(db *database.DB, rdb *redis.Client, allocator *idgen.Allocator, baseURL string) *Handler {
	return &Handler{
		db:          db,
		redisClient: rdb,
		allocator:   allocator,
		baseURL:     baseURL,
	}
}

// Shorten creates a short URL from a long URL.
func (h *Handler) Shorten(c *gin.Context) {
	var req model.ShortenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var shortCode string
	isCustom := false

	if req.Alias != "" {
		if !aliasRegex.MatchString(req.Alias) {
			c.JSON(http.StatusBadRequest, gin.H{"error": "custom alias must be alphanumeric, dashes, or underscores"})
			return
		}

		// Check if custom alias already exists in DB
		var existingID int64
		err := h.db.QueryRow("SELECT id FROM urls WHERE short_code = $1", req.Alias).Scan(&existingID)
		if err != sql.ErrNoRows {
			if err == nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "custom alias is already in use"})
			} else {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to verify custom alias availability"})
			}
			return
		}

		shortCode = req.Alias
		isCustom = true
	}

	// Generate ID
	id, err := h.allocator.NextID(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to generate unique ID: " + err.Error()})
		return
	}

	if shortCode == "" {
		shortCode = idgen.Encode(id)
	}

	var expiresAt *time.Time
	if req.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(req.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	// Save to database
	_, err = h.db.Exec(`
		INSERT INTO urls (id, short_code, long_url, is_custom, expires_at)
		VALUES ($1, $2, $3, $4, $5)
	`, id, shortCode, req.LongURL, isCustom, expiresAt)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to save URL: " + err.Error()})
		return
	}

	// Write to Redis (Cache)
	redisKey := "url:" + shortCode
	ttl := 24 * time.Hour
	if expiresAt != nil {
		ttl = expiresAt.Sub(time.Now())
	}
	if err := h.redisClient.Set(c.Request.Context(), redisKey, req.LongURL, ttl).Err(); err != nil {
		log.Printf("Failed to cache URL in Redis: %v", err)
	}

	c.JSON(http.StatusCreated, model.ShortenResponse{
		ShortURL:  fmt.Sprintf("%s/%s", h.baseURL, shortCode),
		ShortCode: shortCode,
		LongURL:   req.LongURL,
		ExpiresAt: expiresAt,
	})
}

// Redirect handles redirecting from a short code to the long URL.
func (h *Handler) Redirect(c *gin.Context) {
	code := c.Param("code")
	ctx := c.Request.Context()
	redisKey := "url:" + code

	// 1. Check Redis
	val, err := h.redisClient.Get(ctx, redisKey).Result()
	if err == nil {
		// Cache Hit
		if val == "__NOT_FOUND__" {
			c.JSON(http.StatusNotFound, gin.H{"error": "URL not found"})
			return
		}

		h.queueAnalytics(code, c)
		c.Redirect(http.StatusFound, val)
		return
	}

	// 2. Cache Miss, query DB
	var longURL string
	var expiresAt *time.Time
	err = h.db.QueryRow("SELECT long_url, expires_at FROM urls WHERE short_code = $1", code).Scan(&longURL, &expiresAt)
	if err == sql.ErrNoRows {
		// Cache the 404 to prevent cache penetration
		h.redisClient.Set(ctx, redisKey, "__NOT_FOUND__", 5*time.Minute)
		c.JSON(http.StatusNotFound, gin.H{"error": "URL not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	// Check Expiration
	if expiresAt != nil && expiresAt.Before(time.Now()) {
		h.redisClient.Set(ctx, redisKey, "__NOT_FOUND__", 5*time.Minute)
		c.JSON(http.StatusGone, gin.H{"error": "URL has expired"})
		return
	}

	// Cache the URL
	ttl := 24 * time.Hour
	if expiresAt != nil {
		ttl = expiresAt.Sub(time.Now())
	}
	h.redisClient.Set(ctx, redisKey, longURL, ttl)

	// Queue Analytics Click Event
	h.queueAnalytics(code, c)

	c.Redirect(http.StatusFound, longURL)
}

func (h *Handler) queueAnalytics(code string, c *gin.Context) {
	event := model.ClickEvent{
		ShortCode: code,
		ClickTime: time.Now(),
		IPAddress: c.ClientIP(),
		UserAgent: c.GetHeader("User-Agent"),
		Referrer:  c.GetHeader("Referer"),
	}

	data, err := json.Marshal(event)
	if err != nil {
		log.Printf("Failed to marshal click event: %v", err)
		return
	}

	// LPUSH to analytics queue
	if err := h.redisClient.LPush(context.Background(), "queue:analytics", data).Err(); err != nil {
		log.Printf("Failed to push click event to Redis queue: %v", err)
	}
}

// GetAnalytics aggregates and returns analytics for a short code.
func (h *Handler) GetAnalytics(c *gin.Context) {
	code := c.Param("code")

	// Verify short URL exists
	var dummy int
	err := h.db.QueryRow("SELECT 1 FROM urls WHERE short_code = $1", code).Scan(&dummy)
	if err == sql.ErrNoRows {
		c.JSON(http.StatusNotFound, gin.H{"error": "URL not found"})
		return
	} else if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}

	// 1. Total Clicks
	var totalClicks int
	err = h.db.QueryRow("SELECT COUNT(*) FROM analytics WHERE short_code = $1", code).Scan(&totalClicks)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query total clicks"})
		return
	}

	// 2. Clicks over time (last 7 days, grouped by day/hour depending on data density)
	// We'll group by hour/day
	rows, err := h.db.Query(`
		SELECT TO_CHAR(click_time, 'YYYY-MM-DD HH24:00') as period, COUNT(*) as clicks
		FROM analytics
		WHERE short_code = $1 AND click_time >= NOW() - INTERVAL '7 days'
		GROUP BY period
		ORDER BY period ASC
	`, code)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query clicks over time"})
		return
	}
	defer rows.Close()

	var clicksOverTime []model.ClickStats
	for rows.Next() {
		var cs model.ClickStats
		if err := rows.Scan(&cs.Period, &cs.Clicks); err != nil {
			log.Printf("Error scanning click stats: %v", err)
			continue
		}
		clicksOverTime = append(clicksOverTime, cs)
	}

	// 3. Referrers breakdown
	referrerRows, err := h.db.Query(`
		SELECT COALESCE(NULLIF(referrer, ''), 'Direct') as name, COUNT(*) as count
		FROM analytics
		WHERE short_code = $1
		GROUP BY name
		ORDER BY count DESC
		LIMIT 5
	`, code)
	if err == nil {
		defer referrerRows.Close()
	}
	var referrers []model.StatBreakdown
	for referrerRows != nil && referrerRows.Next() {
		var sb model.StatBreakdown
		if err := referrerRows.Scan(&sb.Name, &sb.Count); err == nil {
			// Clean up full referrer URL to domain name for cleaner charts
			if strings.HasPrefix(sb.Name, "http") {
				sb.Name = cleanReferrer(sb.Name)
			}
			referrers = append(referrers, sb)
		}
	}

	// 4. Browsers breakdown
	browserRows, err := h.db.Query(`
		SELECT COALESCE(NULLIF(browser, ''), 'Unknown') as name, COUNT(*) as count
		FROM analytics
		WHERE short_code = $1
		GROUP BY name
		ORDER BY count DESC
		LIMIT 5
	`, code)
	if err == nil {
		defer browserRows.Close()
	}
	var browsers []model.StatBreakdown
	for browserRows != nil && browserRows.Next() {
		var sb model.StatBreakdown
		if err := browserRows.Scan(&sb.Name, &sb.Count); err == nil {
			browsers = append(browsers, sb)
		}
	}

	// 5. OS breakdown
	osRows, err := h.db.Query(`
		SELECT COALESCE(NULLIF(os, ''), 'Unknown') as name, COUNT(*) as count
		FROM analytics
		WHERE short_code = $1
		GROUP BY name
		ORDER BY count DESC
		LIMIT 5
	`, code)
	if err == nil {
		defer osRows.Close()
	}
	var osList []model.StatBreakdown
	for osRows != nil && osRows.Next() {
		var sb model.StatBreakdown
		if err := osRows.Scan(&sb.Name, &sb.Count); err == nil {
			osList = append(osList, sb)
		}
	}

	// 6. Countries breakdown
	countryRows, err := h.db.Query(`
		SELECT COALESCE(NULLIF(country, ''), 'Unknown') as name, COUNT(*) as count
		FROM analytics
		WHERE short_code = $1
		GROUP BY name
		ORDER BY count DESC
		LIMIT 5
	`, code)
	if err == nil {
		defer countryRows.Close()
	}
	var countries []model.StatBreakdown
	for countryRows != nil && countryRows.Next() {
		var sb model.StatBreakdown
		if err := countryRows.Scan(&sb.Name, &sb.Count); err == nil {
			countries = append(countries, sb)
		}
	}

	c.JSON(http.StatusOK, model.URLAnalyticsResponse{
		ShortCode:      code,
		TotalClicks:    totalClicks,
		ClicksOverTime: clicksOverTime,
		Referrers:      referrers,
		Browsers:       browsers,
		OS:             osList,
		Countries:      countries,
	})
}

// Basic helper to extract hostname from referrer URL
func cleanReferrer(ref string) string {
	parts := regexp.MustCompile(`https?://([^/]+)`).FindStringSubmatch(ref)
	if len(parts) > 1 {
		return parts[1]
	}
	return ref
}

