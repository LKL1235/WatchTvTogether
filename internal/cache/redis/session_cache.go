package redis

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type SessionCache struct {
	client goredis.UniversalClient
}

func NewSessionCache(client goredis.UniversalClient) *SessionCache {
	return &SessionCache{client: client}
}

func (c *SessionCache) SetRefreshToken(ctx context.Context, userID, token string, ttl time.Duration) error {
	return c.client.Set(ctx, refreshTokenKey(userID), token, ttl).Err()
}

func (c *SessionCache) GetRefreshToken(ctx context.Context, userID string) (string, error) {
	token, err := c.client.Get(ctx, refreshTokenKey(userID)).Result()
	if err == goredis.Nil {
		return "", nil
	}
	return token, err
}

func (c *SessionCache) BlacklistToken(ctx context.Context, jti string, ttl time.Duration) error {
	return c.client.Set(ctx, blacklistKey(jti), "1", ttl).Err()
}

func (c *SessionCache) IsBlacklisted(ctx context.Context, jti string) (bool, error) {
	exists, err := c.client.Exists(ctx, blacklistKey(jti)).Result()
	return exists > 0, err
}

func refreshTokenKey(userID string) string {
	return "session:refresh:" + userID
}

func blacklistKey(jti string) string {
	return "session:blacklist:" + jti
}
