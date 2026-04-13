# Custom Query Consolidation - Implementation Plan

This document describes a concrete, step-by-step plan to make `custom_query` the unified pricing path and to reduce external calls via:
- background caching for batchable HTTP/API sources,
- request-time aggregation (`FetchPrice`) with `min_responses` semantics based on cache freshness.

It reflects the decisions discussed:
- Cache writers are **batched-only** for the values stored in `MarketToExchangePrices` (in this v1, those values come from background-refreshing batchable HTTP/API sources).
- Request-time cache miss for **cache-backed (batchable)** sources is treated as **source unavailable** (no live fallback); the background refreshers are responsible for keeping caches fresh.
- Freshness TTL for cached HTTP/API values matches `MarketToExchangePrices` TTL.
- `MarketToExchangePrices` cache keying uses `exchangeId = endpoint template name` for batchable HTTP/API sources.

---

## 0) Define “what is a marketId for this request?”

Assumption (confirmed): *every* `custom_query` `QueryConfig.ID` that will be priced has a corresponding chain market param (`MarketParam.QueryData` matches).

Implementation task:
1. Add a single mapping function:
   - `queryId -> marketId` (uint32), by iterating configured market params and matching `QueryData == hex(queryId)`.
2. Fail fast at startup if any queryId in `custom_query_config.toml` cannot map to a market param.

Deliverable:
- `ResolveMarketIdForQuery(queryIDHex string) (marketId uint32, err error)`

---

## 1) Config model: unify “batchable sources” and “asset-specific cache policy”

### 1.1 Extend endpoint templates with batchability
Update the custom query config structs:
- `EndpointTemplate` add:
  - `Batchable bool` (default `false`)

Behavior:
- `Batchable=true` means the source supports a background “bulk refresh” mode.
- For cached availability checks, `exchangeId` is always the **endpoint template name**.

### 1.2 Add per-query/per-market cache policy
Update `EndpointConfig` (per `[[queries.<id>.endpoints]]` entry):
- add one field that defines whether this asset should use cached results or use on-demand:
  - `UseCache bool` (or `CachePolicy = "cached" | "live"`; choose one style and apply consistently).

Rules:
- For `Batchable=true` sources:
  - `UseCache=true` => request-time reads from `MarketToExchangePrices` for `(marketId, exchangeId=endpointName)` and counts success/failure based on freshness.
  - `UseCache=false` => request-time will fetch on-demand for this asset pair/source.
- For `Batchable=false` sources:
  - `UseCache` should be ignored or forced `false` (depending on how strict you want validation).

Deliverables:
- Schema / parser updates in `custom_query/config.go`
- Validation logic to reject invalid combinations early.

---

## 2) Cache changes: extend `MarketToExchangePrices` with per-source reads

### 2.1 Add a new method for per-(market, source) freshness checks
Extend the server cache (`server/types/pricefeed/market_to_exchange_prices.go`) with:
- `GetValidPriceForExchange(marketId uint32, exchangeId string, readTime time.Time) (price uint64, ok bool)`

Semantics:
- Use the same cutoff logic as `GetValidMedianPrices` (same TTL).
- If the stored exchange price is fresh enough, return `(price, true)`.
- Otherwise `(0, false)`.

### 2.2 Ensure cache writers follow “batched-only writers” contract
Background refreshers are the only writers for:
- batchable HTTP/API sources,
- batchable HTTP/API source values stored into `MarketToExchangePrices`.

Request-time should never call `UpdatePrices` to populate `MarketToExchangePrices`.

Deliverables:
- new method implementation + tests.

---

## 3) Keep contract calls unchanged (no step cache)

Contract endpoints and contract handlers should work as they do today:
- contract handlers execute `reader.ReadContract(...)` on-demand,
- handlers still use `MarketToExchangePrices` for `UsdViaID` conversions,
- contract success therefore depends on the freshness of the required USD anchor markets in `MarketToExchangePrices`.

In this v1:
- do not introduce a `ContractStepCache`,
- do not introduce a multicall3 step refresher,
- do not write contract-derived prices into `MarketToExchangePrices`.

Deliverables:
- verify contract handlers still behave correctly when `MarketToExchangePrices` is updated only by background-refreshing batchable HTTP/API sources.

---

## 4) Implement background refresher for batchable HTTP/API sources

### 4.1 Identify batchable endpoints
From config:
- endpoints with `Batchable=true` in `[endpoints.<name>]`.

### 4.2 Refresh schedule flag
Add a flag (or config value) to control refresh frequency for batchable sources:
- e.g. `--batchable-refresh-interval-ms` (exact naming TBD)

### 4.3 Bulk fetch logic
For each batchable endpoint:
1. determine which assets/market pairs it supports (from config endpoints usage)
2. perform one bulk HTTP request per refresh cycle (using the endpoint template and whatever “batchable endpoint shape” you implement)
3. parse response and produce:
   - price per `marketId`
4. update `MarketToExchangePrices` with:
   - `exchangeId = endpoint template name`
   - `lastUpdatedAt = now`

Freshness:
- cache entries will be considered valid until `maxPriceAge` cutoff.

Deliverables:
- background refresh worker for HTTP/API sources
- parsing/mapping strategy for bulk endpoints

---

## 5) Refactor `custom_query.FetchPrice` to use cache-first per endpoint

### 5.1 Update request-time dispatch model
Refactor `FetchPrice` to, per endpoint config:
- decide which mode to use:
  - if `UseCache=true` and endpoint `Batchable=true`:
    - read per-source price from `MarketToExchangePrices.GetValidPriceForExchange(...)`
    - cache hit => success
    - cache miss/stale => error => contributes to `min_responses` failure
    - do not fetch live to fill cache miss
  - if `UseCache=false`:
    - fetch on-demand, but never write to caches

### 5.2 Contract sources request-time computation
Contract endpoints:
- remain on-demand (no cache-first mode for the contract call itself),
- rely on `MarketToExchangePrices` for `UsdViaID` conversions as before.

Therefore, a contract source becomes unavailable when:
- the `UsdViaID` anchor market prices are missing/stale in `MarketToExchangePrices`.

### 5.3 Keep existing aggregation semantics
Do not change:
- `MinResponses`
- aggregation (`median`) and response encoding

Deliverables:
- refactored `custom_query/request.go` + related wiring
- new per-source cache read usage

---

## 6) “Only way to get prices”: change reporter and daemon startup

### 6.1 Reporter should route all query ids through `custom_query.FetchPrice`
Today:
- reporter computes medians from `MarketToExchangePrices` for market params,
- and falls back to `custom_query.FetchPrice` only when querydata doesn’t match.

Change plan:
1. In reporter median computation:
   - resolve `queryId` always (already done)
   - route to `custom_query.FetchPrice` for all query ids
2. Ensure the new background refreshers keep `MarketToExchangePrices` populated so `FetchPrice` and any `UsdViaID` conversions have required cached anchors.

### 6.2 Daemon startup wiring
In `app.go`:
- disable or gate the existing `pricefeed` daemon client path when consolidation flag is enabled
- start:
  - batchable HTTP refresher (writers for the cached values used by `UsdViaID` conversions)

Deliverables:
- flags / config to select old vs new pricing mode
- integration-level verification.

---

## 7) Testing strategy (required to avoid subtle cache/min-response bugs)

### 7.1 Unit tests
- `GetValidPriceForExchange` freshness behavior (TTL boundaries)
- cache miss/stale classification for batchable endpoints inside `FetchPrice`
- contract handlers using `UsdViaID` correctly fail when USD anchor markets are stale/missing

### 7.2 Integration tests (request-time behavior)
- For a query requiring `min_responses`:
  - when caches are fresh, success rate >= min_responses
  - when one endpoint cache is stale, source becomes unavailable and aggregation fails if below min
- contract handler:
  - if USD anchor stale/missing => contract source unavailable

### 7.3 “Never miss unless down/rate limited”
- simulate refresher failures:
  - cache refresh fails; old cached data stays fresh until TTL expiry
  - after expiry, fetch should start failing (expected)

---

## 8) Rollout plan

1. Implement config/schema extensions + per-source cache read method on `MarketToExchangePrices`.
2. Add HTTP batchable refresher for endpoint templates marked `Batchable=true`.
3. Implement `FetchPrice` cache-first dispatch (cache miss => source unavailable) for batchable HTTP/API sources.
4. Route all query ids through `custom_query.FetchPrice` and verify contract handlers still work by using fresh cached USD anchors.
5. Enable consolidation mode in staging; monitor:
   - `SuccessRate`
   - cache hit ratio / missing counters
   - HTTP refresh RPC/HTTP failures

Success criteria:
- medians match prior results within expected tolerances
- cache-driven failures only happen during rate-limit/outage events or post-TTL expiration.

