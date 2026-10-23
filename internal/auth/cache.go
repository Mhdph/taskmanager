package auth

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"time"
)

type cacheEntry struct {
	identity  Identity
	expiresAt time.Time
}

const maxIdentityCacheEntries = 4096

type IdentityCache struct {
	mu    sync.Mutex
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
	c.mu.Lock()
	defer c.mu.Unlock()

	entry, ok := c.items[key]

	if !ok {
		return Identity{}, false
	}

	if time.Now().After(entry.expiresAt) {
		delete(c.items, key)

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
	if len(c.items) >= maxIdentityCacheEntries {
		now := time.Now()
		for k, entry := range c.items {
			if now.After(entry.expiresAt) {
				delete(c.items, k)
			}
		}
		// Eviction only forces a fresh hosted lookup; it never grants access.
		if len(c.items) >= maxIdentityCacheEntries {
			for k := range c.items {
				delete(c.items, k)
				break
			}
		}
	}

	c.items[key] = cacheEntry{
		identity:  identity,
		expiresAt: time.Now().Add(c.ttl),
	}
}

func buildCacheKey(input ResolveInput) string {
	// Encode field boundaries so IDs containing colons cannot share a cache entry.
	encoded, _ := json.Marshal(input)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
