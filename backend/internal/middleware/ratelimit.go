package middleware

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"url-shortener/internal/redis"
)

// RateLimiter returns a Gin middleware that limits requests per IP address using Redis
func RateLimiter(rdb *redis.Client, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ip := c.ClientIP()
		ctx := context.Background()

		// Key Format: rate:127.0.0.1:2026-06-28-17-43
		bucket := time.Now().Format("2006-01-02-15-04")
		key := "rate:" + ip + ":" + bucket

		count, err := rdb.Incr(ctx, key).Result()
		if err != nil {
			// Fail-open in case Redis is unavailable to preserve availability
			c.Next()
			return
		}

		// Set TTL if this is the first request in the bucket
		if count == 1 {
			rdb.Expire(ctx, key, window+time.Second*10)
		}

		if count > int64(limit) {
			c.JSON(http.StatusTooManyRequests, gin.H{
				"error": "Rate limit exceeded. Please try again later.",
			})
			c.Abort()
			return
		}

		c.Next()
	}
}
