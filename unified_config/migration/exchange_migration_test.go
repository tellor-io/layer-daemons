package migration

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tellor-io/layer-daemons/exchange_common"
	"github.com/tellor-io/layer-daemons/pricefeed/client/types"
	"github.com/tellor-io/layer-daemons/unified_config"
)

// helper to build a map of ID -> config for easier assertions.
func buildSourceConfigMap(configs []unified_config.SourceConfig) map[string]unified_config.SourceConfig {
	out := make(map[string]unified_config.SourceConfig, len(configs))
	for _, cfg := range configs {
		out[cfg.ID] = cfg
	}
	return out
}

// helper to build a map of Pair -> config for easier assertions.
func buildAssetPairConfigMap(pairs []unified_config.AssetPairConfig) map[string]unified_config.AssetPairConfig {
	out := make(map[string]unified_config.AssetPairConfig, len(pairs))
	for _, p := range pairs {
		out[p.Pair] = p
	}
	return out
}

func TestDefaultExchangeSourceConfigs_IncludesAllKnownExchanges(t *testing.T) {
	configs := DefaultExchangeSourceConfigs()
	if len(configs) == 0 {
		t.Fatalf("DefaultExchangeSourceConfigs returned no configs")
	}

	cfgByID := buildSourceConfigMap(configs)

	// Spot‑check a few key production exchanges and test exchanges
	expectedIDs := []string{
		string(exchange_common.EXCHANGE_ID_BINANCE),
		string(exchange_common.EXCHANGE_ID_BINANCE_US),
		string(exchange_common.EXCHANGE_ID_BITFINEX),
		string(exchange_common.EXCHANGE_ID_BITSTAMP),
		string(exchange_common.EXCHANGE_ID_COINBASE_RATES),
		string(exchange_common.EXCHANGE_ID_TEST_EXCHANGE),
		string(exchange_common.EXCHANGE_ID_TEST_VOLATILE_EXCHANGE),
		string(exchange_common.EXCHANGE_ID_TEST_FIXED_PRICE_EXCHANGE),
	}

	for _, id := range expectedIDs {
		if _, ok := cfgByID[id]; !ok {
			t.Errorf("expected SourceConfig for exchange ID %q, but none was found", id)
		}
	}
}

func TestDefaultExchangeSourceConfigs_ProducesValidRESTConfigs(t *testing.T) {
	configs := DefaultExchangeSourceConfigs()
	cfgByID := buildSourceConfigMap(configs)

	type want struct {
		baseURL       string
		batchable     bool
		batchStrategy string
	}

	tests := map[string]want{
		string(exchange_common.EXCHANGE_ID_BINANCE): {
			baseURL:       "https://api.binance.com/api/v3/ticker/price",
			batchable:     true,
			batchStrategy: "query_param",
		},
		string(exchange_common.EXCHANGE_ID_BITFINEX): {
			baseURL:       "https://api-pub.bitfinex.com/v2/tickers",
			batchable:     true,
			batchStrategy: "query_param",
		},
		string(exchange_common.EXCHANGE_ID_BITSTAMP): {
			baseURL:       "https://www.bitstamp.net/api/v2/ticker",
			batchable:     false,
			batchStrategy: "",
		},
		string(exchange_common.EXCHANGE_ID_GATE): {
			baseURL:       "https://api.gateio.ws/api/v4/spot/tickers",
			batchable:     true,
			batchStrategy: "query_param",
		},
		string(exchange_common.EXCHANGE_ID_COINBASE_RATES): {
			baseURL:       "https://api.coinbase.com/v2/exchange-rates",
			batchable:     false,
			batchStrategy: "",
		},
	}

	for id, wantCfg := range tests {
		cfg, ok := cfgByID[id]
		if !ok {
			t.Fatalf("expected config for %q, but none was found", id)
		}

		if cfg.Type != "rest" {
			t.Errorf("config %q: expected Type=rest, got %q", id, cfg.Type)
		}
		if cfg.BaseURL != wantCfg.baseURL {
			t.Errorf("config %q: expected BaseURL=%q, got %q", id, wantCfg.baseURL, cfg.BaseURL)
		}
		if cfg.Batchable != wantCfg.batchable {
			t.Errorf("config %q: expected Batchable=%v, got %v", id, wantCfg.batchable, cfg.Batchable)
		}
		if cfg.BatchStrategy != wantCfg.batchStrategy {
			t.Errorf("config %q: expected BatchStrategy=%q, got %q", id, wantCfg.batchStrategy, cfg.BatchStrategy)
		}
		if err := cfg.Validate(); err != nil {
			t.Errorf("config %q: expected valid SourceConfig, got error: %v", id, err)
		}
	}
}

func TestMigrateMarketParamsFromStatic_ProducesExpectedAssetPairsAndSources(t *testing.T) {
	static := map[uint32]*types.MarketParam{
		exchange_common.BTCUSD_ID: {
			Id:           exchange_common.BTCUSD_ID,
			Pair:         `"BTC-USD"`,
			Exponent:     -5,
			MinExchanges: 3,
			// minimal JSON subset containing three exchanges
			ExchangeConfigJson: `{"exchanges":[{"exchangeName":"Binance","ticker":"\"BTCUSDT\""},{"exchangeName":"BinanceUS","ticker":"\"BTCUSD\""},{"exchangeName":"Bitfinex","ticker":"tBTCUSD"}]}`,
			QueryData:          `"btc-usd-querydata"`,
		},
	}

	sources, pairs, err := MigrateMarketParamsFromStatic(static)
	require.NoError(t, err, "migration should succeed for valid static config")

	// We should get back at least the shared default exchange configs plus
	// any additional sources that might be added in the future.
	require.GreaterOrEqual(t, len(sources), len(DefaultExchangeSourceConfigs()))

	pairsByName := buildAssetPairConfigMap(pairs)
	btcPair, ok := pairsByName["BTC-USD"]
	require.True(t, ok, "expected BTC-USD pair in migrated config")

	require.Equal(t, exchange_common.BTCUSD_ID, btcPair.ID)
	require.Equal(t, int32(-5), btcPair.Exponent)
	require.Equal(t, 3, btcPair.MinSources)
	require.Equal(t, "median", btcPair.AggregationMethod)
	require.Equal(t, "btc-usd-querydata", btcPair.QueryData, "query data should be normalized from quoted legacy format")

	require.Len(t, btcPair.Sources, 3, "expected three exchanges for BTC-USD")

	gotSourceIDs := make(map[string]bool)
	for _, s := range btcPair.Sources {
		gotSourceIDs[s.SourceID] = true
	}

	require.True(t, gotSourceIDs["Binance"], "Binance source should be present")
	require.True(t, gotSourceIDs["BinanceUS"], "BinanceUS source should be present")
	require.True(t, gotSourceIDs["Bitfinex"], "Bitfinex source should be present")
}

func TestMigrateMarketParamsFromStatic_ValidatesMinExchangesAndExchangesList(t *testing.T) {
	tests := map[string]struct {
		static       map[uint32]*types.MarketParam
		expectErrSub string
	}{
		"no market params": {
			static:       map[uint32]*types.MarketParam{},
			expectErrSub: "no market params provided",
		},
		"nil entry": {
			static: map[uint32]*types.MarketParam{
				1: nil,
			},
			expectErrSub: "market param 1 is nil",
		},
		"zero min exchanges": {
			static: map[uint32]*types.MarketParam{
				1: {
					Id:                 1,
					Pair:               `"TEST-USD"`,
					ExchangeConfigJson: `{"exchanges":[{"exchangeName":"Binance","ticker":"\"TESTUSDT\""}]}`,
					MinExchanges:       0,
					QueryData:          `"test"`,
				},
			},
			expectErrSub: "MinExchanges must be > 0",
		},
		"min exchanges greater than number of exchanges": {
			static: map[uint32]*types.MarketParam{
				1: {
					Id:                 1,
					Pair:               `"TEST-USD"`,
					ExchangeConfigJson: `{"exchanges":[{"exchangeName":"Binance","ticker":"\"TESTUSDT\""}]}`,
					MinExchanges:       2,
					QueryData:          `"test"`,
				},
			},
			expectErrSub: "MinExchanges (2) > number of exchanges (1)",
		},
		"empty exchanges list": {
			static: map[uint32]*types.MarketParam{
				1: {
					Id:                 1,
					Pair:               `"TEST-USD"`,
					ExchangeConfigJson: `{"exchanges":[]}`,
					MinExchanges:       1,
					QueryData:          `"test"`,
				},
			},
			expectErrSub: "no exchanges defined in ExchangeConfigJson",
		},
	}

	for name, tc := range tests {
		tc := tc
		t.Run(name, func(t *testing.T) {
			_, _, err := MigrateMarketParamsFromStatic(tc.static)
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.expectErrSub)
		})
	}
}

func TestMigrateMarketParams_UsesStaticConfigForNow(t *testing.T) {
	sources, pairs, err := MigrateMarketParamsFromStatic(map[uint32]*types.MarketParam{
		1: {
			Id:                 1,
			Pair:               `"FOO-USD"`,
			Exponent:           -6,
			MinExchanges:       1,
			ExchangeConfigJson: `{"exchanges":[{"exchangeName":"Binance","ticker":"\"FOOUSDT\""}]}`,
			QueryData:          `"foo-query"`,
		},
	})
	require.NoError(t, err)
	require.NotEmpty(t, sources, "expected non-empty source configs from static market params")
	require.NotEmpty(t, pairs, "expected non-empty asset pair configs from static market params")
}

func TestMigrateMarketParams_ReadsTomlFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "market_params.toml")

	// Minimal TOML with a single BTC-USD style entry that mirrors the legacy
	// MarketParam structure we handle in MigrateMarketParamsFromStatic.
	const tomlBody = `
[[markets]]
id = 1
pair = "\"TEST-USD\""
exponent = -6
min_exchanges = 1
exchange_config_json = "{\"exchanges\":[{\"exchangeName\":\"Binance\",\"ticker\":\"\\\"TESTUSDT\\\"\"}]}"
query_data = "\"test-querydata\""
`

	require.NoError(t, os.WriteFile(cfgPath, []byte(tomlBody), 0o600))

	sources, pairs, err := MigrateMarketParams(cfgPath)
	require.NoError(t, err, "file-backed migration should succeed")
	require.NotEmpty(t, sources)
	require.Len(t, pairs, 1)

	p := pairs[0]
	require.Equal(t, uint32(1), p.ID)
	require.Equal(t, "TEST-USD", p.Pair)
	require.Equal(t, int32(-6), p.Exponent)
	require.Equal(t, 1, p.MinSources)
	require.Equal(t, "test-querydata", p.QueryData)
	require.Len(t, p.Sources, 1)
	require.Equal(t, "Binance", p.Sources[0].SourceID)
}

func TestMigrateCustomQueryConfig_BasicRestQueriesAndSources(t *testing.T) {
	// Reuse the existing custom_query test config as a realistic input.
	pwd, err := os.Getwd()
	require.NoError(t, err)

	testConfigPath := filepath.Join(pwd, "..", "..", "custom_query", "testdata", "test_config.toml")

	sources, pairs, err := MigrateCustomQueryConfig(testConfigPath)
	require.NoError(t, err)
	require.NotEmpty(t, sources, "expected REST sources derived from endpoints section")
	require.NotEmpty(t, pairs, "expected asset pairs derived from queries section")

	srcByID := buildSourceConfigMap(sources)

	// Ensure known REST endpoints become REST SourceConfigs.
	for _, id := range []string{"coingecko", "coinpaprika", "curve", "crypto", "etherscan"} {
		cfg, ok := srcByID[id]
		require.True(t, ok, "expected REST source %q", id)
		require.Equal(t, "rest", cfg.Type)
		require.NotEmpty(t, cfg.BaseURL)
		require.NoError(t, cfg.Validate())
	}

	pairsByID := make(map[string]unified_config.AssetPairConfig)
	for _, p := range pairs {
		pairsByID[p.Pair] = p
	}

	// The test config defines two queries: sdai_test_id and trb_test_id.
	for _, id := range []string{"sdai_test_id", "trb_test_id"} {
		p, ok := pairsByID[id]
		require.True(t, ok, "expected asset pair for query %s", id)
		require.Equal(t, id, p.Pair)
		require.Equal(t, "median", p.AggregationMethod)
		require.GreaterOrEqual(t, len(p.Sources), 2)
		for _, s := range p.Sources {
			_, ok := srcByID[s.SourceID]
			require.True(t, ok, "asset pair %s references unknown source %s", id, s.SourceID)
		}
	}
}
