### Custom Query Architecture

This document describes how the `custom_query` system is wired together: configuration, endpoint templates, RPC and contract readers, combined handlers, aggregation, and how it interacts with the existing pricefeed server cache. It also explains how to add new sources, contract handlers, and queries.

---

### High-Level Overview

The Custom Query Service is a **generalized price/data fetcher** intended for markets or metrics that are not covered by the core pricefeed daemon. It:

- Reads a TOML config describing:
  - **HTTP/API endpoints** (`[endpoints]`).
  - **RPC endpoints** for EVM chains (`[rpc_endpoints]`).
  - **Queries** that reference those endpoints (`[queries.<id>]`).
- For a given query id:
  - Builds:
    - REST/RPC endpoint readers.
    - Contract readers.
    - Combined handlers that mix multiple sources.
  - Fetches in parallel from all configured sources.
  - Aggregates successful responses (median, with optional spread checks).
  - Returns an encoded value plus raw per-source results.

It can:

- Call centralized APIs (Coingecko, Curve, CoinPaprika, etc.).
- Call subgraphs / JSON RPC APIs (e.g. Uniswap Graph, custom HTTP APIs).
- Call smart contracts on EVM chains.
- Combine multiple such sources (e.g., on-chain + off-chain).
- Optionally leverage the **existing pricefeed server cache** as an input to on-chain contract logic or RPC handlers.

---

### Configuration Model

#### TOML Structure

Config is TOML-based and lives in a `custom_query` config file (generated or merged by `WriteDefaultConfigToml` / `MergeCustomQueryConfig`).

Top-level sections:

- `[endpoints]`: Named HTTP/RPC API endpoint templates (non-contract).
- `[rpc_endpoints]`: Named blockchain RPC URL lists.
- `[queries]`: Named queries keyed by their query ID (the hash used on-chain).

Example from `custom_query/README.md`:

- Endpoint:
  - `coingecko` with `url_template`, `method`, `timeout`.
- Query:
  - Identified by a 32-byte hex id.
  - Has `aggregation_method`, `min_responses`, `response_type`, and nested `[[queries.<id>.endpoints]]` entries.

#### Endpoint Templates

File: `custom_query/config.go`, `custom_query/constants.go`

- `EndpointTemplate` describes an HTTP endpoint that can be reused across many queries:
  - `URLTemplate`: e.g. `https://api.coingecko.com/api/v3/simple/price?ids={coin_id}&vs_currencies=usd`.
  - `Method`: `"GET"` or `"POST"`.
  - `Timeout`: integer (milliseconds).
  - `ApiKey`: value or `${ENV_VAR}`; resolved by `processApiKeys`.
  - `Headers`: map of header key/value; `"api_key"` is replaced with the actual key.
  - `Query`: optional request body template (e.g., GraphQL for Uniswap subgraphs).

Static defaults for these live in:

- `StaticEndpointTemplateConfig` in `custom_query/constants.go`:
  - Predefines templates for: `coingecko`, `coinpaprika`, `curve`, `crypto`, `coinmarketcap`, `coinbase`, `osmosis`, `uniswapV4ethereum`, `uniswapV3ethereum`, `sushiswapKatana`.

#### RPC Endpoint Templates (EVM)

File: `custom_query/constants.go`

- `RPCEndpointTemplate`:
  - `URLs`: list of RPC URLs, often with environment-variable API keys.
- `StaticRPCEndpointTemplateConfig`:
  - Example: `ethereum` has three RPC URLs (Infura, Alchemy, Ankr).

These are used when building **contract readers** for EVM chains.

#### Queries

File: `custom_query/config.go`, `custom_query/constants.go`

- `QueryConfig` (from config file / static defaults):
  - `ID`: query ID (hash string).
  - `AggregationMethod`: currently `"median"`.
  - `MinResponses`: minimum number of successful, valid responses required.
  - `ResponseType`: e.g. `"ufixed256x18"` for ABI encoding.
  - `MaxSpreadPercent`: optional max allowed spread between min and max successful values.
  - `Endpoints`: slice of `EndpointConfig` describing the data sources for this query.
  - Runtime-only fields (populated by builder, not from TOML):
    - `ContractReaders []ContractHandler`
    - `RpcReaders []RpcHandler`
    - `CombinedReaders []CombinedHandler`

##### EndpointConfig (per query endpoint)

File: `custom_query/config.go`

- `EndpointConfig` fields:
  - `EndpointType`: selects behaviour:
    - `"contract"`: on-chain contract call.
    - `"combined"`: composite handler.
    - anything else: standard REST/RPC endpoint template key (e.g. `"coingecko"`, `"curve"`, `"osmosis"`).
  - `ResponsePath`: JSON path segments for extracting a value from response (non-contract endpoints).
  - `Params`: map of placeholder name → value to fill `URLTemplate` and `Query`.
  - Telemetry:
    - `MarketId`: logical market name for metrics (e.g. `"SDAI-USD"`).
  - Contract-specific:
    - `Handler`: name of contract handler (e.g. `"wsteth_handler"`, `"susdeusd_handler"`).
    - `Chain`: name of RPC endpoints config (e.g. `"ethereum"`).
  - Cosmos/price-conversion specifics:
    - `Invert`: whether to invert a price (e.g., derivedETH to USD).
    - `UsdViaID`: `MarketId` for an existing pricefeed market used to convert an intermediate price to USD.
  - Combined handler-specific:
    - `CombinedSources map[string]string`: source name → `"contract:<chain>"` or `"rpc:<endpointTemplate>"`.
    - `CombinedConfig map[string]any`: arbitrary per-source config (params, response paths, min responses, max spread).

#### Default Config Generation & Merging

File: `custom_query/default_custom_query_config.go`

- `StaticQueriesConfig`: default built-in queries for various markets:
  - Each entry keyed by a query ID string; includes `Endpoints` referencing endpoint types (`coingecko`, `contract`, `combined`, etc.).
  - Examples:
    - `SDAI-USD`, `USDN-USD`, `SUSDS-USD`, `YTOKEN-USD`, `SUSDE-USD`, `TBTC-USD`, `KING-USD`, `stATOM-USD`, `VYUSD-USD`, `SFRXUSD-USD`, `YIELDFI-YETH-USD`, etc.
  - Combined queries:
    - `VYUSD-USD` uses a combined handler `vyusd_price` with `contract:ethereum`.
    - `SUSN-USD` uses `susn_price` with contract + `coinpaprika` + `coingecko`.
    - `SFRXUSD-USD` uses `sfrxusd_price` with contract + curve + coingecko + coinpaprika.

- `GenerateDefaultConfigTomlString`:
  - Uses Go templates to render a TOML string from:
    - `StaticEndpointTemplateConfig`
    - `StaticRPCEndpointTemplateConfig`
    - `StaticQueriesConfig`

- `WriteDefaultConfigToml`:
  - Ensures config directory exists.
  - If file does not exist: writes a new one from static defaults.
  - If file exists: calls `MergeCustomQueryConfig` to merge static defaults into existing config non-destructively.

- `MergeCustomQueryConfig`:
  - Reads existing config into `Config`.
  - Merges:
    - Endpoint templates (existing override defaults; new defaults added).
    - RPC endpoints.
    - Queries (existing override defaults; new defaults added).
  - Re-renders merged TOML via the same template and validates by reading it back.

This gives you a clear split between **shipped defaults** and **user-extensible config**.

---

### Building Query Endpoints (Runtime Wiring)

File: `custom_query/config.go`, function `BuildQueryEndpoints`

**Purpose**: read a TOML file, resolve env vars and templates, and build ready-to-use `QueryConfig`s containing:

- `ContractReaders` (on-chain EVM calls).
- `RpcReaders` (REST/JSON RPC calls).
- `CombinedReaders` (handlers that mix contract and RPC sources).

#### Steps

1. **Read and parse the TOML**
   - Path is computed by `getCustomQueryConfigFilePath(homeDir, localDir, file)`.
   - File is unmarshaled into `Config`:
     - `Endpoints    map[string]EndpointTemplate`
     - `RPCEndpoints map[string]RPCEndpointTemplate`
     - `Queries      map[string]QueryConfig`

2. **Resolve RPC endpoints**
   - For each `RPCEndpointTemplate`:
     - Expands environment variables in `urls`.
     - Skips any URLs still containing `${...}` (missing env vars).
     - Builds `processedRPCEndpoints[chain]` map of `chain → []resolvedURLs`.

3. **Initialize query map**
   - `queryMap[query.ID] = query` for all entries as a starting point.

4. **Process API keys in endpoint templates**
   - `processApiKeys(&config)`:
     - Matches `ApiKey` fields of the form `${ENV_VAR}`.
     - Reads the env var; warns if unset.
     - Replaces the `ApiKey` string with the actual value.
   - This ensures later URL/template expansions can safely inject API keys.

5. **Build readers per query**

For each `query` in `config.Queries`:

- Initialize:
  - `contractReaders []ContractHandler`
  - `rpcReaders []RpcHandler`
  - `combinedReaders []CombinedHandler`
- For each `EndpointConfig` in `query.Endpoints`:

  **A. Combined endpoints (`EndpointType == "combined"`)**

  - Requires `Handler` (name of combined handler).
  - Constructs:
    - `contractReadersMap: map[string]*contractreader.Reader`
    - `rpcReadersMap: map[string]*rpcreader.Reader`
  - For each entry in `CombinedSources`:
    - If value starts with `"contract:"`:
      - Extract `chain`.
      - Look up `processedRPCEndpoints[chain]` URLs.
      - Create an EVM `contract_reader.Reader` with these URLs.
      - Store in `contractReadersMap[sourceName]`.
    - Else, if value starts with `"rpc:"`:
      - Extract `endpointType`.
      - Find corresponding `EndpointTemplate` in `config.Endpoints`.
      - Build a concrete URL:
        - Start from `template.URLTemplate`.
        - Look up `sourceName + "_params"` in `CombinedConfig` (if present).
          - For each key/value, replace `{key}` in URL.
        - Replace `{api_key}` with `template.ApiKey`.
      - Build headers:
        - Clone `template.Headers`, replacing `"api_key"` sentinel with `template.ApiKey`.
      - Determine response path:
        - Look up `sourceName + "_response_path"` in `CombinedConfig`:
          - Accepts `[]string` or `[]any` of strings.
      - Create an `rpcreader.Reader` with:
        - URL, method, query, headers, responsePath, timeout.
      - Store in `rpcReadersMap[sourceName]`.
  - Compute:
    - `minResponses` from `CombinedConfig["min_responses"]`, default 1.
    - `maxSpreadPercent` from `CombinedConfig["max_spread_percent"]`, default 100.
  - Append a `CombinedHandler` to `combinedReaders`:
    - Contains handler name, both reader maps, config map, and thresholds.

  **B. Contract endpoints (`EndpointType == "contract"`)**

  - Requires:
    - `Handler`: contract handler name.
    - `Chain`: chain key for `processedRPCEndpoints`.
  - Looks up URLs for `endpoint.Chain`.
  - Creates `contract_reader.Reader` with those URLs and a 3-second timeout.
  - Appends a `ContractHandler` to `contractReaders`:
    - `Handler`: name.
    - `Reader`: EVM reader.
    - `MarketId`: from `EndpointConfig`.
    - `SourceId`: `endpoint.EndpointType` (e.g. `"contract"`).

  **C. Regular REST/RPC endpoints (anything else)**

  - Fetches `EndpointTemplate` by `EndpointType` from `config.Endpoints`.
  - Renders URL:
    - Finds `{placeholder}` segments via regex.
    - Ensures each placeholder is either:
      - In `endpoint.Params`, or
      - Exactly `"api_key"`, in which case it is replaced by `template.ApiKey`.
    - Replaces placeholders with `endpoint.Params` values.
    - Fails if any placeholder remains unresolved.
  - Builds headers:
    - Copies `template.Headers`.
    - Replaces `"api_key"` sentinel values with `template.ApiKey`.
  - Renders query body:
    - `processedQuery := template.Query`.
    - Replaces `{key}` with `endpoint.Params[key]`.
  - Creates `rpcreader.Reader` with:
    - URL, method, `processedQuery`, processed headers, `endpoint.ResponsePath`, template timeout.
  - Appends a `RpcHandler` to `rpcReaders`:
    - `Handler`: optional handler name; generic logic will be used if empty.
    - `Reader`: HTTP reader.
    - `Invert`: from `EndpointConfig` (for on-chain conversions).
    - `UsdViaID`: from `EndpointConfig` (links to an existing pricefeed market for conversions).
    - `Method`, `EndpointID`, `MarketId`, `SourceId`.

- After processing all endpoints for the query:
  - `queryMap[query.ID]` is overwritten with a `QueryConfig` that has:
    - Aggregation fields.
    - Populated `ContractReaders`, `RpcReaders`, `CombinedReaders`.

Finally, `BuildQueryEndpoints` returns `map[string]QueryConfig` keyed by query id.

---

### Readers and Handlers

#### RPC Reader (Non-Contract HTTP/JSON)

File: `custom_query/rpc/rpc_reader/reader.go`

- `Reader` holds:
  - `client` with:
    - `baseURL`, `method`, `http.Client` (with configured timeout).
  - `timeout`: per-request context timeout (milliseconds).
  - `maxRetries`, `retryDelay`: exponential backoff for fetches.
  - `Headers`: request headers.
  - `ResponsePath`: JSON path to extract final value.
  - `Query`: optional request body (e.g. GraphQL).

Key methods:

- `NewReader(url, method, query string, headers map[string]string, responsePath []string, timeout int)`:
  - Builds an HTTP client and configures timeouts.

- `FetchJSON(ctx)`:
  - Performs up to `maxRetries + 1` attempts:
    - Each attempt uses `attemptFetch` with a per-call timeout.
    - Uses exponential backoff between retries.
  - Increments Prometheus metrics `RPCCallDuration`, `RPCCallSuccess`, `RPCCallErrors`.
  - Returns raw response bytes on success or a combined error after all endpoints fail.

- `attemptFetch(ctx, method)`:
  - Builds a `http.Request` with given method, optional body (`Query` for POST).
  - Applies headers.
  - Ensures HTTP `StatusOK`.
  - Reads the full response body.

- `ExtractValueFromJSON(data, path)`:
  - Unmarshals JSON into `any` (`map[string]any` or `[]any`).
  - Walks the `path` slice:
    - For maps: treat path element as key.
    - For arrays: treat path element as numeric index string.
  - Returns the final value; errors if keys/indices are missing or types are unexpected.

This is the generic HTTP JSON reader used across all non-contract sources.

#### Contract Reader (EVM)

File: `custom_query/contracts/contract_reader/reader.go`

- `Reader` maintains:
  - A list of `ethClient` instances (each holding `ethclient.Client` + `rpc.Client` + URL + health flag).
  - Timeouts and retry behaviour.

Key methods:

- `NewReader(urls []string, timeout int)`:
  - For each URL:
    - Attempts `rpc.Dial`.
    - Registers as an `ethclient`.
  - Ensures at least one client is healthy.

- `ReadContract(ctx, address, functionSig string, args []string)`:
  - Encodes function call:
    - `encodeFunctionCall`:
      - Strips any ` returns (...)` from the function signature.
      - Computes `methodID` as first 4 bytes of Keccak256 hash of `funcPart`.
      - Parses parameter types and encodes `args` via `go-ethereum/accounts/abi`.
  - Builds `ethereum.CallMsg` with address and calldata.
  - Tries each healthy client with retries:
    - Each attempt uses a per-call timeout.
    - On first success: increments `ContractCallSuccess` and returns raw bytes.
    - On repeated failure on a client: marks client as unhealthy.
  - If all clients fail: increments `ContractCallErrors` and returns error.

- `parseArgument`: converts string arguments to types based on the ABI parameter type (uint, int, address, bool, bytes, etc.).

- `Close`: closes all underlying `rpc.Client` connections.

This is a generic EVM contract call engine; specific contract logic is implemented via contract handlers.

#### Contract Handlers

File: `custom_query/contracts/contract_handlers/handlers.go` (plus individual handler files)

- `ContractHandler` interface:

```go
type ContractHandler interface {
	FetchValue(ctx context.Context, client *reader.Reader, priceCache *pricefeedservertypes.MarketToExchangePrices) (float64, error)
}
```

- Implementations (per contract) live in:
  - `custom_query/contracts/contract_handlers/*.go`:
    - `wsteth.go`, `susde.go`, `rocket_pool_eth.go`, `yfield_yeth.go`, `yieldfi-usd.go`, `king.go`, `susds.go`, etc.
  - Each handler:
    - Knows the contract address(es) and function signatures.
    - Knows how to decode contract call results (e.g., scale factors, decimals).
    - Optionally uses `priceCache` (the pricefeed server cache) to convert intermediate ratios (e.g., token/ETH) into USD (`UsdViaID`).

Handlers are registered in a local registry, and resolved by string name when `fetchFromContractEndpoint` is invoked.

#### RPC Handlers

File: `custom_query/rpc/rpc_handler/handlers.go` (and `registry.go`, `osmosis_pool_price_handler.go`, `generic_handler.go`)

- `RpcHandler` interface:

```go
type RpcHandler interface {
	FetchValue(
		ctx context.Context,
		client *reader.Reader,
		invert bool,
		usdViaID uint32,
		priceCache *pricefeedservertypes.MarketToExchangePrices,
	) (float64, error)
}
```

- Implementations:
  - `generic_handler`: likely:
    - Calls `client.FetchJSON`, then `ExtractValueFromJSON` using `ResponsePath`.
    - Parses the extracted result into a `float64`.
  - Specialized handlers:
    - `osmosis_pool_price_handler`:
      - Interprets Osmosis pool JSON structure.
      - May combine with `priceCache` via `UsdViaID` to convert pool token ratios into USD.
  - Handler lookup is done via `GetHandler(name)`; when endpoint’s `Handler` is empty, `"generic"` is used.

These allow you to add custom logic for non-trivial APIs (e.g., AMM pool state → token price).

#### Combined Handlers

File: `custom_query/combined/combined_handler/handlers.go` and specific handlers:

- Combined handler interface:

```go
type CombinedHandler interface {
	FetchValue(
		ctx context.Context,
		contractReaders map[string]*contractreader.Reader,
		rpcReaders map[string]*rpcreader.Reader,
		priceCache *pricefeedservertypes.MarketToExchangePrices,
		minResponses int,
		maxSpreadPercent float64,
	) (float64, error)
}
```

- `ParallelFetcher` helper:
  - `FetchContract`: runs `ReadContract` in a goroutine, records `[]byte` result or error by key.
  - `FetchRPC`: runs `FetchJSON` in a goroutine, records `[]byte` response or error.
  - `Wait`: waits for all goroutines.
  - `GetResult` / `GetBytes`: retrieve typed results by key.
  - `CalculateMedian`: compute median of `[]float64`.

- Specific combined handlers (e.g., `vyusd_price_handler`, `susn_price_handler`, `sfrxusd_price_handler`) do:
  - Issue multiple on-chain reads and/or HTTP requests in parallel via `ParallelFetcher`.
  - Convert raw bytes/JSON into floats.
  - Apply any protocol-specific math (e.g., share price * underlying price, yield-bearing token conversions).
  - Enforce `minResponses` and `maxSpreadPercent` constraints.
  - Return a single `float64` value to `fetchFromCombinedEndpoint`.

Handlers are registered in a registry; `FetchPrice` resolves them by their handler string.

---

### FetchPrice Flow

File: `custom_query/request.go`

Function: `FetchPrice(ctx context.Context, query QueryConfig, priceCache *pricefeedservertypes.MarketToExchangePrices) (*FetchPriceResult, error)`

#### Inputs

- `QueryConfig` built by `BuildQueryEndpoints` (contains readers and handler metadata).
- `priceCache`:
  - A pointer to `MarketToExchangePrices` on the **server** side (`server/types/pricefeed`).
  - Used by contract/RPC/combined handlers to reference existing on-chain prices (e.g., `UsdViaID` conversions).

#### Execution Steps

1. **Context and concurrency setup**
   - Derives a 5-second timeout context for the overall fetch.
   - Computes `totalEndpoints`:
     - `len(query.RpcReaders) + len(query.ContractReaders) + len(query.CombinedReaders)`.
   - Allocates a buffered `results` channel of that size.
   - Uses a `sync.WaitGroup` to track goroutines.

2. **Dispatch per endpoint type**

   - For each `ContractHandler` in `query.ContractReaders`:
     - Launches `fetchFromContractEndpoint` in a goroutine.
   - For each `RpcHandler` in `query.RpcReaders`:
     - Launches `fetchFromRpcEndpoint` in a goroutine.
   - For each `CombinedHandler` in `query.CombinedReaders`:
     - Launches `fetchFromCombinedEndpoint` in a goroutine.
   - Each goroutine:
     - Executes the relevant fetch method.
     - Sends a `Result` into `results` (with `Value`, `Err`, `EndpointID`, `MarketId`, `SourceId`).
   - Once all goroutines complete:
     - `results` channel is closed.

3. **Collect & classify results**

   - Iterates over `results`:
     - Appends each `Result` to `allResults`.
     - If `Err == nil`:
       - Adds to `successfulResults`.
       - Emits telemetry:
         - `emitPriceForTelemetry`: sets gauge for `PriceEncoderUpdatePrice` using `result.Value`, labeled by `MarketId` and `SourceId`.
         - `emitSuccessForTelemetry`: increments `PriceEncoderPriceConversion` success counter similarly.
     - Else:
       - Emits `emitErrorForTelemetry`, with error reason label.

4. **Check minimum responses**

   - If `len(successfulResults) < query.MinResponses`:
     - Returns an error: "insufficient successful responses".

5. **Aggregate successful results**

   - Calls `aggregateResults(successfulResults, query.AggregationMethod, query.ResponseType, query.MaxSpreadPercent)`.
   - For `"median"`:
     - Uses `MedianInHex(values, responseType, maxSpreadPercent)` from `convert_num.go`:
       - Likely:
         - Sorts values.
         - Checks the spread between min and max vs `MaxSpreadPercent`; errors if too high.
         - Computes median.
         - Encodes output into an Ethereum ABI-compatible hex string (matching `responseType`).

6. **Return FetchPriceResult**

   - On success:
     - `EncodedValue`: hex string encoded value (as expected for reporting).
     - `RawResults`: the full list of all results, successes and failures.
     - `QueryID`: `query.ID`.
     - `ResponseType`: `query.ResponseType`.
     - `SuccessRate`: `float64(len(successfulResults)) / float64(totalEndpoints)`.

This is the central API for the custom query system and is likely what the reporter or specific CLIs call.

---

### Interactions with Pricefeed / Existing Cache

Although the custom query system is independent of the pricefeed daemon for fetching data, it **can depend on the existing pricefeed server cache**:

- `FetchPrice` takes `priceCache *pricefeedservertypes.MarketToExchangePrices`.
- Contract handlers and RPC handlers:
  - Use `priceCache` when needing chain-wide or cross-market conversions.
  - Typical pattern:
    - A contract returns a value in units of some token (e.g., `derivedETH`, pool share price, ratio).
    - Handler:
      - Looks up the current USD price for a base asset (e.g., ETH-USD, ATOM-USD) using `UsdViaID` from config.
      - Multiplies/divides appropriately to obtain USD price for the custom asset.

This means:

- Today, the custom query layer is conceptually layered **on top of** the pricefeed daemon:
  - Some handlers rely on the pricefeed’s market prices to complete their calculations.
- If you were to remove the pricefeed daemon in the future:
  - You’d need to:
    - Replace references to `MarketToExchangePrices` with either:
      - On-chain stored prices.
      - Direct queries to other sources.
      - Or a new internal/custom cache inside the custom query layer.
    - Ensure `UsdViaID` and similar config fields are either removed or replaced with direct logic.

---

### Adding / Modifying Sources and Queries

This section focuses on the **current architecture** and extension points.

#### New HTTP / JSON Source (Non-Contract)

1. **Define/extend an `EndpointTemplate`**:
   - Add a new entry to `StaticEndpointTemplateConfig` (optional but recommended), or configure in TOML under `[endpoints.<name>]`:
     - Set `url_template`, `method`, `timeout`.
     - Optionally, `query` (for POST) and `headers` / `api_key`.

2. **Add to a query**:
   - In `StaticQueriesConfig` (for defaults) or in your TOML:
     - Add an endpoint to `[[queries.<id>.endpoints]]`:
       - `endpoint_type = "<name>"` (must match your template).
       - `response_path = ["path", "to", "value"]`.
       - `params = { ... }` for any placeholders in `url_template` and `query`.
       - `market_id = "MARKET-USD"` for telemetry.
       - Optional `handler = "custom_handler_name"` if you write a specialized `RpcHandler`.

3. **Optional custom RPC handler**:
   - Implement `RpcHandler` in `custom_query/rpc/rpc_handler`.
   - Register it in the handler registry.
   - Reference it with `handler = "your_handler_name"` in the endpoint config.

#### New Contract-Based Source

1. **Ensure RPC endpoints are configured**:
   - Add/extend `StaticRPCEndpointTemplateConfig` or TOML `[rpc_endpoints.<chain>]` with URLs.

2. **Implement a `ContractHandler`**:
   - In `custom_query/contracts/contract_handlers`:
     - Create a file (e.g. `mytoken.go`) that implements:
       - `FetchValue(ctx, client *contract_reader.Reader, priceCache *MarketToExchangePrices) (float64, error)`.
     - Within:
       - Use `client.ReadContract` with the token’s contract address, ABI function signatures, arguments.
       - Decode the result to a `float64` representing USD price (using `priceCache` if needed).
   - Register your handler name in the contract handler registry.

3. **Add a contract endpoint to a query**:
   - In `StaticQueriesConfig` or config TOML:
     - Under `[[queries.<id>.endpoints]]`:
       - `endpoint_type = "contract"`.
       - `handler = "your_handler_name"`.
       - `chain = "ethereum"` (or another chain defined under `[rpc_endpoints]`).
       - `market_id = "TOKEN-USD"`.

#### New Combined Source

1. **Implement a `CombinedHandler`**:
   - In `custom_query/combined/combined_handler`:
     - Implement a new type that satisfies:
       - `FetchValue(ctx, contractReaders, rpcReaders, priceCache, minResponses, maxSpreadPercent) (float64, error)`.
     - Use `ParallelFetcher` to:
       - Trigger multiple contract and RPC fetches in parallel.
       - Collect `[]byte` results.
       - Convert them into floats.
       - Enforce min responses and spread constraints.
   - Register the handler name in the combined handler registry.

2. **Configure a combined endpoint in a query**:
   - In `StaticQueriesConfig` or config TOML:
     - Under `[[queries.<id>.endpoints]]`:
       - `endpoint_type = "combined"`.
       - `handler = "<your_handler_name>"`.
       - `combined_sources = { ... }`, e.g.:
         - `ethereum = "contract:ethereum"`.
         - `coingecko = "rpc:coingecko"`.
         - `curve = "rpc:curve"`.
       - `combined_config` with:
         - `min_responses`, `max_spread_percent`.
         - Per-source params keys (`<source>_params`) and response paths (`<source>_response_path`).
         - Any other handler-specific knobs.
       - `market_id = "COMBINED-ASSET-USD"`.

3. **Runtime**:
   - `BuildQueryEndpoints` will:
     - Initialize readers as per `combined_sources`.
     - Attach them to a `CombinedHandler` instance used by `FetchPrice`.

---

### Summary of Architectural Differences vs Pricefeed Daemon

From a source perspective:

- **Pricefeed daemon**:
  - Focused on continuous, periodic polling of a fixed set of exchanges.
  - Maintains an internal, exchange-specific cache.
  - Produces a rich `ExchangeToMarketPrices` map and pushes to the server.

- **Custom query**:
  - Focused on on-demand, per-query fetches defined via TOML.
  - Orchestrates multiple heterogeneous sources (centralized APIs, EVM contracts, AMM pools, etc.).
  - Aggregates and returns a single encoded value per query along with raw source details.
  - Optionally reuses the server’s `MarketToExchangePrices` as an input to more complex calculations.

Understanding these layers and extension points should give you a strong foundation if you decide to consolidate everything into custom queries (or vice versa) in the future. For now, this file serves as a complete map of how the custom query system is wired across sources and contract calls.

---

### Exchange Endpoint Telemetry and Migration Notes

For `endpoint_type = "exchange"`, custom query emits dedicated counters so operators can distinguish live, cache, and refresher behavior:

- `pricefeed_daemon.custom_query_exchange_live_fetch.{success|error}`
- `pricefeed_daemon.custom_query_exchange_cache_read.{success|error}`
- `pricefeed_daemon.custom_query_exchange_refresher.{success|error}`

These counters are labeled with:

- `exchange_id` (canonical exchange id string)
- `query_id` (query hash/id)
- `market_id` (optional human-readable endpoint `market_id` when configured)
- `reason` (error counters only)

Migration guidance:

- Prefer replacing redundant REST templates with `endpoint_type = "exchange"` where the market already exists in canonical `ExchangeConfigJson`.
- Keep `market_id` for observability labels, but do not rely on it for market resolution.
- For cache-backed paths (`use_cache = true`), ensure a cache writer is active (pricefeed daemon and/or custom query exchange refresher).

