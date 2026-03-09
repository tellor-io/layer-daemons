# Unified Pricefeed System - Detailed Implementation Steps

This document breaks down the unified pricefeed plan into granular, actionable steps that can be executed one at a time with an agent.

## Prerequisites

Before starting, ensure you understand:
- The existing codebase structure
- Current `market_params.toml` and `custom_query_config.toml` formats
- Existing handler implementations (SFRXUSDPriceHandler, etc.)
- Contract reader interface

---

## Development Principles

**CRITICAL**: Follow these principles throughout all implementation steps:

### 1. Simplicity and Conciseness
- **Keep implementations as simple as possible** - avoid over-engineering
- Prefer straightforward solutions over complex abstractions
- Each function/struct should have a single, clear responsibility
- Minimize code duplication, but don't abstract prematurely
- Use the simplest data structures that meet requirements
- When in doubt, choose the more readable and maintainable approach

### 2. Test-Driven Development (TDD)
- **Write tests BEFORE implementing functionality** (when possible)
- For each step that creates new code:
  1. Write failing tests first that define the expected behavior
  2. Implement the minimum code to make tests pass
  3. Refactor if needed while keeping tests green
- Tests should verify behavior, not implementation details
- Use mocks/stubs for external dependencies (HTTP clients, contract readers, etc.)
- Run tests frequently during development (`go test ./...`)
- All tests must pass before moving to the next step
- If a step says "Write Tests", do it immediately after or before implementation

### 3. System-Wide Thinking
- **Always consider how each component fits into the overall system**
- Before implementing a step, understand:
  - How it will be used by other components
  - What interfaces it needs to implement
  - How it integrates with existing code
  - What dependencies it has or creates
- Review related steps in the same phase and adjacent phases
- Ensure interfaces are designed to work together seamlessly
- Think about the data flow: config → cache → handlers → aggregator → result
- Consider error propagation: how errors flow through the system

### 4. Ask Questions When Uncertain
- **If anything is unclear, ask questions before proceeding**
- Unclear requirements lead to incorrect implementations
- Questions to ask:
  - How does this interact with existing code?
  - What should happen in edge cases?
  - Are there existing patterns I should follow?
  - What's the expected behavior for error scenarios?
  - How should this be tested?
- Better to clarify upfront than to rework later
- Review existing codebase patterns before implementing new code
- If a step's instructions conflict with codebase patterns, ask for clarification

### Implementation Workflow
For each step:
1. **Understand**: Read the step, related steps, and existing code
2. **Question**: Identify any uncertainties or ambiguities
3. **Test**: Write tests that define expected behavior (TDD)
4. **Implement**: Write the simplest code that passes tests
5. **Verify**: Run tests, check integration points, ensure it fits the system
6. **Refactor**: Clean up while keeping tests green

---

## Phase 1: Core Infrastructure

### Step 1.1: Create Source Configuration Types

**Goal**: Define Go types for source configuration

**Files to Create**:
- `unified_config/sources.go`
- `unified_config/sources_test.go` (write tests first - TDD)

**Implementation**:
- Define `SourceConfig` struct with fields:
  - `ID` (string) - unique source identifier
  - `Type` (string) - "rest", "contract", "rpc" (exchanges are "rest" type)
  - `Batchable` (bool) - whether source supports batching
  - `BatchStrategy` (string) - "query_param", "body", "multicall3", or empty
  - `BatchGroup` (string) - groups sources for batching
  - `UpdateIntervalSeconds` (int) - for batchable sources
  - `CacheTTLSeconds` (int) - cache TTL per source
  - `BaseURL` (string, optional) - for REST sources
  - `ChainID` (uint64, optional) - for contract sources
  - `ContractAddress` (string, optional) - for contract sources
  - `RPCURL` (string, optional) - for RPC sources
  - Additional fields as needed for different source types
- Define validation method `Validate() error`
- Add JSON/TOML tags for serialization

**Verification**:
- Code compiles
- Types are exported and documented

---

### Step 1.2: Create Asset Pair Configuration Types

**Goal**: Define Go types for asset pair configuration

**Files to Create**:
- `unified_config/asset_pairs.go`

**Implementation**:
- Define `AssetPairConfig` struct with fields:
  - `ID` (uint32) - unique pair identifier
  - `Pair` (string) - pair name (e.g., "BTC/USD")
  - `QueryData` (string) - query identifier/data
  - `Exponent` (int32) - price exponent
  - `MinSources` (int) - minimum sources required
  - `Sources` ([]AssetPairSource) - list of sources for this pair
  - `AggregationMethod` (string) - "median", "mean", "weighted"
- Define `AssetPairSource` struct with fields:
  - `SourceID` (string) - reference to source in sources.toml
  - `Weight` (float64, optional) - for weighted aggregation
  - `Required` (bool) - whether source is required
- Define validation method `Validate() error`
- Add JSON/TOML tags for serialization

**Verification**:
- Code compiles
- Types are exported and documented

---

### Step 1.3: Create Configuration Loader

**Goal**: Implement TOML configuration loading

**Files to Create**:
- `unified_config/config.go`

**Implementation**:
- Define `Config` struct containing:
  - `Sources` (map[string]SourceConfig) - source ID to config
  - `AssetPairs` ([]AssetPairConfig) - list of asset pairs
  - `GlobalStalenessThresholdSeconds` (int) - global staleness threshold
- Implement `LoadConfig(sourcesPath, assetPairsPath string) (*Config, error)`:
  - Read `sources.toml` file
  - Parse TOML into map[string]SourceConfig
  - Read `asset_pairs.toml` file
  - Parse TOML into []AssetPairConfig
  - Validate all sources and pairs
  - Return Config struct
- Implement `Validate() error` on Config:
  - Check all asset pair sources reference valid source IDs
  - Validate each source config
  - Validate each asset pair config

**Verification**:
- Code compiles
- Can load valid TOML files
- Returns errors for invalid configs

---

### Step 1.4: Write Tests for Configuration System

**Goal**: Test configuration parsing and validation (TDD: write tests first, then ensure implementation passes)

**Files to Create**:
- `unified_config/sources_test.go`
- `unified_config/asset_pairs_test.go`
- `unified_config/config_test.go`

**Note**: If following strict TDD, write these tests before implementing the types in Steps 1.1-1.3. Otherwise, write tests now to verify existing implementation.

**Test Cases**:
- Valid TOML parsing for sources
- Valid TOML parsing for asset pairs
- Invalid source config (missing required fields)
- Invalid asset pair config (missing required fields)
- Invalid source reference in asset pair
- Duplicate source IDs
- Invalid batch strategy for source type
- Invalid aggregation method
- Validation errors are descriptive

**Verification**:
- All tests pass
- Test coverage > 80%

---

### Step 1.5: Create Cache Types

**Goal**: Define cache data structures and errors

**Files to Create**:
- `unified_config/cache/types.go`

**Implementation**:
- Define `CacheEntry` struct:
  - `Value` (interface{}) - cached value (price or []byte)
  - `Timestamp` (time.Time) - when cached
  - `SourceID` (string) - source identifier
- Define `CacheKey` type (string) with helper:
  - `NewPriceCacheKey(queryID, sourceID string) CacheKey`
  - `NewContractCacheKey(queryID, sourceID, callKey string) CacheKey`
- Define cache errors:
  - `ErrCacheMiss`
  - `ErrCacheExpired`
  - `ErrCacheStale`

**Verification**:
- Code compiles
- Cache key format matches specification

---

### Step 1.6: Implement Price Cache

**Goal**: Thread-safe cache with TTL and staleness checking

**Files to Create**:
- `unified_config/cache/price_cache.go`
- `unified_config/cache/price_cache_test.go` (write tests first - TDD)

**Implementation**:
- Define `PriceCache` struct:
  - `mu` (sync.RWMutex) - for thread-safety
  - `entries` (map[CacheKey]CacheEntry)
  - `globalStalenessThreshold` (time.Duration)
  - `sourceTTLs` (map[string]time.Duration) - per-source TTLs
- Implement methods:
  - `NewPriceCache(globalStalenessThreshold time.Duration, sourceTTLs map[string]time.Duration) *PriceCache`
  - `Get(key CacheKey) (interface{}, error)`:
    - Check if key exists
    - Check if expired (TTL)
    - Check if stale (staleness threshold)
    - Return value or appropriate error
  - `Set(key CacheKey, value interface{}, sourceID string) error`:
    - Thread-safe write
    - Store with current timestamp
  - `Invalidate(key CacheKey) error`
  - `Clear() error`
- Use RWMutex for concurrent reads

**Verification**:
- Code compiles
- Thread-safe operations

---

### Step 1.7: Write Tests for Cache

**Goal**: Test cache operations and thread-safety

**Files to Create**:
- `unified_config/cache/price_cache_test.go`

**Test Cases**:
- Get on empty cache returns ErrCacheMiss
- Set then Get returns value
- Get after TTL expiration returns ErrCacheExpired
- Get after staleness threshold returns ErrCacheStale (but value still available)
- Concurrent reads don't block
- Concurrent writes are safe
- Invalidate removes entry
- Clear removes all entries
- Different TTLs per source work correctly

**Verification**:
- All tests pass
- Race detector passes (`go test -race`)

---

### Step 1.8: Create Result Aggregator

**Goal**: Implement result aggregation logic

**Files to Create**:
- `unified_config/orchestrator/result_aggregator.go`

**Implementation**:
- Define `PriceResult` struct:
  - `Price` (float64)
  - `SourceID` (string)
  - `Timestamp` (time.Time)
  - `Weight` (float64, optional)
- Implement aggregation methods:
  - `AggregateMedian(results []PriceResult) (float64, error)`:
    - Sort by price
    - Return middle value
    - Handle even number of results
  - `AggregateMean(results []PriceResult) (float64, error)`:
    - Calculate average
  - `AggregateWeighted(results []PriceResult) (float64, error)`:
    - Weighted average using weights
- Handle edge cases:
  - Empty results
  - Single result
  - Invalid prices (NaN, Inf)

**Verification**:
- Code compiles
- Aggregation logic is correct

---

### Step 1.9: Write Tests for Result Aggregator

**Goal**: Test aggregation methods

**Files to Create**:
- `unified_config/orchestrator/result_aggregator_test.go`

**Test Cases**:
- Median with odd number of results
- Median with even number of results
- Mean calculation
- Weighted average calculation
- Empty results returns error
- Single result works
- NaN/Inf values handled correctly

**Verification**:
- All tests pass

---

### Step 1.10: Implement Query Orchestrator (Basic)

**Goal**: Basic orchestrator that routes queries and checks cache

**Files to Create**:
- `unified_config/orchestrator/query_orchestrator.go`
- `unified_config/orchestrator/query_orchestrator_test.go` (write tests first - TDD)

**System Integration Note**: This component integrates Config, Cache, and SourceHandlers. Ensure interfaces align with how these components will be used in Phase 2 (batching) and Phase 3 (integration).

**Implementation**:
- Define `QueryOrchestrator` struct:
  - `config` (*Config)
  - `cache` (*PriceCache)
  - `sourceHandlers` (map[string]SourceHandler) - interface for fetching
- Define `SourceHandler` interface:
  - `FetchPrice(queryID, sourceID string) (float64, error)`
- Implement `NewQueryOrchestrator(config *Config, cache *PriceCache) *QueryOrchestrator`
- Implement `GetPrice(queryID string) (float64, error)`:
  - Find asset pair by queryID
  - For each source in pair:
    - Check cache first
    - If cache hit and fresh, return
    - If cache stale, trigger update (async) but return cached value
    - If cache miss, fetch immediately
  - Aggregate results from multiple sources
  - Return aggregated price
- For now, use placeholder handlers that return errors

**Verification**:
- Code compiles
- Cache checking logic works

---

### Step 1.11: Write Tests for Query Orchestrator (Basic)

**Goal**: Test orchestrator routing and cache logic

**Files to Create**:
- `unified_config/orchestrator/query_orchestrator_test.go`

**Test Cases**:
- Cache hit returns immediately
- Cache miss triggers fetch
- Stale cache triggers update but returns cached value
- Multiple sources aggregated correctly
- Error handling when sources fail
- MinSources validation

**Verification**:
- All tests pass

---

## Phase 2: Batching System

### Step 2.1: Create Batch Types

**Goal**: Define types for batch operations

**Files to Create**:
- `unified_config/batch/types.go`

**Implementation**:
- Define `PendingQuery` struct:
  - `QueryID` (string)
  - `SourceID` (string)
  - `Timestamp` (time.Time) - when query was requested
- Define `BatchGroup` struct:
  - `GroupID` (string)
  - `SourceID` (string)
  - `PendingQueries` ([]PendingQuery)
  - `LastUpdate` (time.Time)
- Define `BatchResult` struct:
  - `QueryID` (string)
  - `SourceID` (string)
  - `Value` (interface{})
  - `Error` (error)

**Verification**:
- Code compiles

---

### Step 2.2: Create Batch Collector

**Goal**: Collect pending queries for batching

**Files to Create**:
- `unified_config/batch/collector.go`

**Implementation**:
- Define `BatchCollector` struct:
  - `mu` (sync.Mutex)
  - `groups` (map[string]*BatchGroup) - keyed by groupID
- Implement methods:
  - `NewBatchCollector() *BatchCollector`
  - `AddQuery(queryID, sourceID, groupID string) error`:
    - Add query to appropriate batch group
    - Thread-safe
  - `GetGroup(groupID string) (*BatchGroup, error)`:
    - Return batch group
    - Clear pending queries after retrieval
  - `GetAllGroups() map[string]*BatchGroup`:
    - Return all groups with pending queries
    - Clear after retrieval

**Verification**:
- Code compiles
- Thread-safe operations

---

### Step 2.3: Write Tests for Batch Collector

**Goal**: Test batch collection logic

**Files to Create**:
- `unified_config/batch/collector_test.go`

**Test Cases**:
- AddQuery adds to correct group
- GetGroup returns and clears queries
- Multiple groups work independently
- Thread-safe concurrent access

**Verification**:
- All tests pass

---

### Step 2.4: Implement Batch Scheduler

**Goal**: Schedule batch updates per source

**Files to Create**:
- `unified_config/batch/scheduler.go`

**Implementation**:
- Define `BatchScheduler` struct:
  - `config` (*Config)
  - `collector` (*BatchCollector)
  - `cache` (*PriceCache)
  - `sourceHandlers` (map[string]SourceHandler)
  - `timers` (map[string]*time.Timer) - per source
  - `mu` (sync.Mutex)
- Implement `NewBatchScheduler(config *Config, collector *BatchCollector, cache *PriceCache) *BatchScheduler`
- Implement `Start() error`:
  - For each batchable source:
    - Create timer with UpdateIntervalSeconds
    - Trigger immediate update on startup
    - Schedule periodic updates
- Implement `updateSource(sourceID string) error`:
  - Get pending queries for source's batch group
  - Execute batch fetch via handler
  - Store results in cache
  - Clear pending queries
- Implement `TriggerImmediateUpdate(sourceID string) error`:
  - Immediately update source (for stale cache)
  - Reset timer
- Implement `Stop() error`:
  - Stop all timers

**Verification**:
- Code compiles
- Scheduler can start and stop

---

### Step 2.5: Write Tests for Batch Scheduler

**Goal**: Test scheduler timing and triggers

**Files to Create**:
- `unified_config/batch/scheduler_test.go`

**Test Cases**:
- Startup triggers immediate update for all batchable sources
- Periodic updates occur at correct intervals
- Immediate update resets timer
- Stale cache triggers immediate update
- Stop cancels all timers
- Multiple sources scheduled independently

**Verification**:
- All tests pass
- Use fake time for deterministic tests

---

### Step 2.6: Implement REST Query Parameter Batching

**Goal**: Batch multiple queries in URL query parameters

**Files to Create**:
- `unified_config/batch/rest_query_param.go`

**Implementation**:
- Define `QueryParamBatcher` struct:
  - `baseURL` (string)
  - `paramName` (string) - query parameter name (e.g., "ids")
  - `separator` (string) - separator for values (e.g., ",")
- Implement `NewQueryParamBatcher(baseURL, paramName, separator string) *QueryParamBatcher`
- Implement `BatchFetch(queryIDs []string) (map[string]float64, error)`:
  - Build URL with all query IDs in query parameter
  - Make HTTP GET request
  - Parse response (JSON)
  - Extract individual prices by query ID
  - Return map[queryID]price
- Handle response format (may need source-specific parsing)

**Verification**:
- Code compiles
- Can batch multiple IDs in URL

---

### Step 2.7: Implement REST Body Batching

**Goal**: Batch multiple queries in request body

**Files to Create**:
- `unified_config/batch/rest_body.go`

**Implementation**:
- Define `BodyBatcher` struct:
  - `baseURL` (string)
  - `endpoint` (string)
- Implement `NewBodyBatcher(baseURL, endpoint string) *BodyBatcher`
- Implement `BatchFetch(queryIDs []string) (map[string]float64, error)`:
  - Build request body with query IDs (JSON)
  - Make HTTP POST request
  - Parse response (JSON)
  - Extract individual prices by query ID
  - Return map[queryID]price
- Handle response format (may need source-specific parsing)

**Verification**:
- Code compiles
- Can batch multiple IDs in body

---

### Step 2.8: Implement REST Batch Handler

**Goal**: Route to appropriate REST batching strategy

**Files to Create**:
- `unified_config/batch/rest_handler.go`

**Implementation**:
- Define `RESTBatchHandler` struct:
  - `queryParamBatcher` (*QueryParamBatcher, optional)
  - `bodyBatcher` (*BodyBatcher, optional)
  - `strategy` (string)
- Implement `NewRESTBatchHandler(sourceConfig SourceConfig) (*RESTBatchHandler, error)`:
  - Create appropriate batcher based on BatchStrategy
  - Return handler
- Implement `BatchFetch(queryIDs []string) (map[string]float64, error)`:
  - Route to appropriate batcher
  - Return results
- Integrate with HTTP client (use existing or create new)

**Verification**:
- Code compiles
- Routes to correct strategy

---

### Step 2.9: Write Tests for REST Batch Handler

**Goal**: Test REST batching strategies

**Files to Create**:
- `unified_config/batch/rest_handler_test.go`

**Test Cases**:
- Query param batching builds correct URL
- Body batching sends correct request
- Response parsing extracts individual prices
- Error handling for HTTP failures
- Error handling for invalid responses
- Use httptest for mocking

**Verification**:
- All tests pass

---

### Step 2.10: Create Batch Cache for Contract Calls

**Goal**: Cache raw contract call results

**Files to Create**:
- `custom_query/contracts/contract_reader/batch_cache.go`

**Implementation**:
- Define `BatchCache` struct:
  - `mu` (sync.RWMutex)
  - `results` (map[string][]byte) - keyed by CallID
  - `timestamps` (map[string]time.Time)
- Implement methods:
  - `NewBatchCache() *BatchCache`
  - `Set(callID string, result []byte) error`
  - `Get(callID string) ([]byte, error)`
  - `Clear() error`
- Similar to PriceCache but stores []byte instead of interface{}

**Verification**:
- Code compiles

---

### Step 2.11: Write Tests for Batch Cache

**Goal**: Test contract call result caching

**Files to Create**:
- `custom_query/contracts/contract_reader/batch_cache_test.go`

**Test Cases**:
- Set and Get work correctly
- Get on miss returns error
- Thread-safe operations
- Clear removes all entries

**Verification**:
- All tests pass

---

### Step 2.12: Implement Multicall3 Executor

**Goal**: Execute batched contract calls via Multicall3

**Files to Create**:
- `custom_query/contracts/contract_reader/multicall3_executor.go`

**Implementation**:
- Study existing contract reader to understand:
  - How to make contract calls
  - Multicall3 interface
  - Chain connection setup
- Define `Multicall3Executor` struct:
  - `chainID` (uint64)
  - `multicallAddress` (common.Address)
  - `client` (ethclient.Client or similar)
- Define `Call` struct:
  - `Target` (common.Address)
  - `CallData` ([]byte)
  - `CallID` (string) - for result routing
- Implement `Execute(calls []Call) (map[string][]byte, error)`:
  - Build Multicall3 aggregate() call
  - Execute on chain
  - Parse results
  - Map results back to CallIDs
  - Return map[CallID]result

**Verification**:
- Code compiles
- Can execute Multicall3 calls (may need integration test)

---

### Step 2.13: Write Tests for Multicall3 Executor

**Goal**: Test Multicall3 execution

**Files to Create**:
- `custom_query/contracts/contract_reader/multicall3_executor_test.go`

**Test Cases**:
- Single call execution
- Multiple calls execution
- Result routing by CallID
- Error handling for failed calls
- Use mock client if possible

**Verification**:
- All tests pass

---

### Step 2.14: Implement Batch Collector for Contract Calls

**Goal**: Collect contract calls for batching

**Files to Create**:
- `custom_query/contracts/contract_reader/batch_collector.go`

**Implementation**:
- Define `ContractBatchCollector` struct:
  - `mu` (sync.Mutex)
  - `batches` (map[string]map[string][]Call) - keyed by chainID, then batchGroup
- Implement methods:
  - `NewContractBatchCollector() *ContractBatchCollector`
  - `AddCall(chainID, batchGroup, callID string, target common.Address, callData []byte) error`
  - `GetBatch(chainID, batchGroup string) ([]Call, error)`:
    - Return calls for batch
    - Clear after retrieval
  - `GetAllBatches() map[string]map[string][]Call`:
    - Return all batches
    - Clear after retrieval

**Verification**:
- Code compiles

---

### Step 2.15: Write Tests for Contract Batch Collector

**Goal**: Test contract call collection

**Files to Create**:
- `custom_query/contracts/contract_reader/batch_collector_test.go`

**Test Cases**:
- AddCall adds to correct batch
- GetBatch returns and clears calls
- Multiple chains/groups work independently
- Thread-safe operations

**Verification**:
- All tests pass

---

### Step 2.16: Implement BatchedReader Wrapper

**Goal**: Wrap existing Reader to intercept and batch calls

**Files to Create**:
- `custom_query/contracts/contract_reader/batched_reader.go`

**Implementation**:
- Study existing `Reader` interface
- Define `BatchedReader` struct:
  - `reader` (Reader) - wrapped reader
  - `collector` (*ContractBatchCollector)
  - `executor` (*Multicall3Executor)
  - `cache` (*BatchCache)
  - `enabled` (bool) - whether batching is enabled
- Implement `Reader` interface methods:
  - Intercept `Read()` calls
  - Extract call key from context (see Step 2.17)
  - If batching enabled:
    - Add call to collector
    - Check cache for result
    - If cache miss, return error to trigger batch execution
  - If batching disabled:
    - Forward to wrapped reader
- Implement `ExecuteBatch(chainID, batchGroup string) error`:
  - Get calls from collector
  - Execute via Multicall3Executor
  - Store results in cache
  - Return errors

**Verification**:
- Code compiles
- Implements Reader interface

---

### Step 2.17: Update ParallelFetcher to Pass Call Keys

**Goal**: Pass call keys through context for BatchedReader

**Files to Modify**:
- `custom_query/combined/combined_handler/handlers.go`

**Implementation**:
- Study existing `ParallelFetcher` implementation
- Define context key type:
  - `type callKeyType string`
  - `const callKey callKeyType = "callKey"`
- Update `ParallelFetcher` to:
  - Generate unique call key for each contract call
  - Pass call key in context when creating Reader
  - Format: `{queryID}-{sourceID}-{callKey}` or similar
- Ensure handlers can extract call key if needed

**Verification**:
- Code compiles
- Existing tests still pass
- Call keys are passed correctly

---

### Step 2.18: Write Tests for BatchedReader

**Goal**: Test call interception and batching

**Files to Create**:
- `custom_query/contracts/contract_reader/batched_reader_test.go`

**Test Cases**:
- Disabled batching forwards to wrapped reader
- Enabled batching collects calls
- Cache hit returns immediately
- Cache miss triggers batch execution
- Results routed correctly by CallID
- Multiple calls per handler work
- Use mock Reader for testing

**Verification**:
- All tests pass

---

## Phase 3: Integration

### Step 3.1: Document Current Sources from Original Pricefeed Setup

**Goal**: Create a comprehensive list of all sources currently used in the original pricefeed setup for later migration

**Files to Examine**:
- `constants/static_exchange_details.go` - Lists all exchange sources
- `constants/static_market_params_config.go` - Shows which exchanges are used for each pair
- `pricefeed/client/sources/*` - Individual exchange implementations
- `custom_query/config.go` - Custom query REST endpoints, contract sources, RPC sources

**Action**:
- Create a document listing all sources that need migration:
  - **Exchange Sources** (from `StaticExchangeDetails`):
    - Binance, BinanceUS, Bitfinex, Bitstamp, Coinbase Rates, Crypto.com, Gate, Huobi, Kraken, KuCoin, MEXC, OKX
    - Test exchanges (test_exchange, test_volatile_exchange, test_fixed_price_exchange)
  - **Custom Query REST Sources**: Document all endpoints from `custom_query_config.toml`
  - **Contract Sources**: Document all contract readers and chains
  - **RPC Sources**: Document all RPC endpoints
- For each source, note:
  - Current API endpoint/URL
  - Whether it supports batching (query_param, body) or needs on-demand calls
  - Symbol/ticker mapping format
  - Any special configuration needed
- **Key Insight**: The sources used in original pricefeed are the same sources used in custom_query setup, so migration should be straightforward

**Deliverable**:
- Create `unified_config/migration/SOURCE_MIGRATION_LIST.md` documenting all sources

**Verification**:
- Complete list of all sources documented
- Ready for API investigation and migration planning

---

### Step 3.2: Investigate Exchange APIs for Batching Support

**Goal**: Check each exchange API to determine if it supports batching or needs on-demand calls

**Action**:
- For each exchange in the migration list (from Step 3.1):
  - Review exchange API documentation
  - Check if exchange supports:
    - **Query parameter batching**: Multiple symbols in URL params (e.g., `?symbols=BTC,ETH,SOL`)
    - **Body batching**: Multiple symbols in POST request body
    - **On-demand only**: Single symbol per request
  - Document findings in `SOURCE_MIGRATION_LIST.md`
  - Note the exact API endpoint format for each approach
- **Important**: We are NOT using ticker endpoints - we're using direct price/symbol endpoints
- Focus on endpoints that return current prices for specific symbols/pairs

**Deliverable**:
- Updated `SOURCE_MIGRATION_LIST.md` with batching capability for each source

**Verification**:
- Each source has documented batching capability
- API endpoints identified (not ticker endpoints)

---

### Step 3.3: Create Migration Source Configs

**Goal**: Generate REST source configurations for all exchanges based on API investigation

**Files to Create/Modify**:
- `unified_config/migration/exchange_migration.go` - Migration logic
- Update `SOURCE_MIGRATION_LIST.md` with generated config examples

**Implementation**:
- For each exchange source:
  - Create `SourceConfig` with `Type = "rest"`
  - Set `BaseURL` from exchange API
  - Set `Endpoint` to price/symbol endpoint (NOT ticker endpoint)
  - Set `Batchable = true/false` based on Step 3.2 findings
  - Set `BatchStrategy` if batchable ("query_param" or "body")
  - Set `UpdateIntervalSeconds` for batchable sources
  - Set `CacheTTLSeconds` appropriately
  - Add any exchange-specific fields needed for symbol mapping
- **Key Point**: All exchanges become REST sources - no special handling needed
- Use the same REST handlers that custom_query sources use

**Verification**:
- All exchange sources have valid SourceConfig structures
- Configs match API capabilities from Step 3.2

---

### Step 3.4: Write Tests for Migration Utilities

**Goal**: Test migration from old configs to unified format

**Files to Create**:
- `unified_config/migration/exchange_migration_test.go`

**Test Cases**:
- Market params migration produces correct REST source configs
- Market params migration produces correct asset pairs
- Custom query config migration produces correct sources and pairs
- All fields preserved during migration
- Invalid old configs handled gracefully
- Exchange sources correctly converted to REST sources
- Symbol/ticker mappings preserved

**Verification**:
- All tests pass
- Migration accuracy verified

---

### Step 3.5: Implement Migration Utilities

**Goal**: Convert old configs to new unified format

**Files to Create**:
- `unified_config/migration/exchange_migration.go`

**Implementation**:
- Implement `MigrateMarketParams(marketParamsPath string) ([]SourceConfig, []AssetPairConfig, error)`:
  - Read `market_params.toml`
  - Use exchange source configs from Step 3.3
  - Convert market params to `[]AssetPairConfig`
  - Map exchange sources to new REST source IDs
  - **Remove ticker references** - use direct price endpoints
- Implement `MigrateCustomQueryConfig(customQueryPath string) ([]SourceConfig, []AssetPairConfig, error)`:
  - Read `custom_query_config.toml`
  - Extract REST endpoints → `sources.toml` format
  - Extract contract sources → `sources.toml` format
  - Extract RPC sources → `sources.toml` format
  - Extract queries → `asset_pairs.toml` format
- **Key principles**:
  - All sources (exchanges, REST APIs, contracts, RPC) go into `sources.toml`
  - All asset pairs go into `asset_pairs.toml`
  - No special exchange handling - everything uses unified REST/contract/RPC handlers
  - Ticker system completely removed

**Verification**:
- Code compiles
- Migration produces valid unified configs
- Migrated configs match original functionality

---

### Step 3.6: Integrate Orchestrator with Batch Scheduler

**Goal**: Connect orchestrator to batch scheduler

**Files to Modify**:
- `unified_config/orchestrator/query_orchestrator.go`

**System Integration Note**: This step connects Phase 1 (orchestrator) with Phase 2 (batching). Ensure the integration is clean and follows the interfaces defined in previous steps. Review how batchable vs non-batchable sources are handled.

**Implementation**:
- Add `scheduler` (*BatchScheduler) to QueryOrchestrator
- Update `GetPrice()`:
  - For batchable sources:
    - Add query to batch collector
    - Check cache
    - If stale, trigger immediate update via scheduler
  - For non-batchable sources:
    - Fetch immediately
- Register source handlers:
  - REST sources (including exchanges) → RESTBatchHandler or on-demand REST handler
  - Contract sources → BatchedReader
  - All sources use unified handler system (no special exchange handler)

**Verification**:
- Code compiles
- Orchestrator uses scheduler correctly

---

### Step 3.7: Update App.go to Load Unified Config

**Goal**: Replace old config loading with unified config

**Files to Modify**:
- `app.go`

**Implementation**:
- Find where `market_params.toml` and `custom_query_config.toml` are loaded
- Replace with:
  - `config.LoadConfig("config/sources.toml", "config/asset_pairs.toml")`
- Initialize:
  - PriceCache
  - BatchCollector
  - BatchScheduler
  - QueryOrchestrator
- Start BatchScheduler
- Pass orchestrator to components that need it

**Verification**:
- Code compiles
- App starts successfully

---

### Step 3.8: Update Reporter to Use Orchestrator

**Goal**: Replace custom query calls with orchestrator

**Files to Modify**:
- `reporter/client/median.go`

**Implementation**:
- Find `customquery.FetchPrice()` calls
- Replace with `orchestrator.GetPrice(queryID)`
- Update error handling if needed
- Ensure all query IDs are correct

**Verification**:
- Code compiles
- Reporter uses orchestrator

---

### Step 3.9: Update Test Mode

**Goal**: Test mode validates unified configs

**Files to Modify**:
- `cmd/test_mode.go`

**Implementation**:
- Update to load unified configs
- Validate configs on startup
- Test price fetching via orchestrator
- Display config validation results

**Verification**:
- Code compiles
- Test mode works with new configs

---

### Step 3.10: Write Integration Tests

**Goal**: End-to-end tests for full system

**Files to Create**:
- `unified_config/integration_test.go`

**Test Cases**:
- Full price fetch flow with batching
- Multiple sources per pair
- Cache behavior in real flows
- Batch scheduler triggers updates
- REST batching works end-to-end
- Multicall3 batching works end-to-end
- Error scenarios and fallbacks
- Use test configs and mocks

**Verification**:
- All integration tests pass

---

## Phase 4: Migration & Cleanup

### Step 4.1: Generate Default Configs

**Goal**: Create default sources.toml and asset_pairs.toml

**Files to Create**:
- `unified_config/default_sources.go`
- `unified_config/default_asset_pairs.go`

**Implementation**:
- Implement `GenerateDefaultSources() ([]SourceConfig, error)`:
  - Convert all StaticExchangeDetails to SourceConfig with `Type = "rest"`
  - Use API investigation results from Step 3.2 to determine batchable status
  - Set `BatchStrategy` for batchable exchanges
  - Set `BaseURL` and `Endpoint` (NOT ticker endpoints - use direct price endpoints)
  - Convert all custom query REST sources to SourceConfig
  - Convert all custom query contract sources to SourceConfig
  - Convert all custom query RPC sources to SourceConfig
  - Set appropriate batchable flags
  - Set update intervals
  - **Note**: All sources (exchanges, REST, contracts, RPC) use unified system - no tickers
- Implement `GenerateDefaultAssetPairs() ([]AssetPairConfig, error)`:
  - Convert market_params to AssetPairConfig
  - Reference correct source IDs
- Write TOML files to `config/` directory

**Verification**:
- Code compiles
- Generated configs are valid

---

### Step 4.2: Write Tests for Default Config Generation

**Goal**: Test config generation

**Files to Create**:
- `unified_config/default_config_test.go`

**Test Cases**:
- Generated sources match original functionality
- Generated asset pairs match original functionality
- All sources have valid configs
- All pairs reference valid sources

**Verification**:
- All tests pass

---

### Step 4.3: Verify All Functionality Migrated

**Goal**: Ensure nothing is missing

**Action**:
- Compare old system behavior with new system
- Test all existing price feeds
- Verify all exchanges work (as REST sources, not tickers)
- Verify all custom queries work
- Verify batching works for batchable exchanges
- Check error handling
- Verify performance (batching reduces calls)

**Verification**:
- All functionality works
- No regressions

---

### Step 4.4: Remove Old Config Usage and Ticker System

**Goal**: Clean up old config files, code, and ticker-based system

**Files to Remove/Modify**:
- Remove direct usage of `market_params.toml`
- Remove direct usage of `custom_query_config.toml`
- Remove ticker-based exchange code (no longer needed)
- Remove `StaticExchangeDetails` usage (replaced by unified sources)
- Keep files for reference but don't load them
- Update documentation

**Verification**:
- Code compiles
- No references to old configs remain (except migration code)
- No ticker endpoint code remains
- All sources use unified REST/contract/RPC handlers

---

### Step 4.5: Final Documentation

**Goal**: Document the new system

**Files to Create/Update**:
- Update README if exists
- Document config file formats
- Document how to add new sources
- Document how to add new asset pairs

**Verification**:
- Documentation is complete and accurate

---

## Summary

This breakdown provides **45 specific steps** that can be executed one at a time. Each step:
- Has a clear goal
- Lists specific files to create/modify
- Provides implementation details
- Includes verification criteria

**Dependencies**:
- Steps 1.1-1.4 must be completed before 1.5-1.11
- Steps 2.1-2.5 must be completed before 2.6-2.9
- Steps 2.10-2.17 must be completed before 2.18
- Phase 1 must be completed before Phase 2
- Phase 2 must be completed before Phase 3
- Phase 3 must be completed before Phase 4

**Testing Strategy** (Test-Driven Development):
- **Write tests FIRST** before implementing functionality (TDD approach)
- Each component is tested immediately after or during implementation
- Tests define expected behavior and drive implementation
- Integration tests verify end-to-end functionality
- All tests must pass before moving to next step
- Use mocks/stubs for external dependencies
- Run `go test ./...` frequently during development

**Estimated Complexity**:
- Steps 1.1-1.11: Medium (foundation)
- Steps 2.1-2.18: High (batching is complex)
- Steps 3.1-3.10: Medium (integration)
- Steps 4.1-4.5: Low (cleanup)
