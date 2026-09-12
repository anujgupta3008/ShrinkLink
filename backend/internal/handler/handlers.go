package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"url-shortener/internal/model"
	"url-shortener/internal/redis"
	"url-shortener/internal/service"
)

type Handler struct {
	urlService       *service.URLService
	analyticsService *service.AnalyticsService
	userService      *service.UserService
	redisClient      *redis.Client
	baseURL          string
}

func NewHandler(
	urlService *service.URLService,
	analyticsService *service.AnalyticsService,
	userService *service.UserService,
	rdb *redis.Client,
	baseURL string,
) *Handler {
	return &Handler{
		urlService:       urlService,
		analyticsService: analyticsService,
		userService:      userService,
		redisClient:      rdb,
		baseURL:          baseURL,
	}
}

// Shorten creates a short URL from a long URL.
func (h *Handler) Shorten(c *gin.Context) {
	var req model.ShortenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Read user ID and plan from Gin context (set by auth middleware in Level 5)
	// For now (Level 3), it's always anonymous.
	var userID *string
	userPlan := model.PlanFree
	if uid, exists := c.Get("userID"); exists {
		idStr := uid.(string)
		userID = &idStr
	}
	if plan, exists := c.Get("userPlan"); exists {
		userPlan = plan.(model.Plan)
	}

	ip := c.ClientIP()

	resp, err := h.urlService.Shorten(c.Request.Context(), service.ShortenParams{
		LongURL:   req.LongURL,
		Alias:     req.Alias,
		ExpiresIn: req.ExpiresIn,
		UserID:    userID,
		UserPlan:  userPlan,
		IPAddress: ip,
	})

	if err != nil {
		if errors.Is(err, service.ErrAliasInUse) || errors.Is(err, service.ErrInvalidAlias) || errors.Is(err, service.ErrQuotaExceeded) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		slog.Error("Failed to shorten URL", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// Redirect handles redirecting from a short code to the long URL.
func (h *Handler) Redirect(c *gin.Context) {
	code := c.Param("code")
	ctx := c.Request.Context()

	longURL, err := h.urlService.Resolve(ctx, code)
	if err != nil {
		if errors.Is(err, service.ErrURLNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "URL not found"})
			return
		}
		if errors.Is(err, service.ErrURLExpired) {
			c.JSON(http.StatusGone, gin.H{"error": "URL has expired"})
			return
		}
		slog.Error("Failed to resolve URL", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	// Queue Analytics Click Event asynchronously
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
		slog.Error("Failed to marshal click event", "err", err)
		return
	}

	// LPUSH to analytics queue
	if err := h.redisClient.LPush(context.Background(), "queue:analytics", data).Err(); err != nil {
		slog.Error("Failed to push click event to Redis queue", "err", err)
	}
}

// GetAnalytics returns analytics for a short code.
func (h *Handler) GetAnalytics(c *gin.Context) {
	code := c.Param("code")
	ctx := c.Request.Context()

	// Verify URL exists and check ownership (if authenticated)
	url, err := h.urlService.GetByCode(ctx, code)
	if err != nil {
		slog.Error("Failed to fetch URL for analytics", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "database error"})
		return
	}
	if url == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "URL not found"})
		return
	}

	// TODO (Level 8): Implement analytics gating based on user plan / ownership here.

	analytics, err := h.analyticsService.GetByCode(ctx, code)
	if err != nil {
		slog.Error("Failed to fetch analytics", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query analytics"})
		return
	}

	// Clean referrers
	for i, ref := range analytics.Referrers {
		if len(ref.Name) > 4 && ref.Name[:4] == "http" {
			analytics.Referrers[i].Name = cleanReferrer(ref.Name)
		}
	}

	// Add live click count from Redis (unflushed clicks)
	liveCount, _ := h.analyticsService.GetLiveCount(ctx, code)
	analytics.TotalClicks += int(liveCount)

	c.JSON(http.StatusOK, analytics)
}

// GetAllURLs returns all shortened URLs from the database.
// Temporarily kept for backward compatibility (Level 11 will change this to User's URLs).
func (h *Handler) GetAllURLs(c *gin.Context) {
	// For now we just return an empty array until auth is fully integrated.
	c.JSON(http.StatusOK, []interface{}{})
}

// Basic helper to extract hostname from referrer URL
func cleanReferrer(ref string) string {
	parts := regexp.MustCompile(`https?://([^/]+)`).FindStringSubmatch(ref)
	if len(parts) > 1 {
		return parts[1]
	}
	return ref
}
