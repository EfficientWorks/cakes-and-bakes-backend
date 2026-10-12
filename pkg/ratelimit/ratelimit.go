// Package ratelimit provides a small fixed-window limiter backed by Redis,
// used for login attempts and OTP resend throttling. A nil *Limiter is a
// valid no-op (every call to Allow returns true) so local development and
// Postman testing work without Redis running.
package ratelimit

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

type Limiter struct {
	client *redis.Client
}

// New parses redisURL and returns a Limiter, or nil (no-op) if redisURL is
// empty or unreachable — rate limiting degrades gracefully rather than
// blocking local dev/testing on a Redis dependency.
func New(redisURL string) *Limiter {
	if redisURL == "" {
		return nil
	}
	opt, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil
	}
	client := redis.NewClient(opt)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil
	}
	return &Limiter{client: client}
}

// Allow reports whether one more action under key is permitted within the
// fixed window, incrementing the counter as a side effect.
func (l *Limiter) Allow(ctx context.Context, key string, limit int, window time.Duration) (bool, error) {
	if l == nil {
		return true, nil
	}
	count, err := l.client.Incr(ctx, key).Result()
	if err != nil {
		return true, fmt.Errorf("ratelimit incr: %w", err)
	}
	if count == 1 {
		l.client.Expire(ctx, key, window)
	}
	return count <= int64(limit), nil
}
