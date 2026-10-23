package auth

import (
	"fmt"
	"testing"
	"time"
)

func TestCacheKeyPreservesIdentityBoundaries(t *testing.T) {
	a := ResolveInput{Token: "shared-token", UserID: "alice:tenant", AppID: "one"}
	b := ResolveInput{Token: "shared-token", UserID: "alice", AppID: "tenant:one"}
	if buildCacheKey(a) == buildCacheKey(b) {
		t.Fatal("different tenants share an identity cache key")
	}
	for _, input := range []ResolveInput{
		{Token: "other-token", UserID: a.UserID, AppID: a.AppID},
		{Token: a.Token, UserID: a.UserID, AppID: a.AppID, FromSupportMenu: "true"},
	} {
		if buildCacheKey(a) == buildCacheKey(input) {
			t.Fatal("different credentials or support modes share a cache key")
		}
	}
}

func TestIdentityCacheBoundAndExpiry(t *testing.T) {
	c := NewIdentityCache(time.Minute)
	for i := 0; i < maxIdentityCacheEntries+10; i++ {
		c.Set(fmt.Sprint(i), Identity{UserID: "alice"})
	}
	if len(c.items) > maxIdentityCacheEntries {
		t.Fatal("cache grows without a bound")
	}
	c.items["expired"] = cacheEntry{identity: Identity{UserID: "old"}, expiresAt: time.Now().Add(-time.Second)}
	if _, ok := c.Get("expired"); ok {
		t.Fatal("expired identity was reused")
	}
	c.Set("fresh", Identity{UserID: "new"})
	if id, ok := c.Get("fresh"); !ok || id.UserID != "new" {
		t.Fatal("fresh identity missing")
	}
}
