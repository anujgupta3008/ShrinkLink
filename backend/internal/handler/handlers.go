package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"time"

	"github.com/gin-gonic/gin"
	"url-shortener/internal/middleware"
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

// ---------------------------------------------------------------------------
// POST /api/shorten
// ---------------------------------------------------------------------------

// Shorten creates a short URL from a long URL.
// If the caller is authenticated (userID in context from FirebaseAuth middleware),
// the link is created under their account with plan-based quota enforcement.
// Otherwise, daily per-IP anonymous quota applies.
func (h *Handler) Shorten(c *gin.Context) {
	var req model.ShortenRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Read user ID and plan from Gin context (set by FirebaseAuth middleware)
	var userID *string
	userPlan := model.PlanFree
	if uid, exists := middleware.GetUserID(c); exists {
		userID = &uid
		userPlan = middleware.GetUserPlan(c)
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
		if errors.Is(err, service.ErrAliasInUse) || errors.Is(err, service.ErrInvalidAlias) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if errors.Is(err, service.ErrQuotaExceeded) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "quota exceeded",
				"message": "You have reached your monthly link creation limit. Upgrade to Pro for 500 links/month.",
			})
			return
		}
		slog.Error("Failed to shorten URL", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}

	c.JSON(http.StatusCreated, resp)
}

// ---------------------------------------------------------------------------
// GET /:code — Redirect
// ---------------------------------------------------------------------------

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
	ctx := context.Background()
	if err := h.redisClient.LPush(ctx, "queue:analytics", data).Err(); err != nil {
		slog.Error("Failed to push click event to Redis queue", "err", err)
	}

	// INCR live click counter
	h.redisClient.Incr(ctx, "clicks:"+code)
}

// ---------------------------------------------------------------------------
// GET /api/analytics/:code
// ---------------------------------------------------------------------------

// GetAnalytics returns analytics for a short code.
// Level 8: Requires authentication + link ownership.
func (h *Handler) GetAnalytics(c *gin.Context) {
	code := c.Param("code")
	ctx := c.Request.Context()

	// 1. Require authentication
	userID, authenticated := middleware.GetUserID(c)
	if !authenticated {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "login required to view analytics"})
		return
	}

	// 2. Verify URL exists
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

	// 3. Verify ownership
	if url.UserID == nil || *url.UserID != userID {
		c.JSON(http.StatusForbidden, gin.H{"error": "you do not own this link. Claim it first to view analytics."})
		return
	}

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

// ---------------------------------------------------------------------------
// GET /api/urls — User's links (Level 6)
// ---------------------------------------------------------------------------

// GetAllURLs returns URLs. If the user is authenticated, returns their owned
// links. Otherwise returns an empty array (anonymous users can't list links).
func (h *Handler) GetAllURLs(c *gin.Context) {
	userID, authenticated := middleware.GetUserID(c)
	if !authenticated {
		c.JSON(http.StatusOK, []interface{}{})
		return
	}

	urls, err := h.urlService.ListUserURLs(c.Request.Context(), userID)
	if err != nil {
		slog.Error("Failed to list user URLs", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch URLs"})
		return
	}

	// Build response with short_url field
	type URLEntry struct {
		ShortCode  string     `json:"short_code"`
		ShortURL   string     `json:"short_url"`
		LongURL    string     `json:"long_url"`
		CreatedAt  time.Time  `json:"created_at"`
		ExpiresAt  *time.Time `json:"expires_at,omitempty"`
		ClickCount int        `json:"click_count"`
	}

	result := make([]URLEntry, 0, len(urls))
	for _, u := range urls {
		result = append(result, URLEntry{
			ShortCode:  u.ShortCode,
			ShortURL:   fmt.Sprintf("%s/%s", h.baseURL, u.ShortCode),
			LongURL:    u.LongURL,
			CreatedAt:  u.CreatedAt,
			ExpiresAt:  u.ExpiresAt,
			ClickCount: u.ClickCount,
		})
	}

	c.JSON(http.StatusOK, result)
}

// ---------------------------------------------------------------------------
// GET /api/me — User profile + quota (Level 5)
// ---------------------------------------------------------------------------

// GetMe returns the authenticated user's profile including plan and quota usage.
// Requires authentication (protected route).
func (h *Handler) GetMe(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}
	userPlan := middleware.GetUserPlan(c)

	profile, err := h.userService.GetProfile(c.Request.Context(), userID, userPlan)
	if err != nil {
		slog.Error("Failed to get user profile", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to fetch profile"})
		return
	}

	c.JSON(http.StatusOK, profile)
}

// ---------------------------------------------------------------------------
// POST /api/claim — Claim anonymous link (Level 6)
// ---------------------------------------------------------------------------

// ClaimURL assigns ownership of an anonymous link to the authenticated user.
// The link must currently have no owner (user_id IS NULL).
func (h *Handler) ClaimURL(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req struct {
		ShortCode string `json:"short_code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// Verify the URL exists and is unclaimed
	url, err := h.urlService.GetByCode(c.Request.Context(), req.ShortCode)
	if err != nil {
		slog.Error("Failed to fetch URL for claim", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "internal server error"})
		return
	}
	if url == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "URL not found"})
		return
	}
	if url.UserID != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "this link is already owned by a user"})
		return
	}

	userPlan := middleware.GetUserPlan(c)

	if err := h.urlService.ClaimURL(c.Request.Context(), req.ShortCode, userID, userPlan); err != nil {
		if errors.Is(err, service.ErrQuotaExceeded) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error":   "quota exceeded",
				"message": "You have reached your monthly link limit. Upgrade to Pro for more.",
			})
			return
		}
		slog.Error("Failed to claim URL", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to claim URL"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "link claimed successfully", "short_code": req.ShortCode})
}

// ---------------------------------------------------------------------------
// POST /api/upgrade — Switch plan (Level 7)
// ---------------------------------------------------------------------------

// UpgradePlan switches the authenticated user's plan.
// For now this is a direct toggle (no payment). Payment integration comes in Level 13.
func (h *Handler) UpgradePlan(c *gin.Context) {
	userID, exists := middleware.GetUserID(c)
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "not authenticated"})
		return
	}

	var req struct {
		Plan string `json:"plan" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	newPlan := model.Plan(req.Plan)
	if newPlan != model.PlanFree && newPlan != model.PlanPro {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid plan, must be 'free' or 'pro'"})
		return
	}

	if err := h.userService.UpgradePlan(c.Request.Context(), userID, newPlan); err != nil {
		slog.Error("Failed to upgrade plan", "err", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to update plan"})
		return
	}

	// Invalidate the user-sync cache so the next request picks up the new plan
	h.redisClient.Del(c.Request.Context(), "user_sync:"+c.GetString("firebaseUID"))

	c.JSON(http.StatusOK, gin.H{
		"message": "plan updated successfully",
		"plan":    newPlan,
		"limit":   model.PlanLimits[newPlan],
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
