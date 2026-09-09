package idgen

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"url-shortener/internal/redis"
)

// minIDOffset ensures auto-generated IDs never produce very short codes.
// Base62(100000) = "q0U" (3 chars). IDs above this produce codes of 4+ characters.
// This prevents codes like "1", "2", or "z" which look like accidents.
const minIDOffset int64 = 100_000

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

	// If this is the very first allocation (counter started below the minimum),
	// jump the counter up to minIDOffset so all generated codes are at least
	// 4 characters long. SET is only called once in the lifetime of the cluster.
	if newMax < minIDOffset {
		if err := a.redisClient.Set(ctx, "global_url_id_counter", minIDOffset, 0).Err(); err != nil {
			return errors.New("failed to set minimum ID offset in Redis: " + err.Error())
		}
		newMax = minIDOffset
	}

	atomic.StoreInt64(&a.currentID, newMax-a.rangeSize)
	atomic.StoreInt64(&a.maxID, newMax)

	return nil
}
