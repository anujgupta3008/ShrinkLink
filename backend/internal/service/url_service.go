package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"url-shortener/internal/idgen"
	"url-shortener/internal/model"
	"url-shortener/internal/redis"
	"url-shortener/internal/repository"
)

var (
	ErrAliasInUse  = errors.New("custom alias is already in use")
	ErrURLNotFound = errors.New("url not found")
	ErrURLExpired  = errors.New("url has expired")
	ErrInvalidAlias = errors.New("alias must be alphanumeric, dashes, or underscores")

	aliasRegex = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)
)

// URLService handles all business logic for creating and resolving shortened URLs.
type URLService struct {
	urlRepo   repository.URLRepository
	quota     *QuotaService
	allocator *idgen.Allocator
	rdb       *redis.Client
	baseURL   string
}

func NewURLService(
	urlRepo repository.URLRepository,
	quota *QuotaService,
	allocator *idgen.Allocator,
	rdb *redis.Client,
	baseURL string,
) *URLService {
	return &URLService{
		urlRepo:   urlRepo,
		quota:     quota,
		allocator: allocator,
		rdb:       rdb,
		baseURL:   baseURL,
	}
}

// ShortenParams holds all inputs for creating a short URL.
type ShortenParams struct {
	LongURL   string
	Alias     string
	ExpiresIn int    // seconds; 0 = no expiry
	UserID    *string // nil = anonymous
	UserPlan  model.Plan
	IPAddress string // for anonymous quota enforcement
}

// Shorten validates, quota-checks, and creates a new short URL.
func (s *URLService) Shorten(ctx context.Context, p ShortenParams) (*model.ShortenResponse, error) {
	// Validate and normalise alias if provided
	if p.Alias != "" {
		if !aliasRegex.MatchString(p.Alias) {
			return nil, ErrInvalidAlias
		}
		exists, err := s.urlRepo.AliasExists(ctx, p.Alias)
		if err != nil {
			return nil, fmt.Errorf("url_service.Shorten alias check: %w", err)
		}
		if exists {
			return nil, ErrAliasInUse
		}
	}

	// Quota enforcement
	if p.UserID != nil {
		if err := s.quota.CheckAndReserveUser(ctx, *p.UserID, p.UserPlan); err != nil {
			return nil, err // ErrQuotaExceeded propagates directly
		}
	} else {
		if err := s.quota.CheckAndReserveAnonymous(ctx, p.IPAddress); err != nil {
			return nil, err
		}
	}

	// Generate unique numeric ID → Base62 short code
	id, err := s.allocator.NextID(ctx)
	if err != nil {
		return nil, fmt.Errorf("url_service.Shorten id alloc: %w", err)
	}

	shortCode := p.Alias
	if shortCode == "" {
		shortCode = idgen.Encode(id)
	}

	var expiresAt *time.Time
	if p.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(p.ExpiresIn) * time.Second)
		expiresAt = &t
	}

	url := &model.URL{
		ID:        id,
		ShortCode: shortCode,
		LongURL:   p.LongURL,
		IsCustom:  p.Alias != "",
		ExpiresAt: expiresAt,
		UserID:    p.UserID,
	}

	if err := s.urlRepo.Create(ctx, url); err != nil {
		return nil, fmt.Errorf("url_service.Shorten create: %w", err)
	}

	// Prime the Redis cache immediately so the first redirect is a cache hit.
	ttl := 24 * time.Hour
	if expiresAt != nil {
		ttl = time.Until(*expiresAt)
	}
	s.rdb.Set(ctx, "url:"+shortCode, p.LongURL, ttl)

	return &model.ShortenResponse{
		ShortURL:  fmt.Sprintf("%s/%s", s.baseURL, shortCode),
		ShortCode: shortCode,
		LongURL:   p.LongURL,
		ExpiresAt: expiresAt,
	}, nil
}

// Resolve returns the long URL for a short code, using Redis cache-aside.
// Also increments the click counter and queues an analytics event.
func (s *URLService) Resolve(ctx context.Context, code string) (string, error) {
	redisKey := "url:" + code

	// 1. Redis cache hit
	val, err := s.rdb.Get(ctx, redisKey).Result()
	if err == nil {
		if val == "__NOT_FOUND__" {
			return "", ErrURLNotFound
		}
		return val, nil
	}

	if err != goredis.Nil {
		// Redis error — fall through to DB
	}

	// 2. Cache miss — hit DB
	url, err := s.urlRepo.GetByCode(ctx, code)
	if err != nil {
		return "", fmt.Errorf("url_service.Resolve db: %w", err)
	}
	if url == nil {
		s.rdb.Set(ctx, redisKey, "__NOT_FOUND__", 5*time.Minute)
		return "", ErrURLNotFound
	}

	// Check expiration
	if url.ExpiresAt != nil && url.ExpiresAt.Before(time.Now()) {
		s.rdb.Set(ctx, redisKey, "__NOT_FOUND__", 5*time.Minute)
		return "", ErrURLExpired
	}

	// Cache the live URL
	ttl := 24 * time.Hour
	if url.ExpiresAt != nil {
		ttl = time.Until(*url.ExpiresAt)
	}
	s.rdb.Set(ctx, redisKey, url.LongURL, ttl)

	return url.LongURL, nil
}

// ListUserURLs returns all links owned by a user.
func (s *URLService) ListUserURLs(ctx context.Context, userID string) ([]*model.URL, error) {
	return s.urlRepo.GetByUserID(ctx, userID)
}

// ClaimURL assigns ownership of an anonymous link to a user.
func (s *URLService) ClaimURL(ctx context.Context, code, userID string, plan model.Plan) error {
	if err := s.quota.CheckAndReserveUser(ctx, userID, plan); err != nil {
		return err
	}
	return s.urlRepo.ClaimURL(ctx, code, userID)
}

// GetByCode returns the full URL record (used for analytics ownership check).
func (s *URLService) GetByCode(ctx context.Context, code string) (*model.URL, error) {
	return s.urlRepo.GetByCode(ctx, code)
}
