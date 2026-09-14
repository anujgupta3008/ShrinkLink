package model

import (
	"time"
)

// Plan represents a user's subscription tier.
type Plan string

const (
	PlanFree Plan = "free"
	PlanPro  Plan = "pro"
)

// PlanLimits maps each plan to its monthly link creation limit.
var PlanLimits = map[Plan]int{
	PlanFree: 20,
	PlanPro:  500,
}

// AnonymousLimitPerDay is the max links an anonymous (unauthenticated) IP can create per day.
const AnonymousLimitPerDay = 5

// User represents a registered user in the system.
// Created/updated on first authenticated request via Firebase ID token.
type User struct {
	ID          string    `json:"id" db:"id"`                     // UUID primary key
	FirebaseUID string    `json:"firebase_uid" db:"firebase_uid"` // Firebase UID (immutable)
	Email       string    `json:"email" db:"email"`
	Plan        Plan      `json:"plan" db:"plan"` // "free" | "pro"
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
}

// URL represents a shortened link.
type URL struct {
	ID         int64      `json:"id" db:"id"`
	ShortCode  string     `json:"short_code" db:"short_code"`
	LongURL    string     `json:"long_url" db:"long_url"`
	IsCustom   bool       `json:"is_custom" db:"is_custom"`
	CreatedAt  time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty" db:"expires_at"`
	UserID     *string    `json:"user_id,omitempty" db:"user_id"`   // NULL = anonymous
	ClickCount int        `json:"click_count" db:"click_count"`     // Denormalised counter
}

type ClickEvent struct {
	ShortCode string    `json:"short_code"`
	ClickTime time.Time `json:"click_time"`
	IPAddress string    `json:"ip_address"`
	UserAgent string    `json:"user_agent"`
	Referrer  string    `json:"referrer"`
}

type AnalyticsRecord struct {
	ID        int64     `json:"id" db:"id"`
	ShortCode string    `json:"short_code" db:"short_code"`
	ClickTime time.Time `json:"click_time" db:"click_time"`
	IPAddress string    `json:"ip_address" db:"ip_address"`
	UserAgent string    `json:"user_agent" db:"user_agent"`
	Referrer  string    `json:"referrer" db:"referrer"`
	Country   string    `json:"country" db:"country"`
	Browser   string    `json:"browser" db:"browser"`
	OS        string    `json:"os" db:"os"`
}

type ShortenRequest struct {
	LongURL string `json:"long_url" binding:"required,url"`
	// Alias binding uses only max=10 here. Character validation (a-zA-Z0-9_-)
	// is done by aliasRegex in handler/handlers.go, since the `alphanum` tag
	// incorrectly rejects valid characters like dashes and underscores.
	Alias     string `json:"alias,omitempty" binding:"omitempty,max=10"`
	ExpiresIn int    `json:"expires_in,omitempty"` // Expiration in seconds
}

type ShortenResponse struct {
	ShortURL  string     `json:"short_url"`
	ShortCode string     `json:"short_code"`
	LongURL   string     `json:"long_url"`
	ExpiresAt *time.Time `json:"expires_at,omitempty"`
}

type ClickStats struct {
	Period string `json:"period"`
	Clicks int    `json:"clicks"`
}

type StatBreakdown struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type URLAnalyticsResponse struct {
	ShortCode      string          `json:"short_code"`
	TotalClicks    int             `json:"total_clicks"`
	ClicksOverTime []ClickStats    `json:"clicks_over_time"`
	Referrers      []StatBreakdown `json:"referrers"`
	Browsers       []StatBreakdown `json:"browsers"`
	OS             []StatBreakdown `json:"os"`
	Countries      []StatBreakdown `json:"countries"`
}

// MeResponse is returned by GET /api/me.
type MeResponse struct {
	UserID string    `json:"user_id"`
	Email  string    `json:"email"`
	Plan   Plan      `json:"plan"`
	Usage  UsageInfo `json:"usage"`
}

// UsageInfo shows the caller's current-month link creation usage.
type UsageInfo struct {
	Used  int `json:"used"`
	Limit int `json:"limit"`
}
