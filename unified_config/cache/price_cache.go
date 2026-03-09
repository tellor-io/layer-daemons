package cache

import (
	"sync"
	"time"
)

// PriceCache is a thread-safe cache for price data with TTL and staleness checking.
type PriceCache struct {
	mu                       sync.RWMutex
	entries                  map[CacheKey]CacheEntry
	globalStalenessThreshold time.Duration
	sourceTTLs               map[string]time.Duration
}

// NewPriceCache creates a new PriceCache with the given staleness threshold and per-source TTLs.
// If a source is not in sourceTTLs, it will not have a TTL (won't expire).
// globalStalenessThreshold is used to determine if a cached value is stale (but still available).
func NewPriceCache(globalStalenessThreshold time.Duration, sourceTTLs map[string]time.Duration) *PriceCache {
	ttlMap := make(map[string]time.Duration)
	if sourceTTLs != nil {
		for k, v := range sourceTTLs {
			ttlMap[k] = v
		}
	}
	return &PriceCache{
		entries:                  make(map[CacheKey]CacheEntry),
		globalStalenessThreshold: globalStalenessThreshold,
		sourceTTLs:               ttlMap,
	}
}

// Get retrieves a value from the cache.
// Returns:
//   - The value and nil if the entry exists and is fresh
//   - The value and ErrCacheStale if the entry exists but is stale (exceeded staleness threshold)
//   - nil and ErrCacheExpired if the entry exists but has expired (TTL exceeded)
//   - nil and ErrCacheMiss if the entry does not exist
func (c *PriceCache) Get(key CacheKey) (interface{}, error) {
	c.mu.RLock()
	entry, exists := c.entries[key]
	c.mu.RUnlock()

	if !exists {
		return nil, ErrCacheMiss
	}

	now := time.Now()
	age := now.Sub(entry.Timestamp)

	// Check if expired (TTL exceeded)
	if ttl, hasTTL := c.sourceTTLs[entry.SourceID]; hasTTL {
		if age > ttl {
			return nil, ErrCacheExpired
		}
	}

	// Check if stale (staleness threshold exceeded)
	if age > c.globalStalenessThreshold {
		return entry.Value, ErrCacheStale
	}

	return entry.Value, nil
}

// Set stores a value in the cache with the current timestamp.
func (c *PriceCache) Set(key CacheKey, value interface{}, sourceID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries[key] = CacheEntry{
		Value:     value,
		Timestamp: time.Now(),
		SourceID:  sourceID,
	}

	return nil
}

// Invalidate removes a specific entry from the cache.
func (c *PriceCache) Invalidate(key CacheKey) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.entries, key)
	return nil
}

// Clear removes all entries from the cache.
func (c *PriceCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[CacheKey]CacheEntry)
	return nil
}
