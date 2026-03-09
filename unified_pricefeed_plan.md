# Unified Pricefeed System Implementation Plan

## Overview

This plan implements the unified pricefeed system as described in the design documents. The system will:

- Consolidate `market_params` and `custom_query_config` into unified `sources.toml` and `asset_pairs.toml`
- Implement intelligent caching with staleness thresholds
- Add batching support for REST APIs (query_param, body) and Multicall3 contract calls
- Migrate existing exchange sources to the new system
- Replace the old system entirely (no parallel operation)

## Architecture Components

```
Configuration Layer (sources.toml, asset_pairs.toml)
    ↓
Query Orchestrator (routes queries, aggregates results)
    ↓
┌──────────────┬──────────────┬──────────────┐
│ Cache Layer  │ Batch Scheduler│ On-Demand   │
│              │              │ Query        │
└──────────────┴──────────────┴──────────────┘
    ↓                    ↓
REST Batch Handler  Multicall3 Handler
```

## Implementation Structure

### 1. Configuration System

**Files to Create:**

- `unified_config/config.go` - Configuration loading and parsing
- `unified_config/sources.go` - Source configuration types
- `unified_config/asset_pairs.go` - Asset pair configuration types
- `unified_config/default_config.go` - Default config generation

**Key Types:**

```go
// Source configuration
type SourceConfig struct {
    Type                    string
    Batchable               bool
    BatchStrategy           string  // "query_param", "body", "multicall3"
    BatchGroup              string
    UpdateIntervalSeconds  int     // For batchable sources
    CacheTTLSeconds         int
    // ... source-specific fields
}

// Asset pair configuration
type AssetPairConfig struct {
    ID                uint32
    Pair              string
    QueryData         string
    Exponent          int32
    MinSources        int
    Sources           []AssetPairSource
    // ... aggregation config
}
```

**Configuration Files:**

- `config/sources.toml` - All price sources (exchanges, REST APIs, contracts, RPC)
- `config/asset_pairs.toml` - Asset pairs and their source references

### 2. Cache Layer

**Files to Create:**

- `unified_config/cache/price_cache.go` - Main cache implementation
- `unified_config/cache/types.go` - Cache types and errors

**Key Features:**

- Cache key format: `{queryId}-{sourceId}` for prices, `{queryId}-{sourceId}-{callKey}` for contract calls
- Global staleness threshold (configurable)
- Thread-safe with RWMutex
- TTL per source

### 3. Query Orchestrator

**Files to Create:**

- `unified_config/orchestrator/query_orchestrator.go` - Main orchestrator
- `unified_config/orchestrator/result_aggregator.go` - Result aggregation logic

**Responsibilities:**

- Route queries to cache or on-demand handlers
- Check cache for batchable sources
- Trigger immediate updates for expired cache
- Aggregate results from multiple sources
- Handle fallback logic

### 4. Batch Scheduler

**Files to Create:**

- `unified_config/batch/scheduler.go` - Batch scheduler per source
- `unified_config/batch/collector.go` - Collects pending queries
- `unified_config/batch/types.go` - Batch types

**Key Features:**

- Per-source update intervals (for batchable sources)
- Startup immediate update for all batchable sources
- Staleness threshold triggers immediate updates
- Groups queries by batch_group for execution

### 5. REST Batch Handler

**Files to Create:**

- `unified_config/batch/rest_handler.go` - REST API batching
- `unified_config/batch/rest_query_param.go` - Query parameter batching
- `unified_config/batch/rest_body.go` - Body batching

**Strategies:**

- `query_param`: Batch multiple values in query parameters (e.g., CoinGecko)
- `body`: Batch multiple queries in request body (custom APIs)

### 6. Multicall3 Batch Handler

**Files to Create:**

- `custom_query/contracts/contract_reader/batched_reader.go` - BatchedReader wrapper
- `custom_query/contracts/contract_reader/batch_collector.go` - Collects contract calls
- `custom_query/contracts/contract_reader/multicall3_executor.go` - Multicall3 execution
- `custom_query/contracts/contract_reader/batch_cache.go` - Cache for batch results

**Key Features:**

- Wraps existing `Reader` to intercept calls
- Groups calls by chain and batch_group
- Executes via Multicall3 aggregate()
- Caches raw `[]byte` results by CallID
- Handlers parse results using existing logic (no changes needed)

**Call Key System:**

- Extend `ParallelFetcher` to pass call keys via context
- BatchedReader extracts call key from context
- CallID format: `{queryId}-{sourceId}-{callKey}`

### 7. Handler Integration

**Files to Modify:**

- `custom_query/combined/combined_handler/handlers.go` - Update ParallelFetcher to pass call keys in context
- Keep existing handlers unchanged (SFRXUSDPriceHandler, SUSNPriceHandler, etc.)
- Handlers will automatically use BatchedReader when configured

**Files to Keep (Historical):**

- All existing handlers remain for reference
- They will work with new system via BatchedReader wrapper

### 8. Exchange Source Migration

**Files to Create:**

- `unified_config/migration/exchange_migration.go` - Migration utilities

**Migration:**

- Convert `StaticExchangeDetails` to `sources.toml` entries as REST sources (`Type = "rest"`)
- Convert `market_params.toml` to `asset_pairs.toml`
- **No ticker-based approach**: Exchanges are treated as regular REST sources
- Exchange sources can be `batchable = true` (if API supports batching) or `batchable = false` (on-demand)
- Use existing REST batch handlers - no special exchange handler needed

### 9. Integration with Existing System

**Files to Modify:**

- `app.go` - Load unified config instead of old configs
- `reporter/client/median.go` - Use unified orchestrator
- `cmd/test_mode.go` - Test unified config system
- `custom_query/request.go` - Integrate with unified orchestrator (or replace)

**Integration Points:**

- Replace `customquery.FetchPrice()` calls with orchestrator
- Replace exchange price fetching with orchestrator
- Update test mode to validate unified configs

### 10. Configuration Defaults

**Files to Create:**

- `unified_config/default_sources.go` - Default sources.toml generation
- `unified_config/default_asset_pairs.go` - Default asset_pairs.toml generation

**Migration Utilities:**

- Convert existing `market_params.toml` → `asset_pairs.toml`
- Convert existing `custom_query_config.toml` → `sources.toml` + `asset_pairs.toml`
- Preserve all existing functionality

## Implementation Order (Test-Driven)

Each component is implemented **followed immediately by its tests** before moving to the next component.

### Phase 1: Core Infrastructure

1. **Configuration System**

   - Implement: `unified_config/config.go`, `sources.go`, `asset_pairs.go`
   - Test: `config_test.go`, `sources_test.go`, `asset_pairs_test.go`
   - Verify: TOML parsing, validation, error handling

2. **Cache Layer**

   - Implement: `unified_config/cache/price_cache.go`
   - Test: `cache/price_cache_test.go`
   - Verify: TTL, staleness, thread-safety

3. **Query Orchestrator**

   - Implement: `unified_config/orchestrator/query_orchestrator.go`
   - Test: `orchestrator/query_orchestrator_test.go`
   - Verify: Routing, cache checks, aggregation

### Phase 2: Batching System

4. **Batch Scheduler**

   - Implement: `unified_config/batch/scheduler.go`, `collector.go`
   - Test: `batch/scheduler_test.go`
   - Verify: Intervals, startup updates, staleness triggers

5. **REST Batch Handler**

   - Implement: `unified_config/batch/rest_handler.go`
   - Test: `batch/rest_handler_test.go`
   - Verify: Query param and body batching strategies

6. **Multicall3 Handler**

   - Implement: `custom_query/contracts/contract_reader/batched_reader.go`
   - Test: `contract_reader/batched_reader_test.go`
   - Verify: Call interception, batching, result routing

7. **ParallelFetcher Update**

   - Implement: Update `custom_query/combined/combined_handler/handlers.go`
   - Test: `combined_handler/handlers_test.go` (update existing)
   - Verify: Call key passing via context

### Phase 3: Integration

8. **Exchange Migration**

   - Implement: `unified_config/migration/exchange_migration.go`
   - Test: `migration/exchange_migration_test.go`
   - Verify: Config conversion accuracy

9. **App Integration**

   - Implement: Update `app.go`
   - Test: `app_test.go` (integration tests)
   - Verify: Config loading, price fetching

10. **Reporter Integration**

    - Implement: Update `reporter/client/median.go`
    - Test: `reporter/client/median_test.go` (update existing)
    - Verify: Price queries work correctly

11. **Test Mode Update**

    - Implement: Update `cmd/test_mode.go`
    - Test: `cmd/test_mode_test.go`
    - Verify: Validates unified configs

### Phase 4: Migration & Cleanup

12. **Default Configs**

    - Implement: `unified_config/default_config.go`
    - Test: `default_config_test.go`
    - Verify: Config generation works

13. **End-to-End Integration Tests**

    - Test: Full flows with batching, caching, multiple sources
    - Verify: System works end-to-end

14. **Remove Old System**

    - Remove: Direct usage of `market_params`, `custom_query_config`
    - Verify: All functionality migrated

## Key Design Decisions

1. **BatchedReader Wrapper**: Wraps existing Reader, no changes to handlers needed
2. **Call Key via Context**: ParallelFetcher passes keys through context to BatchedReader
3. **Cache at Call Level**: Contract results cached as raw bytes, handlers parse them
4. **Per-Source Intervals**: Each batchable source has its own update interval
5. **Staleness Threshold**: Global threshold triggers immediate updates for expired cache
6. **Startup Updates**: All batchable sources updated immediately on startup

## File Structure

```
unified_config/
├── config.go              # Main config loading
├── sources.go             # Source config types
├── asset_pairs.go         # Asset pair config types
├── default_config.go      # Default config generation
├── cache/
│   ├── price_cache.go     # Main cache implementation
│   └── types.go           # Cache types
├── orchestrator/
│   ├── query_orchestrator.go
│   └── result_aggregator.go
├── batch/
│   ├── scheduler.go       # Batch scheduler
│   ├── collector.go      # Query collector
│   ├── rest_handler.go   # REST batching
│   ├── rest_query_param.go
│   ├── rest_body.go
│   └── types.go
└── migration/
    └── exchange_migration.go

custom_query/contracts/contract_reader/
├── batched_reader.go      # NEW: BatchedReader wrapper
├── batch_collector.go     # NEW: Collects calls
├── multicall3_executor.go # NEW: Multicall3 execution
└── batch_cache.go         # NEW: Batch result cache
```

## Testing Strategy

### Test-Driven Development Approach

Tests are written **immediately after** each component implementation to verify functionality as we build. This ensures:

- Components work correctly before integration
- Bugs are caught early
- Refactoring is safe with test coverage
- Documentation through test cases

### Test Structure

**Unit Tests** (paired with each implementation):

- Configuration parsing and validation
- Cache operations (TTL, staleness, thread-safety)
- Query orchestrator routing and aggregation
- Batch scheduler timing and triggers
- REST batch handler strategies
- Multicall3 handler batching
- ParallelFetcher context passing

**Integration Tests**:

- End-to-end price fetch flows
- Batching across multiple sources
- Cache behavior in real scenarios
- Error handling and fallbacks
- Migration utilities

**Test Patterns** (following existing codebase style):

- Use `testify/assert` and `testify/require`
- Use `httptest` for HTTP mocking
- Test success cases, error cases, and edge cases
- Use table-driven tests where appropriate
- Test concurrent operations for thread-safety

### Test Files Structure

```
unified_config/
├── config_test.go
├── sources_test.go
├── asset_pairs_test.go
├── cache/
│   ├── price_cache_test.go
│   └── types_test.go
├── orchestrator/
│   ├── query_orchestrator_test.go
│   └── result_aggregator_test.go
├── batch/
│   ├── scheduler_test.go
│   ├── rest_handler_test.go
│   └── integration_test.go
└── migration/
    └── exchange_migration_test.go

custom_query/contracts/contract_reader/
├── batched_reader_test.go
├── batch_collector_test.go
├── multicall3_executor_test.go
└── batch_cache_test.go
```

### Key Test Scenarios

**Configuration Tests**:

- Valid TOML parsing
- Invalid configs (missing fields, wrong types)
- Source validation (batchable flags, intervals)
- Asset pair validation (min_sources, aggregation methods)

**Cache Tests**:

- TTL expiration
- Staleness threshold triggers
- Concurrent read/write safety
- Cache key format correctness
- Cache invalidation

**Orchestrator Tests**:

- Cache hit returns immediately
- Cache miss triggers update
- Stale cache triggers immediate update
- Result aggregation (median, mean, weighted)
- Fallback to next source on error

**Batch Scheduler Tests**:

- Per-source interval timing
- Startup immediate updates
- Staleness threshold triggers
- Batch group execution
- Timer reset after immediate update

**REST Batch Handler Tests**:

- Query parameter batching (CoinGecko)
- Body batching (custom APIs)
- URL construction
- Response parsing and routing
- Batch size limits

**Multicall3 Handler Tests**:

- Call interception
- Batch collection by chain/group
- Multicall3 execution
- Result routing via CallID
- Call key extraction from context
- Multiple calls per handler

**Integration Tests**:

- Full price fetch with batching
- Multiple sources per pair
- Cache behavior in real flows
- Error scenarios and recovery
- Migration from old configs

## Migration Notes

- Old configs (`market_params.toml`, `custom_query_config.toml`) will be replaced
- Existing handlers remain unchanged (work via wrappers)
- Exchange sources migrated to unified config
- Test mode updated to work with new system