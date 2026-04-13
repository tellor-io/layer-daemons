# Exchange Source Unification - Implementation Plan

This document lays out a **phased**, step-by-step plan to expose legacy pricefeed exchanges as a first-class `endpoint_type` in `custom_query_config.toml`, with **one canonical definition** of ticker, adjustment, and inversion per `(exchange, market)`.

---

## Goal and design principle

**Target outcome**

- Reporter keeps routing SpotPrice work through `custom_query.FetchPrice`.
- `custom_query` can use existing HTTP templates, contracts, combined handlers, and **exchange sources** (Binance, Kraken, etc.) via a new endpoint type.
- Cache-backed pricing continues to use `MarketToExchangePrices` where applicable.

**Single source of truth (non-negotiable)**

- Custom query must **not** introduce a parallel symbol or conversion model.
- An exchange endpoint only selects **which** already-wired exchange leg to use for a query’s market: `exchange_id` plus the market implied by the query (and optional human-readable `market_id` on the endpoint for telemetry/docs).
- **Ticker, `AdjustByMarket`, and `Invert`** come from the same place pricefeed uses for that `(ExchangeId, MarketId)`—e.g. parsed `ExchangeConfigJson` from `market_params` / `StaticMarketParamStartupConfig`, or an explicitly shared in-repo map that stays in sync with that JSON. No free-form TOML ticker that could drift from chain-shaped config.
- Optional: allow a **redundant** `ticker` field in TOML for readability only if `BuildQueryEndpoints` **fails** when it does not equal the canonical ticker (strict equality check).

---

## Scope

### In scope

- New `endpoint_type` (recommended name: `"exchange"`).
- Config validation and runtime wiring that reuse `constants.StaticExchangeDetails`, `pricefeed/client/queryhandler`, and encoder-relevant `MarketConfig` data.
- Phased delivery: schema/validation first, live fetch, then cache + refresher.

### Out of scope (initial phases)

- Deleting the legacy pricefeed package tree in one shot.
- Redesigning chain market-param lifecycle or on-chain protos.
- A second price cache type.

---

## Endpoint shape (TOML)

**Required**

- `endpoint_type = "exchange"`
- `exchange_id` — must match `pricefeed` `ExchangeId` strings (e.g. `Binance`, `CoinbaseRates`).

**Optional**

- `use_cache` — default false until Phase 4; when true, read-only from `MarketToExchangePrices`.
- `market_id` — human-readable pair label for metrics/logging; must not override resolver (`ResolveMarketIdForQuery(query.id)` remains authoritative for chain `MarketId`).

**Deprecated / not authoritative**

- `ticker` — omit in the canonical design, or require exact match to canonical config if present.
- `response_path`, `params`, URL templates — must be empty / ignored for this type.

**Example (canonical — no ticker in file)**

```toml
[[queries.my_query.endpoints]]
endpoint_type = "exchange"
exchange_id = "Binance"
use_cache = true
market_id = "BTC-USD"  # optional; for labels only
```

---

# Phased implementation

Complete phases in order unless noted. Each phase should be mergeable and testable on its own.

---

## Phase 1 — Canonical lookup and config validation

**Objective:** Any `exchange` endpoint in TOML is rejected at `BuildQueryEndpoints` unless `(exchange_id, market_id_for_query)` exists in the **same** exchange–market registry pricefeed uses (ticker + adjust + invert).

### Step 1.1 — Define the lookup API

- Add a small package or `custom_query` helper, e.g. `CanonicalExchangeMarketConfig(exchangeID, marketID uint32) (*MarketConfig, bool)`, backed by one of:
  - parsed data from the same `market_params` / `ExchangeConfigJson` path used at startup, or
  - a single exported map built from that parsing (avoid duplicating `testutil`-only maps in production).
- Document which file or generator is authoritative so ops and code reviews stay aligned.

### Step 1.2 — Extend `EndpointConfig`

- Add `ExchangeID string` with `toml:"exchange_id"`.
- Add optional `Ticker string` with `toml:"ticker"` **only** if implementing strict optional verification; otherwise omit field from docs/defaults.

### Step 1.3 — `BuildQueryEndpoints` branch for `endpoint_type == "exchange"`

- Before REST template resolution:
  - Resolve chain `MarketId` from `query.ID` via `ResolveMarketIdForQuery` (same as existing checks in `app.go`).
  - Parse `exchange_id` into `types.ExchangeId`; reject if unknown to `constants.StaticExchangeDetails` (or your validated allowlist).
  - Call canonical lookup for `(exchange_id, market_id)`; **error** if missing.
  - If TOML `ticker` is present: require `strings.EqualFold` or exact match to `MarketConfig.Ticker` (choose one and document).
  - Reject if `response_path` non-empty, `params` non-empty, or other REST-only fields conflict.

### Step 1.4 — Default / template TOML

- Update `default_custom_query_config.go` (if used) so any example exchange endpoints follow the canonical shape (no spurious ticker unless equality-checked).

### Phase 1 exit criteria

- Invalid pair `(exchange, market for query)` fails fast at daemon startup with a clear error.
- No runtime struct for fetch yet required; unit tests only for validation + lookup.

---

## Phase 2 — Runtime handler struct and build wiring

**Objective:** Produced `QueryConfig` includes a list of exchange handlers carrying everything needed for live and (later) cache paths.

### Step 2.1 — Add `ExchangeHandler` (name flexible)

Fields should include at least:

- `ExchangeID` (`types.ExchangeId`)
- `QueryID`, `MarketId` (string label), `SourceId` (e.g. `"exchange"` or the exchange string for metrics)
- `UseCache` bool
- **Embedded or referenced `MarketConfig`** (ticker, adjust, invert) from Phase 1 lookup—do not store a duplicate string if avoidable.

### Step 2.2 — Extend `QueryConfig`

- Add `ExchangeReaders []ExchangeHandler` (or equivalent slice).

### Step 2.3 — `BuildQueryEndpoints`

- On success for `exchange` endpoints, append to `ExchangeReaders` with fully populated struct.

### Phase 2 exit criteria

- Config load produces `ExchangeReaders`; `FetchPrice` may still ignore them until Phase 3.
- Tests: golden build for one valid and one invalid exchange endpoint.

---

## Phase 3 — Live fetch path (`use_cache=false`)

**Objective:** `FetchPrice` invokes the existing pricefeed query stack for exchange endpoints without writing to `MarketToExchangePrices` at request time.

### Step 3.1 — Adapter / fetch helper

- Implement `fetchFromExchangeEndpoint(ctx, handler, priceCache)`:
  - Load `ExchangeQueryDetails` from `constants.StaticExchangeDetails[handler.ExchangeID]`.
  - Build a minimal `MutableExchangeMarketConfig` (or reuse types expected by `ExchangeQueryHandler`) containing **only** the markets needed for this query: the target market plus any `AdjustByMarket` dependency from canonical `MarketConfig`.
  - Obtain `marketPriceExponent` map for those `MarketId`s from the same static market param source used elsewhere in custom_query.
  - Call `ExchangeQueryHandler.Query` with a shared `RequestHandler` implementation (match pricefeed daemon pattern).

### Step 3.2 — Normalize to float

- Reuse pricefeed/encoder expectations: raw uint64 / exponent → same float convention as other `FetchPrice` paths (aligned with `StaticMarketParamsConfig`).

### Step 3.3 — Wire `FetchPrice`

- Count `len(query.ExchangeReaders)` in total endpoints and launch goroutines analogously to RPC/contract.
- Map successes/errors into existing `Result`, aggregation, `min_responses`, and spread logic.

### Step 3.4 — Tests

- Unit tests with fake HTTP or stub handler: one exchange, direct USD pair; one with adjust-by if available in test fixtures.
- Regression: queries without exchange endpoints unchanged.

### Phase 3 exit criteria

- End-to-end: a query with only `use_cache=false` exchange endpoints returns a median/hex result in test or dev harness.

---

## Phase 4 — Cache-backed path and refresher (`use_cache=true`)

**Objective:** Cached reads use `GetValidPriceForExchange(marketId, exchangeIdString, now)` with **`exchangeIdString` equal to the canonical `ExchangeId`** used when writing cache entries.

### Step 4.1 — Document cache producers

- Clarify in code comments: entries are written by (a) batchable HTTP refresh, (b) **new** exchange refresher, and/or (c) gRPC `UpdateMarketPrices` from a standalone pricefeed process. Reporters with pricefeed disabled must run the exchange refresher (or another writer) or `use_cache=true` will miss.

### Step 4.2 — Exchange refresher worker

- Similar to `StartBatchableRefresher`: periodic loop over all `ExchangeHandler` with `UseCache==true`.
- For each: live fetch using **Phase 3** logic (or shared inner function), then `MarketToExchangePrices.UpdatePrices` with updates shaped like existing daemon messages (`MarketId`, `ExchangeId` string, fixed-point price, timestamp).
- Ensure written price encoding matches what `fetchFromBatchableCacheEndpoint`-style readers expect when converted to float.

### Step 4.3 — `fetchFromExchangeCache` (or merge into one fetch function)

- If `UseCache`: resolve `marketID uint32`, call `GetValidPriceForExchange`, convert uint64 → float via `StaticMarketParamsConfig` (same as RPC cache path).

### Step 4.4 — Flags

- Add daemon flag for refresh interval (e.g. `--exchange-cache-refresh-interval-ms` or reuse a shared “spot refresh” interval if product prefers one knob).

### Phase 4 exit criteria

- Integration test: refresher writes → `FetchPrice` with `use_cache=true` returns fresh value; stale value returns structured error per existing patterns.

---

## Phase 5 — Telemetry, docs, migration

### Step 5.1 — Metrics and logs

- Labels: `exchange_id`, query id, optional `market_id` string.
- Counters: live success/fail, cache hit/miss, refresher write errors.

### Step 5.2 — README / PRICEFEED_SOURCE_ARCHITECTURE

- Short subsection: how `custom_query` `exchange` endpoints relate to `ExchangeConfigJson` and `StaticExchangeDetails`.

### Step 5.3 — Migrate one or two production queries

- Replace redundant HTTP template sources where an exchange integration already exists; verify parity.

### Phase 5 exit criteria

- Operators can configure exchange sources without touching URL templates; misconfigurations fail at startup.

---

## Testing checklist (by phase)

| Phase | Focus |
|-------|--------|
| 1 | Lookup missing/unknown exchange; optional ticker mismatch |
| 2 | `QueryConfig` population |
| 3 | Live fetch, adjust markets, aggregation with other endpoint types |
| 4 | Refresher write + cache read + staleness |
| 5 | Metrics + migration spot-check |

---

## Resolved decisions (updated)

| Topic | Decision |
|-------|-----------|
| Endpoint type name | `"exchange"` |
| Ticker in TOML | **Canonical lookup**; optional `ticker` only with strict equality to canonical value |
| Cache miss at request time | **Strict** (no silent live fallback unless you add an explicit flag later) |
| Refresh interval | Single global interval in Phase 4; per-exchange later if needed |

---

## Success criteria

- `custom_query_config.toml` can reference exchange APIs **without** duplicating conversion rules in TOML.
- Reporter remains on `custom_query.FetchPrice` only.
- Cache-backed exchange endpoints use `(chain MarketId, canonical ExchangeId string)` consistently with writers.
- Existing non-exchange custom query behavior is unchanged.
