package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"url-shortener/internal/model"
	"url-shortener/internal/redis"
)

// ErrQuotaExceeded is returned when a user or anonymous IP has hit their link limit.
var ErrQuotaExceeded = errors.New("quota exceeded")

// QuotaService enforces monthly (for users) and daily (for anonymous IPs) link creation limits.
// All checks are O(1) Redis counter operations — never a Postgres query on the hot path.
type QuotaService struct {
	rdb *redis.Client
}

func NewQuotaService(rdb *redis.Client) *QuotaService {
	return &QuotaService{rdb: rdb}
}

// CheckAndReserveUser atomically checks + increments the caller's monthly usage counter.
// Key format: quota:{userID}:{YYYY-MM}  TTL: ~35 days (rolls over naturally).
func (s *QuotaService) CheckAndReserveUser(ctx context.Context, userID string, plan model.Plan) error {
	limit := model.PlanLimits[plan]
	key := fmt.Sprintf("quota:%s:%s", userID, time.Now().Format("2006-01"))

	count, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("quota check (user): %w", err)
	}

	// Set TTL only on first use in the month (count == 1).
	if count == 1 {
		s.rdb.Expire(ctx, key, 35*24*time.Hour)
	}

	if int(count) > limit {
		// Roll back the increment so we don't over-count.
		s.rdb.Decr(ctx, key)
		return ErrQuotaExceeded
	}
	return nil
}

// CheckAndReserveAnonymous enforces a daily per-IP cap for unauthenticated shortening.
// Key format: anon_quota:{ip}:{YYYY-MM-DD}  TTL: 25 hours.
func (s *QuotaService) CheckAndReserveAnonymous(ctx context.Context, ip string) error {
	key := fmt.Sprintf("anon_quota:%s:%s", ip, time.Now().Format("2006-01-02"))

	count, err := s.rdb.Incr(ctx, key).Result()
	if err != nil {
		return fmt.Errorf("quota check (anon): %w", err)
	}
	if count == 1 {
		s.rdb.Expire(ctx, key, 25*time.Hour)
	}

	if int(count) > model.AnonymousLimitPerDay {
		s.rdb.Decr(ctx, key)
		return ErrQuotaExceeded
	}
	return nil
}

// GetUsage returns how many links the user has created this month and their plan limit.
func (s *QuotaService) GetUsage(ctx context.Context, userID string, plan model.Plan) (used, limit int, err error) {
	key := fmt.Sprintf("quota:%s:%s", userID, time.Now().Format("2006-01"))
	val, e := s.rdb.Get(ctx, key).Int()
	if e != nil && e != goredis.Nil {
		return 0, 0, fmt.Errorf("get usage: %w", e)
	}
	return val, model.PlanLimits[plan], nil
}
