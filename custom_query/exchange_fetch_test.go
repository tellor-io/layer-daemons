package customquery

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/require"

	"github.com/tellor-io/layer-daemons/constants"
	"github.com/tellor-io/layer-daemons/exchange_common"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
	servertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func TestConvertedUsdFloatFromExchangeRawPrices_trbNoAdjust(t *testing.T) {
	id := exchange_common.TRBUSD_ID
	mc := pricefeedtypes.MarketConfig{Ticker: "TRBUSDT", Invert: false}
	v, err := convertedUsdFloatFromExchangeRawPrices(id, mc, map[uint32]uint64{id: 4_000_000})
	require.NoError(t, err)
	require.InDelta(t, 4.0, v, 1e-9)
}

func TestFetchFromExchangeEndpoint_useCacheRejected(t *testing.T) {
	h := ExchangeHandler{
		ExchangeID:    exchange_common.EXCHANGE_ID_BINANCE,
		ChainMarketID: exchange_common.TRBUSD_ID,
		MarketId:      "trb",
		MarketConfig:  pricefeedtypes.MarketConfig{Ticker: "TRBUSDT"},
		UseCache:      true,
	}
	res := fetchFromExchangeEndpoint(context.Background(), h, nil, &http.Client{})
	require.Error(t, res.Err)
	require.Contains(t, res.Err.Error(), "price cache is nil")
}

func TestFetchFromExchangeEndpoint_mockBinanceResponse(t *testing.T) {
	params := marketParamsFromStatic(t)
	SetMarketParamsForQueryResolver(params)
	q := QueryConfig{ID: trbQueryIDHex(t)}
	ep := EndpointConfig{EndpointType: "exchange", ExchangeID: string(exchange_common.EXCHANGE_ID_BINANCE)}
	h, err := buildExchangeHandler(q, ep, nil)
	require.NoError(t, err)

	body := fmt.Sprintf(
		`[{"symbol":%q,"askPrice":"3.5","bidPrice":"3.5","lastPrice":"3.5"}]`,
		h.MarketConfig.Ticker,
	)

	client := &http.Client{
		Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	}

	res := fetchFromExchangeEndpoint(context.Background(), h, nil, client)
	require.NoError(t, res.Err)
	require.Greater(t, res.Value, 0.0)
	require.Equal(t, string(exchange_common.EXCHANGE_ID_BINANCE), res.SourceId)
	require.Contains(t, res.EndpointID, "Binance")
}

func TestFetchFromExchangeEndpoint_cachePath(t *testing.T) {
	cache := pricefeedservertypes.NewMarketToExchangePrices(1 * time.Minute)
	now := time.Now()
	cache.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: exchange_common.TRBUSD_ID,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     exchange_common.EXCHANGE_ID_BINANCE,
					Price:          2_000_000,
					LastUpdateTime: &now,
				},
			},
		},
	})

	h := ExchangeHandler{
		ExchangeID:    exchange_common.EXCHANGE_ID_BINANCE,
		ChainMarketID: exchange_common.TRBUSD_ID,
		MarketId:      "trb",
		UseCache:      true,
	}
	res := fetchFromExchangeEndpoint(context.Background(), h, cache, nil)
	require.NoError(t, res.Err)
	require.InDelta(t, 2.0, res.Value, 1e-9)
}

func queryIDHexForMarketID(t *testing.T, mid uint32) string {
	t.Helper()
	p := constants.StaticMarketParamsConfig[mid]
	require.NotNil(t, p)
	return strings.Trim(p.QueryData, `"`)
}

func TestRefreshExchangeEndpointsOnce_batchesOneHTTPPerVenue(t *testing.T) {
	params := marketParamsFromStatic(t)
	SetMarketParamsForQueryResolver(params)
	SetUnifiedExponentOverrides(nil)

	qBtc := QueryConfig{ID: queryIDHexForMarketID(t, exchange_common.BTCUSD_ID)}
	qEth := QueryConfig{ID: queryIDHexForMarketID(t, exchange_common.ETHUSD_ID)}
	ep := EndpointConfig{
		EndpointType: "exchange",
		ExchangeID:   string(exchange_common.EXCHANGE_ID_BINANCE),
		UseCache:     true,
	}
	hBtc, err := buildExchangeHandler(qBtc, ep, nil)
	require.NoError(t, err)
	hEth, err := buildExchangeHandler(qEth, ep, nil)
	require.NoError(t, err)

	var reqCount atomic.Int32
	symBtc := hBtc.MarketConfig.Ticker
	symEth := hEth.MarketConfig.Ticker
	body := fmt.Sprintf(
		`[{"symbol":%q,"askPrice":"50000","bidPrice":"50000","lastPrice":"50000"},{"symbol":%q,"askPrice":"3000","bidPrice":"3000","lastPrice":"3000"}]`,
		symBtc,
		symEth,
	)
	client := &http.Client{
		Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			reqCount.Add(1)
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	}

	cache := pricefeedservertypes.NewMarketToExchangePrices(1 * time.Minute)
	RefreshExchangeEndpointsOnce(context.Background(), log.NewNopLogger(), []ExchangeHandler{hBtc, hEth}, cache, client)

	require.Equal(t, int32(1), reqCount.Load(), "expected one batched HTTP request for Binance")

	rawBtc, ok := cache.GetValidPriceForExchange(exchange_common.BTCUSD_ID, exchange_common.EXCHANGE_ID_BINANCE, time.Now())
	require.True(t, ok)
	require.Greater(t, rawBtc, uint64(0))
	rawEth, ok := cache.GetValidPriceForExchange(exchange_common.ETHUSD_ID, exchange_common.EXCHANGE_ID_BINANCE, time.Now())
	require.True(t, ok)
	require.Greater(t, rawEth, uint64(0))
}

func TestRefreshExchangeEndpointsOnce_writesCache(t *testing.T) {
	params := marketParamsFromStatic(t)
	SetMarketParamsForQueryResolver(params)
	q := QueryConfig{ID: trbQueryIDHex(t)}
	ep := EndpointConfig{EndpointType: "exchange", ExchangeID: string(exchange_common.EXCHANGE_ID_BINANCE), UseCache: true}
	h, err := buildExchangeHandler(q, ep, nil)
	require.NoError(t, err)

	body := fmt.Sprintf(
		`[{"symbol":%q,"askPrice":"7.5","bidPrice":"7.5","lastPrice":"7.5"}]`,
		h.MarketConfig.Ticker,
	)
	client := &http.Client{
		Transport: roundTripperFunc(func(_ *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(body)),
				Header:     http.Header{"Content-Type": []string{"application/json"}},
			}, nil
		}),
	}

	cache := pricefeedservertypes.NewMarketToExchangePrices(1 * time.Minute)
	RefreshExchangeEndpointsOnce(context.Background(), log.NewNopLogger(), []ExchangeHandler{h}, cache, client)

	raw, ok := cache.GetValidPriceForExchange(exchange_common.TRBUSD_ID, exchange_common.EXCHANGE_ID_BINANCE, time.Now())
	require.True(t, ok)
	require.Equal(t, uint64(7_500_000), raw)
}
