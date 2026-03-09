package cache

import (
	"errors"
	"fmt"
	"time"
)

// CacheEntry represents a single entry in the cache.
type CacheEntry struct {
	// Value is the cached value (price as float64 or []byte for contract calls)
	Value interface{}

	// Timestamp is when the entry was cached
	Timestamp time.Time

	// SourceID is the identifier of the source that provided this value
	SourceID string
}

// CacheKey is a type alias for string that represents a cache key.
type CacheKey string

// NewPriceCacheKey creates a cache key for a price query.
// Format: "{queryID}-{sourceID}"
func NewPriceCacheKey(queryID, sourceID string) CacheKey {
	return CacheKey(fmt.Sprintf("%s-%s", queryID, sourceID))
}

// NewContractCacheKey creates a cache key for a contract call.
// Format: "{queryID}-{sourceID}-{callKey}"
func NewContractCacheKey(queryID, sourceID, callKey string) CacheKey {
	return CacheKey(fmt.Sprintf("%s-%s-%s", queryID, sourceID, callKey))
}

// Cache errors
var (
	// ErrCacheMiss indicates that the requested key was not found in the cache
	ErrCacheMiss = errors.New("cache miss")

	// ErrCacheExpired indicates that the cached entry has expired (TTL exceeded)
	ErrCacheExpired = errors.New("cache entry expired")

	// ErrCacheStale indicates that the cached entry is stale (staleness threshold exceeded)
	// but the value is still available in the cache
	ErrCacheStale = errors.New("cache entry stale")
)
