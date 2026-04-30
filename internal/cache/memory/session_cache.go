package memory

import (
	"context"
	"sync"
	"time"
)

type sessionEntry struct {
	value     string
	expiresAt time.Time
}

type SessionCache struct {
	mu            sync.RWMutex
	refreshTokens map[string]sessionEntry
	blacklist     map[string]time.Time
	now           func() time.Time
}

func NewSessionCache() *SessionCache {
	return &SessionCache{
		refreshTokens: make(map[string]sessionEntry),
		blacklist:     make(map[string]time.Time),
		now:           time.Now,
	}
}

func (c *SessionCache) SetRefreshToken(ctx context.Context, userID, token string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refreshTokens[userID] = sessionEntry{value: token, expiresAt: c.now().Add(ttl)}
	return nil
}

func (c *SessionCache) GetRefreshToken(ctx context.Context, userID string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.refreshTokens[userID]
	if !ok {
		return "", nil
	}
	if !entry.expiresAt.IsZero() && !entry.expiresAt.After(c.now()) {
		delete(c.refreshTokens, userID)
		return "", nil
	}
	return entry.value, nil
}

func (c *SessionCache) DeleteRefreshToken(ctx context.Context, userID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.refreshTokens, userID)
	return nil
}

func (c *SessionCache) BlacklistToken(ctx context.Context, jti string, ttl time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.blacklist[jti] = c.now().Add(ttl)
	return nil
}

func (c *SessionCache) IsBlacklisted(ctx context.Context, jti string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	expiresAt, ok := c.blacklist[jti]
	if !ok {
		return false, nil
	}
	if !expiresAt.IsZero() && !expiresAt.After(c.now()) {
		delete(c.blacklist, jti)
		return false, nil
	}
	return true, nil
}
