package redis

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/SV1Stail/online_market/internal/observability"
)

func Open(ctx context.Context, redisURL string, logger *slog.Logger) (*goredis.Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if strings.TrimSpace(redisURL) == "" {
		return nil, fmt.Errorf("Redis URL is required")
	}
	options, err := goredis.ParseURL(redisURL)
	if err != nil {
		// Parse errors may contain the URL with credentials, so do not log or wrap them.
		return nil, fmt.Errorf("parse Redis configuration")
	}
	options.DialTimeout = 5 * time.Second
	options.ReadTimeout = 5 * time.Second
	options.WriteTimeout = 5 * time.Second
	options.MaxRetries = 1

	client := goredis.NewClient(options)
	if err := client.Ping(ctx).Err(); err != nil {
		observability.LogError(ctx, logger, "redis.ping", err)
		_ = client.Close()
		return nil, fmt.Errorf("ping Redis: %w", err)
	}
	return client, nil
}
