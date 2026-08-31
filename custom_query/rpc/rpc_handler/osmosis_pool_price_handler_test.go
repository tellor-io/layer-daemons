package rpchandler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	reader "github.com/tellor-io/layer-daemons/custom_query/rpc/rpc_reader"
	"github.com/tellor-io/layer-daemons/exchange_common"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	pricefeedtypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

const (
	osmosisUSDN = "ibc/0C39BD03B5C57A1753A9B73164705871A9B549F1A5226CFD7E39BE7BF73CF8CF"
	osmosisUSDC = "ibc/498A0751C798A0D9A389AA3691123DADA57DAA4FE165D5C75894505B876BA6E4"
)

func TestOsmosisPoolPriceHandlerWeightedPool(t *testing.T) {
	const fixture = `{
  "pool": {
    "@type": "/osmosis.gamm.v1beta1.Pool",
    "id": "3061",
    "pool_assets": [
      {
        "token": {
          "denom": "ibc/0C39BD03B5C57A1753A9B73164705871A9B549F1A5226CFD7E39BE7BF73CF8CF",
          "amount": "1001889"
        },
        "weight": "536870912000000"
      },
      {
        "token": {
          "denom": "ibc/498A0751C798A0D9A389AA3691123DADA57DAA4FE165D5C75894505B876BA6E4",
          "amount": "1005041"
        },
        "weight": "536870912000000"
      }
    ],
    "total_weight": "1073741824000000"
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixture))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodGet, "", nil, []string{"pool"}, 5000, map[string]string{
		"target_token":    osmosisUSDN,
		"quote_token":     osmosisUSDC,
		"target_decimals": "6",
		"quote_decimals":  "6",
	}, 1)
	require.NoError(t, err)

	var h OsmosisPoolPriceHandler
	v, err := h.FetchValue(context.Background(), rdr, false, exchange_common.USDCUSD_ID, usdcPriceCache(t), 0)
	require.NoError(t, err)
	require.InDelta(t, 1005041.0/1001889.0, v, 1e-12)
}

func TestOsmosisPoolPriceHandlerConcentratedPoolTargetAsToken1(t *testing.T) {
	const fixture = `{
  "pool": {
    "@type": "/osmosis.concentratedliquidity.v1beta1.Pool",
    "id": "1",
    "token0": "quote",
    "token1": "target",
    "current_sqrt_price": "2.0"
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixture))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodGet, "", nil, []string{"pool"}, 5000, map[string]string{
		"target_token": "target",
		"quote_token":  "quote",
	}, 1)
	require.NoError(t, err)

	var h OsmosisPoolPriceHandler
	v, err := h.FetchValue(context.Background(), rdr, false, exchange_common.USDCUSD_ID, usdcPriceCache(t), 0)
	require.NoError(t, err)
	require.InDelta(t, 0.25, v, 1e-12)
}

func usdcPriceCache(t *testing.T) *pricefeedtypes.MarketToExchangePrices {
	t.Helper()

	cache := pricefeedtypes.NewMarketToExchangePrices(time.Minute)
	now := time.Now()
	cache.UpdatePrices([]*pricefeedservertypes.MarketPriceUpdate{
		{
			MarketId: exchange_common.USDCUSD_ID,
			ExchangePrices: []*pricefeedservertypes.ExchangePrice{
				{ExchangeId: "a", Price: 1_000_000, LastUpdateTime: &now},
				{ExchangeId: "b", Price: 1_000_000, LastUpdateTime: &now},
			},
		},
	})
	return cache
}
