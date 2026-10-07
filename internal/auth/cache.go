package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"sync"
	"time"
)

type cacheEntry struct {
	identity  Identity
	expiresAt time.Time
}

type IdentityCache struct {
	mu    sync.RWMutex
	items map[string]cacheEntry
	ttl   time.Duration
}

func NewIdentityCache(ttl time.Duration) *IdentityCache {
	return &IdentityCache{
		items: make(map[string]cacheEntry),
		ttl:   ttl,
	}
}

func (c *IdentityCache) Get(key string) (Identity, bool) {
	c.mu.RLock()

	entry, ok := c.items[key]

	c.mu.RUnlock()

	if !ok {
		return Identity{}, false
	}

	if time.Now().After(entry.expiresAt) {
		c.mu.Lock()
		delete(c.items, key)
		c.mu.Unlock()

		return Identity{}, false
	}

	return entry.identity, true
}

func (c *IdentityCache) Set(
	key string,
	identity Identity,
) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.items[key] = cacheEntry{
		identity:  identity,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func buildCacheKey(input ResolveInput) string {
	sum := sha256.Sum256([]byte(input.Token))

	tokenHash := hex.EncodeToString(sum[:])

	return input.UserID +
		":" +
		input.AppID +
		":" +
		tokenHash +
		":" +
		input.FromSupportMenu
}
