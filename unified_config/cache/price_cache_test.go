package cache

import (
	"sync"
	"testing"
	"time"
)

func TestPriceCache_GetOnEmptyCache(t *testing.T) {
	cache := NewPriceCache(5*time.Minute, nil)
	key := NewPriceCacheKey("query1", "source1")

	_, err := cache.Get(key)
	if err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss, got %v", err)
	}
}

func TestPriceCache_SetThenGet(t *testing.T) {
	cache := NewPriceCache(5*time.Minute, nil)
	key := NewPriceCacheKey("query1", "source1")
	value := 123.45

	err := cache.Set(key, value, "source1")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	result, err := cache.Get(key)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if result != value {
		t.Errorf("expected %v, got %v", value, result)
	}
}

func TestPriceCache_GetAfterTTLExpiration(t *testing.T) {
	// Use a very short TTL for testing
	sourceTTLs := map[string]time.Duration{
		"source1": 100 * time.Millisecond,
	}
	cache := NewPriceCache(5*time.Minute, sourceTTLs)
	key := NewPriceCacheKey("query1", "source1")
	value := 123.45

	err := cache.Set(key, value, "source1")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Wait for TTL to expire
	time.Sleep(150 * time.Millisecond)

	_, err = cache.Get(key)
	if err != ErrCacheExpired {
		t.Errorf("expected ErrCacheExpired, got %v", err)
	}
}

func TestPriceCache_GetAfterStalenessThreshold(t *testing.T) {
	// Use a short staleness threshold but longer TTL
	stalenessThreshold := 100 * time.Millisecond
	sourceTTLs := map[string]time.Duration{
		"source1": 5 * time.Minute,
	}
	cache := NewPriceCache(stalenessThreshold, sourceTTLs)
	key := NewPriceCacheKey("query1", "source1")
	value := 123.45

	err := cache.Set(key, value, "source1")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Wait for staleness threshold to be exceeded
	time.Sleep(150 * time.Millisecond)

	// Get should return ErrCacheStale but still return the value
	result, err := cache.Get(key)
	if err != ErrCacheStale {
		t.Errorf("expected ErrCacheStale, got %v", err)
	}

	// Value should still be available
	if result != value {
		t.Errorf("expected value %v to still be available, got %v", value, result)
	}
}

func TestPriceCache_ConcurrentReads(t *testing.T) {
	cache := NewPriceCache(5*time.Minute, nil)
	key := NewPriceCacheKey("query1", "source1")
	value := 123.45

	err := cache.Set(key, value, "source1")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Launch multiple concurrent reads
	var wg sync.WaitGroup
	numReaders := 100
	wg.Add(numReaders)

	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			result, err := cache.Get(key)
			if err != nil {
				t.Errorf("Get failed: %v", err)
				return
			}
			if result != value {
				t.Errorf("expected %v, got %v", value, result)
			}
		}()
	}

	wg.Wait()
}

func TestPriceCache_ConcurrentWrites(t *testing.T) {
	cache := NewPriceCache(5*time.Minute, nil)
	key := NewPriceCacheKey("query1", "source1")

	// Launch multiple concurrent writes
	var wg sync.WaitGroup
	numWriters := 100
	wg.Add(numWriters)

	for i := 0; i < numWriters; i++ {
		go func(val float64) {
			defer wg.Done()
			err := cache.Set(key, val, "source1")
			if err != nil {
				t.Errorf("Set failed: %v", err)
			}
		}(float64(i))
	}

	wg.Wait()

	// After all writes, should be able to read a value (any of the written values)
	result, err := cache.Get(key)
	if err != nil {
		t.Fatalf("Get failed after concurrent writes: %v", err)
	}
	if result == nil {
		t.Error("expected a value after concurrent writes, got nil")
	}
}

func TestPriceCache_Invalidate(t *testing.T) {
	cache := NewPriceCache(5*time.Minute, nil)
	key := NewPriceCacheKey("query1", "source1")
	value := 123.45

	err := cache.Set(key, value, "source1")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Verify it's in cache
	_, err = cache.Get(key)
	if err != nil {
		t.Fatalf("Get failed before invalidate: %v", err)
	}

	// Invalidate
	err = cache.Invalidate(key)
	if err != nil {
		t.Fatalf("Invalidate failed: %v", err)
	}

	// Verify it's gone
	_, err = cache.Get(key)
	if err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss after invalidate, got %v", err)
	}
}

func TestPriceCache_Clear(t *testing.T) {
	cache := NewPriceCache(5*time.Minute, nil)
	key1 := NewPriceCacheKey("query1", "source1")
	key2 := NewPriceCacheKey("query2", "source2")

	err := cache.Set(key1, 123.45, "source1")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}
	err = cache.Set(key2, 678.90, "source2")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Verify both are in cache
	_, err = cache.Get(key1)
	if err != nil {
		t.Fatalf("Get key1 failed: %v", err)
	}
	_, err = cache.Get(key2)
	if err != nil {
		t.Fatalf("Get key2 failed: %v", err)
	}

	// Clear
	err = cache.Clear()
	if err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	// Verify both are gone
	_, err = cache.Get(key1)
	if err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss for key1 after clear, got %v", err)
	}
	_, err = cache.Get(key2)
	if err != ErrCacheMiss {
		t.Errorf("expected ErrCacheMiss for key2 after clear, got %v", err)
	}
}

func TestPriceCache_DifferentTTLsPerSource(t *testing.T) {
	sourceTTLs := map[string]time.Duration{
		"source1": 100 * time.Millisecond,
		"source2": 500 * time.Millisecond,
	}
	cache := NewPriceCache(5*time.Minute, sourceTTLs)

	key1 := NewPriceCacheKey("query1", "source1")
	key2 := NewPriceCacheKey("query2", "source2")

	err := cache.Set(key1, 123.45, "source1")
	if err != nil {
		t.Fatalf("Set key1 failed: %v", err)
	}
	err = cache.Set(key2, 678.90, "source2")
	if err != nil {
		t.Fatalf("Set key2 failed: %v", err)
	}

	// Wait for source1's TTL to expire but not source2's
	time.Sleep(150 * time.Millisecond)

	// source1 should be expired
	_, err = cache.Get(key1)
	if err != ErrCacheExpired {
		t.Errorf("expected ErrCacheExpired for source1, got %v", err)
	}

	// source2 should still be valid
	result, err := cache.Get(key2)
	if err != nil {
		t.Errorf("expected no error for source2, got %v", err)
	}
	if result != 678.90 {
		t.Errorf("expected 678.90 for source2, got %v", result)
	}

	// Wait for source2's TTL to expire
	time.Sleep(400 * time.Millisecond)

	// source2 should now be expired
	_, err = cache.Get(key2)
	if err != ErrCacheExpired {
		t.Errorf("expected ErrCacheExpired for source2, got %v", err)
	}
}

func TestPriceCache_DefaultTTLWhenSourceNotInMap(t *testing.T) {
	// Only provide TTL for source1, not source2
	sourceTTLs := map[string]time.Duration{
		"source1": 100 * time.Millisecond,
	}
	cache := NewPriceCache(5*time.Minute, sourceTTLs)

	key2 := NewPriceCacheKey("query2", "source2")
	err := cache.Set(key2, 678.90, "source2")
	if err != nil {
		t.Fatalf("Set key2 failed: %v", err)
	}

	// Wait longer than source1's TTL
	time.Sleep(150 * time.Millisecond)

	// source2 should still be valid (no TTL means it doesn't expire)
	result, err := cache.Get(key2)
	if err != nil {
		t.Errorf("expected no error for source2 (no TTL), got %v", err)
	}
	if result != 678.90 {
		t.Errorf("expected 678.90 for source2, got %v", result)
	}
}

func TestPriceCache_StalenessButNotExpired(t *testing.T) {
	// Staleness threshold shorter than TTL
	stalenessThreshold := 100 * time.Millisecond
	sourceTTLs := map[string]time.Duration{
		"source1": 5 * time.Minute,
	}
	cache := NewPriceCache(stalenessThreshold, sourceTTLs)
	key := NewPriceCacheKey("query1", "source1")
	value := 123.45

	err := cache.Set(key, value, "source1")
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Wait for staleness but not TTL expiration
	time.Sleep(150 * time.Millisecond)

	// Should get ErrCacheStale (not ErrCacheExpired)
	result, err := cache.Get(key)
	if err != ErrCacheStale {
		t.Errorf("expected ErrCacheStale, got %v", err)
	}
	if result != value {
		t.Errorf("expected value %v to still be available, got %v", value, result)
	}
}
