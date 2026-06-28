package model

import (
	"time"
)

type URL struct {
	ID        int64      `json:"id" db:"id"`
	ShortCode string     `json:"short_code" db:"short_code"`
	LongURL   string     `json:"long_url" db:"long_url"`
	IsCustom  bool       `json:"is_custom" db:"is_custom"`
	CreatedAt time.Time  `json:"created_at" db:"created_at"`
	ExpiresAt *time.Time `json:"expires_at,omitempty" db:"expires_at"`
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
}

type ShortenRequest struct {
	LongURL   string `json:"long_url" binding:"required,url"`
	Alias     string `json:"alias,omitempty" binding:"omitempty,alphanum,max=10"`
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
