package batch

import (
	"fmt"
	"sync"
	"time"

	"github.com/tellor-io/layer-daemons/unified_config"
	"github.com/tellor-io/layer-daemons/unified_config/cache"
)

// BatchHandler is an interface for batch fetching prices from sources.
// It allows fetching multiple queryIDs in a single batch operation.
type BatchHandler interface {
	// BatchFetch fetches prices for multiple queryIDs from the given sourceID.
	// Returns a map of queryID -> price, or an error if the batch fetch fails.
	BatchFetch(sourceID string, queryIDs []string) (map[string]float64, error)
}

// BatchScheduler schedules batch updates for batchable sources.
// It manages timers for periodic updates and handles immediate updates.
type BatchScheduler struct {
	config        *unified_config.Config
	collector     *BatchCollector
	cache         *cache.PriceCache
	batchHandlers map[string]BatchHandler
	timers        map[string]*time.Timer
	mu            sync.Mutex
}

// NewBatchScheduler creates a new BatchScheduler with the given config, collector, and cache.
func NewBatchScheduler(config *unified_config.Config, collector *BatchCollector, cache *cache.PriceCache) *BatchScheduler {
	return &BatchScheduler{
		config:        config,
		collector:     collector,
		cache:         cache,
		batchHandlers: make(map[string]BatchHandler),
		timers:        make(map[string]*time.Timer),
	}
}

// RegisterBatchHandler registers a batch handler for a source.
func (s *BatchScheduler) RegisterBatchHandler(sourceID string, handler BatchHandler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batchHandlers[sourceID] = handler
}

// Start starts the batch scheduler for all batchable sources.
// It triggers an immediate update for each batchable source and schedules periodic updates.
func (s *BatchScheduler) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for sourceID, sourceConfig := range s.config.Sources {
		if !sourceConfig.Batchable {
			continue
		}

		// Trigger immediate update on startup
		if err := s.updateSourceUnlocked(sourceID); err != nil {
			// Log error but continue with other sources
			// TODO: Add logging
		}

		// Schedule periodic updates
		interval := time.Duration(sourceConfig.UpdateIntervalSeconds) * time.Second
		timer := s.createTimer(sourceID, interval)
		s.timers[sourceID] = timer
	}

	return nil
}

// createTimer creates a timer for a source that reschedules itself after each update.
// Must be called with the mutex locked.
func (s *BatchScheduler) createTimer(sourceID string, interval time.Duration) *time.Timer {
	return time.AfterFunc(interval, func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Update the source
		if err := s.updateSourceUnlocked(sourceID); err != nil {
			// TODO: Add logging
		}

		// Reschedule the timer
		if _, exists := s.timers[sourceID]; exists {
			s.timers[sourceID] = s.createTimer(sourceID, interval)
		}
	})
}

// updateSourceUnlocked updates a source by fetching pending queries and caching results.
// Must be called with the mutex locked.
func (s *BatchScheduler) updateSourceUnlocked(sourceID string) error {
	sourceConfig, exists := s.config.Sources[sourceID]
	if !exists {
		return fmt.Errorf("source %q not found in config", sourceID)
	}

	if !sourceConfig.Batchable {
		return fmt.Errorf("source %q is not batchable", sourceID)
	}

	// Get batch handler
	handler, exists := s.batchHandlers[sourceID]
	if !exists {
		return fmt.Errorf("no batch handler registered for source %q", sourceID)
	}

	// Get pending queries for this source's batch group
	groupID := sourceConfig.BatchGroup
	if groupID == "" {
		// If no batch group specified, use sourceID as groupID
		groupID = sourceID
	}

	group, err := s.collector.GetGroup(groupID)
	if err != nil {
		return fmt.Errorf("failed to get batch group %q: %w", groupID, err)
	}

	// Extract queryIDs from pending queries
	queryIDs := make([]string, 0, len(group.PendingQueries))
	for _, query := range group.PendingQueries {
		queryIDs = append(queryIDs, query.QueryID)
	}

	// If no queries, still call handler (it may want to update all queries for this source)
	// But we'll pass an empty slice
	if len(queryIDs) == 0 {
		queryIDs = []string{}
	}

	// Execute batch fetch
	results, err := handler.BatchFetch(sourceID, queryIDs)
	if err != nil {
		// Return error but don't fail completely - other sources can still update
		return fmt.Errorf("batch fetch failed for source %q: %w", sourceID, err)
	}

	// Cache the results
	for queryID, price := range results {
		key := cache.NewPriceCacheKey(queryID, sourceID)
		if err := s.cache.Set(key, price, sourceID); err != nil {
			// Log error but continue caching other results
			// TODO: Add logging
		}
	}

	return nil
}

// TriggerImmediateUpdate triggers an immediate update for a source and resets its timer.
func (s *BatchScheduler) TriggerImmediateUpdate(sourceID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Update the source immediately
	if err := s.updateSourceUnlocked(sourceID); err != nil {
		return err
	}

	// Reset the timer
	sourceConfig, exists := s.config.Sources[sourceID]
	if !exists {
		return fmt.Errorf("source %q not found in config", sourceID)
	}

	if !sourceConfig.Batchable {
		return fmt.Errorf("source %q is not batchable", sourceID)
	}

	// Stop existing timer
	if timer, exists := s.timers[sourceID]; exists && timer != nil {
		timer.Stop()
	}

	// Reschedule timer
	interval := time.Duration(sourceConfig.UpdateIntervalSeconds) * time.Second
	s.timers[sourceID] = s.createTimer(sourceID, interval)

	return nil
}

// Stop stops all timers and cancels scheduled updates.
func (s *BatchScheduler) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for sourceID, timer := range s.timers {
		if timer != nil {
			timer.Stop()
		}
		delete(s.timers, sourceID)
	}

	return nil
}
