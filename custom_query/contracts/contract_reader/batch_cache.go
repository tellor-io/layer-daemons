package reader

import (
	"errors"
	"sync"
	"time"
)

// ErrBatchCacheMiss indicates that the requested callID was not found in the cache
var ErrBatchCacheMiss = errors.New("batch cache miss")

// BatchCache is a thread-safe cache for raw contract call results.
// It stores []byte results keyed by CallID with timestamps.
type BatchCache struct {
	mu         sync.RWMutex
	results    map[string][]byte
	timestamps map[string]time.Time
}

// NewBatchCache creates a new BatchCache.
func NewBatchCache() *BatchCache {
	return &BatchCache{
		results:    make(map[string][]byte),
		timestamps: make(map[string]time.Time),
	}
}

// Set stores a result in the cache with the current timestamp.
// If the callID already exists, it will be overwritten.
func (c *BatchCache) Set(callID string, result []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Make a copy of the result to avoid external modifications
	resultCopy := make([]byte, len(result))
	copy(resultCopy, result)

	c.results[callID] = resultCopy
	c.timestamps[callID] = time.Now()

	return nil
}

// Get retrieves a result from the cache.
// Returns:
//   - The result and nil if the callID exists
//   - nil and ErrBatchCacheMiss if the callID does not exist
func (c *BatchCache) Get(callID string) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	result, exists := c.results[callID]
	if !exists {
		return nil, ErrBatchCacheMiss
	}

	// Return a copy to avoid external modifications
	resultCopy := make([]byte, len(result))
	copy(resultCopy, result)

	return resultCopy, nil
}

// Clear removes all entries from the cache.
func (c *BatchCache) Clear() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.results = make(map[string][]byte)
	c.timestamps = make(map[string]time.Time)

	return nil
}
