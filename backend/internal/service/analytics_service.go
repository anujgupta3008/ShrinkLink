package service

import (
	"context"
	"fmt"

	goredis "github.com/redis/go-redis/v9"
	"url-shortener/internal/model"
	"url-shortener/internal/redis"
	"url-shortener/internal/repository"
)

// AnalyticsService handles analytics queries and live click counters.
type AnalyticsService struct {
	analyticsRepo repository.AnalyticsRepository
	rdb           *redis.Client
}

func NewAnalyticsService(analyticsRepo repository.AnalyticsRepository, rdb *redis.Client) *AnalyticsService {
	return &AnalyticsService{analyticsRepo: analyticsRepo, rdb: rdb}
}

// GetByCode fetches full analytics for a short code (6 concurrent DB queries).
func (s *AnalyticsService) GetByCode(ctx context.Context, code string) (*model.URLAnalyticsResponse, error) {
	result, err := s.analyticsRepo.GetByCode(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("analytics_service.GetByCode: %w", err)
	}
	return result, nil
}

// GetLiveCount returns the real-time click counter from Redis.
// The worker increments `clicks:{code}` on every redirect.
// Returns 0 gracefully if the key doesn't exist yet.
func (s *AnalyticsService) GetLiveCount(ctx context.Context, code string) (int64, error) {
	count, err := s.rdb.Get(ctx, "clicks:"+code).Int64()
	if err == goredis.Nil {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("analytics_service.GetLiveCount: %w", err)
	}
	return count, nil
}

// GetUniqueVisitors returns the HyperLogLog estimate of unique visitor IPs
// for a given short code. Uses Redis PFCOUNT (Level 9).
func (s *AnalyticsService) GetUniqueVisitors(ctx context.Context, code string) (int64, error) {
	count, err := s.rdb.PFCount(ctx, "uv:"+code).Result()
	if err != nil {
		return 0, fmt.Errorf("analytics_service.GetUniqueVisitors: %w", err)
	}
	return count, nil
}
