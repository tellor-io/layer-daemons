# Custom Query Consolidation - Architecture & Workflow

This document describes the target architecture and operational workflow for making `custom_query` the unified pricing path.

The core goals:
- One pricing mechanism (reporter routes all query ids through `custom_query.FetchPrice`).
- Background caching for batchable HTTP/API sources.
- Strong freshness semantics:
  - cache TTL uses the same freshness window (`maxPriceAge`),
  - request-time cache miss for cache-backed (batchable) sources means “source unavailable” (no live fallback),
  - contract prices require fresh `UsdViaID` anchor markets from `MarketToExchangePrices`.
- Cache writers are “batched-only”:
  - `MarketToExchangePrices` only stores results produced by background batchable HTTP/API writers.

---

## Components

### 1) Unified config (assets/sources concept)
You may keep two TOML files (e.g. `assets.toml` and `sources.toml`), but the implementation treats them as a single internal model:

- `assets`:
  - defines the universe of `query_id` (asset pair) ids that the chain reports on,
  - defines per-asset/per-source cache policy (batch vs live) when a source supports batching.
- `sources`:
  - defines endpoint templates and whether a source is `batchable=true`,
  - defines how to bulk refresh a batchable source.

The internal goal is to produce a `custom_query` query graph equivalent to today’s:
- `QueryConfig` (query_id)
- `EndpointConfig` (source endpoints)

### 2) Request-time aggregator: `custom_query.FetchPrice`
`FetchPrice(ctx, queryConfig, priceCache)` produces:
- encoded value for the query (median aggregation)
- success/failure classification per source

Refactor focus:
- per endpoint, decide cached vs live according to cache policy and batchability
- for cached endpoints, use “per-source read” from `MarketToExchangePrices`
- for contract endpoints, execute on-demand as today, but require USD anchor freshness from `MarketToExchangePrices`
- on any missing/stale input, the source becomes unavailable and counts against `min_responses`

### 3) Cache layer: `MarketToExchangePrices` (batched-only writers)
Stores cached prices per:
- `marketId` (Tellor market param id)
- `exchangeId` (identity of the pricing source slot)

Keying rule:
- batchable HTTP/API sources:
  - `exchangeId = endpoint template name`

Freshness:
- uses TTL identical to existing median cache logic (`maxPriceAge`)

Extension:
- add per-(market,exchange) freshness read method:
  - `GetValidPriceForExchange(marketId, exchangeId, readTime) (price, ok)`

### 4) Contract execution (unchanged for now)
Contract endpoints execute on-demand and keep the existing contract handler behavior as-is.

Contract success still depends on USD conversions that read from `MarketToExchangePrices` via `UsdViaID`. If the required USD anchor markets are missing/stale, the contract source becomes unavailable for that request.

### 5) Batchable HTTP/API refresher (scheduled)
Background worker responsible for:
- periodically refreshing `batchable=true` endpoint templates
- writing per-asset prices into `MarketToExchangePrices`

Refresh cadence:
- controlled by a flag (so “never miss unless down/rate limited” is achievable)

---

## Data model and keying rules

### MarketId resolution
Every `custom_query` query id should map to a pricefeed/chain market param id:
- `query_id -> marketId` via matching `MarketParam.QueryData`

Assumption:
- all query ids used by reporter exist in market params.

### Cached value identity
- Cached HTTP/API source price uses:
  - `(marketId, exchangeId=endpointName)`

---

## Background workflow (refresh loops)

### A) Batchable HTTP refresh loop
1. Scheduler selects which batchable endpoint templates need refreshing.
2. For each batchable endpoint:
   - determine which assets/market pairs to update based on config and `UseCache=true`
3. Bulk fetch the endpoint once per refresh cycle.
4. Parse results into per-asset prices.
5. For each `marketId`:
   - write to `MarketToExchangePrices` under `exchangeId = endpointName`.

Freshness behavior:
- if the endpoint is down/rate limited, writes fail,
- old values remain until they cross TTL.

### B) Contract execution (on-demand)
Contract calls execute on-demand (no step cache refresh loop in this v1).

If the required USD anchor markets are missing/stale in `MarketToExchangePrices`, contract sources fail and count as unavailable for `min_responses`.

---

## Request-time workflow (`custom_query.FetchPrice`)

For each query id in the reporter cycle:
1. Build `queryConfig` endpoints (same runtime wiring concept as today).
2. For each endpoint/source:
   - if it’s cache-backed batchable and `UseCache=true`:
     - call `MarketToExchangePrices.GetValidPriceForExchange(marketId, exchangeId=endpointName)`
     - if ok => successful result contributes to aggregation
     - if not ok => endpoint contributes an error (counts toward `min_responses` failure)
   - if it’s contract-backed:
    - execute the contract call on-demand as it does today
    - require USD anchor markets fresh in `MarketToExchangePrices` (via `UsdViaID`)
    - if USD anchor markets are missing/stale => contract source error
   - if it’s non-batchable or `UseCache=false`:
     - on-demand fetching may be allowed, but:
       - `UsdViaID` anchor lookups remain cache-only (batched-only writers)
3. Enforce `min_responses`:
   - only successful sources count
4. Aggregate successful values:
   - `median` with spread constraints
5. Encode output to `response_type` (ABI numeric encoding)

---

## End-to-end execution summary

### Diagram (high-level)

```mermaid
flowchart TD
  subgraph BG[Background Refreshers]
    H[Batchable HTTP Refresher] --> C1[MarketToExchangePrices]
  end

  subgraph RT[Request-time Aggregation]
    F[custom_query.FetchPrice] --> C1
    F --> OUT[EncodedValue + aggregation]
  end

  reporter[Reporter] -->|calls for query ids| F
end
```

### Why “miss => unavailable” still works
- Under normal operation, refresh loops keep cache entries fresh.
- Cache misses happen primarily when:
  - sources are down,
  - rate-limited,
  - or after TTL expiry.
- Since request-time does not backfill, `min_responses` naturally prevents using stale/invalid inputs.

---

## Rollout notes (optional)
- Keep old pricefeed daemon gated behind a feature flag until parity is verified.
- Start with:
  - batchable HTTP refresher only,
  - then enable reporter routing for all query ids and verify contract handlers work using fresh USD anchor caches.

