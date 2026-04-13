# Oracle config migration (operators)

## Single TOML

- **Default path:** `$HOME/config/custom_query_config.toml` (override with `--oracle-config-file` and `--oracle-config-dir` on the node).
- **`BuildQueryEndpoints`** now returns reporter-ready `[]MarketParam` from the same load pass. The daemon no longer reads `market_params.toml` when the oracle file defines **`[[markets]]`**.
- If **`[[markets]]` is absent** and **`$HOME/config/market_params.toml` exists**, behavior matches the previous two-file setup (market params from disk, exchange legs from static defaults when `ticker` is omitted on exchange endpoints).
- If **`[[markets]]` is absent** and **`market_params.toml` is missing**, the daemon falls back to **compiled static** market params (suitable for tests and minimal trees).

## Query fields

- **`chain_market_id`** (optional): explicit uint32 chain market id. When set, it overrides keccak / `QueryData` resolution for that query.
- **`query_data`**, **`pair`**, **`exponent`**, **`market_min_exchanges`**, **`market_min_price_change_ppm`**, **`market_exchange_config_json`**: optional inline market metadata when a row is not listed under **`[[markets]]`** but **`chain_market_id`** is set.

## Exchange endpoints

- **`ticker`** / **`invert`** / **`adjust_by_market_id`** / **`adjust_ticker`**: when **`ticker` is set**, these define the venue leg (unified source of truth).
- When **`ticker` is omitted**, ticker / adjust / invert are taken from **compiled static** exchange config (same as legacy `ExchangeConfigJson` in the binary). You cannot set **`adjust_*`** without **`ticker`**.
- Conflicting definitions for the same **`(exchange_id, chain_market_id)`** fail at load.

## Exchange cache refresher

- **`use_cache=true`:** one **`ExchangeQueryHandler.Query` per `exchange_id` per tick** (batched symbols). Live **`use_cache=false`** path still uses **`fetchLiveExchangeRawPrice`** per endpoint (one request per source per fetch), as before.

## Manual checklist

1. Single file: set **`[[markets]]`** and drop **`market_params.toml`**, or keep **`market_params.toml`** until migration is complete.
2. Logs: with multiple cache-backed Binance legs, expect **one** `"Exchange refresh failed"` / success path **per venue** per tick, not per endpoint.
3. Run **`go test ./...`** after upgrading.

See **`oracle_config.example.toml`** for a minimal **`[[markets]]`** example.
