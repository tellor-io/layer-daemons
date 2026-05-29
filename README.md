# Daemon

**Note:** Daemon services code was adopted from [dYdX](https://github.com/dydxprotocol/v4-chain/tree/main/protocol/daemons) and reconfigured.

## Configuration

The daemon loads environment variables from the current directory's `.env` file, or from `../.env` when run from a subdirectory. See [`env.example`](./env.example) for a complete starting point.

Most CLI flags can also be provided as environment variables by uppercasing the flag name and replacing `-` or `.` with `_`, for example `--keyring-backend` becomes `KEYRING_BACKEND`. `LAYER_HOME` is preferred for the Layer home directory so the daemon does not accidentally use the shell's `HOME`.

Layer endpoint configuration can be provided with comma-separated environment variables:

```sh
RPC_NODES=http://node_endpoint1:26657,http://node_endpoint2:26657
GRPC_NODES=127.0.0.1:9090,node2:9090
```

The first endpoint in each list is treated as the primary endpoint. Later entries are used as ordered fallbacks.

Both endpoint types are required when starting the reporter daemon:

- `GRPC_NODES` / `--grpc` configures Cosmos gRPC query services.
- `RPC_NODES` / `--node` configures CometBFT RPC. The reporter uses this for startup chain ID validation, block/status polling, transaction broadcast, and transaction lookup while waiting for inclusion.

Endpoint env vars take precedence over the existing CLI flags:

- `RPC_NODES` is preferred over `--node`.
- `GRPC_NODES` is preferred over `--grpc`.
- If an env var is unset, the daemon preserves the old behavior by using the matching flag value as a single endpoint.

At startup, the daemon checks the configured gRPC and CometBFT RPC endpoints for a matching chain ID and starts with the first healthy matching endpoints. The reporter keeps both endpoint lists and falls back to later nodes for network/client failures. gRPC fallback is used for reporter chain queries, while RPC fallback is used for status checks, transaction lookup, and transaction broadcast. The reporter also periodically probes the primary endpoints and switches back when they are healthy again. It does not switch endpoints for semantic chain failures such as out-of-gas responses, non-zero tx result codes, or normal tx-not-found polling.

The pricefeed client is started with the selected gRPC endpoint only. Endpoint-list fallback currently applies to the reporter client's chain query and transaction paths, not to the pricefeed client.

Ethereum JSON-RPC configuration uses the same comma-separated primary/fallback pattern:

```sh
BRIDGE_CHAIN_RPC_NODES=https://mainnet.infura.io/v3/YOUR_INFURA_API_KEY,https://eth-mainnet.g.alchemy.com/v2/YOUR_ALCHEMY_API_KEY
```

`BRIDGE_CHAIN_RPC_NODES` is used by token bridge deposit monitoring. The first endpoint is tried first; later entries are ordered fallbacks.

Ethereum mainnet custom query contract reads use the built-in mainnet endpoint templates by default. If `BRIDGE_CHAIN_RPC_NODES` points at a non-mainnet bridge chain such as Sepolia, set `ETH_MAINNET_RPC_NODES` to a comma-separated Ethereum mainnet endpoint list for custom queries.

Custom query API keys are read from the generated `custom_query_config.toml` entries that reference environment placeholders. The current built-in templates use `CMC_PRO_API_KEY`, `CGPRO_API_KEY`, and `SUBGRAPH_API_KEY`.

## Task loops

## PriceFetcher

- Will query exchanges for prices once or multiple times based on whether the API supports single vs multi markets; i.e. whether an API needs to be queried for each pair individually or can return multiple pairs at once. [See here for exchange details](./constants/static_exchange_details.go).

## PriceEncoder

- Will update the cache with queried prices, encode them appropriately, and make adjustments when `adjustByMarket` is defined.

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
ExchangeConfigJson = "{\"exchanges\":[{\"exchangeName\":\"Binance\",\"ticker\":\"\\\"ETHBTC\\\"\"},{\"exchangeName\":\"Bitfinex\",\"ticker\":\"tETHBTC\",\"adjustByMarket\":\"BTC-USD\"}]}" // This is an example showing how to use adjustByMarket. You can use ETH-USD without adjustByMarket.
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
    // represents "$10,000". Therefore `10 ^ Exponent` represents the smallest
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
    // Query data is the market pair representation in layer
    QueryData string
}
```

**Note:**
A price is valid by default up to 30 seconds; to change this to a different default edit the `constants.MaxPriceAge`

**Also:** Config files are written to homedir/.layer/config/.
To change/add exchange details or market pairs edit the files `pricefeed_exchange_config.toml` or `market_params.toml` respectively.

## Keyring Password File

When running the reporter daemon with `--keyring-backend file`, set `KEYRING_PASSWORD_FILE` to a file containing the keyring password. This lets the daemon unlock the account without requiring an interactive terminal prompt.

For this systemd file-keyring setup, set `LAYER_HOME` to the same home directory that contains the daemon config and keyring files. This keeps the non-interactive service from resolving `home` from the service user's shell environment instead of the intended Layer home.

Example systemd service snippet:

```ini
[Service]
User=reporter
Environment="KEYRING_PASSWORD_FILE=/etc/layer-daemons/reporter-keyring-password"
Environment="LAYER_HOME=/home/reporter/.layer"
Environment="GRPC_NODES=your-grpc-host:9090,your-fallback-grpc-host:9090"
Environment="RPC_NODES=tcp://your-rpc-host:26657,tcp://your-fallback-rpc-host:26657"
ExecStart=/usr/local/bin/reporterd \
  --keyring-backend file \
  --from your-key-name
```

Create the password file so only the service user can read it:

```bash
sudo install -d -m 700 -o reporter -g reporter /etc/layer-daemons
sudo install -m 600 -o reporter -g reporter /dev/null /etc/layer-daemons/reporter-keyring-password
sudo sh -c 'printf "%s\n" "YOUR_KEYRING_PASSWORD" > /etc/layer-daemons/reporter-keyring-password'
```

Make sure the service `User` can read the file. When `KEYRING_PASSWORD_FILE` is set, startup fails and the daemon exits if the file cannot be read, is empty, or cannot unlock the configured `--from` account. If `KEYRING_PASSWORD_FILE` is not set, the daemon falls back to reading the keyring password from stdin.

## Reward Withdrawals And Auto-Unbonding

The reporter periodically withdraws earned tips/rewards with `MsgWithdrawTip`. The interval is configured by `WITHDRAW_FREQUENCY` in seconds and defaults to `43200` (12 hours). By default, the validator operator address is derived from the reporter account address. If the reporter account is delegated to a different validator, set `REPORTERS_VALIDATOR_ADDRESS` to that validator's `tellorvaloper...` address.

Auto-unbonding is optional and can be configured by CLI flags or equivalent environment variables:

| Flag | Environment | Type | Default | Description |
|------|-------------|------|---------|-------------|
| `--auto-unbonding-frequency` | `AUTO_UNBONDING_FREQUENCY` | uint32 | `0` | Enables unbonding every N days (`0` = disabled, valid enabled range is 1-21). |
| `--auto-unbonding-amount` | `AUTO_UNBONDING_AMOUNT` | uint32 | `0` | Amount of `loya` to unbond each time. Required when frequency is enabled. |
| `--auto-unbonding-max-stake-percentage` | `AUTO_UNBONDING_MAX_STAKE_PERCENTAGE` | decimal string | `0.0` | Optional cap from `0.0` to `1.0`; if the configured amount exceeds this share of stake, the unbond is skipped. |

Gas estimates are cached per transaction type. `--refresh-gas-estimates-interval` / `REFRESH_GAS_ESTIMATES_INTERVAL` resets cached estimates and gas-adjustment levels periodically; it defaults to `12h`, and values `<=0` disable the refresh loop.

### Median Server

The median server can query median values from an endpoint or the CLI. See usage [here](../x/oracle/client/cli/query_all_get_median.go).
Query all median values, or a median value for specific query data, using the following commands respectively.
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

| Flag | Environment | Type | Description | Required (if enabled) |
|------|-------------|------|-------------|-----------------------|
| `--price-guard-enabled` | `PRICE_GUARD_ENABLED` | bool | Enables the price guard mechanism. | No |
| `--price-guard-threshold` | `PRICE_GUARD_THRESHOLD` | float64 | Maximum allowed percentage change (e.g. `0.5` = 50%). Submissions exceeding this change from the last reported price are blocked. | Yes |
| `--price-guard-max-age` | `PRICE_GUARD_MAX_AGE` | duration | Time after which a stored price is considered expired (e.g. `1h`). If the last price is expired, the new price is accepted regardless of deviation. | Yes |
| `--price-guard-update-on-blocked` | `PRICE_GUARD_UPDATE_ON_BLOCKED` | bool | If true, updates the internal "last known price" to the new value even if submission was blocked. If false, keeps the old price as the baseline. | Yes |

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

| Flag | Environment | Type | Default | Description |
|------|-------------|------|---------|-------------|
| `--auto-balance-to-keep` | `AUTO_BALANCE_TO_KEEP` | uint64 | `0` | Target wallet balance in **loya** (`0` = disabled). Any amount above this, minus the gas reserve below, is bridged. |
| `--auto-balance-execution-time` | `AUTO_BALANCE_EXECUTION_TIME` | string | `00:00` | UTC time to check balance and bridge, format **`HH:MM`** with hour 0-23 and minute 0-59 (e.g. `03:00`, `15:30`). |
| `--auto-balance-bridge-to-eth-addr` | `AUTO_BALANCE_BRIDGE_TO_ETH_ADDR` | string | `""` | Ethereum recipient for bridged tokens. Required when `--auto-balance-to-keep > 0`. May include or omit the `0x` prefix. Validated with standard hex address checks at startup. |

### Behavior

1. **Schedule:** Once per UTC day at `--auto-balance-execution-time`, the daemon queries the reporter wallet's `loya` balance.
2. **Amount:** `bridge_amount = wallet_balance - auto-balance-to-keep - 1_000_000` (a fixed **1 TRB** reserve in loya is left for future gas). If `bridge_amount <= 0`, nothing is sent.
3. **Broadcast:** The transaction uses the shared broadcast path with RPC endpoint fallback and gas-adjustment retries for out-of-gas responses. Other failures are logged; the next balance check happens at the next scheduled UTC time.
4. **Shutdown:** Bridge txs are enqueued with `trySend` so shutdown does not panic on a closed channel.

### Startup validation

When `--auto-balance-to-keep > 0`, the reporter **fails to start** if:

- `--auto-balance-bridge-to-eth-addr` is missing or not a valid Ethereum address
- `--auto-balance-execution-time` is not valid `HH:MM` (hour 0-23, minute 0-59)

### Example

Keep 5 TRB in the wallet (5_000_000 loya), run the check daily at 03:00 UTC, and bridge excess to an Ethereum address:

```bash
LAYER_HOME=/home/reporter/.layer \
GRPC_NODES=your-grpc-host:9090 \
RPC_NODES=tcp://your-rpc-host:26657 \
reporterd \
  --from your-key-name \
  --auto-balance-to-keep=5000000 \
  --auto-balance-execution-time=03:00 \
  --auto-balance-bridge-to-eth-addr=0x0000000000000000000000000000000000000000
```

**Note:** Amounts are in **loya** (micro-denom), not whole TRB. `1 TRB = 1_000_000 loya`.
