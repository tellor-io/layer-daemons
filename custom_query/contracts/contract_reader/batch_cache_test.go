package reader

import (
	"errors"
	"sync"
	"testing"
)

// TestBatchCache_SetAndGet tests that Set and Get work correctly
func TestBatchCache_SetAndGet(t *testing.T) {
	cache := NewBatchCache()
	callID := "test-call-1"
	result := []byte{0x01, 0x02, 0x03, 0x04}

	err := cache.Set(callID, result)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	retrieved, err := cache.Get(callID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(retrieved) != len(result) {
		t.Errorf("expected result length %d, got %d", len(result), len(retrieved))
	}

	for i := range result {
		if retrieved[i] != result[i] {
			t.Errorf("expected result[%d] = %d, got %d", i, result[i], retrieved[i])
		}
	}
}

// TestBatchCache_GetOnMiss tests that Get returns an error when the callID is not found
func TestBatchCache_GetOnMiss(t *testing.T) {
	cache := NewBatchCache()
	callID := "non-existent-call"

	_, err := cache.Get(callID)
	if err == nil {
		t.Error("expected error on cache miss, got nil")
	}

	// Check that it's the expected error type
	if !errors.Is(err, ErrBatchCacheMiss) {
		t.Errorf("expected ErrBatchCacheMiss, got %v", err)
	}
}

// TestBatchCache_ConcurrentReads tests that concurrent reads are thread-safe
func TestBatchCache_ConcurrentReads(t *testing.T) {
	cache := NewBatchCache()
	callID := "test-call-1"
	result := []byte{0x01, 0x02, 0x03, 0x04}

	err := cache.Set(callID, result)
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
			retrieved, err := cache.Get(callID)
			if err != nil {
				t.Errorf("Get failed: %v", err)
				return
			}
			if len(retrieved) != len(result) {
				t.Errorf("expected result length %d, got %d", len(result), len(retrieved))
				return
			}
			for j := range result {
				if retrieved[j] != result[j] {
					t.Errorf("expected result[%d] = %d, got %d", j, result[j], retrieved[j])
					return
				}
			}
		}()
	}

	wg.Wait()
}

// TestBatchCache_ConcurrentWrites tests that concurrent writes are thread-safe
func TestBatchCache_ConcurrentWrites(t *testing.T) {
	cache := NewBatchCache()
	callID := "test-call-1"

	// Launch multiple concurrent writes with different values
	var wg sync.WaitGroup
	numWriters := 100
	wg.Add(numWriters)

	for i := 0; i < numWriters; i++ {
		go func(val byte) {
			defer wg.Done()
			result := []byte{val, val + 1, val + 2}
			err := cache.Set(callID, result)
			if err != nil {
				t.Errorf("Set failed: %v", err)
			}
		}(byte(i))
	}

	wg.Wait()

	// After all writes, should be able to read a value (any of the written values)
	retrieved, err := cache.Get(callID)
	if err != nil {
		t.Fatalf("Get failed after concurrent writes: %v", err)
	}
	if len(retrieved) == 0 {
		t.Error("expected a value after concurrent writes, got empty slice")
	}
}

// TestBatchCache_ConcurrentReadsAndWrites tests that concurrent reads and writes are thread-safe
func TestBatchCache_ConcurrentReadsAndWrites(t *testing.T) {
	cache := NewBatchCache()
	callID := "test-call-1"
	initialResult := []byte{0x01, 0x02, 0x03}

	err := cache.Set(callID, initialResult)
	if err != nil {
		t.Fatalf("Initial Set failed: %v", err)
	}

	var wg sync.WaitGroup
	numReaders := 50
	numWriters := 50
	wg.Add(numReaders + numWriters)

	// Concurrent readers
	for i := 0; i < numReaders; i++ {
		go func() {
			defer wg.Done()
			_, err := cache.Get(callID)
			// It's okay if we get an error (cache miss) during concurrent writes
			// but we shouldn't panic
			_ = err
		}()
	}

	// Concurrent writers
	for i := 0; i < numWriters; i++ {
		go func(val byte) {
			defer wg.Done()
			result := []byte{val, val + 1, val + 2}
			err := cache.Set(callID, result)
			if err != nil {
				t.Errorf("Set failed: %v", err)
			}
		}(byte(i))
	}

	wg.Wait()

	// After all operations, should be able to read a value
	retrieved, err := cache.Get(callID)
	if err != nil {
		t.Fatalf("Get failed after concurrent operations: %v", err)
	}
	if len(retrieved) == 0 {
		t.Error("expected a value after concurrent operations, got empty slice")
	}
}

// TestBatchCache_Clear tests that Clear removes all entries
func TestBatchCache_Clear(t *testing.T) {
	cache := NewBatchCache()
	callID1 := "test-call-1"
	callID2 := "test-call-2"
	result1 := []byte{0x01, 0x02}
	result2 := []byte{0x03, 0x04}

	err := cache.Set(callID1, result1)
	if err != nil {
		t.Fatalf("Set callID1 failed: %v", err)
	}
	err = cache.Set(callID2, result2)
	if err != nil {
		t.Fatalf("Set callID2 failed: %v", err)
	}

	// Verify both are in cache
	_, err = cache.Get(callID1)
	if err != nil {
		t.Fatalf("Get callID1 failed before clear: %v", err)
	}
	_, err = cache.Get(callID2)
	if err != nil {
		t.Fatalf("Get callID2 failed before clear: %v", err)
	}

	// Clear
	err = cache.Clear()
	if err != nil {
		t.Fatalf("Clear failed: %v", err)
	}

	// Verify both are gone
	_, err = cache.Get(callID1)
	if err == nil {
		t.Error("expected error for callID1 after clear, got nil")
	}
	if !errors.Is(err, ErrBatchCacheMiss) {
		t.Errorf("expected ErrBatchCacheMiss for callID1 after clear, got %v", err)
	}

	_, err = cache.Get(callID2)
	if err == nil {
		t.Error("expected error for callID2 after clear, got nil")
	}
	if !errors.Is(err, ErrBatchCacheMiss) {
		t.Errorf("expected ErrBatchCacheMiss for callID2 after clear, got %v", err)
	}
}

// TestBatchCache_Overwrite tests that setting the same callID overwrites the previous value
func TestBatchCache_Overwrite(t *testing.T) {
	cache := NewBatchCache()
	callID := "test-call-1"
	result1 := []byte{0x01, 0x02}
	result2 := []byte{0x03, 0x04, 0x05}

	err := cache.Set(callID, result1)
	if err != nil {
		t.Fatalf("First Set failed: %v", err)
	}

	// Verify first value
	retrieved, err := cache.Get(callID)
	if err != nil {
		t.Fatalf("Get after first Set failed: %v", err)
	}
	if len(retrieved) != len(result1) {
		t.Errorf("expected first result length %d, got %d", len(result1), len(retrieved))
	}

	// Overwrite with second value
	err = cache.Set(callID, result2)
	if err != nil {
		t.Fatalf("Second Set failed: %v", err)
	}

	// Verify second value
	retrieved, err = cache.Get(callID)
	if err != nil {
		t.Fatalf("Get after second Set failed: %v", err)
	}
	if len(retrieved) != len(result2) {
		t.Errorf("expected second result length %d, got %d", len(result2), len(retrieved))
	}

	for i := range result2 {
		if retrieved[i] != result2[i] {
			t.Errorf("expected result2[%d] = %d, got %d", i, result2[i], retrieved[i])
		}
	}
}

// TestBatchCache_EmptyResult tests that empty byte slices can be stored and retrieved
func TestBatchCache_EmptyResult(t *testing.T) {
	cache := NewBatchCache()
	callID := "test-call-1"
	result := []byte{}

	err := cache.Set(callID, result)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	retrieved, err := cache.Get(callID)
	if err != nil {
		t.Fatalf("Get failed: %v", err)
	}

	if len(retrieved) != 0 {
		t.Errorf("expected empty result, got length %d", len(retrieved))
	}
}

// TestBatchCache_MultipleEntries tests that multiple entries can be stored independently
func TestBatchCache_MultipleEntries(t *testing.T) {
	cache := NewBatchCache()
	callID1 := "test-call-1"
	callID2 := "test-call-2"
	callID3 := "test-call-3"
	result1 := []byte{0x01, 0x02}
	result2 := []byte{0x03, 0x04, 0x05}
	result3 := []byte{0x06}

	err := cache.Set(callID1, result1)
	if err != nil {
		t.Fatalf("Set callID1 failed: %v", err)
	}
	err = cache.Set(callID2, result2)
	if err != nil {
		t.Fatalf("Set callID2 failed: %v", err)
	}
	err = cache.Set(callID3, result3)
	if err != nil {
		t.Fatalf("Set callID3 failed: %v", err)
	}

	// Verify all three entries
	retrieved1, err := cache.Get(callID1)
	if err != nil {
		t.Fatalf("Get callID1 failed: %v", err)
	}
	if len(retrieved1) != len(result1) {
		t.Errorf("expected callID1 length %d, got %d", len(result1), len(retrieved1))
	}

	retrieved2, err := cache.Get(callID2)
	if err != nil {
		t.Fatalf("Get callID2 failed: %v", err)
	}
	if len(retrieved2) != len(result2) {
		t.Errorf("expected callID2 length %d, got %d", len(result2), len(retrieved2))
	}

	retrieved3, err := cache.Get(callID3)
	if err != nil {
		t.Fatalf("Get callID3 failed: %v", err)
	}
	if len(retrieved3) != len(result3) {
		t.Errorf("expected callID3 length %d, got %d", len(result3), len(retrieved3))
	}
}
