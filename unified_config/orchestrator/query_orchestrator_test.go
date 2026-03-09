package orchestrator

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/tellor-io/layer-daemons/unified_config"
	"github.com/tellor-io/layer-daemons/unified_config/batch"
	"github.com/tellor-io/layer-daemons/unified_config/cache"
)

// mockSourceHandler is a mock implementation of SourceHandler for testing
type mockSourceHandler struct {
	prices    map[string]float64 // queryID -> price
	errors    map[string]error   // queryID -> error
	callCount map[string]int     // queryID -> number of times FetchPrice was called
}

func newMockSourceHandler() *mockSourceHandler {
	return &mockSourceHandler{
		prices:    make(map[string]float64),
		errors:    make(map[string]error),
		callCount: make(map[string]int),
	}
}

func (m *mockSourceHandler) FetchPrice(queryID, sourceID string) (float64, error) {
	key := queryID + "-" + sourceID
	m.callCount[key]++
	if err, ok := m.errors[key]; ok {
		return 0, err
	}
	if price, ok := m.prices[key]; ok {
		return price, nil
	}
	return 0, errors.New("price not found")
}

func (m *mockSourceHandler) setPrice(queryID, sourceID string, price float64) {
	key := queryID + "-" + sourceID
	m.prices[key] = price
}

func (m *mockSourceHandler) setError(queryID, sourceID string, err error) {
	key := queryID + "-" + sourceID
	m.errors[key] = err
}

func (m *mockSourceHandler) getCallCount(queryID, sourceID string) int {
	key := queryID + "-" + sourceID
	return m.callCount[key]
}

// mockBatchScheduler is a lightweight mock of BatchScheduler that lets us
// verify that the orchestrator triggers immediate updates when expected
// without depending on scheduler internals.
type mockBatchScheduler struct {
	triggeredSources []string
	mu               sync.Mutex
}

func (m *mockBatchScheduler) TriggerImmediateUpdate(sourceID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggeredSources = append(m.triggeredSources, sourceID)
	return nil
}

func (m *mockBatchScheduler) getTriggeredSources() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]string, len(m.triggeredSources))
	copy(result, m.triggeredSources)
	return result
}

func (m *mockBatchScheduler) reset() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.triggeredSources = m.triggeredSources[:0]
}

func TestQueryOrchestrator_CacheHit(t *testing.T) {
	// Setup config
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:   "source1",
				Type: "rest",
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "source1"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	// Setup cache with a fresh entry
	priceCache := cache.NewPriceCache(60*time.Second, nil)
	key := cache.NewPriceCacheKey("btc-usd", "source1")
	priceCache.Set(key, 50000.0, "source1")

	// Setup orchestrator
	handler := newMockSourceHandler()
	orchestrator := NewQueryOrchestrator(config, priceCache)
	orchestrator.sourceHandlers["source1"] = handler

	// Get price - should return cached value without calling handler
	price, err := orchestrator.GetPrice("btc-usd")
	if err != nil {
		t.Fatalf("GetPrice() returned error: %v", err)
	}
	if price != 50000.0 {
		t.Errorf("GetPrice() = %v, want %v", price, 50000.0)
	}

	// Verify handler was not called
	if handler.getCallCount("btc-usd", "source1") != 0 {
		t.Errorf("Expected handler not to be called, but it was called %d times", handler.getCallCount("btc-usd", "source1"))
	}
}

func TestQueryOrchestrator_CacheMiss(t *testing.T) {
	// Setup config
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:   "source1",
				Type: "rest",
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "source1"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	// Setup empty cache
	priceCache := cache.NewPriceCache(60*time.Second, nil)

	// Setup orchestrator with handler that returns a price
	handler := newMockSourceHandler()
	handler.setPrice("btc-usd", "source1", 50000.0)
	orchestrator := NewQueryOrchestrator(config, priceCache)
	orchestrator.sourceHandlers["source1"] = handler

	// Get price - should fetch from handler
	price, err := orchestrator.GetPrice("btc-usd")
	if err != nil {
		t.Fatalf("GetPrice() returned error: %v", err)
	}
	if price != 50000.0 {
		t.Errorf("GetPrice() = %v, want %v", price, 50000.0)
	}

	// Verify handler was called
	if handler.getCallCount("btc-usd", "source1") != 1 {
		t.Errorf("Expected handler to be called once, but it was called %d times", handler.getCallCount("btc-usd", "source1"))
	}

	// Verify price was cached
	cachedPrice, err := priceCache.Get(cache.NewPriceCacheKey("btc-usd", "source1"))
	if err != nil {
		t.Fatalf("Expected price to be cached, but got error: %v", err)
	}
	if cachedPrice.(float64) != 50000.0 {
		t.Errorf("Cached price = %v, want %v", cachedPrice, 50000.0)
	}
}

func TestQueryOrchestrator_StaleCache(t *testing.T) {
	// Setup config
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:   "source1",
				Type: "rest",
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "source1"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	// Setup cache with a stale entry (older than staleness threshold)
	// Use a very short staleness threshold for testing
	priceCache := cache.NewPriceCache(1*time.Nanosecond, nil)
	key := cache.NewPriceCacheKey("btc-usd", "source1")
	priceCache.Set(key, 50000.0, "source1")
	time.Sleep(2 * time.Nanosecond) // Ensure entry is stale

	// Setup orchestrator with handler
	handler := newMockSourceHandler()
	handler.setPrice("btc-usd", "source1", 51000.0) // New price
	orchestrator := NewQueryOrchestrator(config, priceCache)
	orchestrator.sourceHandlers["source1"] = handler

	// Get price - should return stale cached value immediately
	// Note: The spec says "trigger update (async) but return cached value"
	// For now, we'll return the cached value. Async update triggering will be added in Phase 2.
	price, err := orchestrator.GetPrice("btc-usd")
	// For stale cache, we should still get the value (even if there's an error)
	// The implementation should return the stale value
	if err != nil && err != cache.ErrCacheStale {
		t.Fatalf("GetPrice() returned unexpected error: %v", err)
	}

	// Should return the stale cached value
	if price != 50000.0 {
		t.Errorf("GetPrice() = %v, want %v (stale cached value)", price, 50000.0)
	}
}

func TestQueryOrchestrator_MultipleSources_Aggregation(t *testing.T) {
	// Setup config with multiple sources
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:   "source1",
				Type: "rest",
			},
			"source2": {
				ID:   "source2",
				Type: "rest",
			},
			"source3": {
				ID:   "source3",
				Type: "rest",
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        2,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "source1"},
					{SourceID: "source2"},
					{SourceID: "source3"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	// Setup empty cache
	priceCache := cache.NewPriceCache(60*time.Second, nil)

	// Setup orchestrator with handlers
	handler1 := newMockSourceHandler()
	handler1.setPrice("btc-usd", "source1", 50000.0)
	handler2 := newMockSourceHandler()
	handler2.setPrice("btc-usd", "source2", 51000.0)
	handler3 := newMockSourceHandler()
	handler3.setPrice("btc-usd", "source3", 52000.0)

	orchestrator := NewQueryOrchestrator(config, priceCache)
	orchestrator.sourceHandlers["source1"] = handler1
	orchestrator.sourceHandlers["source2"] = handler2
	orchestrator.sourceHandlers["source3"] = handler3

	// Get price - should aggregate from all sources
	price, err := orchestrator.GetPrice("btc-usd")
	if err != nil {
		t.Fatalf("GetPrice() returned error: %v", err)
	}

	// Median of [50000, 51000, 52000] = 51000
	expected := 51000.0
	if price != expected {
		t.Errorf("GetPrice() = %v, want %v (median)", price, expected)
	}
}

func TestQueryOrchestrator_ErrorHandling_SourceFailure(t *testing.T) {
	// Setup config with multiple sources
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:   "source1",
				Type: "rest",
			},
			"source2": {
				ID:   "source2",
				Type: "rest",
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        1, // Only need 1 source
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "source1"},
					{SourceID: "source2"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	// Setup empty cache
	priceCache := cache.NewPriceCache(60*time.Second, nil)

	// Setup orchestrator with one failing source and one working source
	handler1 := newMockSourceHandler()
	handler1.setError("btc-usd", "source1", errors.New("source1 failed"))
	handler2 := newMockSourceHandler()
	handler2.setPrice("btc-usd", "source2", 51000.0)

	orchestrator := NewQueryOrchestrator(config, priceCache)
	orchestrator.sourceHandlers["source1"] = handler1
	orchestrator.sourceHandlers["source2"] = handler2

	// Get price - should succeed with working source
	price, err := orchestrator.GetPrice("btc-usd")
	if err != nil {
		t.Fatalf("GetPrice() returned error: %v", err)
	}
	if price != 51000.0 {
		t.Errorf("GetPrice() = %v, want %v", price, 51000.0)
	}
}

func TestQueryOrchestrator_MinSources_Validation(t *testing.T) {
	// Setup config requiring 2 sources
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:   "source1",
				Type: "rest",
			},
			"source2": {
				ID:   "source2",
				Type: "rest",
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        2, // Require 2 sources
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "source1"},
					{SourceID: "source2"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	// Setup empty cache
	priceCache := cache.NewPriceCache(60*time.Second, nil)

	// Setup orchestrator with only one working source
	handler1 := newMockSourceHandler()
	handler1.setPrice("btc-usd", "source1", 50000.0)
	handler2 := newMockSourceHandler()
	handler2.setError("btc-usd", "source2", errors.New("source2 failed"))

	orchestrator := NewQueryOrchestrator(config, priceCache)
	orchestrator.sourceHandlers["source1"] = handler1
	orchestrator.sourceHandlers["source2"] = handler2

	// Get price - should fail because we don't have MinSources (2) successful results
	_, err := orchestrator.GetPrice("btc-usd")
	if err == nil {
		t.Error("GetPrice() expected error for insufficient sources, got nil")
	}
}

func TestQueryOrchestrator_UnknownQueryID(t *testing.T) {
	// Setup config
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:   "source1",
				Type: "rest",
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "source1"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	priceCache := cache.NewPriceCache(60*time.Second, nil)
	orchestrator := NewQueryOrchestrator(config, priceCache)

	// Try to get price for unknown query ID
	_, err := orchestrator.GetPrice("unknown-query")
	if err == nil {
		t.Error("GetPrice() expected error for unknown query ID, got nil")
	}
}

func TestQueryOrchestrator_AggregationMethods(t *testing.T) {
	tests := []struct {
		name              string
		aggregationMethod string
		prices            []float64
		expected          float64
	}{
		{
			name:              "median",
			aggregationMethod: "median",
			prices:            []float64{100.0, 200.0, 300.0},
			expected:          200.0,
		},
		{
			name:              "mean",
			aggregationMethod: "mean",
			prices:            []float64{100.0, 200.0, 300.0},
			expected:          200.0, // (100 + 200 + 300) / 3
		},
		{
			name:              "weighted",
			aggregationMethod: "weighted",
			prices:            []float64{100.0, 200.0},
			expected:          150.0, // (100 * 0.5) + (200 * 0.5) = 150
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup config
			sources := make(map[string]unified_config.SourceConfig)
			var pairSources []unified_config.AssetPairSource

			for i := range tt.prices {
				sourceID := fmt.Sprintf("source%d", i+1)
				sources[sourceID] = unified_config.SourceConfig{
					ID:   sourceID,
					Type: "rest",
				}
				weight := 0.0
				if tt.aggregationMethod == "weighted" {
					weight = 1.0 / float64(len(tt.prices))
				}
				pairSources = append(pairSources, unified_config.AssetPairSource{
					SourceID: sourceID,
					Weight:   weight,
				})
			}

			config := &unified_config.Config{
				Sources: sources,
				AssetPairs: []unified_config.AssetPairConfig{
					{
						ID:                1,
						Pair:              "TEST/USD",
						QueryData:         "test-usd",
						MinSources:        len(tt.prices),
						AggregationMethod: tt.aggregationMethod,
						Sources:           pairSources,
					},
				},
				GlobalStalenessThresholdSeconds: 60,
			}

			// Setup empty cache
			priceCache := cache.NewPriceCache(60*time.Second, nil)

			// Setup orchestrator with handlers
			orchestrator := NewQueryOrchestrator(config, priceCache)
			for i, price := range tt.prices {
				sourceID := fmt.Sprintf("source%d", i+1)
				handler := newMockSourceHandler()
				handler.setPrice("test-usd", sourceID, price)
				orchestrator.sourceHandlers[sourceID] = handler
			}

			// Get price
			gotPrice, err := orchestrator.GetPrice("test-usd")
			if err != nil {
				t.Fatalf("GetPrice() returned error: %v", err)
			}
			if gotPrice != tt.expected {
				t.Errorf("GetPrice() = %v, want %v", gotPrice, tt.expected)
			}
		})
	}
}

// TestQueryOrchestrator_BatchableSource_CacheMiss_RegistersQuery ensures that for
// batchable sources a cache miss causes the orchestrator to add the query to the
// batch collector and trigger an immediate scheduler update instead of doing an
// on-demand fetch.
func TestQueryOrchestrator_BatchableSource_CacheMiss_RegistersQuery(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"batchable_source": {
				ID:                    "batchable_source",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "batchable_source"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	priceCache := cache.NewPriceCache(60*time.Second, nil)

	// Setup orchestrator with batching wired in
	orchestrator := NewQueryOrchestrator(config, priceCache)
	collector := batch.NewBatchCollector()
	mockScheduler := &mockBatchScheduler{}
	orchestrator.WithBatching(mockScheduler, collector)

	// We don't register a source handler because batchable sources on cache miss
	// should not call FetchPrice directly.

	// Call GetPrice; because cache is empty and source is batchable we expect
	// no price yet and an error due to insufficient sources.
	_, err := orchestrator.GetPrice("btc-usd")
	if err == nil {
		t.Fatal("expected error due to insufficient sources when cache is empty for batchable source")
	}

	// Verify that the query was registered with the collector.
	group, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup returned error: %v", err)
	}
	if len(group.PendingQueries) != 1 {
		t.Fatalf("expected 1 pending query in collector, got %d", len(group.PendingQueries))
	}
	if group.PendingQueries[0].QueryID != "btc-usd" {
		t.Errorf("expected queryID btc-usd, got %s", group.PendingQueries[0].QueryID)
	}
	if group.PendingQueries[0].SourceID != "batchable_source" {
		t.Errorf("expected sourceID batchable_source, got %s", group.PendingQueries[0].SourceID)
	}

	// Verify that TriggerImmediateUpdate was called for the batchable source
	triggered := mockScheduler.getTriggeredSources()
	if len(triggered) != 1 {
		t.Fatalf("expected TriggerImmediateUpdate to be called once, got %d calls", len(triggered))
	}
	if triggered[0] != "batchable_source" {
		t.Errorf("expected TriggerImmediateUpdate to be called with sourceID 'batchable_source', got %s", triggered[0])
	}
}

// TestQueryOrchestrator_BatchableSource_StaleCache_TriggersUpdate verifies that
// when a batchable source has stale cache, the orchestrator returns the stale
// value but also triggers an immediate scheduler update.
func TestQueryOrchestrator_BatchableSource_StaleCache_TriggersUpdate(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"batchable_source": {
				ID:                    "batchable_source",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "batchable_source"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	// Setup cache with a stale entry (older than staleness threshold)
	// Use a very short staleness threshold for testing
	priceCache := cache.NewPriceCache(1*time.Nanosecond, nil)
	key := cache.NewPriceCacheKey("btc-usd", "batchable_source")
	priceCache.Set(key, 50000.0, "batchable_source")
	time.Sleep(2 * time.Nanosecond) // Ensure entry is stale

	// Setup orchestrator with batching wired in
	orchestrator := NewQueryOrchestrator(config, priceCache)
	collector := batch.NewBatchCollector()
	mockScheduler := &mockBatchScheduler{}
	orchestrator.WithBatching(mockScheduler, collector)

	// Get price - should return stale cached value immediately
	price, err := orchestrator.GetPrice("btc-usd")
	// For stale cache, we should still get the value (even if there's an error)
	if err != nil && err != cache.ErrCacheStale {
		t.Fatalf("GetPrice() returned unexpected error: %v", err)
	}

	// Should return the stale cached value
	if price != 50000.0 {
		t.Errorf("GetPrice() = %v, want %v (stale cached value)", price, 50000.0)
	}

	// Verify that the query was registered with the collector
	group, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup returned error: %v", err)
	}
	if len(group.PendingQueries) != 1 {
		t.Fatalf("expected 1 pending query in collector, got %d", len(group.PendingQueries))
	}
	if group.PendingQueries[0].QueryID != "btc-usd" {
		t.Errorf("expected queryID btc-usd, got %s", group.PendingQueries[0].QueryID)
	}

	// Verify that TriggerImmediateUpdate was called for the batchable source
	triggered := mockScheduler.getTriggeredSources()
	if len(triggered) != 1 {
		t.Fatalf("expected TriggerImmediateUpdate to be called once, got %d calls", len(triggered))
	}
	if triggered[0] != "batchable_source" {
		t.Errorf("expected TriggerImmediateUpdate to be called with sourceID 'batchable_source', got %s", triggered[0])
	}
}

// TestQueryOrchestrator_BatchableSource_CacheHit_RegistersQuery verifies that
// even when cache is fresh, batchable sources still register queries for future
// batch updates to keep the cache warm.
func TestQueryOrchestrator_BatchableSource_CacheHit_RegistersQuery(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"batchable_source": {
				ID:                    "batchable_source",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "batchable_source"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	// Setup cache with a fresh entry
	priceCache := cache.NewPriceCache(60*time.Second, nil)
	key := cache.NewPriceCacheKey("btc-usd", "batchable_source")
	priceCache.Set(key, 50000.0, "batchable_source")

	// Setup orchestrator with batching wired in
	orchestrator := NewQueryOrchestrator(config, priceCache)
	collector := batch.NewBatchCollector()
	mockScheduler := &mockBatchScheduler{}
	orchestrator.WithBatching(mockScheduler, collector)

	// Get price - should return cached value
	price, err := orchestrator.GetPrice("btc-usd")
	if err != nil {
		t.Fatalf("GetPrice() returned error: %v", err)
	}
	if price != 50000.0 {
		t.Errorf("GetPrice() = %v, want %v", price, 50000.0)
	}

	// Verify that the query was registered with the collector (for future updates)
	group, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup returned error: %v", err)
	}
	if len(group.PendingQueries) != 1 {
		t.Fatalf("expected 1 pending query in collector, got %d", len(group.PendingQueries))
	}
	if group.PendingQueries[0].QueryID != "btc-usd" {
		t.Errorf("expected queryID btc-usd, got %s", group.PendingQueries[0].QueryID)
	}

	// Verify that TriggerImmediateUpdate was NOT called for fresh cache
	triggered := mockScheduler.getTriggeredSources()
	if len(triggered) != 0 {
		t.Errorf("expected TriggerImmediateUpdate not to be called for fresh cache, but got %d calls: %v", len(triggered), triggered)
	}
}

// TestQueryOrchestrator_RealBatchScheduler_Integration verifies that a real
// BatchScheduler can be used with the orchestrator (interface compatibility check).
func TestQueryOrchestrator_RealBatchScheduler_Integration(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"batchable_source": {
				ID:                    "batchable_source",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "btc-usd",
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "batchable_source"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 60,
	}

	priceCache := cache.NewPriceCache(60*time.Second, nil)
	collector := batch.NewBatchCollector()
	realScheduler := batch.NewBatchScheduler(config, collector, priceCache)

	// Verify that the real scheduler implements the interface
	var schedulerInterface SchedulerInterface = realScheduler
	if schedulerInterface == nil {
		t.Fatal("BatchScheduler does not implement SchedulerInterface")
	}

	// Setup orchestrator with real scheduler
	orchestrator := NewQueryOrchestrator(config, priceCache)
	orchestrator.WithBatching(realScheduler, collector)

	// This test just verifies interface compatibility - the real scheduler
	// would need batch handlers registered to actually work, but the interface
	// contract is satisfied.
	if orchestrator.scheduler == nil {
		t.Error("scheduler should be set after WithBatching")
	}
	if orchestrator.collector == nil {
		t.Error("collector should be set after WithBatching")
	}
}
