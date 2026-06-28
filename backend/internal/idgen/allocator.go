package idgen

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"url-shortener/internal/redis"
)

type Allocator struct {
	redisClient *redis.Client
	rangeSize   int64
	currentID   int64
	maxID       int64
	mu          sync.Mutex
}

func NewAllocator(rdb *redis.Client, rangeSize int64) *Allocator {
	return &Allocator{
		redisClient: rdb,
		rangeSize:   rangeSize,
	}
}

// NextID returns a unique 64-bit integer ID.
func (a *Allocator) NextID(ctx context.Context) (int64, error) {
	for {
		curr := atomic.LoadInt64(&a.currentID)
		max := atomic.LoadInt64(&a.maxID)

		if curr < max {
			next := curr + 1
			if atomic.CompareAndSwapInt64(&a.currentID, curr, next) {
				return next, nil
			}
			// Retry on collision
			continue
		}

		// Range exhausted, fetch next block
		if err := a.fetchNextBlock(ctx); err != nil {
			return 0, err
		}
	}
}

func (a *Allocator) fetchNextBlock(ctx context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	// Double-check under lock
	curr := atomic.LoadInt64(&a.currentID)
	max := atomic.LoadInt64(&a.maxID)
	if curr < max {
		return nil
	}

	newMax, err := a.redisClient.IncrBy(ctx, "global_url_id_counter", a.rangeSize).Result()
	if err != nil {
		return errors.New("failed to fetch next ID range from Redis: " + err.Error())
	}

	atomic.StoreInt64(&a.currentID, newMax-a.rangeSize)
	atomic.StoreInt64(&a.maxID, newMax)

	return nil
}
