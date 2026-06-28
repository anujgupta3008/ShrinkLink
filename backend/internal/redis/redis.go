package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
	"url-shortener/internal/config"
)

type Client struct {
	*redis.Client
}

func NewClient(cfg *config.Config) (*Client, error) {
	dbNum, err := strconv.Atoi(cfg.RedisDB)
	if err != nil {
		dbNum = 0
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:     fmt.Sprintf("%s:%s", cfg.RedisHost, cfg.RedisPort),
		Password: cfg.RedisPassword,
		DB:       dbNum,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis: %w", err)
	}

	return &Client{rdb}, nil
}
