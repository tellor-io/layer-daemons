### Pricefeed Source & Config Architecture

This document describes how the pricefeed daemon is wired from **sources (exchanges)** through **config**, **caching**, and **server ingestion**, and what is required to add a new **exchange source** or **market pair**.

It is complementary to `README.md`, which focuses more on defaults and CLI usage.

---

### Components Overview

- **Sources (`pricefeed/client/sources/*`)**: Per-exchange code that parses HTTP responses into ticker → price mappings.
- **PriceFetcher (`pricefeed/client/price_fetcher`)**: Periodically calls exchange HTTP APIs using source functions and emits raw market prices.
- **PriceEncoder (`pricefeed/client/price_encoder`)**: Converts raw per-exchange prices into canonical USD market prices and writes them into the in-memory cache.
- **In‑memory cache (`ExchangeToMarketPrices`)**: Goroutine-safe map of exchange → market → latest price + timestamp.
- **Mutable configs (`PricefeedMutableMarketConfigs`)**: Central owner of dynamic market + per-exchange mapping and conversion rules, hot‑reloaded from chain params.
- **Updater (`SubTaskRunner.StartPriceUpdater`)**: Periodically reads the in‑memory cache and pushes prices to the pricefeed gRPC server.
- **Server cache (`server/pricefeed.go`)**: On-chain side that stores aggregated market → exchange prices and validates updates.

---

### Data & Control Flow (High Level)

1. **Config ingestion**
   - Chain params provide a list of `MarketParam` records (see `README.md` for structure).
   - Each `MarketParam` includes:
     - `Id`, `Pair` (e.g. `"BTC-USD"`), `Exponent`, `MinExchanges`, `MinPriceChangePpm`, `QueryData`.
     - `ExchangeConfigJson`: JSON describing, per exchange, which ticker to use and how to convert it.
   - The daemon parses these into:
     - `MutableMarketConfig` (per market, common across exchanges).
     - `MutableExchangeMarketConfig` (per exchange, mapping from `MarketId` → `MarketConfig`).

2. **Startup wiring**
   - `PricefeedMutableMarketConfigsImpl` is instantiated with the canonical set of exchanges.
   - For each exchange:
     - A **PriceFetcher** and **PriceEncoder** are created.
     - Each one registers itself as an `ExchangeConfigUpdater` via:
       - `PricefeedMutableMarketConfigs.AddPriceFetcher(...)`
       - `PricefeedMutableMarketConfigs.AddPriceEncoder(...)`
   - `PricefeedMutableMarketConfigsImpl` now owns:
     - The latest `MutableExchangeMarketConfig` per exchange.
     - The latest `MutableMarketConfig` per market.
     - The set of subscribed updaters (`PriceFetcher` + `PriceEncoder`) per exchange.

3. **Periodic querying & encoding**
   - For each exchange:
     - A `PriceFetcher` task loop runs according to `ExchangeQueryConfig` (interval, timeout, max queries).
     - For each task loop:
       - It queries the relevant HTTP endpoint(s) using the exchange’s **source function**.
       - It emits `PriceFetcherSubtaskResponse` values into a shared buffered channel.
     - The `PriceEncoder` for that exchange:
       - Consumes responses from the buffered channel.
       - Converts raw prices to USD‑quoted canonical market prices using the configs and index prices.
       - Updates the shared `ExchangeToMarketPrices` cache.

4. **Price updates to the server**
   - `SubTaskRunner.StartPriceUpdater` periodically:
     - Calls `ExchangeToMarketPrices.GetAllPrices()` to get all exchange → market prices.
     - Transforms them into an `UpdateMarketPricesRequest` (per market, list of `ExchangePrice`).
     - Calls `PriceFeedService.UpdateMarketPrices` gRPC on the server.
   - The server validates the update and stores it in `MarketToExchangePrices`, which the app uses for on‑chain logic and querying.

---

### Config Layer in Detail

#### Market‑level config

File: `pricefeed/client/types/mutable_market_config.go`

- `MutableMarketConfig` captures cross‑exchange metadata for a market:
  - `Id`: unique numeric id (defined in `exchange_common/market_id.go`).
  - `Pair`: human‑readable name, e.g. `"BTC-USD"`.
  - `Exponent`: decimal exponent for fixed‑point representation.
  - `MinExchanges`: minimum number of exchanges needed for an index price to be considered valid.

These come from `MarketParam` chain params and are validated (`Pair` non‑empty, `MinExchanges > 0`).

#### Per‑exchange market mapping

File: `pricefeed/client/types/market_config.go`

- `MarketConfig` defines how a specific market is resolved on a specific exchange:
  - `Ticker`: exchange‑specific symbol string used in the HTTP request and response mapping.
  - `AdjustByMarket` (optional):
    - References another `MarketId` used to convert the ticker’s quote currency into USD.
    - Example: to get `BTC-USD` from `BTC-USDT` on some exchange, use `AdjustByMarket = USDT-USD`.
  - `Invert`:
    - Controls whether to invert the basic price or the adjusted price.
    - Without `AdjustByMarket`, `Invert` means: `marketPrice = 1 / tickerPrice`.
    - With `AdjustByMarket`, `Invert` typically means: `marketPrice = adjustByIndexPrice / tickerPrice`.

File: `pricefeed/client/types/mutable_exchange_market_config.go`

- `MutableExchangeMarketConfig` groups `MarketConfig` per exchange:
  - `Id`: `ExchangeId`.
  - `MarketToMarketConfig`: map `MarketId` → `MarketConfig`.
  - Supported markets on the exchange are the keys of `MarketToMarketConfig`.
  - Validation ensures:
    - Every market referenced here exists as a `MutableMarketConfig`.
    - Every `AdjustByMarket` points to a valid market for which there is a config.

#### Source of truth: `ExchangeConfigJson` in params

File: `pricefeed/client/types/exchange_config_json.go`

- `ExchangeConfigJson` and `ExchangeMarketConfigJson` represent the JSON embedded in `MarketParam.ExchangeConfigJson`.
  - For each market, `ExchangeConfigJson.Exchanges` is a list of `ExchangeMarketConfigJson` with:
    - `exchangeName`: canonical `ExchangeId` string.
    - `ticker`: raw ticker string as used by the source.
    - `adjustByMarket` (optional): string name of another market pair.
    - `invert` (optional): bool.
  - Validation ensures:
    - Exchange names match the known exchange ids.
    - Ticker is non‑empty.
    - If `adjustByMarket` is set, its pair string maps to an existing `MarketId`.

File: `pricefeed/client/types/price_feed_mutable_market_configs.go`

- `PricefeedMutableMarketConfigsImpl` owns:
  - `mutableExchangeToConfigs`: `ExchangeId` → `MutableExchangeMarketConfig`.
  - `mutableMarketToConfigs`: `MarketId` → `MutableMarketConfig`.
  - `mutableExchangeConfigUpdaters`: `ExchangeId` → `{ PriceFetcher, PriceEncoder }`.
  - A `WaitGroup` (`updatersInitialized`) to ensure all updaters are registered before first update.
- `UpdateMarkets`:
  - Parses `[]MarketParam` into new mutable configs via `ValidateAndTransformParams`.
  - Computes which exchanges had config changes.
  - Swaps the configs under a mutex.
  - Notifies both the `PriceEncoder` and `PriceFetcher` for changed exchanges via:
    - `ExchangeConfigUpdater.UpdateMutableExchangeConfig(newExchangeConfig, newMarketConfigs)`.
  - Always updates the encoder before the fetcher.

This layer is the **single source of truth** for dynamic market/exchange configuration. Both fetchers and encoders are updated through a common interface, keeping them in sync.

---

### Source Functions (Per‑Exchange)

Directory: `pricefeed/client/sources/`

Common utilities live in `pricefeed/client/sources/util.go` and friends:

- Defines the `Ticker` interface:
  - `GetPair() string`
  - `GetAskPrice() string`
  - `GetBidPrice() string`
  - `GetLastPrice() string`
- Provides helpers like:
  - `GetMedianPricesFromTickers`: Given a slice of `Ticker`s, `tickerToExponent`, and a resolver (e.g. median), compute:
    - `tickerToPrice map[string]uint64`
    - `unavailableTickers map[string]error`
  - `GetUint64MedianFromReverseShiftedBigFloatValues`: handles decimal shifting and big‑float conversions.
  - `GetApiResponseValidator`: configures the validator (e.g. `positive-float-string`).
  - `IsGenericExchangeError`: regex‑based detection of generic 5xx / exchange‑side failure strings.

Each exchange implements:

- A concrete ticker struct that implements `sources.Ticker`.
- A price function with the signature:

```go
func(
	response *http.Response,
	tickerToExponent map[string]int32,
	resolver pricefeedtypes.Resolver,
) (tickerToPrice map[string]uint64, unavailableTickers map[string]error, err error)
```

Example: `pricefeed/client/sources/binance/binance.go`

- Defines `BinanceTicker`, implements `Ticker`, and `BinancePriceFunction`:
  - Decodes a JSON list of `BinanceTicker`.
  - Calls `sources.GetMedianPricesFromTickers`.

These price functions are plugged into `ExchangeQueryDetails` at startup.

---

### Exchange Query Details & Config

File: `pricefeed/client/types/exchange_query_details.go`

- `ExchangeQueryDetails` bundles:
  - `Exchange`: `ExchangeId`.
  - `Url`: HTTP endpoint.
  - `PriceFunction`: function pointer (the source function).
  - `IsMultiMarket`: whether a single HTTP response returns multiple tickers, or one per pair.

File: `pricefeed/client/types/exchange_query_config.go`

- `ExchangeQueryConfig` holds:
  - `ExchangeId`
  - `IntervalMs`: delay between task loops.
  - `TimeoutMs`: HTTP timeout.
  - `MaxQueries`: max number of HTTP calls per loop for single‑market APIs (rate limiting).

These are static per exchange and are passed into the fetcher at creation.

---

### PriceFetcher: Retrieving Raw Prices

File: `pricefeed/client/price_fetcher/price_fetcher.go`

#### Purpose

- For a specific exchange:
  - Periodically select which markets to query based on the latest config.
  - Call the appropriate HTTP endpoint(s) with `ExchangeQueryDetails`.
  - Use the source price function to turn responses into `MarketPriceTimestamp` values.
  - Push results (price or error) into a shared buffered channel for the encoder.

#### Structure

- `PriceFetcher` fields:
  - `exchangeQueryConfig`: `ExchangeQueryConfig`.
  - `exchangeDetails`: `ExchangeQueryDetails` (URL, price function, multi‑market flag).
  - `queryHandler`: plumbing to actually perform HTTP requests and call the price function.
  - `logger`, `bCh` (buffered channel to encoder).
  - `mutableState`: internal state containing:
    - Copy of `MutableExchangeMarketConfig` for this exchange.
    - Per‑market exponents.
    - Market id ring to control query ordering / distribution.

#### Config updates

- `PriceFetcher` implements `ExchangeConfigUpdater`:
  - `GetExchangeId() ExchangeId`
  - `UpdateMutableExchangeConfig(newExchangeConfig, newMarketConfigs)`:
    - Validates ids match.
    - Validates that every market has a valid `MutableMarketConfig` and conversion references.
    - Recomputes:
      - `marketExponents` map.
      - Market ids ring.
    - Stores into `mutableState`.

Whenever `PricefeedMutableMarketConfigsImpl.UpdateMarkets` detects a change for this exchange, it will call this method with the new config.

#### Query scheduling

- `getNumQueriesPerTaskLoop`:
  - If `IsMultiMarket`, always 1 (one HTTP request for all markets).
  - Otherwise, `min(MaxQueries, numMarkets)`.
- `RunTaskLoop(requestHandler)`:
  - Captures a snapshot of current `mutableState` into `taskLoopDefinition`.
  - If `IsMultiMarket` and there are any markets:
    - Runs a single `runSubTask` for all markets.
  - Else:
    - Spins one goroutine per market in this loop (up to `MaxQueries`).
    - Each goroutine calls `runSubTask` for a single market.
  - Waits for all subtasks to finish before returning.

#### Subtasks and responses

- `runSubTask`:
  - Builds a `context.WithTimeout` using `exchangeQueryConfig.TimeoutMs`.
  - Calls `queryHandler.Query(...)` with:
    - `exchangeDetails` (URL, price function, multi‑market flag).
    - Current `mutableExchangeConfig`.
    - `marketIds` in this subtask.
    - `requestHandler` (HTTP client wrapper).
    - `marketExponents`.
  - Receives:
    - `[]*types.MarketPriceTimestamp` (prices).
    - Error, if any.
  - On error:
    - Writes a single `PriceFetcherSubtaskResponse{Price: nil, Err: err}` into `bCh`.
    - Optionally emits availability metrics (all markets in this subtask are unavailable).
  - On success:
    - For each `MarketPriceTimestamp`:
      - Rejects zero prices as invalid.
      - Logs debug info.
      - Writes `PriceFetcherSubtaskResponse{Price: price, Err: nil}` into `bCh`.
    - Emits market availability metrics (per market).

#### Buffered channel semantics

- `bCh` is shared between all subtasks for this exchange.
- `writeToBufferedChannel`:
  - Logs error if the channel is full (size == `constants.FixedBufferSize`).
  - Performs a blocking send.
- The channel is closed by `SubTaskRunner.StartPriceFetcher` when the daemon stops for this exchange; that’s the signal to the encoder that no more prices will arrive.

---

### PriceEncoder: Conversion & Caching

File: `pricefeed/client/price_encoder/price_encoder.go`

#### Purpose

- Convert raw per‑exchange prices from `PriceFetcher` into canonical USD market prices using:
  - Market exponents.
  - `MarketConfig` conversion rules (`AdjustByMarket`, `Invert`).
  - Index prices from other markets via the shared cache.
- Update `ExchangeToMarketPrices` with the converted price.
- Handle and categorize errors, emitting appropriate metrics and logs.

#### Structure

- `PriceEncoderImpl` fields:
  - `exchangeId`
  - `exchangeToMarketPrices`: shared cache.
  - `logger`
  - `bCh`: `<-chan *PriceFetcherSubtaskResponse` (read‑only).
  - `mutableState`: holds:
    - Derived `PriceConversionDetails` per market:
      - exponent for the market.
      - optional `AdjustByMarketDetails` (market id, exponent, min exchanges).
      - whether to invert.
  - `isPastGracePeriod`: initially false; set to true after a fixed startup delay to avoid noisy conversion errors while the cache is still filling.

#### Config updates

- Implements `ExchangeConfigUpdater`:
  - `UpdateMutableExchangeConfig(newConfig, newMarketConfigs)`:
    - Validates exchange id.
    - Validates the exchange config against `MutableMarketConfig`s.
    - Rebuilds an internal map `MarketId` → `MutableMarketConfig` and calls `mutableState.Update`.

This is called whenever `PricefeedMutableMarketConfigsImpl` detects a changed config, before the corresponding fetcher is updated.

#### Conversion logic and caching

- `UpdatePrice(marketPriceTimestamp)`:
  - Calls `convertPriceUpdate` to transform the raw price:
    - Looks up `PriceConversionDetails` for this market from `mutableState`.
    - If there is **no `AdjustByMarket`**:
      - If `Invert == false`: price is passed through.
      - If `Invert == true`: price is inverted using exponent and `prices.Invert`.
    - If there **is an `AdjustByMarket`**:
      - Calls `exchangeToMarketPrices.GetIndexPrice(adjustByMarketId, cutoffTime, medianResolver)`:
        - `cutoffTime` = `now - constants.MaxPriceAge` (e.g. 30 seconds).
        - Resolver is typically median.
      - Ensures at least `MinExchanges` prices are available; otherwise returns an error.
      - Combines `tickerPrice` and `adjustByIndexPrice`:
        - If `Invert == false`:
          - `price = tickerPrice * adjustByIndexPrice` (with proper exponent handling).
        - If `Invert == true`:
          - `price = adjustByIndexPrice / tickerPrice`.
  - On any conversion error:
    - Logs (info or error depending on `isPastGracePeriod`).
    - Emits a conversion error metric.
  - On success:
    - Writes updated price into shared cache:
      - `exchangeToMarketPrices.UpdatePrice(exchangeId, convertedMarketPriceTimestamp)`.
    - Emits a success metric and gauge for the converted price.

#### Processing fetcher responses

- `ProcessPriceFetcherResponse(response)`:
  - Panic if `response == nil` (should only happen on programming error).
  - If `response.Err == nil`:
    - Calls `UpdatePrice(response.Price)`.
  - Else:
    - Classifies the error into:
      - `context.DeadlineExceeded` (timeout).
      - `constants.ErrRateLimiting` (rate limit).
      - `ExchangeError` (exchange‑specific error from source price function).
      - `IsGenericExchangeError` (internal exchange failures / 5xx‑like behaviours).
      - `syscall.ECONNRESET` (connection reset).
      - Other errors (logged as generic failure).
    - Logs appropriately (mostly info‑level for expected but undesirable behaviours) and updates failure metrics.

---

### Shared Exchange/Market Price Cache

File: `pricefeed/client/types/exchange_to_market_prices.go`

#### Purpose

- Provides a goroutine‑safe view of all current prices:
  - `ExchangeId` → `MarketId` → `{price, lastUpdatedAt}`.
- Exposes APIs to:
  - Update a single market price for an exchange.
  - Read all prices, grouped by exchange.
  - Compute index prices (e.g. for `AdjustByMarket`) using a median (or other resolver) across exchanges.

#### Interface and implementation

- `ExchangeToMarketPrices` interface:
  - `UpdatePrice(exchangeId, *MarketPriceTimestamp)`
  - `GetAllPrices() map[ExchangeId][]MarketPriceTimestamp`
  - `GetIndexPrice(marketId, cutoffTime, resolver) (medianPrice, numPricesMedianized)`
- `ExchangeToMarketPricesImpl`:
  - `ExchangeMarketPrices: map[ExchangeId]*MarketToPrice`
  - `NewExchangeToMarketPrices(exchangeIds []ExchangeId)`:
    - Pre‑allocates a `MarketToPrice` for each exchange.
    - Validates there are no duplicate exchange ids.
  - `UpdatePrice`:
    - Delegates to `MarketToPrice.UpdatePrice`, which enforces monotonic timestamps.
  - `GetAllPrices`:
    - Collects `MarketToPrice.GetAllPrices()` for each exchange.
  - `GetIndexPrice`:
    - For each exchange:
      - Calls `MarketToPrice.GetValidPriceForMarket(marketId, cutoffTime)`.
      - Includes only prices newer than `cutoffTime`.
    - Collects all valid prices into a slice.
    - If empty: returns `(0, 0)`.
    - Otherwise calls resolver (e.g. median) and returns `(median, len(prices))`.

This cache is used both by:

- The `PriceEncoder` to obtain adjustment/index prices for `AdjustByMarket`.
- The `PriceUpdater` to send snapshot updates to the server.

---

### Updater & Server Side

#### Daemon → Server: PriceUpdater

File: `pricefeed/client/subtask_runner.go`

- `StartPriceUpdater`:
  - Runs in the daemon main goroutine.
  - Every tick:
    - Calls `RunPriceUpdaterTaskLoop(ctx, exchangeToMarketPrices, priceFeedServiceClient, logger)`.
    - On success: `c.ReportSuccess()`.
    - On failure: `c.ReportFailure(err)`.
- `RunPriceUpdaterTaskLoop`:
  - Reads all cached prices:
    - `priceUpdates := exchangeToMarketPrices.GetAllPrices()`.
  - Transforms into a gRPC request:
    - `transformPriceUpdates` builds:
      - For each exchange:
        - For each `MarketPriceTimestamp`:
          - Appends an `ExchangePrice` to the appropriate `MarketPriceUpdate` for that `MarketId`.
  - If there are **no** `MarketPriceUpdates`:
    - Logs and returns `ErrEmptyMarketPriceUpdate`.
  - Else:
    - Calls `priceFeedServiceClient.UpdateMarketPrices(ctx, request)`.

#### Server ingestion & cache

File: `server/pricefeed.go`

- `Server.UpdateMarketPrices(ctx, req)`:
  - Validates `req`:
    - Non‑empty list of `MarketPriceUpdates`.
    - For each update:
      - Every `ExchangePrice`:
        - Must not have `Price == constants.DefaultPrice`.
        - Must have `LastUpdateTime != nil`.
  - Writes prices into `s.marketToExchange`:
    - `MarketToExchangePrices.UpdatePrices(req.MarketPriceUpdates)`.
  - Emits metrics and returns success.

This is the final, app‑visible cache of prices, but conceptually it mirrors the structure of the daemon’s earlier cache, now aggregated per market.

---

### How to Add a New Exchange Source

This section focuses on the **source side** (client/daemon). You may also need to adjust defaults (`constants/static_exchange_details.go`, `configs/default_pricefeed_exchange_config.go`) and chain params (`market_params.toml`) as described in `README.md`.

**Steps:**

1. **Define the exchange’s ticker model and price function**
   - Create a new package: `pricefeed/client/sources/<your_exchange>/`.
   - Implement a ticker struct that satisfies `sources.Ticker`:
     - Fields reflect the exchange’s JSON schema (e.g. `symbol`, `askPrice`, `bidPrice`, `lastPrice`).
     - Add `validate` tags as needed (e.g. `positive-float-string`).
     - Implement `GetPair`, `GetAskPrice`, `GetBidPrice`, `GetLastPrice`.
   - Implement a price function:
     - Signature: `func(response *http.Response, tickerToExponent map[string]int32, resolver pricefeedtypes.Resolver) (map[string]uint64, map[string]error, error)`.
     - Decode the HTTP response into a slice of your ticker struct(s).
     - Call `sources.GetMedianPricesFromTickers` to compute per‑ticker median prices.

2. **Wire the source into `ExchangeQueryDetails`**
   - Add a new entry to wherever `ExchangeQueryDetails` is constructed (typically in a config/static details file such as `constants/static_exchange_details.go`):
     - Assign a canonical `ExchangeId` string (e.g. `"NewExchange"`).
     - Set:
       - `Url` to the HTTP endpoint.
       - `PriceFunction` to your new source price function.
       - `IsMultiMarket` based on whether this endpoint returns many tickers at once.

3. **Configure query behaviour**
   - In `pricefeed_exchange_config.toml` (or the default config Go file):
     - Add a new `[[exchanges]]` block for this exchange id:
       - `ExchangeId = "NewExchange"`
       - `IntervalMs`, `TimeoutMs`, `MaxQueries` tuned to the exchange’s rate limits.

4. **Include the exchange among canonical ids**
   - Ensure `ExchangeId` enumeration / constants include `"NewExchange"` and that it is part of the canonical exchange set used to:
     - Initialize `PricefeedMutableMarketConfigsImpl`.
     - Initialize `ExchangeToMarketPrices` via `NewExchangeToMarketPrices`.

Once these steps are done, the daemon will:

- Instantiate a `PriceFetcher` and `PriceEncoder` for the new exchange.
- Subscribe them to config updates.
- Start querying and encoding prices according to its `ExchangeQueryConfig`.

---

### How to Add a New Market Pair (Client-Side View)

This is summarized in `README.md`, but here is the context from the source/architecture side.

1. **Define a new `MarketId`**
   - In `exchange_common/market_id.go`:
     - Add a constant like:
       - `NEWPAIR_ID uint32 = <unique-number>`.

2. **Add a static default `MarketParam` (if using defaults)**
   - In `constants/static_market_params_config.go` (or similar defaults file):
     - Add an entry keyed by your new `MarketId`:
       - `Id` = your `MarketId` constant.
       - `Pair` = `"NEWPAIR-USD"` (or whatever pair string).
       - `Exponent`, `MinExchanges`, `MinPriceChangePpm`, `QueryData`.
       - `ExchangeConfigJson` describing all participating exchanges:
         - Example structure:
           - `{"exchanges":[{"exchangeName":"Binance","ticker":"\"NEWUSDT\""},{"exchangeName":"CoinbasePro","ticker":"NEW-USD","adjustByMarket":"USDT-USD"}]}`

3. **Or, update `market_params.toml` directly**
   - Append a `[[market_params]]` entry with the same fields as above.

4. **Ensure participating exchanges are wired**
   - For each exchange referenced in `ExchangeConfigJson`:
     - Make sure:
       - The exchange has an `ExchangeQueryDetails` entry and query config.
       - The source price function for that exchange understands the ticker format you specify.

From the daemon’s perspective:

- On the next config reload:
  - `PricefeedMutableMarketConfigsImpl.UpdateMarkets` will parse the new `MarketParam`.
  - Each participating exchange’s `MutableExchangeMarketConfig` will get a new entry for this `MarketId`.
  - The `MutableMarketConfig` for the new market will be added.
  - The exchange’s `PriceFetcher` and `PriceEncoder` will receive a new `UpdateMutableExchangeConfig` call:
    - Fetcher: will start scheduling queries that include the new market’s ticker.
    - Encoder: will have the necessary conversion rules, including any `AdjustByMarket` and `MinExchanges`.

---

### Caching & Staleness Semantics

**On the daemon side:**

- `ExchangeToMarketPrices` only updates a price if the incoming `LastUpdatedAt` timestamp is newer than the existing one.
- `PriceEncoder` uses `constants.MaxPriceAge` and `MinExchanges` from `MutableMarketConfig`:
  - Cached prices older than `MaxPriceAge` are ignored for index price calculations.
  - Fewer than `MinExchanges` fresh prices means no valid index price for adjustment.
- During startup:
  - `PriceEncoder` runs with a grace period where conversion failures are logged at info instead of error.
  - This avoids alert noise while the cache is still warming up.

**On the server side:**

- `UpdateMarketPrices`:
  - Rejects updates with:
    - Zero `MarketPriceUpdates`.
    - Any `ExchangePrice` with default price (e.g. 0) or `LastUpdateTime == nil`.
  - Writes new prices into its own `MarketToExchangePrices` cache.

---

### Where to Look for Specific Behaviours

- **Adding/understanding exchanges and defaults**
  - `constants/static_exchange_details.go`
  - `configs/default_pricefeed_exchange_config.go`
  - `pricefeed_exchange_config.toml`

- **Adding/understanding markets**
  - `exchange_common/market_id.go`
  - `constants/static_market_params_config.go`
  - `configs/default_market_param_config.go`
  - `market_params.toml`

- **Source‑level behaviours**
  - `pricefeed/client/sources/util.go` (validators, medianization, generic error detection).
  - `pricefeed/client/sources/<exchange>/*.go` (each price function).

- **Config update & synchronization**
  - `pricefeed/client/types/price_feed_mutable_market_configs.go`
  - `pricefeed/client/types/mutable_exchange_market_config.go`
  - `pricefeed/client/types/mutable_market_config.go`

- **Fetching, encoding, and caching**
  - `pricefeed/client/price_fetcher/price_fetcher.go`
  - `pricefeed/client/price_encoder/price_encoder.go`
  - `pricefeed/client/types/exchange_to_market_prices.go`
  - `pricefeed/client/subtask_runner.go`

- **Server ingestion**
  - `server/pricefeed.go`

This file should give you a single place to reason about how all the pieces connect and what you need to touch when adding or changing sources and market pairs.

---

### Custom Query `exchange` Endpoints (Unification)

`custom_query` now supports `endpoint_type = "exchange"` as a first-class source that reuses the same exchange wiring as pricefeed.

- `exchange_id` must match a canonical entry from `StaticExchangeDetails` (for example `Binance`, `CoinbaseRates`).
- The query's chain `MarketId` is resolved from `query.id`, then validated against the canonical `(exchange_id, market_id)` mapping parsed from `MarketParam.ExchangeConfigJson`.
- `Ticker`, `AdjustByMarket`, and `Invert` are taken from that canonical mapping; TOML is not authoritative for conversion logic.
- `use_cache = true` reads from `MarketToExchangePrices.GetValidPriceForExchange(market_id, exchange_id, now)` using the canonical `exchange_id` string, matching cache writers.
- `use_cache = false` uses the existing `ExchangeQueryHandler` stack and source functions from `pricefeed/client/sources/*`, so there is no duplicate exchange adapter layer.

Operational notes:

- If reporter mode depends on cache-backed exchange endpoints, ensure there is an active cache writer (pricefeed process and/or custom query exchange refresher).
- Misconfigured `(exchange_id, market_id)` pairs fail during `BuildQueryEndpoints` startup validation, before runtime fetches.

