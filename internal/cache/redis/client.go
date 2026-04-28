package redis

import (
	"errors"
	"strings"

	goredis "github.com/redis/go-redis/v9"
)

// NewClient returns a Redis client. When redisURL is non-empty (e.g. REDIS_URL from Vercel / Upstash),
// it is parsed with TLS support for rediss://; otherwise addr is used as host:port.
func NewClient(addr, redisURL string) (goredis.UniversalClient, error) {
	u := strings.TrimSpace(redisURL)
	if u != "" {
		opts, err := goredis.ParseURL(u)
		if err != nil {
			return nil, err
		}
		return goredis.NewClient(opts), nil
	}
	a := strings.TrimSpace(addr)
	if a == "" {
		return nil, errors.New("redis: REDIS_URL or REDIS_ADDR is required when CACHE_BACKEND=redis")
	}
	return goredis.NewClient(&goredis.Options{Addr: a}), nil
}
