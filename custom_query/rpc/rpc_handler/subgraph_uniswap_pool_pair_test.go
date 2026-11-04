package rpchandler

import (
	"context"
	"fmt"
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

func susdeUsdtPoolParams() map[string]string {
	return map[string]string{
		"target_token": "0x9d39a5de30e57443bff2a8307a4256c8797a3497",
		"quote_token":  "0xdac17f958d2ee523a2206206994597c13d831ec7",
	}
}

func TestSubgraphUniswapPoolPairHandler_targetAsToken0(t *testing.T) {
	const gql = `{
  "data": {
    "pool": {
      "token0": { "id": "0x9d39a5de30e57443bff2a8307a4256c8797a3497" },
      "token1": { "id": "0xdac17f958d2ee523a2206206994597c13d831ec7" },
      "token0Price": "0.5",
      "token1Price": "2.0"
    }
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(gql))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodPost, `{}`, map[string]string{"Content-Type": "application/json"}, nil, 5000, susdeUsdtPoolParams(), 1)
	require.NoError(t, err)

	cache := pricefeedtypes.NewMarketToExchangePrices(time.Minute)
	now := time.Now()
	cache.UpdatePrices([]*pricefeedservertypes.MarketPriceUpdate{
		{
			MarketId: exchange_common.USDTUSD_ID,
			ExchangePrices: []*pricefeedservertypes.ExchangePrice{
				{ExchangeId: "a", Price: 1_000_000, LastUpdateTime: &now},
				{ExchangeId: "b", Price: 1_000_000, LastUpdateTime: &now},
			},
		},
	})

	var h SubgraphUniswapPoolPairHandler
	v, err := h.FetchValue(context.Background(), rdr, false, exchange_common.USDTUSD_ID, cache, 0)
	require.NoError(t, err)
	require.InDelta(t, 2.0, v, 1e-9)
}

func TestSubgraphUniswapPoolPairHandler_targetAsToken1(t *testing.T) {
	const gql = `{
  "data": {
    "pool": {
      "token0": { "id": "0xdac17f958d2ee523a2206206994597c13d831ec7" },
      "token1": { "id": "0x9d39a5de30e57443bff2a8307a4256c8797a3497" },
      "token0Price": "2.0",
      "token1Price": "0.5"
    }
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(gql))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodPost, `{}`, map[string]string{"Content-Type": "application/json"}, nil, 5000, susdeUsdtPoolParams(), 1)
	require.NoError(t, err)

	cache := pricefeedtypes.NewMarketToExchangePrices(time.Minute)
	now := time.Now()
	cache.UpdatePrices([]*pricefeedservertypes.MarketPriceUpdate{
		{
			MarketId: exchange_common.USDTUSD_ID,
			ExchangePrices: []*pricefeedservertypes.ExchangePrice{
				{ExchangeId: "a", Price: 1_000_000, LastUpdateTime: &now},
				{ExchangeId: "b", Price: 1_000_000, LastUpdateTime: &now},
			},
		},
	})

	var h SubgraphUniswapPoolPairHandler
	v, err := h.FetchValue(context.Background(), rdr, false, exchange_common.USDTUSD_ID, cache, 0)
	require.NoError(t, err)
	require.InDelta(t, 2.0, v, 1e-9)
}

func TestSubgraphUniswapPoolPairHandler_invert(t *testing.T) {
	const gql = `{
  "data": {
    "pool": {
      "token0": { "id": "0x9d39a5de30e57443bff2a8307a4256c8797a3497" },
      "token1": { "id": "0xdac17f958d2ee523a2206206994597c13d831ec7" },
      "token0Price": "0.25",
      "token1Price": "4.0"
    }
  }
}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(gql))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodPost, `{}`, map[string]string{"Content-Type": "application/json"}, nil, 5000, susdeUsdtPoolParams(), 1)
	require.NoError(t, err)

	cache := pricefeedtypes.NewMarketToExchangePrices(time.Minute)
	now := time.Now()
	cache.UpdatePrices([]*pricefeedservertypes.MarketPriceUpdate{
		{
			MarketId: exchange_common.USDTUSD_ID,
			ExchangePrices: []*pricefeedservertypes.ExchangePrice{
				{ExchangeId: "a", Price: 1_000_000, LastUpdateTime: &now},
				{ExchangeId: "b", Price: 1_000_000, LastUpdateTime: &now},
			},
		},
	})

	var h SubgraphUniswapPoolPairHandler
	v, err := h.FetchValue(context.Background(), rdr, true, exchange_common.USDTUSD_ID, cache, 0)
	require.NoError(t, err)
	require.InDelta(t, 0.25, v, 1e-9)
}

func TestSubgraphUniswapPoolPairHandler_graphqlErrors(t *testing.T) {
	const gql = `{"errors":[{"message":"bad query"}],"data":null}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(gql))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodPost, `{}`, map[string]string{"Content-Type": "application/json"}, nil, 5000, susdeUsdtPoolParams(), 1)
	require.NoError(t, err)

	var h SubgraphUniswapPoolPairHandler
	_, err = h.FetchValue(context.Background(), rdr, false, exchange_common.USDTUSD_ID, pricefeedtypes.NewMarketToExchangePrices(time.Minute), 0)
	require.Error(t, err)
}

func TestSubgraphUniswapPoolPairHandler_missingUsdVia(t *testing.T) {
	var h SubgraphUniswapPoolPairHandler
	_, err := h.FetchValue(context.Background(), nil, false, 0, pricefeedtypes.NewMarketToExchangePrices(time.Minute), 0)
	require.Error(t, err)
}

func TestSubgraphUniswapPoolPairHandler_missingParams(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data":{"pool":{}}}`))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodPost, `{}`, map[string]string{"Content-Type": "application/json"}, nil, 5000, nil, 1)
	require.NoError(t, err)

	var h SubgraphUniswapPoolPairHandler
	_, err = h.FetchValue(context.Background(), rdr, false, exchange_common.USDTUSD_ID, pricefeedtypes.NewMarketToExchangePrices(time.Minute), 0)
	require.Error(t, err)
}

func TestSubgraphUniswapPoolPairHandler_metaBlockTimestampStale(t *testing.T) {
	staleTs := time.Now().Add(-30 * time.Minute).Unix()
	gql := fmt.Sprintf(`{
  "data": {
    "pool": {
      "token0": { "id": "0x9d39a5de30e57443bff2a8307a4256c8797a3497" },
      "token1": { "id": "0xdac17f958d2ee523a2206206994597c13d831ec7" },
      "token0Price": "0.5",
      "token1Price": "2.0"
    },
    "_meta": { "block": { "timestamp": %d } }
  }
}`, staleTs)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(gql))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodPost, `{}`, map[string]string{"Content-Type": "application/json"}, nil, 5000, susdeUsdtPoolParams(), 1)
	require.NoError(t, err)

	cache := pricefeedtypes.NewMarketToExchangePrices(time.Minute)
	now := time.Now()
	cache.UpdatePrices([]*pricefeedservertypes.MarketPriceUpdate{
		{
			MarketId: exchange_common.USDTUSD_ID,
			ExchangePrices: []*pricefeedservertypes.ExchangePrice{
				{ExchangeId: "a", Price: 1_000_000, LastUpdateTime: &now},
				{ExchangeId: "b", Price: 1_000_000, LastUpdateTime: &now},
			},
		},
	})

	var h SubgraphUniswapPoolPairHandler
	_, err = h.FetchValue(context.Background(), rdr, false, exchange_common.USDTUSD_ID, cache, 10*time.Minute)
	require.Error(t, err)
	require.Contains(t, err.Error(), "data age")
}

func TestSubgraphUniswapPoolPairHandler_metaBlockTimestampFresh(t *testing.T) {
	freshTs := time.Now().Add(-1 * time.Minute).Unix()
	gql := fmt.Sprintf(`{
  "data": {
    "pool": {
      "token0": { "id": "0x9d39a5de30e57443bff2a8307a4256c8797a3497" },
      "token1": { "id": "0xdac17f958d2ee523a2206206994597c13d831ec7" },
      "token0Price": "0.5",
      "token1Price": "2.0"
    },
    "_meta": { "block": { "timestamp": %d } }
  }
}`, freshTs)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(gql))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodPost, `{}`, map[string]string{"Content-Type": "application/json"}, nil, 5000, susdeUsdtPoolParams(), 1)
	require.NoError(t, err)

	cache := pricefeedtypes.NewMarketToExchangePrices(time.Minute)
	now := time.Now()
	cache.UpdatePrices([]*pricefeedservertypes.MarketPriceUpdate{
		{
			MarketId: exchange_common.USDTUSD_ID,
			ExchangePrices: []*pricefeedservertypes.ExchangePrice{
				{ExchangeId: "a", Price: 1_000_000, LastUpdateTime: &now},
				{ExchangeId: "b", Price: 1_000_000, LastUpdateTime: &now},
			},
		},
	})

	var h SubgraphUniswapPoolPairHandler
	v, err := h.FetchValue(context.Background(), rdr, false, exchange_common.USDTUSD_ID, cache, 10*time.Minute)
	require.NoError(t, err)
	require.InDelta(t, 2.0, v, 1e-9)
}
