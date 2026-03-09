# Source Migration List

This document lists all sources currently used in the original pricefeed setup that need to be migrated to the unified pricefeed system.

## Exchange Sources

These are the exchange sources currently defined in `constants/static_exchange_details.go` and used in `constants/static_market_params_config.go`.

**Important**: The `Current URL` values below are **legacy ticker-style endpoints from the original pricefeed** and are documented here **for reference only**. The unified pricefeed **must not use these ticker endpoints**; all unified `SourceConfig` entries will point to new, direct price/symbol endpoints (or aggregator/on-chain sources) that provide the required token coverage.

### Production Exchanges

| Exchange ID | Exchange Name | Current URL | Current Endpoint Type | Notes |
|------------|---------------|-------------|----------------------|-------|
| `EXCHANGE_ID_BINANCE` | Binance | `https://data-api.binance.vision/api/v3/ticker/24hr` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_BINANCE_US` | BinanceUS | `https://api.binance.us/api/v3/ticker/24hr` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_BITFINEX` | Bitfinex | `https://api-pub.bitfinex.com/v2/tickers?symbols=ALL` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_BITSTAMP` | Bitstamp | `https://www.bitstamp.net/api/v2/ticker/` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_CRYPTO_COM` | Crypto.com | `https://api.crypto.com/v2/public/get-ticker` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_GATE` | Gate.io | `https://api.gateio.ws/api/v4/spot/tickers` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_HUOBI` | Huobi | `https://api.huobi.pro/market/tickers` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_KRAKEN` | Kraken | `https://api.kraken.com/0/public/Ticker` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_KUCOIN` | KuCoin | `https://api.kucoin.com/api/v1/market/allTickers` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_MEXC` | MEXC | `https://api.mexc.com/api/v3/ticker/24hr` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_OKX` | OKX | `https://www.okx.com/api/v5/market/tickers?instType=SPOT` | Ticker endpoint | Currently uses ticker endpoint - needs direct price endpoint |
| `EXCHANGE_ID_COINBASE_RATES` | Coinbase Rates | `https://api.coinbase.com/v2/exchange-rates` | Exchange rates endpoint | Different format - uses exchange rates API |

### Test Exchanges

| Exchange ID | Exchange Name | Current URL | Current Endpoint Type | Notes |
|------------|---------------|-------------|----------------------|-------|
| `EXCHANGE_ID_TEST_EXCHANGE` | Test Exchange | N/A | Test implementation | Test exchange for development |
| `EXCHANGE_ID_TEST_VOLATILE_EXCHANGE` | Test Volatile Exchange | N/A | Test implementation | Test exchange with volatile prices |
| `EXCHANGE_ID_TEST_FIXED_PRICE_EXCHANGE` | Test Fixed Price Exchange | N/A | Test implementation | Test exchange with fixed prices |

### Exchange Usage in Market Params

The following asset pairs use these exchanges (from `constants/static_market_params_config.go`):

- **SAGA-USD**: Binance, Gate, Huobi, Kraken, MEXC
- **BTC-USD**: Binance, BinanceUS, Bitfinex, Bitstamp, Crypto.com, Kraken, OKX
- **ETH-USD**: Binance, BinanceUS, Bitfinex, Bitstamp, Crypto.com, Kraken, OKX
- **TRB-USD**: Binance, Crypto.com, OKX, CoinbaseRates
- **USDC-USD**: Binance, OKX, Huobi, Bitstamp, Gate, Kraken, KuCoin, MEXC
- **USDT-USD**: BinanceUS, Bitstamp, Crypto.com
- **ATOM-USD**: BinanceUS, Kraken, Crypto.com, OKX, CoinbaseRates
- **STETH-USD**: Huobi, OKX, Gate
- **USDE-USD**: Kraken, Gate, KuCoin, Binance

**Note**: Each exchange uses different ticker symbol formats (e.g., `"BTCUSDT"`, `"BTC/USD"`, `"tBTCUSD"`, etc.). These will need to be mapped to direct price endpoint parameters.

---

## Custom Query REST Sources

These are REST API endpoints defined in `custom_query_config.toml` (endpoints section). Based on the codebase analysis:

### Known REST Endpoints (from handlers and examples)

| Endpoint Name | URL Template | Method | Notes |
|--------------|--------------|--------|-------|
| `coingecko` | `https://api.coingecko.com/api/v3/simple/price?ids={coin_id}&vs_currencies=usd` | GET | Used in sFRXUSD handler |
| `coinpaprika` | `https://api.coinpaprika.com/v1/tickers/{coin_id}?quotes=USD` | GET | Used in sFRXUSD handler |
| `curve` | `https://prices.curve.finance/v1/usd_price/ethereum/{contract_address}` | GET | Used in sFRXUSD handler |
| `crypto` | `https://api.crypto.com/v2/public/get-ticker?instrument_name={instrument_name}` | GET | Example from test config |
| `etherscan` | `https://api.etherscan.io/api?module=block&action=getblocknobytime&timestamp={timestamp}&closest=before&apikey={api_key}` | GET | Example from test config |

**Note**: The actual list of REST endpoints will be determined from the production `custom_query_config.toml` file. These are examples found in test configs and handler code.

---

## Contract Sources

Contract sources are defined in `custom_query_config.toml` under queries with `endpoint_type = "contract"`. They require:
- `handler`: Contract handler name
- `chain`: Chain name (e.g., "ethereum", "polygon", "arbitrum", etc.)
- RPC endpoints configured in `rpc_endpoints` section

### Supported Chains (from `chainNameToChainID` mapping)

| Chain Name | Chain ID | Notes |
|-----------|----------|-------|
| `ethereum` | 1 | Mainnet |
| `polygon` | 137 | Polygon PoS |
| `arbitrum` | 42161 | Arbitrum One |
| `optimism` | 10 | Optimism |
| `base` | 8453 | Base |
| `bsc` | 56 | Binance Smart Chain |
| `avalanche` | 43114 | Avalanche C-Chain |

### Contract Handlers (from `contract_handlers/registry.go`)

| Handler Name | Description |
|-------------|-------------|
| `wsteth_handler` | Wrapped Staked ETH handler |
| `susds_handler` | sUSD handler |
| `reth_handler` | Rocket Pool ETH handler |
| `king_handler` | KING token handler |
| `yieldfi_yeth_handler` | YieldFi yETH handler |
| `yieldfi_yusd_handler` | YieldFi yUSD handler |
| `susdeusd_handler` | sUSDE/USD handler |

**Note**: Contract sources use Multicall3 for batching. Each contract call is identified by a call key and grouped by chain ID and batch group.

---

## RPC Sources

RPC sources are defined in `custom_query_config.toml` under `rpc_endpoints` section. They provide blockchain RPC URLs for contract readers.

### RPC Endpoint Structure

Each RPC endpoint is keyed by chain name and contains:
- `urls`: Array of RPC endpoint URLs (supports environment variable expansion)

**Note**: The actual list of RPC endpoints will be determined from the production `custom_query_config.toml` file. Common chains include ethereum, polygon, arbitrum, optimism, base, bsc, and avalanche.

---

## Combined Handlers

Combined handlers use multiple sources (contract + RPC) to calculate prices. They are defined in `custom_query_config.toml` with `endpoint_type = "combined"`.

### Known Combined Handlers

| Handler Name | Sources Used | Description |
|-------------|--------------|-------------|
| `sfrxusd_price` | Contract: ethereum<br>RPC: coingecko, curve, coinpaprika | Calculates sFRXUSD price using contract data and FRX/USD spot prices |
| `susn_price` | Contract: ethereum<br>RPC: (configurable) | Calculates SUSN price using contract conversion rate and RPC prices |
| `vyusd_price` | Contract: ethereum<br>RPC: (configurable) | Calculates vYUSD price (implementation details in handler) |

**Note**: Combined handlers will need special handling in the unified system as they combine multiple source types.

---

## Migration Notes

### Key Changes Required

1. **Exchange Sources**: 
   - All exchanges currently use ticker endpoints that return all market data
   - Need to migrate to direct price endpoints that accept specific symbol/pair parameters
   - Each exchange has different symbol formats that need to be mapped

2. **Batching Strategy**:
   - Need to investigate if each exchange supports:
     - Query parameter batching (e.g., `?symbols=BTCUSDT,ETHUSDT`)
     - Body batching (POST with multiple symbols in body)
     - On-demand only (single symbol per request)

3. **Symbol Mapping**:
   - Current system uses ticker symbols (e.g., `"BTCUSDT"`, `"BTC/USD"`, `"tBTCUSD"`)
   - New system needs to map query IDs to exchange-specific symbol formats
   - Symbol format varies by exchange

4. **Custom Query Sources**:
   - REST endpoints use URL templates with placeholders
   - Need to determine if they support batching or are on-demand only
   - Contract sources use Multicall3 for batching (already implemented)

5. **Test Exchanges**:
   - Test exchanges may not need real API endpoints
   - Can be configured as mock sources for testing

---

## Step 3.2: Exchange API Batching Investigation

**Goal**: Determine if each exchange supports batching via direct price endpoints (NOT ticker endpoints).

### Investigation Approach

For each exchange, we need to check:
1. **Direct Price Endpoint**: Does the exchange have an endpoint that accepts a specific symbol/pair parameter?
2. **Batching Support**: Can multiple symbols be queried in a single request?
   - **Query Parameter Batching**: Multiple symbols in URL query params (e.g., `?symbols=BTCUSDT,ETHUSDT`)
   - **Body Batching**: Multiple symbols in POST request body
   - **On-Demand Only**: Single symbol per request (no batching)

### Exchange API Investigation Results

**Note**: The following findings are based on common exchange API patterns. **Actual API verification is required** before implementation.

#### Binance / BinanceUS
- **Direct Price Endpoint**: `/api/v3/ticker/price?symbol=BTCUSDT` (single symbol)
- **Batching Support**: `/api/v3/ticker/price?symbols=["BTCUSDT","ETHUSDT"]` (query param with JSON array) OR `/api/v3/ticker/price?symbols=BTCUSDT,ETHUSDT` (comma-separated)
- **Batching Strategy**: `query_param` (needs verification)
- **Status**: ⚠️ **Needs API verification**

#### Bitfinex
- **Direct Price Endpoint**: `/v2/ticker/tBTCUSD` (single symbol)
- **Batching Support**: `/v2/tickers?symbols=tBTCUSD,tETHUSD` (query param, comma-separated)
- **Batching Strategy**: `query_param` (needs verification)
- **Status**: ⚠️ **Needs API verification**

#### Bitstamp
- **Direct Price Endpoint**: `/api/v2/ticker/btcusd` (single symbol, lowercase)
- **Batching Support**: Unknown - may require multiple requests
- **Batching Strategy**: `on_demand` (likely - needs verification)
- **Status**: ⚠️ **Needs API verification**

#### Crypto.com
- **Direct Price Endpoint**: `/v2/public/get-ticker?instrument_name=BTC_USD` (single instrument)
- **Batching Support**: Unknown - may require multiple requests
- **Batching Strategy**: `on_demand` (likely - needs verification)
- **Status**: ⚠️ **Needs API verification**

#### Gate.io
- **Direct Price Endpoint**: `/api/v4/spot/tickers?currency_pair=BTC_USDT` (single pair)
- **Batching Support**: `/api/v4/spot/tickers?currency_pairs=BTC_USDT,ETH_USDT` (query param, comma-separated)
- **Batching Strategy**: `query_param` (needs verification)
- **Status**: ⚠️ **Needs API verification**

#### Huobi
- **Direct Price Endpoint**: `/market/detail/merged?symbol=btcusdt` (single symbol, lowercase)
- **Batching Support**: Unknown - may require multiple requests
- **Batching Strategy**: `on_demand` (likely - needs verification)
- **Status**: ⚠️ **Needs API verification**

#### Kraken
- **Direct Price Endpoint**: `/0/public/Ticker?pair=BTCUSD` (single pair)
- **Batching Support**: `/0/public/Ticker?pair=BTCUSD,ETHUSD` (query param, comma-separated)
- **Batching Strategy**: `query_param` (needs verification)
- **Status**: ⚠️ **Needs API verification**

#### KuCoin
- **Direct Price Endpoint**: `/api/v1/market/orderbook/level1?symbol=BTC-USDT` (single symbol)
- **Batching Support**: Unknown - may require multiple requests
- **Batching Strategy**: `on_demand` (likely - needs verification)
- **Status**: ⚠️ **Needs API verification**

#### MEXC
- **Direct Price Endpoint**: `/api/v3/ticker/price?symbol=BTCUSDT` (single symbol)
- **Batching Support**: `/api/v3/ticker/price?symbols=BTCUSDT,ETHUSDT` (query param, comma-separated - similar to Binance)
- **Batching Strategy**: `query_param` (needs verification)
- **Status**: ⚠️ **Needs API verification**

#### OKX
- **Direct Price Endpoint**: `/api/v5/market/ticker?instId=BTC-USDT` (single instrument)
- **Batching Support**: `/api/v5/market/ticker?instId=BTC-USDT,ETH-USDT` (query param, comma-separated)
- **Batching Strategy**: `query_param` (needs verification)
- **Status**: ⚠️ **Needs API verification**

#### Coinbase Rates
- **Direct Price Endpoint**: `/v2/exchange-rates?currency=BTC` (single currency)
- **Batching Support**: Unknown - exchange rates API may not support batching
- **Batching Strategy**: `on_demand` (likely - needs verification)
- **Status**: ⚠️ **Needs API verification**

### Verification Required

**Action Items**:
1. Test each exchange's direct price endpoint with a single symbol
2. Test batching support (if applicable) with multiple symbols
3. Verify response format and symbol format requirements
4. Document exact endpoint URLs and parameter formats
5. Note any rate limits or restrictions

### Custom Query REST Endpoints

For custom query REST endpoints (CoinGecko, CoinPaprika, Curve, etc.):
- **CoinGecko**: `/api/v3/simple/price?ids=bitcoin,ethereum&vs_currencies=usd` - supports query param batching
- **CoinPaprika**: `/v1/tickers/{coin_id}` - single coin per request, likely `on_demand`
- **Curve**: `/v1/usd_price/ethereum/{contract_address}` - single address per request, likely `on_demand`

**Status**: ⚠️ **Needs verification for each endpoint**

---

## Step 3.3: Generated Exchange SourceConfig Examples

The following `SourceConfig` examples correspond to the Go definitions in
`unified_config/migration/exchange_migration.go`. All exchanges are modeled as
`Type = "rest"` sources that point at direct price/symbol endpoints (not ticker‑style
“all markets” endpoints).

### Example TOML snippets

```toml
[[sources]]
id = "Binance"
type = "rest"
base_url = "https://api.binance.com/api/v3/ticker/price"
batchable = true
batch_strategy = "query_param"
batch_group = "exchanges"
update_interval_seconds = 30
cache_ttl_seconds = 15

[[sources]]
id = "BinanceUS"
type = "rest"
base_url = "https://api.binance.us/api/v3/ticker/price"
batchable = true
batch_strategy = "query_param"
batch_group = "exchanges"
update_interval_seconds = 30
cache_ttl_seconds = 15

[[sources]]
id = "Bitfinex"
type = "rest"
base_url = "https://api-pub.bitfinex.com/v2/tickers"
batchable = true
batch_strategy = "query_param"
batch_group = "exchanges"
update_interval_seconds = 30
cache_ttl_seconds = 15

[[sources]]
id = "Bitstamp"
type = "rest"
base_url = "https://www.bitstamp.net/api/v2/ticker"
batchable = false
batch_group = "exchanges"
cache_ttl_seconds = 15

[[sources]]
id = "CryptoCom"
type = "rest"
base_url = "https://api.crypto.com/v2/public/get-ticker"
batchable = false
batch_group = "exchanges"
cache_ttl_seconds = 15

[[sources]]
id = "Gate"
type = "rest"
base_url = "https://api.gateio.ws/api/v4/spot/tickers"
batchable = true
batch_strategy = "query_param"
batch_group = "exchanges"
update_interval_seconds = 30
cache_ttl_seconds = 15

[[sources]]
id = "Huobi"
type = "rest"
base_url = "https://api.huobi.pro/market/detail/merged"
batchable = false
batch_group = "exchanges"
cache_ttl_seconds = 15

[[sources]]
id = "Kraken"
type = "rest"
base_url = "https://api.kraken.com/0/public/Ticker"
batchable = true
batch_strategy = "query_param"
batch_group = "exchanges"
update_interval_seconds = 30
cache_ttl_seconds = 15

[[sources]]
id = "Kucoin"
type = "rest"
base_url = "https://api.kucoin.com/api/v1/market/orderbook/level1"
batchable = false
batch_group = "exchanges"
cache_ttl_seconds = 15

[[sources]]
id = "Mexc"
type = "rest"
base_url = "https://api.mexc.com/api/v3/ticker/price"
batchable = true
batch_strategy = "query_param"
batch_group = "exchanges"
update_interval_seconds = 30
cache_ttl_seconds = 15

[[sources]]
id = "Okx"
type = "rest"
base_url = "https://www.okx.com/api/v5/market/ticker"
batchable = true
batch_strategy = "query_param"
batch_group = "exchanges"
update_interval_seconds = 30
cache_ttl_seconds = 15

[[sources]]
id = "CoinbaseRates"
type = "rest"
base_url = "https://api.coinbase.com/v2/exchange-rates"
batchable = false
batch_group = "exchanges"
cache_ttl_seconds = 15
```

Test exchanges are modeled similarly, but with simple placeholder URLs and
no batching:

```toml
[[sources]]
id = "TestExchange"
type = "rest"
base_url = "https://example.com/test-exchange"
batchable = false
batch_group = "test_exchanges"
cache_ttl_seconds = 15

[[sources]]
id = "TestVolatileExchange"
type = "rest"
base_url = "https://example.com/test-volatile-exchange"
batchable = false
batch_group = "test_exchanges"
cache_ttl_seconds = 15

[[sources]]
id = "TestFixedPriceExchange"
type = "rest"
base_url = "https://example.com/test-fixed-price-exchange"
batchable = false
batch_group = "test_exchanges"
cache_ttl_seconds = 15
```

These examples are intended as a concrete reference for how exchange sources
will appear in `sources.toml` after migration.

## Next Steps

1. **Step 3.2 (Complete)**: Documented investigation approach and initial findings
2. **Step 3.2 (Remaining)**: Verify actual API endpoints and batching support through testing
3. **Step 3.3 (Complete)**: Generated migration source configs based on current API assumptions
4. **Step 3.4-3.5**: Implement migration utilities to convert old configs to new format
