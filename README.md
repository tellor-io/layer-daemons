# Daemon

**Note:** Daemon services code was adopted from dydx [](https://github.com/dydxprotocol/v4-chain/tree/main/protocol/daemons) and reconfigured.

## Task loops

## PriceFetcher

- Will query exchanges for prices once or multiple times based on wether the api supports single vs multi markets; ie wether an api needs to be queried for each pair individually or can return multiple pairs at once, [See here for exchange details](./constants/static_exchange_details.go).

## PriceEncoder

- Will update cache with the queried prices and encode appropriately also make adjustments as necessary based on if adjustByMarket is defined.

### Configuration

## Exchange Config default

```go
[[exchanges]]
ExchangeId = "Binance"  // exchange identifier
IntervalMs = 2500  // Delays between sending api requests
TimeoutMs = 3000  // Max timeout
MaxQueries = 1  // Max number of calls in a loop.
```

Defaults for exchange information can be found [here](./configs/default_pricefeed_exchange_config.go)

## Market Pair defaults

Defaults for market pair can be found [here](./configs/default_market_param_config.go)

example:

```go
[[market_params]]
ExchangeConfigJson = "{\"exchanges\":[{\"exchangeName\":\"Binance\",\"ticker\":\"\\\"ETHBTC\\\"\"},{\"exchangeName\":\"Bitfinex\",\"ticker\":\"tETHBTC\",\"adjustByMarket\":\"BTC-USD\"}]}" // this is just an example to show how to use adjustByMarket.  you can use ETH-USD without adjustbymarket
Exponent = -6
Id = 2
MinExchanges = 1
MinPriceChangePpm = 1000
Pair = "ETH-BTC"
QueryData = "0000.."
```

```go
type MarketParam struct {
    // Unique, sequentially-generated value.
    Id uint32
    // The human-readable name of the market pair (e.g. `BTC-USD`).
    Pair string
    // Static value. The exponent of the price.
    // For example if `Exponent == -5` then a `Value` of `1,000,000,000`
    // represents “$10,000`. Therefore `10 ^ Exponent` represents the smallest
    // price step (in dollars) that can be recorded.
    Exponent int32
    // The minimum number of exchanges that should be reporting a live price for
    // a price update to be considered valid.
    MinExchanges uint32
    // The minimum allowable change in `price` value that would cause a price
    // update on the network. Measured as `1e-6` (parts per million).
    MinPriceChangePpm uint32
    // A string of json that encodes the configuration for resolving the price
    // of this market on various exchanges.
    ExchangeConfigJson string
    // Query data is the market pair represention in layer
    QueryData string
}
```

**Note:**
A price is valid by default up to 30 seconds; to change this to a different default edit the `constants.MaxPriceAge`

**Also:** Config files are written to homedir/.layer/config/.
To change/add exchange details or market pairs edit the files `pricefeed_exchange_config.toml` or `market_params.toml` respectively.

### Median Server

Median server was added for a way to query median values that were from an endpoint or cli. See usage [here](../x/oracle/client/cli/query_all_get_median.go).
All median values or median value given query data using the following commands respectively.
`layerd query oracle get-all-median-values`
`layerd query oracle get-median-value <querydata>`

## How to add a market pair as defaults to be queried with existing APIs [Exchange_Details](./constants/static_exchange_details.go)?

- Add market_id for your pair in [exchange_common](./exchange_common/market_id.go)

```go
const (
    BTCUSD_ID uint32 = 0
    ETHUSD_ID uint32 = 1
    TRBUSD_ID uint32 = 69
    NEWPAIR_ID uint32 = <unique-number>
)
```

- Add market param config to [static_market_params_config](./constants/static_market_params_config.go)

```go
exchange_common.TRBUSD_ID: {
        Id:                 exchange_common.TRBUSD_ID,
        Pair:               `"TRB-USD"`,
        Exponent:           -6,
        MinExchanges:       1,
        MinPriceChangePpm:  1000,
        ExchangeConfigJson: `{\"exchanges\":[{\"exchangeName\":\"Binance\",\"ticker\":\"\\\"TRBUSDT\\\"\"},{\"exchangeName\":\"Bybit\",\"ticker\":\"TRBUSDT\"},{\"exchangeName\":\"CoinbasePro\",\"ticker\":\"TRB-USD\"}]}`,
        QueryData:          `"00000000000000000000000000000000000000000000000000000000000000400000000000000000000000000000000000000000000000000000000000000080000000000000000000000000000000000000000000000000000000000000000953706f745072696365000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000c0000000000000000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000000800000000000000000000000000000000000000000000000000000000000000003747262000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000037573640000000000000000000000000000000000000000000000000000000000"`,
    },
```

## Price Guard

The Price Guard is a safety mechanism that prevents the reporter from submitting prices that deviate too significantly from the last reported price.

### Flags

| Flag | Type | Description | Required (if enabled) |
|------|------|-------------|---------------------|
| `--price-guard-enabled` | bool | Enables the price guard mechanism | No |
| `--price-guard-threshold` | float64 | Maximum allowed percentage change (e.g., 0.5 = 50%). Submissions exceeding this change from the last reported price will be blocked. | Yes |
| `--price-guard-max-age` | duration | Time after which a stored price is considered expired (e.g., "1h"). If the last price is expired, the new price is accepted regardless of deviation. | Yes |
| `--price-guard-update-on-blocked` | bool | If true, updates the internal "last known price" to the new value even if submission was blocked. If false, keeps the old price as the baseline. | Yes |

### Notes

1. **First Submission:** Always allowed.
2. **Expired Price:** If time since last update > `max-age`, the new price is accepted and becomes the new baseline.
3. **Deviation Check:** Calculates percentage change: `abs(new - old) / old`.
   - If change > `threshold`: Submission is BLOCKED.
   - If change <= `threshold`: Submission is ALLOWED.
4. **Update on Blocked:**
   - If `true`: A blocked price becomes the new baseline for future checks.
   - If `false`: The old price remains the baseline; future submissions must be within threshold of the *old* price.

## Auto balance-to-keep

The reporter daemon can keep a target **loya** balance in the reporter wallet and automatically bridge any excess to Ethereum once per day. This uses Layer’s `MsgWithdrawTokens` bridge message (`isBridge` gas bucket, same tx pipeline as other bridge operations).

### Flags

Configure via CLI flags only (not environment variables):

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--auto-balance-to-keep` | uint64 | `0` | Target wallet balance in **loya** (`0` = disabled). Any amount above this (minus the gas reserve below) is bridged. |
| `--auto-balance-execution-time` | string | `00:00` | UTC time to check balance and bridge, format **`HH:MM`** with two-digit hour and minute (e.g. `03:00`, `15:30`). |
| `--auto-balance-eth-addr` | string | `""` | Ethereum recipient for bridged tokens. Required when `--auto-balance-to-keep > 0`. May include or omit the `0x` prefix. Validated with standard hex address checks at startup. |

### Behavior

1. **Schedule:** Once per UTC day at `--auto-balance-execution-time`, the daemon queries the reporter wallet’s `loya` balance.
2. **Amount:** `bridge_amount = wallet_balance - auto-balance-to-keep - 1_000_000` (a fixed **1 TRB** reserve in loya is left for future gas). If `bridge_amount <= 0`, nothing is sent.
3. **Already bridged today:** After a successful on-chain withdraw, further runs that UTC day are skipped. This guard is **in-memory only**; restarting the daemon the same day may attempt another bridge until one succeeds again.
4. **Retries:** Failed broadcast or non-zero tx code is retried up to **3** times via the tx channel before giving up until the next scheduled run.
5. **Shutdown:** Bridge txs are enqueued with `trySend` so shutdown does not panic on a closed channel.

### Startup validation

When `--auto-balance-to-keep > 0`, the reporter **fails to start** if:

- `--auto-balance-eth-addr` is missing or not a valid Ethereum address
- `--auto-balance-execution-time` is not valid `HH:MM` (two-digit hour/minute, hour 0–23, minute 0–59)

### Example

Keep 5 TRB in the wallet (5_000_000 loya), run the check daily at 03:00 UTC, and bridge excess to an Ethereum address:

```bash
reporterd start \
  --auto-balance-to-keep=5000000 \
  --auto-balance-execution-time=03:00 \
  --auto-balance-eth-addr=0x6Ec401744008f4B018Ed9A36f76e6629799Ee50E \
  # ... other required reporter flags (--home, --from, --grpc, --node, etc.)
```

**Note:** Amounts are in **loya** (micro-denom), not whole TRB. `1 TRB = 1_000_000 loya`.
