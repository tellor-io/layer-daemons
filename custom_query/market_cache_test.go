package customquery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	rpcreader "github.com/tellor-io/layer-daemons/custom_query/rpc/rpc_reader"
	"github.com/tellor-io/layer-daemons/exchange_common"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	pricefeed "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

func TestFetchPriceWithMarketCacheReader(t *testing.T) {
	priceCache := pricefeed.NewMarketToExchangePrices(time.Minute)
	now := time.Now()
	priceCache.UpdatePrices([]*pricefeedservertypes.MarketPriceUpdate{
		{
			MarketId: exchange_common.SUSDSUSD_ID,
			ExchangePrices: []*pricefeedservertypes.ExchangePrice{
				{
					ExchangeId:     string(exchange_common.EXCHANGE_ID_CURVE),
					Price:          1_250_000,
					LastUpdateTime: &now,
				},
			},
		},
	})

	result, err := FetchPrice(context.Background(), QueryConfig{
		ID:                "cache-test",
		AggregationMethod: "median",
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		MaxSpreadPercent:  100,
		MarketCacheReaders: []MarketCacheHandler{
			{
				EndpointID:    endpointTypeMarketCache,
				MarketId:      "SUSDS-USD",
				CacheMarketId: exchange_common.SUSDSUSD_ID,
				ExchangeId:    string(exchange_common.EXCHANGE_ID_CURVE),
			},
		},
	}, priceCache)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 1.0, result.SuccessRate)
	require.Len(t, result.RawResults, 1)
	require.NoError(t, result.RawResults[0].Err)
	require.Equal(t, 1.25, result.RawResults[0].Value)
	require.NotEmpty(t, result.EncodedValue)
}

func TestFetchPriceWithMarketCacheReaderMissingPrice(t *testing.T) {
	priceCache := pricefeed.NewMarketToExchangePrices(time.Minute)

	result, err := FetchPrice(context.Background(), QueryConfig{
		ID:                "cache-test",
		AggregationMethod: "median",
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		MaxSpreadPercent:  100,
		MarketCacheReaders: []MarketCacheHandler{
			{
				EndpointID:    endpointTypeMarketCache,
				MarketId:      "SUSDS-USD",
				CacheMarketId: exchange_common.SUSDSUSD_ID,
				ExchangeId:    string(exchange_common.EXCHANGE_ID_CURVE),
			},
		},
	}, priceCache)

	require.Error(t, err)
	require.NotNil(t, result)
	require.Equal(t, 0.0, result.SuccessRate)
	require.Len(t, result.RawResults, 1)
	require.EqualError(t, result.RawResults[0].Err, "no valid cached price found for market ID 102 on exchange Curve")
}

func TestFetchPriceReturnsAfterMinResponses(t *testing.T) {
	fastServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewEncoder(w).Encode(map[string]float64{"price": 1.25}))
	}))
	defer fastServer.Close()

	slowServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(time.Second)
		require.NoError(t, json.NewEncoder(w).Encode(map[string]float64{"price": 99}))
	}))
	defer slowServer.Close()

	fastReader, err := rpcreader.NewReader(fastServer.URL, http.MethodGet, "", nil, []string{"price"}, 1000, nil)
	require.NoError(t, err)
	slowReader, err := rpcreader.NewReader(slowServer.URL, http.MethodGet, "", nil, []string{"price"}, 1000, nil)
	require.NoError(t, err)

	start := time.Now()
	result, err := FetchPrice(context.Background(), QueryConfig{
		ID:                "early-success-test",
		AggregationMethod: "median",
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		MaxSpreadPercent:  100,
		RpcReaders: []RpcHandler{
			{
				Reader:     fastReader,
				EndpointID: "fast",
				MarketId:   "TEST-USD",
				SourceId:   "fast",
			},
			{
				Reader:     slowReader,
				EndpointID: "slow",
				MarketId:   "TEST-USD",
				SourceId:   "slow",
			},
		},
	}, nil)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.NotEmpty(t, result.EncodedValue)
	require.Less(t, time.Since(start), 500*time.Millisecond)
	require.Len(t, result.RawResults, 1)
	require.Equal(t, "fast", result.RawResults[0].EndpointID)
}
