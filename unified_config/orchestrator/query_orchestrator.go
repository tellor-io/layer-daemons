package orchestrator

import (
	"fmt"
	"time"

	"github.com/tellor-io/layer-daemons/unified_config"
	"github.com/tellor-io/layer-daemons/unified_config/batch"
	"github.com/tellor-io/layer-daemons/unified_config/cache"
)

// SourceHandler is an interface for fetching prices from sources.
type SourceHandler interface {
	// FetchPrice fetches a price for the given queryID and sourceID.
	// Returns the price and an error if the fetch fails.
	FetchPrice(queryID, sourceID string) (float64, error)
}

// SchedulerInterface defines the interface for triggering batch updates.
// This allows us to mock the scheduler in tests.
type SchedulerInterface interface {
	// TriggerImmediateUpdate triggers an immediate update for a source and resets its timer.
	TriggerImmediateUpdate(sourceID string) error
}

// QueryOrchestrator orchestrates price queries by routing to sources,
// checking cache, and aggregating results.
type QueryOrchestrator struct {
	config         *unified_config.Config
	cache          *cache.PriceCache
	sourceHandlers map[string]SourceHandler

	// scheduler coordinates batch updates for batchable sources.
	// It is optional so that existing unit tests that don't care about
	// batching can still construct a minimal orchestrator.
	scheduler SchedulerInterface

	// collector is used to register pending queries for batchable sources.
	// When GetPrice is called for a batchable source we add the query to
	// the collector so the scheduler can pick it up on the next run.
	collector *batch.BatchCollector
}

// NewQueryOrchestrator creates a new QueryOrchestrator with the given config and cache.
func NewQueryOrchestrator(config *unified_config.Config, cache *cache.PriceCache) *QueryOrchestrator {
	return &QueryOrchestrator{
		config:         config,
		cache:          cache,
		sourceHandlers: make(map[string]SourceHandler),
	}
}

// WithBatching wires a BatchScheduler and BatchCollector into the orchestrator.
// This keeps construction explicit and testable without forcing batching on all
// callers.
func (o *QueryOrchestrator) WithBatching(scheduler SchedulerInterface, collector *batch.BatchCollector) {
	o.scheduler = scheduler
	o.collector = collector
}

// GetAssetPairConfig retrieves the asset pair configuration for the given queryID.
// Returns nil if no asset pair is found.
func (o *QueryOrchestrator) GetAssetPairConfig(queryID string) *unified_config.AssetPairConfig {
	for i := range o.config.AssetPairs {
		if o.config.AssetPairs[i].QueryData == queryID {
			return &o.config.AssetPairs[i]
		}
	}
	return nil
}

// GetPrice retrieves the aggregated price for the given queryID.
// It checks the cache first, fetches from sources if needed, and aggregates results.
func (o *QueryOrchestrator) GetPrice(queryID string) (float64, error) {
	// Find asset pair by queryID (QueryData field)
	pair := o.GetAssetPairConfig(queryID)
	if pair == nil {
		return 0, fmt.Errorf("no asset pair found for queryID: %s", queryID)
	}

	// Collect results from all sources
	var results []PriceResult

	for _, source := range pair.Sources {
		sourceID := source.SourceID
		key := cache.NewPriceCacheKey(queryID, sourceID)

		sourceCfg, ok := o.config.Sources[sourceID]
		if !ok {
			// Unknown source in pair; skip it so other sources can still contribute.
			continue
		}

		// Check cache first
		cachedValue, err := o.cache.Get(key)
		if err == nil {
			// Cache hit and fresh - use it
			price, ok := cachedValue.(float64)
			if !ok {
				return 0, fmt.Errorf("cached value is not a float64 for queryID %s, sourceID %s", queryID, sourceID)
			}
			results = append(results, PriceResult{
				Price:     price,
				SourceID:  sourceID,
				Timestamp: time.Now(), // We don't track exact timestamp in cache entry, use current time
				Weight:    source.Weight,
			})

			// For batchable sources, we still want to ensure the query is
			// registered for future batch updates so the cache stays warm.
			if sourceCfg.Batchable && o.collector != nil {
				groupID := sourceCfg.BatchGroup
				if groupID == "" {
					groupID = sourceID
				}
				_ = o.collector.AddQuery(queryID, sourceID, groupID)
			}
			continue
		}

		// Handle cache errors
		if err == cache.ErrCacheStale {
			// Cache stale - return cached value but trigger update (async).
			// For batchable sources we:
			//   - register the query with the collector
			//   - trigger an immediate scheduler update
			price, ok := cachedValue.(float64)
			if !ok {
				return 0, fmt.Errorf("cached value is not a float64 for queryID %s, sourceID %s", queryID, sourceID)
			}
			results = append(results, PriceResult{
				Price:     price,
				SourceID:  sourceID,
				Timestamp: time.Now(),
				Weight:    source.Weight,
			})

			if sourceCfg.Batchable && o.collector != nil && o.scheduler != nil {
				groupID := sourceCfg.BatchGroup
				if groupID == "" {
					groupID = sourceID
				}
				_ = o.collector.AddQuery(queryID, sourceID, groupID)
				// Ignore scheduler error here; caller still gets the stale value.
				_ = o.scheduler.TriggerImmediateUpdate(sourceID)
			}
			continue
		}

		if sourceCfg.Batchable {
			// For batchable sources on cache miss/expired we rely on the batch
			// scheduler instead of doing an immediate fetch:
			//   - add the query to the collector
			//   - trigger an immediate update so results are filled soon
			if o.collector != nil {
				groupID := sourceCfg.BatchGroup
				if groupID == "" {
					groupID = sourceID
				}
				_ = o.collector.AddQuery(queryID, sourceID, groupID)
			}

			if o.scheduler != nil {
				_ = o.scheduler.TriggerImmediateUpdate(sourceID)
			}

			// Do not fetch directly; we will pick up the value on a subsequent call
			// once the scheduler has populated the cache.
			continue
		}

		if err == cache.ErrCacheExpired || err == cache.ErrCacheMiss {
			// Non-batchable source: fetch immediately on cache miss/expired.
		} else {
			// Unexpected error
			return 0, fmt.Errorf("unexpected cache error for queryID %s, sourceID %s: %w", queryID, sourceID, err)
		}

		// Cache miss or expired for non-batchable source - fetch from source
		handler, exists := o.sourceHandlers[sourceID]
		if !exists {
			// Handler not registered - skip this source
			continue
		}

		price, err := handler.FetchPrice(queryID, sourceID)
		if err != nil {
			// Source failed - skip this source
			continue
		}

		// Cache the fetched price
		if err := o.cache.Set(key, price, sourceID); err != nil {
			// Log error but continue
			// TODO: Add logging
		}

		results = append(results, PriceResult{
			Price:     price,
			SourceID:  sourceID,
			Timestamp: time.Now(),
			Weight:    source.Weight,
		})
	}

	// Validate MinSources
	if len(results) < pair.MinSources {
		return 0, fmt.Errorf("insufficient sources: got %d, need at least %d for queryID %s", len(results), pair.MinSources, queryID)
	}

	// Aggregate results
	var aggregatedPrice float64
	var aggErr error

	switch pair.AggregationMethod {
	case "median":
		aggregatedPrice, aggErr = AggregateMedian(results)
	case "mean":
		aggregatedPrice, aggErr = AggregateMean(results)
	case "weighted":
		aggregatedPrice, aggErr = AggregateWeighted(results)
	default:
		return 0, fmt.Errorf("unknown aggregation method: %s", pair.AggregationMethod)
	}

	if aggErr != nil {
		return 0, fmt.Errorf("aggregation failed for queryID %s: %w", queryID, aggErr)
	}

	return aggregatedPrice, nil
}
