package batch

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/tellor-io/layer-daemons/unified_config"
	"github.com/tellor-io/layer-daemons/unified_config/cache"
)

// mockBatchHandler is a mock implementation of BatchHandler for testing
type mockBatchHandler struct {
	mu           sync.Mutex
	batchResults map[string]map[string]float64 // sourceID -> queryID -> price
	batchErrors  map[string]error              // sourceID -> error
	callCount    map[string]int                // sourceID -> number of times BatchFetch was called
	callHistory  []batchCall                   // history of batch calls
}

type batchCall struct {
	sourceID  string
	queryIDs  []string
	timestamp time.Time
}

func newMockBatchHandler() *mockBatchHandler {
	return &mockBatchHandler{
		batchResults: make(map[string]map[string]float64),
		batchErrors:  make(map[string]error),
		callCount:    make(map[string]int),
		callHistory:  make([]batchCall, 0),
	}
}

func (m *mockBatchHandler) BatchFetch(sourceID string, queryIDs []string) (map[string]float64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.callCount[sourceID]++
	m.callHistory = append(m.callHistory, batchCall{
		sourceID:  sourceID,
		queryIDs:  queryIDs,
		timestamp: time.Now(),
	})

	if err, ok := m.batchErrors[sourceID]; ok {
		return nil, err
	}

	if results, ok := m.batchResults[sourceID]; ok {
		// Return only the requested queryIDs
		filtered := make(map[string]float64)
		for _, qid := range queryIDs {
			if price, exists := results[qid]; exists {
				filtered[qid] = price
			}
		}
		return filtered, nil
	}

	return nil, errors.New("no results configured")
}

func (m *mockBatchHandler) setBatchResults(sourceID string, results map[string]float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchResults[sourceID] = results
}

func (m *mockBatchHandler) setBatchError(sourceID string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.batchErrors[sourceID] = err
}

func (m *mockBatchHandler) getCallCount(sourceID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callCount[sourceID]
}

func (m *mockBatchHandler) getCallHistory() []batchCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	result := make([]batchCall, len(m.callHistory))
	copy(result, m.callHistory)
	return result
}

func TestBatchScheduler_NewBatchScheduler(t *testing.T) {
	config := &unified_config.Config{
		Sources: make(map[string]unified_config.SourceConfig),
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)

	scheduler := NewBatchScheduler(config, collector, priceCache)
	if scheduler == nil {
		t.Fatal("NewBatchScheduler returned nil")
	}
	if scheduler.timers == nil {
		t.Error("timers map should be initialized")
	}
}

func TestBatchScheduler_StartTriggersImmediateUpdate(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
		},
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)
	handler := newMockBatchHandler()
	handler.setBatchResults("source1", map[string]float64{
		"query1": 100.0,
		"query2": 200.0,
	})

	scheduler := NewBatchScheduler(config, collector, priceCache)
	scheduler.RegisterBatchHandler("source1", handler)

	// Add some queries to the collector
	collector.AddQuery("query1", "source1", "group1")
	collector.AddQuery("query2", "source1", "group1")

	// Start the scheduler
	err := scheduler.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Verify immediate update was triggered
	callCount := handler.getCallCount("source1")
	if callCount < 1 {
		t.Errorf("expected at least 1 batch fetch call, got %d", callCount)
	}

	// Verify results were cached
	key1 := cache.NewPriceCacheKey("query1", "source1")
	key2 := cache.NewPriceCacheKey("query2", "source1")

	value1, err1 := priceCache.Get(key1)
	value2, err2 := priceCache.Get(key2)

	if err1 != nil {
		t.Errorf("expected query1 to be cached, got error: %v", err1)
	}
	if err2 != nil {
		t.Errorf("expected query2 to be cached, got error: %v", err2)
	}

	if value1.(float64) != 100.0 {
		t.Errorf("expected cached value 100.0 for query1, got %v", value1)
	}
	if value2.(float64) != 200.0 {
		t.Errorf("expected cached value 200.0 for query2, got %v", value2)
	}

	// Cleanup
	scheduler.Stop()
}

func TestBatchScheduler_PeriodicUpdates(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 1, // 1 second for fast testing
			},
		},
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)
	handler := newMockBatchHandler()
	handler.setBatchResults("source1", map[string]float64{
		"query1": 100.0,
	})

	scheduler := NewBatchScheduler(config, collector, priceCache)
	scheduler.RegisterBatchHandler("source1", handler)

	// Add query to collector
	collector.AddQuery("query1", "source1", "group1")

	// Start scheduler
	err := scheduler.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer scheduler.Stop()

	// Wait for initial update
	time.Sleep(200 * time.Millisecond)
	initialCalls := handler.getCallCount("source1")

	// Wait for periodic update (should happen after 1 second)
	time.Sleep(1200 * time.Millisecond)
	finalCalls := handler.getCallCount("source1")

	if finalCalls <= initialCalls {
		t.Errorf("expected periodic update, initial calls: %d, final calls: %d", initialCalls, finalCalls)
	}
}

func TestBatchScheduler_TriggerImmediateUpdate(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
		},
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)
	handler := newMockBatchHandler()
	handler.setBatchResults("source1", map[string]float64{
		"query1": 150.0,
	})

	scheduler := NewBatchScheduler(config, collector, priceCache)
	scheduler.RegisterBatchHandler("source1", handler)

	// Start scheduler
	err := scheduler.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer scheduler.Stop()

	// Clear initial call count (from startup)
	time.Sleep(100 * time.Millisecond)
	handler.callCount = make(map[string]int)

	// Add query to collector
	collector.AddQuery("query1", "source1", "group1")

	// Trigger immediate update
	err = scheduler.TriggerImmediateUpdate("source1")
	if err != nil {
		t.Fatalf("TriggerImmediateUpdate failed: %v", err)
	}

	// Give it a moment to process
	time.Sleep(100 * time.Millisecond)

	// Verify update was triggered
	callCount := handler.getCallCount("source1")
	if callCount < 1 {
		t.Errorf("expected at least 1 batch fetch call after immediate trigger, got %d", callCount)
	}

	// Verify result was cached
	key := cache.NewPriceCacheKey("query1", "source1")
	value, err := priceCache.Get(key)
	if err != nil {
		t.Errorf("expected query1 to be cached after immediate update, got error: %v", err)
	}
	if value.(float64) != 150.0 {
		t.Errorf("expected cached value 150.0, got %v", value)
	}
}

func TestBatchScheduler_StopCancelsTimers(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 1,
			},
		},
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)
	handler := newMockBatchHandler()

	scheduler := NewBatchScheduler(config, collector, priceCache)
	scheduler.RegisterBatchHandler("source1", handler)

	// Start scheduler
	err := scheduler.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Stop scheduler
	err = scheduler.Stop()
	if err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Get call count after stop
	callCountAfterStop := handler.getCallCount("source1")

	// Wait longer - no more calls should happen
	time.Sleep(1500 * time.Millisecond)
	callCountAfterWait := handler.getCallCount("source1")

	if callCountAfterWait != callCountAfterStop {
		t.Errorf("expected no more calls after Stop, before wait: %d, after wait: %d", callCountAfterStop, callCountAfterWait)
	}
}

func TestBatchScheduler_MultipleSourcesIndependent(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
			"source2": {
				ID:                    "source2",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group2",
				UpdateIntervalSeconds: 60,
			},
		},
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)
	handler1 := newMockBatchHandler()
	handler1.setBatchResults("source1", map[string]float64{"query1": 100.0})
	handler2 := newMockBatchHandler()
	handler2.setBatchResults("source2", map[string]float64{"query2": 200.0})

	scheduler := NewBatchScheduler(config, collector, priceCache)
	scheduler.RegisterBatchHandler("source1", handler1)
	scheduler.RegisterBatchHandler("source2", handler2)

	// Add queries to different sources
	collector.AddQuery("query1", "source1", "group1")
	collector.AddQuery("query2", "source2", "group2")

	// Start scheduler
	err := scheduler.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer scheduler.Stop()

	// Give it time to process
	time.Sleep(200 * time.Millisecond)

	// Verify both sources were updated
	if handler1.getCallCount("source1") < 1 {
		t.Error("expected source1 to be updated")
	}
	if handler2.getCallCount("source2") < 1 {
		t.Error("expected source2 to be updated")
	}

	// Verify both results were cached
	key1 := cache.NewPriceCacheKey("query1", "source1")
	key2 := cache.NewPriceCacheKey("query2", "source2")

	value1, err1 := priceCache.Get(key1)
	value2, err2 := priceCache.Get(key2)

	if err1 != nil {
		t.Errorf("expected query1 to be cached, got error: %v", err1)
	}
	if err2 != nil {
		t.Errorf("expected query2 to be cached, got error: %v", err2)
	}

	if value1.(float64) != 100.0 {
		t.Errorf("expected cached value 100.0 for query1, got %v", value1)
	}
	if value2.(float64) != 200.0 {
		t.Errorf("expected cached value 200.0 for query2, got %v", value2)
	}
}

func TestBatchScheduler_OnlyBatchableSourcesScheduled(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"batchable": {
				ID:                    "batchable",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
			"nonbatchable": {
				ID:        "nonbatchable",
				Type:      "rest",
				Batchable: false,
			},
		},
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)
	handler := newMockBatchHandler()

	scheduler := NewBatchScheduler(config, collector, priceCache)
	scheduler.RegisterBatchHandler("batchable", handler)

	// Start scheduler
	err := scheduler.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer scheduler.Stop()

	// Verify only batchable source has a timer
	if len(scheduler.timers) != 1 {
		t.Errorf("expected 1 timer for batchable source, got %d", len(scheduler.timers))
	}

	if _, exists := scheduler.timers["batchable"]; !exists {
		t.Error("expected timer for batchable source")
	}

	if _, exists := scheduler.timers["nonbatchable"]; exists {
		t.Error("unexpected timer for non-batchable source")
	}
}

func TestBatchScheduler_UpdateSourceHandlesErrors(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
		},
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)
	handler := newMockBatchHandler()
	handler.setBatchError("source1", errors.New("batch fetch failed"))

	scheduler := NewBatchScheduler(config, collector, priceCache)
	scheduler.RegisterBatchHandler("source1", handler)

	// Add query to collector
	collector.AddQuery("query1", "source1", "group1")

	// Start scheduler
	err := scheduler.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer scheduler.Stop()

	// Give it time to process
	time.Sleep(200 * time.Millisecond)

	// Verify handler was called (even though it failed)
	callCount := handler.getCallCount("source1")
	if callCount < 1 {
		t.Errorf("expected handler to be called even on error, got %d calls", callCount)
	}

	// Verify nothing was cached (since fetch failed)
	key := cache.NewPriceCacheKey("query1", "source1")
	_, err = priceCache.Get(key)
	if err == nil {
		t.Error("expected cache miss after batch fetch error")
	}
}

func TestBatchScheduler_UpdateSourceWithEmptyQueries(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
			},
		},
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)
	handler := newMockBatchHandler()

	scheduler := NewBatchScheduler(config, collector, priceCache)
	scheduler.RegisterBatchHandler("source1", handler)

	// Start scheduler (no queries in collector)
	err := scheduler.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer scheduler.Stop()

	// Give it time to process
	time.Sleep(200 * time.Millisecond)

	// Verify handler was still called (even with no queries)
	// This is expected behavior - the scheduler should still attempt to fetch
	callCount := handler.getCallCount("source1")
	if callCount < 1 {
		t.Errorf("expected handler to be called on startup, got %d calls", callCount)
	}
}

func TestBatchScheduler_TriggerImmediateUpdateResetsTimer(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 2, // 2 seconds
			},
		},
	}
	collector := NewBatchCollector()
	priceCache := cache.NewPriceCache(5*time.Minute, nil)
	handler := newMockBatchHandler()
	handler.setBatchResults("source1", map[string]float64{"query1": 100.0})

	scheduler := NewBatchScheduler(config, collector, priceCache)
	scheduler.RegisterBatchHandler("source1", handler)

	// Start scheduler
	err := scheduler.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer scheduler.Stop()

	// Wait for initial update
	time.Sleep(200 * time.Millisecond)
	initialCalls := handler.getCallCount("source1")

	// Trigger immediate update after 500ms
	time.Sleep(500 * time.Millisecond)
	collector.AddQuery("query1", "source1", "group1")
	err = scheduler.TriggerImmediateUpdate("source1")
	if err != nil {
		t.Fatalf("TriggerImmediateUpdate failed: %v", err)
	}

	// Wait a bit
	time.Sleep(200 * time.Millisecond)
	afterImmediateCalls := handler.getCallCount("source1")

	// Verify immediate update happened
	if afterImmediateCalls <= initialCalls {
		t.Errorf("expected immediate update to trigger, initial: %d, after: %d", initialCalls, afterImmediateCalls)
	}

	// Wait for next periodic update (should be ~2 seconds after immediate update)
	time.Sleep(2500 * time.Millisecond)
	finalCalls := handler.getCallCount("source1")

	// Should have at least one more call after the immediate update
	if finalCalls <= afterImmediateCalls {
		t.Errorf("expected periodic update after immediate trigger, after immediate: %d, final: %d", afterImmediateCalls, finalCalls)
	}
}
