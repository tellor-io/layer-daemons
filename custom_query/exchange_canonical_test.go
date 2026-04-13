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
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
)

func marketParamsFromStatic(t *testing.T) []pricefeedtypes.MarketParam {
	t.Helper()
	out := make([]pricefeedtypes.MarketParam, 0, len(constants.StaticMarketParamsConfig))
	for _, p := range constants.StaticMarketParamsConfig {
		require.NotNil(t, p)
		out = append(out, *p)
	}
	return out
}

func trbQueryIDHex(t *testing.T) string {
	t.Helper()
	p := constants.StaticMarketParamsConfig[exchange_common.TRBUSD_ID]
	require.NotNil(t, p)
	return strings.Trim(p.QueryData, `"`)
}

func TestBuildCanonicalExchangeMarketRegistry(t *testing.T) {
	reg, err := BuildCanonicalExchangeMarketRegistry(marketParamsFromStatic(t))
	require.NoError(t, err)
	require.NotEmpty(t, reg)

	mc, ok := CanonicalExchangeMarketConfig(reg, exchange_common.EXCHANGE_ID_BINANCE, exchange_common.TRBUSD_ID)
	require.True(t, ok)
	require.NotEmpty(t, mc.Ticker)
}

func TestBuildExchangeHandler_success(t *testing.T) {
	params := marketParamsFromStatic(t)
	SetMarketParamsForQueryResolver(params)
	reg, err := BuildCanonicalExchangeMarketRegistry(params)
	require.NoError(t, err)

	q := QueryConfig{ID: trbQueryIDHex(t)}
	ep := EndpointConfig{EndpointType: "exchange", ExchangeID: string(exchange_common.EXCHANGE_ID_BINANCE)}
	h, err := buildExchangeHandler(q, ep, nil)
	require.NoError(t, err)
	require.Equal(t, string(exchange_common.EXCHANGE_ID_BINANCE), h.ExchangeID)
	require.Equal(t, q.ID, h.QueryID)
	require.Equal(t, exchange_common.TRBUSD_ID, h.ChainMarketID)
	require.False(t, h.UseCache)

	mc, _ := CanonicalExchangeMarketConfig(reg, exchange_common.EXCHANGE_ID_BINANCE, exchange_common.TRBUSD_ID)
	ep2 := EndpointConfig{EndpointType: "exchange", ExchangeID: string(exchange_common.EXCHANGE_ID_BINANCE), Ticker: mc.Ticker}
	_, err = buildExchangeHandler(q, ep2, nil)
	require.NoError(t, err)

	epLabel := EndpointConfig{
		EndpointType: "exchange",
		ExchangeID:   string(exchange_common.EXCHANGE_ID_BINANCE),
		MarketId:     "TRB-USD",
	}
	h3, err := buildExchangeHandler(q, epLabel, nil)
	require.NoError(t, err)
	require.Equal(t, "TRB-USD", h3.MarketId)
}

func TestBuildExchangeHandler_unknownExchange(t *testing.T) {
	params := marketParamsFromStatic(t)
	SetMarketParamsForQueryResolver(params)

	q := QueryConfig{ID: trbQueryIDHex(t)}
	ep := EndpointConfig{EndpointType: "exchange", ExchangeID: "NotAnExchange"}
	_, err := buildExchangeHandler(q, ep, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown exchange_id")
}

func TestBuildExchangeHandler_pairNotOnExchange(t *testing.T) {
	params := marketParamsFromStatic(t)
	SetMarketParamsForQueryResolver(params)

	q := QueryConfig{ID: trbQueryIDHex(t)}
	ep := EndpointConfig{EndpointType: "exchange", ExchangeID: string(exchange_common.EXCHANGE_ID_BITSTAMP)}
	_, err := buildExchangeHandler(q, ep, nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "no exchange config")
}

func TestBuildExchangeHandler_tomlTickerAuthoritative(t *testing.T) {
	params := marketParamsFromStatic(t)
	SetMarketParamsForQueryResolver(params)

	q := QueryConfig{ID: trbQueryIDHex(t)}
	ep := EndpointConfig{
		EndpointType: "exchange",
		ExchangeID:   string(exchange_common.EXCHANGE_ID_BINANCE),
		Ticker:       "CUSTOMTRB",
		Invert:       false,
	}
	h, err := buildExchangeHandler(q, ep, nil)
	require.NoError(t, err)
	require.Equal(t, "CUSTOMTRB", h.MarketConfig.Ticker)
}

func TestBuildExchangeHandler_rejectsRESTFields(t *testing.T) {
	params := marketParamsFromStatic(t)
	SetMarketParamsForQueryResolver(params)

	q := QueryConfig{ID: trbQueryIDHex(t)}
	ep := EndpointConfig{
		EndpointType: "exchange",
		ExchangeID:   string(exchange_common.EXCHANGE_ID_BINANCE),
		ResponsePath: []string{"a"},
	}
	_, err := buildExchangeHandler(q, ep, nil)
	require.Error(t, err)
}

func TestBuildExchangeHandler_preservesUseCache(t *testing.T) {
	params := marketParamsFromStatic(t)
	SetMarketParamsForQueryResolver(params)

	q := QueryConfig{ID: trbQueryIDHex(t)}
	ep := EndpointConfig{
		EndpointType: "exchange",
		ExchangeID:   string(exchange_common.EXCHANGE_ID_BINANCE),
		UseCache:     true,
	}
	h, err := buildExchangeHandler(q, ep, nil)
	require.NoError(t, err)
	require.True(t, h.UseCache)
}

func TestBuildQueryEndpoints_exchangeEndpoint(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))

	mpToml := configs.GenerateDefaultMarketParamsTomlString()
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, constants.MarketParamsConfigFileName), mpToml.Bytes(), 0o644))

	customToml := `[queries.trb]
id = "` + trbQueryIDHex(t) + `"
aggregation_method = "median"
min_responses = 1
max_spread_percent = 100.0
response_type = "ufixed256x18"
[[queries.trb.endpoints]]
endpoint_type = "exchange"
exchange_id = "Binance"
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "exchange_only.toml"), []byte(customToml), 0o644))

	params := configs.ReadMarketParamsConfigFile(dir)
	SetMarketParamsForQueryResolver(params)

	queryID := trbQueryIDHex(t)
	qm, _, err := BuildQueryEndpoints(dir, "config", "exchange_only.toml")
	require.NoError(t, err)
	require.Contains(t, qm, queryID)
	qc, ok := qm[queryID]
	require.True(t, ok)
	require.Empty(t, qc.RpcReaders)
	require.Empty(t, qc.ContractReaders)
	require.Len(t, qc.ExchangeReaders, 1)
	ex := qc.ExchangeReaders[0]
	require.Equal(t, "Binance", ex.ExchangeID)
	require.Equal(t, trbQueryIDHex(t), ex.QueryID)
	require.Equal(t, exchange_common.TRBUSD_ID, ex.ChainMarketID)
	require.Equal(t, "exchange", ex.SourceId)
	require.NotEmpty(t, ex.MarketConfig.Ticker)
}

func TestBuildQueryEndpoints_exchangeEndpointInvalid(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, "config")
	require.NoError(t, os.MkdirAll(cfgDir, 0o755))

	mpToml := configs.GenerateDefaultMarketParamsTomlString()
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, constants.MarketParamsConfigFileName), mpToml.Bytes(), 0o644))

	customToml := `[queries.trb]
id = "` + trbQueryIDHex(t) + `"
aggregation_method = "median"
min_responses = 1
max_spread_percent = 100.0
response_type = "ufixed256x18"
[[queries.trb.endpoints]]
endpoint_type = "exchange"
exchange_id = "NotAnExchange"
`
	require.NoError(t, os.WriteFile(filepath.Join(cfgDir, "exchange_bad.toml"), []byte(customToml), 0o644))

	params := configs.ReadMarketParamsConfigFile(dir)
	SetMarketParamsForQueryResolver(params)

	_, _, err := BuildQueryEndpoints(dir, "config", "exchange_bad.toml")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown exchange_id")
}
