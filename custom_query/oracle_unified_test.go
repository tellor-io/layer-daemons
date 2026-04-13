package customquery

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tellor-io/layer-daemons/configs"
	"github.com/tellor-io/layer-daemons/constants"
	"github.com/tellor-io/layer-daemons/exchange_common"
)

func TestPrepareDaemonMarketParams_syntheticDuplicateMarketID(t *testing.T) {
	c := Config{
		Markets: []OracleMarketEntry{
			{ID: 1, Pair: "A", Exponent: -6, QueryData: "abcd", ExchangeConfigJSON: "{}"},
			{ID: 1, Pair: "B", Exponent: -6, QueryData: "ef01", ExchangeConfigJSON: "{}"},
		},
	}
	_, _, err := buildSyntheticMarketParams(c)
	require.Error(t, err)
	require.Contains(t, err.Error(), "duplicate id")
}

func TestResolveMarketIDForQueryConfig_prefersChainMarketID(t *testing.T) {
	q := QueryConfig{ID: "deadbeef", ChainMarketID: 42}
	mid, err := ResolveMarketIDForQueryConfig(q)
	require.NoError(t, err)
	require.Equal(t, uint32(42), mid)
}

func TestBuildQueryEndpoints_exchangeLegConflict(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))

	mpToml := configs.GenerateDefaultMarketParamsTomlString()
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, constants.MarketParamsConfigFileName), mpToml.Bytes(), 0o644))

	qid := trbQueryIDHex(t)
	// Same query, two exchange endpoints for same venue + chain market with incompatible tickers.
	customToml := `[queries.trb]
id = "` + qid + `"
aggregation_method = "median"
min_responses = 1
max_spread_percent = 100.0
response_type = "ufixed256x18"
[[queries.trb.endpoints]]
endpoint_type = "exchange"
exchange_id = "Binance"
ticker = "TRBUSDT"
invert = false
[[queries.trb.endpoints]]
endpoint_type = "exchange"
exchange_id = "Binance"
ticker = "OTHERTRB"
invert = false
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "conflict.toml"), []byte(customToml), 0o644))

	_, _, err := BuildQueryEndpoints(dir, "config", "conflict.toml")
	require.Error(t, err)
	require.Contains(t, strings.ToLower(err.Error()), "conflict")
}

func TestBuildQueryEndpoints_adjustLegRequiredInToml(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))

	mpToml := configs.GenerateDefaultMarketParamsTomlString()
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, constants.MarketParamsConfigFileName), mpToml.Bytes(), 0o644))

	qid := queryIDHexForMarketID(t, exchange_common.USDTUSD_ID)
	customToml := `[queries.usdt]
id = "` + qid + `"
aggregation_method = "median"
min_responses = 1
max_spread_percent = 100.0
response_type = "ufixed256x18"
[[queries.usdt.endpoints]]
endpoint_type = "exchange"
exchange_id = "Binance"
ticker = "USDTUSD"
adjust_by_market_id = 1
invert = false
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "bad_adjust.toml"), []byte(customToml), 0o644))

	_, _, err := BuildQueryEndpoints(dir, "config", "bad_adjust.toml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "adjust_ticker")
}
