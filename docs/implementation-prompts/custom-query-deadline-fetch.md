# Implementation Prompt: Custom Query Deadline-Aware Fetch

## Context

This repo has two price-fetching paths. **This task only changes the custom query path.**

Custom queries are fetched in `custom_query/request.go` via `FetchPrice`, called from `reporter/client/median.go` when a spot price report is not a standard CEX market param.

### Current behavior (problem)

1. `FetchPrice` hardcodes a **5 second** parent context timeout.
2. It launches all endpoint goroutines and **blocks on `wg.Wait()`** until every goroutine completes before aggregating.
3. `median()` calls `FetchPrice(context.Background(), ...)` — it **ignores** the reporter's `ctx` deadline entirely.
4. Per-endpoint template timeouts default to **5000ms** (`custom_query/constants.go`).
5. Contract readers use **3 retries** with **3 second** per-call timeouts (`custom_query/config.go` passes `3` to `contractreader.NewReader`).
6. Occasionally a hung source eats the full 5 seconds even when enough other sources succeeded quickly.

### Production constraints

- Most nodes use **`timeout_config = 1s`** in chain `config.toml`.
- Average block time is **~2.6s**.
- Effective reporting window from query origination is roughly **~3.6s total**, but the daemon often picks up the query **200–800ms late** (spot price monitor polls every 200ms).
- After fetch, the reporter still needs time to **sign, broadcast, and get included in a block** (~1.5–2.5s).
- Source metrics show **average latency < 500ms**; the problem is occasional hangs, not slow averages.

### Desired behavior

Implement **deadline-aware collection** that:

1. **Maximizes source count** by waiting as long as safely possible within a bounded fetch window.
2. **Aggregates immediately** when **all sources have responded** (success or failure) **AND** `MinResponses` is met — do not wait for the collection deadline if nothing else is pending.
3. **Cuts hung sources** via short per-source timeouts so one bad endpoint cannot burn the whole window.
4. **Respects the reporter context deadline** passed from report generation.
5. Allows **conditional retries** within the per-source budget for transient failures.

---

## Goals

### Primary

- Refactor `FetchPrice` to use a deadline-aware collection loop instead of `wg.Wait()` + `for result := range results`.
- Thread `context.Context` from the reporter through `median` → `FetchPrice`.
- Replace the hardcoded 5s fetch timeout with a configurable, context-aware budget (~**1.2–1.5s default**).
- Add early-finish when all sources are accounted for and `MinResponses` is met.

### Secondary

- Tighten per-source timeouts/retries on the hot path (RPC, contract, combined handlers).
- Apply the same deadline-aware pattern inside combined handlers that use `ParallelFetcher`.
- Add unit tests covering early finish, deadline expiry, hung sources, and insufficient responses.
- Remove debug `fmt.Println` calls in `custom_query/request.go` while touching the file.

### Non-goals

- Do not change the CEX pricefeed path (`pricefeed/client/...`).
- Do not change aggregation logic (`MedianInHex`, spread checks) unless required for testing.
- Do not change reporter tx TTL (`GetUniqueUnorderedTimeout` = 30s) in this task.
- Do not commit unless explicitly asked.

---

## Files to modify

| File | Change |
|---|---|
| `custom_query/request.go` | Main collection loop refactor |
| `custom_query/config.go` | Add fetch timeout + per-source timeout config fields |
| `custom_query/constants.go` | Lower default endpoint template timeouts |
| `reporter/client/median.go` | Accept/pass `ctx` to `FetchPrice` |
| `reporter/client/broadcast_message.go` | Pass `ctx` into `median` |
| `custom_query/contracts/contract_reader/reader.go` | Honor parent ctx between retries; consider configurable max retries |
| `custom_query/rpc/rpc_reader/reader.go` | Review timeout units; ensure parent ctx respected |
| `custom_query/combined/combined_handler/handlers.go` | Deadline-aware `ParallelFetcher` or equivalent |
| `custom_query/combined/combined_handler/sfrxusd_price_handler.go` | Use new parallel fetch deadline behavior |
| `cmd/test_mode.go` | Already passes ctx; verify still works |
| New test file(s) under `custom_query/` | Collection loop tests |

---

## Detailed design

### 1. Thread context from reporter

**Today:**
```go
// reporter/client/median.go
results, err := customquery.FetchPrice(context.Background(), queryConfig, c.MarketToExchange)
```

**Change:**
- Update `median` signature to `median(ctx context.Context, querydata []byte)`.
- Update `GenerateAndBroadcastSpotPriceReport` to call `c.median(ctx, qd)`.
- Find all other `median(...)` call sites and update accordingly.

`FetchPrice` must derive its collection deadline from the passed-in ctx.

### 2. Fetch deadline calculation

Add constants (in `custom_query/constants.go` or a small helper file):

```go
const (
    DefaultFetchTimeout           = 1500 * time.Millisecond
    DefaultAggregationBuffer      = 100 * time.Millisecond
    DefaultPostFetchReserve       = 2000 * time.Millisecond // sign + broadcast + inclusion buffer
    DefaultPerSourceTimeout       = 700 * time.Millisecond
    DefaultPerSourceRetryTimeout  = 500 * time.Millisecond
    DefaultRetryDelay             = 75 * time.Millisecond
    DefaultMaxSourceRetries       = 1
)
```

Add optional fields to `QueryConfig` (TOML-configurable):

```go
FetchTimeoutMs        int // 0 = use default
PerSourceTimeoutMs  int // 0 = use default
MaxSourceRetries    int // 0 = use default
```

Compute collection deadline inside `FetchPrice`:

```go
func collectionDeadline(ctx context.Context, query QueryConfig) time.Time {
    fetchTimeout := DefaultFetchTimeout
    if query.FetchTimeoutMs > 0 {
        fetchTimeout = time.Duration(query.FetchTimeoutMs) * time.Millisecond
    }

    deadline := time.Now().Add(fetchTimeout)

    if ctxDeadline, ok := ctx.Deadline(); ok {
        // Reserve time for aggregation + post-fetch work
        safeDeadline := ctxDeadline.Add(-DefaultAggregationBuffer - DefaultPostFetchReserve)
        if safeDeadline.Before(deadline) {
            deadline = safeDeadline
        }
    }
    return deadline
}
```

Use `context.WithCancel` + `context.WithDeadline` for the fetch operation. Call `cancel()` when aggregating to abort in-flight requests.

Replace hardcoded `5*time.Second`.

### 3. Deadline-aware collection loop

Replace the current `wg.Wait()` / channel-close pattern.

**Pseudocode:**

```go
fetchCtx, cancel := context.WithCancel(parentCtx)
defer cancel()

totalEndpoints := len(rpc) + len(contract) + len(combined)
results := make(chan Result, totalEndpoints)

// Launch goroutines (same as today, but use fetchCtx)
for each endpoint {
    go func() {
        results <- fetchFromX(fetchCtx, ...)
    }()
}

collectionDeadline := collectionDeadline(parentCtx, query)
timer := time.NewTimer(time.Until(collectionDeadline))
defer timer.Stop()

var allResults []Result
var successfulResults []Result
pending := totalEndpoints

collectAndMaybeAggregate := func() (*FetchPriceResult, string, error, bool) {
    if len(successfulResults) < query.MinResponses {
        return nil, "", fmt.Errorf("insufficient..."), false
    }
    encoded, err := aggregateResults(successfulResults, ...)
    if err != nil {
        return fetchPriceResultFromCollected(...), "", err, true
    }
    return fetchPriceResultFromCollected(...), encoded, nil, true
}

for pending > 0 {
    select {
    case result := <-results:
        pending--
        allResults = append(allResults, result)
        if result.Err == nil {
            successfulResults = append(successfulResults, result)
            emit success metrics
        } else {
            emit error metrics
        }

        // EARLY FINISH: all sources accounted for AND min met
        if pending == 0 && len(successfulResults) >= query.MinResponses {
            cancel() // abort any stragglers
            fr, _, err, done := collectAndMaybeAggregate()
            return fr, err  // err may be nil or aggregation error
        }

    case <-timer.C:
        cancel()
        pending = 0 // stop waiting; proceed to deadline handling
        break
    case <-fetchCtx.Done():
        pending = 0
        break
    }
}

// Deadline reached or context cancelled — aggregate if possible
if len(successfulResults) >= query.MinResponses {
    return collectAndMaybeAggregate()
}
return fetchPriceResultFromCollected(...), fmt.Errorf("insufficient successful responses: ...")
```

**Important:**
- Track `pending` explicitly; do not rely on channel close.
- Early finish requires **`pending == 0`**, not just `len(allResults) == totalEndpoints`.
- Do **not** early-finish when `MinResponses` is met but sources are still pending — keep collecting until all respond or deadline hits.
- On deadline: cancel in-flight work and aggregate with whatever successes were collected.

### 4. Per-source timeout and retry behavior

**RPC reader** (`custom_query/rpc/rpc_reader/reader.go`):
- Review the timeout units bug: `NewReader` sets `http.Client.Timeout` as `timeout * time.Second` while `reader.timeout` uses `timeout * time.Millisecond`. Template timeout `5000` means 5s attempt timeout but 5000s HTTP client timeout. Fix if straightforward: use consistent milliseconds for both, or document and fix as part of this task.
- Keep retries at most **1** on the reporter hot path.
- Only retry transient errors: timeout, connection reset, 429, 502/503/504.
- Skip retry if remaining collection time `< 400ms` (pass deadline or remaining duration into fetch helpers if needed).

**Contract reader** (`custom_query/contracts/contract_reader/reader.go`):
- Currently: up to 4 attempts (initial + 3 retries), `time.Sleep` between retries without checking parent ctx.
- Change to respect parent ctx cancellation between retries.
- Reduce default max retries to **1** for readers created in `config.go` (or make configurable).
- Use per-call timeout of ~700ms (configurable), not 3s.

**Combined handlers** (`custom_query/combined/combined_handler/`):
- `ParallelFetcher.Wait()` currently waits for all sub-fetches indefinitely.
- Refactor to accept a context/deadline and use the same early-finish rule internally:
  - When all sub-sources (RPC + contract) have responded and internal `minResponses` is met → proceed.
  - Otherwise wait until parent ctx deadline.
- Apply to `sfrxusd_price_handler.go` and any other combined handlers using `ParallelFetcher`.

### 5. Config changes

Add to `QueryConfig` TOML schema in `custom_query/config.go`:

```toml
[queries.my_query]
fetch_timeout_ms = 1500        # optional, default 1500
per_source_timeout_ms = 700    # optional, default 700
max_source_retries = 1         # optional, default 1
min_responses = 2
```

Wire through `BuildQueryEndpoints`.

Lower defaults in `StaticEndpointTemplateConfig` (`custom_query/constants.go`) from `Timeout: 5000` to `Timeout: 1500` (milliseconds as used by rpc reader attempt timeout).

### 6. Cleanup

Remove these debug prints from `custom_query/request.go`:
- `fmt.Println("Successful results:", successfulResults)`
- `fmt.Println("Contract value:", value)`
- `fmt.Println("Combined value:", value)`

---

## Test requirements

Add tests in `custom_query/` (new file like `request_collection_test.go` is fine). Use httptest / mocked handlers where possible.

### Required test cases

1. **Early finish**: 5 sources, 3 succeed quickly, 2 fail quickly, `MinResponses=2` → aggregates without waiting for full deadline. Assert elapsed time << deadline.

2. **Wait for slow source**: 3 sources, 2 return at 100ms, 1 returns at 600ms, `MinResponses=2` → waits for third source, aggregates with 3 successes (if all succeed).

3. **Hung source cut off**: 3 sources, 2 succeed at 100ms, 1 hangs → per-source timeout fires, deadline or pending==0 reached, aggregates with 2 successes if `MinResponses=2`.

4. **Deadline without min**: 3 sources, 1 succeeds, 2 hang/fail → returns insufficient responses error; `RawResults` populated.

5. **Context deadline respected**: Pass a ctx with 300ms remaining and large configured fetch timeout → collection stops around 300ms minus buffers.

6. **All sources fail**: Returns error with diagnostics.

7. **Regression**: Existing `custom_query` tests still pass.

Use `testing/synctest` or simple timed tests with short deadlines (100–200ms) for determinism.

---

## Suggested constants (starting point)

These are starting defaults — tune if tests show they're too aggressive:

| Setting | Default |
|---|---|
| `DefaultFetchTimeout` | 1500ms |
| `DefaultAggregationBuffer` | 100ms |
| `DefaultPostFetchReserve` | 2000ms |
| `DefaultPerSourceTimeout` | 700ms |
| `DefaultPerSourceRetryTimeout` | 500ms |
| `DefaultRetryDelay` | 75ms |
| `DefaultMaxSourceRetries` | 1 |

---

## Implementation order

1. Add config fields + constants + deadline helper (no behavior change yet).
2. Thread `ctx` through reporter → `median` → `FetchPrice`.
3. Refactor `FetchPrice` collection loop (core change).
4. Tighten RPC/contract reader retry/timeout behavior.
5. Update combined handler `ParallelFetcher` pattern.
6. Lower default template timeouts.
7. Add tests.
8. Run `go test ./custom_query/... ./reporter/client/...` and fix failures.

---

## Acceptance criteria

- [ ] Hung custom query source no longer blocks aggregation for up to 5 seconds when other sources succeeded.
- [ ] When all sources respond and `MinResponses` is met, aggregation happens immediately (early finish).
- [ ] When sources are still pending, fetch waits until collection deadline to maximize source count.
- [ ] `FetchPrice` respects the reporter `ctx` deadline.
- [ ] Default fetch budget is ~1.5s, not 5s.
- [ ] Contract/RPC retries are bounded and ctx-aware.
- [ ] Combined handlers use the same deadline-aware collection semantics.
- [ ] New unit tests cover early finish, deadline, and hung source scenarios.
- [ ] No debug `fmt.Println` left in `request.go`.
- [ ] All existing relevant tests pass.

---

## Reference: current code locations

```
custom_query/request.go:63          FetchPrice (main change)
custom_query/request.go:69          hardcoded 5s timeout (remove)
custom_query/request.go:104-108   wg.Wait blocking pattern (replace)
reporter/client/median.go:16        median() — add ctx param
reporter/client/median.go:41        context.Background() call (fix)
reporter/client/broadcast_message.go:88  c.median(qd) — pass ctx
custom_query/config.go:69           QueryConfig struct — add fields
custom_query/config.go:272          contractreader.NewReader(urls, 3) — reduce retries/timeout
custom_query/constants.go:7+        endpoint Timeout: 5000 — lower to 1500
custom_query/combined/combined_handler/handlers.go:86  ParallelFetcher.Wait()
```

---

## Notes for implementer

- Follow existing code conventions in this repo. Keep the diff focused — do not refactor unrelated code.
- Minimize scope: this is a behavior fix for reporting latency, not a general retry framework.
- The CEX path already behaves correctly at median time (reads cache, never waits for stragglers). Custom queries should feel similar: collect in parallel during a bounded window, never let one hang dominate.
- If `DefaultPostFetchReserve` makes the fetch window too small when ctx has no deadline, fall back to `DefaultFetchTimeout` only.
- Grep for all `median(` call sites after changing the signature.
- Grep for all `FetchPrice(` call sites after changes.
