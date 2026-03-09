package unified_config

import "github.com/tellor-io/layer-daemons/exchange_common"

// DefaultExchangeSourceConfigs generates unified SourceConfig entries for all
// known exchange sources in the legacy pricefeed setup.
//
// These configs:
//   - Treat all exchanges as REST sources (Type = "rest")
//   - Use direct price/symbol endpoints instead of legacy ticker endpoints
//   - Mark sources as batchable or on-demand-only based on the Step 3.2 findings
func DefaultExchangeSourceConfigs() []SourceConfig {
	return []SourceConfig{
		// Production exchanges
		{
			ID:                    string(exchange_common.EXCHANGE_ID_BINANCE),
			Type:                  "rest",
			BaseURL:               "https://api.binance.com/api/v3/ticker/price",
			Batchable:             true,
			BatchStrategy:         "query_param",
			BatchGroup:            "exchanges",
			UpdateIntervalSeconds: 30,
			CacheTTLSeconds:       15,
		},
		{
			ID:                    string(exchange_common.EXCHANGE_ID_BINANCE_US),
			Type:                  "rest",
			BaseURL:               "https://api.binance.us/api/v3/ticker/price",
			Batchable:             true,
			BatchStrategy:         "query_param",
			BatchGroup:            "exchanges",
			UpdateIntervalSeconds: 30,
			CacheTTLSeconds:       15,
		},
		{
			ID:   string(exchange_common.EXCHANGE_ID_BITFINEX),
			Type: "rest",
			// Uses /v2/tickers with comma-separated symbols for batching
			BaseURL:               "https://api-pub.bitfinex.com/v2/tickers",
			Batchable:             true,
			BatchStrategy:         "query_param",
			BatchGroup:            "exchanges",
			UpdateIntervalSeconds: 30,
			CacheTTLSeconds:       15,
		},
		{
			ID:      string(exchange_common.EXCHANGE_ID_BITSTAMP),
			Type:    "rest",
			BaseURL: "https://www.bitstamp.net/api/v2/ticker",
			// Bitstamp is treated as on-demand (no batching support)
			Batchable:       false,
			BatchStrategy:   "",
			BatchGroup:      "exchanges",
			CacheTTLSeconds: 15,
		},
		{
			ID:                    string(exchange_common.EXCHANGE_ID_CRYPTO_COM),
			Type:                  "rest",
			BaseURL:               "https://api.crypto.com/v2/public/get-ticker",
			Batchable:             false, // treated as on-demand until batching is verified
			BatchStrategy:         "",
			BatchGroup:            "exchanges",
			UpdateIntervalSeconds: 0,
			CacheTTLSeconds:       15,
		},
		{
			ID:                    string(exchange_common.EXCHANGE_ID_GATE),
			Type:                  "rest",
			BaseURL:               "https://api.gateio.ws/api/v4/spot/tickers",
			Batchable:             true,
			BatchStrategy:         "query_param",
			BatchGroup:            "exchanges",
			UpdateIntervalSeconds: 30,
			CacheTTLSeconds:       15,
		},
		{
			ID:      string(exchange_common.EXCHANGE_ID_HUOBI),
			Type:    "rest",
			BaseURL: "https://api.huobi.pro/market/detail/merged",
			// Huobi is treated as on-demand (no documented batching)
			Batchable:       false,
			BatchStrategy:   "",
			BatchGroup:      "exchanges",
			CacheTTLSeconds: 15,
		},
		{
			ID:                    string(exchange_common.EXCHANGE_ID_KRAKEN),
			Type:                  "rest",
			BaseURL:               "https://api.kraken.com/0/public/Ticker",
			Batchable:             true,
			BatchStrategy:         "query_param",
			BatchGroup:            "exchanges",
			UpdateIntervalSeconds: 30,
			CacheTTLSeconds:       15,
		},
		{
			ID:      string(exchange_common.EXCHANGE_ID_KUCOIN),
			Type:    "rest",
			BaseURL: "https://api.kucoin.com/api/v1/market/orderbook/level1",
			// KuCoin is treated as on-demand (no clear batching support)
			Batchable:       false,
			BatchStrategy:   "",
			BatchGroup:      "exchanges",
			CacheTTLSeconds: 15,
		},
		{
			ID:                    string(exchange_common.EXCHANGE_ID_MEXC),
			Type:                  "rest",
			BaseURL:               "https://api.mexc.com/api/v3/ticker/price",
			Batchable:             true,
			BatchStrategy:         "query_param",
			BatchGroup:            "exchanges",
			UpdateIntervalSeconds: 30,
			CacheTTLSeconds:       15,
		},
		{
			ID:                    string(exchange_common.EXCHANGE_ID_OKX),
			Type:                  "rest",
			BaseURL:               "https://www.okx.com/api/v5/market/ticker",
			Batchable:             true,
			BatchStrategy:         "query_param",
			BatchGroup:            "exchanges",
			UpdateIntervalSeconds: 30,
			CacheTTLSeconds:       15,
		},
		{
			ID:      string(exchange_common.EXCHANGE_ID_COINBASE_RATES),
			Type:    "rest",
			BaseURL: "https://api.coinbase.com/v2/exchange-rates",
			// Coinbase Rates is treated as on-demand (no batching)
			Batchable:       false,
			BatchStrategy:   "",
			BatchGroup:      "exchanges",
			CacheTTLSeconds: 15,
		},

		// Test exchanges – configured as simple, non-batchable REST sources.
		{
			ID:      string(exchange_common.EXCHANGE_ID_TEST_EXCHANGE),
			Type:    "rest",
			BaseURL: "https://example.com/test-exchange",
			// No real batching semantics; kept simple for tests/dev
			Batchable:       false,
			BatchStrategy:   "",
			BatchGroup:      "test_exchanges",
			CacheTTLSeconds: 15,
		},
		{
			ID:              string(exchange_common.EXCHANGE_ID_TEST_VOLATILE_EXCHANGE),
			Type:            "rest",
			BaseURL:         "https://example.com/test-volatile-exchange",
			Batchable:       false,
			BatchStrategy:   "",
			BatchGroup:      "test_exchanges",
			CacheTTLSeconds: 15,
		},
		{
			ID:              string(exchange_common.EXCHANGE_ID_TEST_FIXED_PRICE_EXCHANGE),
			Type:            "rest",
			BaseURL:         "https://example.com/test-fixed-price-exchange",
			Batchable:       false,
			BatchStrategy:   "",
			BatchGroup:      "test_exchanges",
			CacheTTLSeconds: 15,
		},
	}
}
