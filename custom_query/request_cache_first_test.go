package customquery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tellor-io/layer-daemons/constants"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
	servertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

func TestFetchPrice_CacheFirstBatchableEndpoint_Success(t *testing.T) {
	const (
		queryID  = "abcd1234"
		marketID = uint32(900001)
	)

	origMarketParam := constants.StaticMarketParamsConfig[marketID]
	constants.StaticMarketParamsConfig[marketID] = &pricefeedtypes.MarketParam{
		Id:       marketID,
		Exponent: -6,
		QueryData: queryID,
	}
	defer func() {
		if origMarketParam != nil {
			constants.StaticMarketParamsConfig[marketID] = origMarketParam
		} else {
			delete(constants.StaticMarketParamsConfig, marketID)
		}
	}()

	origResolverParams := marketParamsForQueryResolver
	SetMarketParamsForQueryResolver([]pricefeedtypes.MarketParam{
		{Id: marketID, QueryData: queryID},
	})
	defer SetMarketParamsForQueryResolver(origResolverParams)

	priceCache := pricefeedservertypes.NewMarketToExchangePrices(1 * time.Minute)
	now := time.Now()
	priceCache.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: marketID,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     "coingecko",
					Price:          1_230_000, // 1.23 with exponent -6
					LastUpdateTime: &now,
				},
			},
		},
	})

	query := QueryConfig{
		ID:                queryID,
		AggregationMethod: "median",
		MaxSpreadPercent:  100.0,
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		RpcReaders: []RpcHandler{
			{
				QueryID:    queryID,
				UseCache:   true,
				Batchable:  true,
				EndpointID: "coingecko",
				MarketId:   "TEST-USD",
				SourceId:   "coingecko",
			},
		},
	}

	result, err := FetchPrice(context.Background(), query, priceCache)
	require.NoError(t, err)
	require.NotEmpty(t, result.EncodedValue)
	require.Len(t, result.RawResults, 1)
	require.NoError(t, result.RawResults[0].Err)
	require.InDelta(t, 1.23, result.RawResults[0].Value, 1e-9)
}

func TestFetchPrice_CacheFirstBatchableEndpoint_StaleCacheCountsAsFailure(t *testing.T) {
	const (
		queryID  = "feedface"
		marketID = uint32(900002)
	)

	origMarketParam := constants.StaticMarketParamsConfig[marketID]
	constants.StaticMarketParamsConfig[marketID] = &pricefeedtypes.MarketParam{
		Id:        marketID,
		Exponent:  -6,
		QueryData: queryID,
	}
	defer func() {
		if origMarketParam != nil {
			constants.StaticMarketParamsConfig[marketID] = origMarketParam
		} else {
			delete(constants.StaticMarketParamsConfig, marketID)
		}
	}()

	origResolverParams := marketParamsForQueryResolver
	SetMarketParamsForQueryResolver([]pricefeedtypes.MarketParam{
		{Id: marketID, QueryData: queryID},
	})
	defer SetMarketParamsForQueryResolver(origResolverParams)

	priceCache := pricefeedservertypes.NewMarketToExchangePrices(1 * time.Minute)
	stale := time.Now().Add(-2 * time.Minute)
	priceCache.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: marketID,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     "coingecko",
					Price:          1_230_000,
					LastUpdateTime: &stale,
				},
			},
		},
	})

	query := QueryConfig{
		ID:                queryID,
		AggregationMethod: "median",
		MaxSpreadPercent:  100.0,
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		RpcReaders: []RpcHandler{
			{
				QueryID:    queryID,
				UseCache:   true,
				Batchable:  true,
				EndpointID: "coingecko",
				MarketId:   "TEST-USD",
				SourceId:   "coingecko",
			},
		},
	}

	_, err := FetchPrice(context.Background(), query, priceCache)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "insufficient successful responses"))
}

func TestFetchPrice_CacheFirstBatchableEndpoints_MinResponsesSatisfiedWithOneStale(t *testing.T) {
	const (
		queryID  = "cafe1234"
		marketID = uint32(900003)
	)

	origMarketParam := constants.StaticMarketParamsConfig[marketID]
	constants.StaticMarketParamsConfig[marketID] = &pricefeedtypes.MarketParam{
		Id:        marketID,
		Exponent:  -6,
		QueryData: queryID,
	}
	defer func() {
		if origMarketParam != nil {
			constants.StaticMarketParamsConfig[marketID] = origMarketParam
		} else {
			delete(constants.StaticMarketParamsConfig, marketID)
		}
	}()

	origResolverParams := marketParamsForQueryResolver
	SetMarketParamsForQueryResolver([]pricefeedtypes.MarketParam{
		{Id: marketID, QueryData: queryID},
	})
	defer SetMarketParamsForQueryResolver(origResolverParams)

	priceCache := pricefeedservertypes.NewMarketToExchangePrices(1 * time.Minute)
	now := time.Now()
	stale := now.Add(-2 * time.Minute)
	priceCache.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: marketID,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     "coingecko",
					Price:          1_230_000,
					LastUpdateTime: &now,
				},
				{
					ExchangeId:     "coinpaprika",
					Price:          1_240_000,
					LastUpdateTime: &stale,
				},
			},
		},
	})

	query := QueryConfig{
		ID:                queryID,
		AggregationMethod: "median",
		MaxSpreadPercent:  100.0,
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		RpcReaders: []RpcHandler{
			{
				QueryID:    queryID,
				UseCache:   true,
				Batchable:  true,
				EndpointID: "coingecko",
				MarketId:   "TEST-USD",
				SourceId:   "coingecko",
			},
			{
				QueryID:    queryID,
				UseCache:   true,
				Batchable:  true,
				EndpointID: "coinpaprika",
				MarketId:   "TEST-USD",
				SourceId:   "coinpaprika",
			},
		},
	}

	result, err := FetchPrice(context.Background(), query, priceCache)
	require.NoError(t, err)
	require.NotEmpty(t, result.EncodedValue)
	require.Equal(t, 0.5, result.SuccessRate)
}

func TestFetchPrice_CacheFirstBatchableEndpoints_MinResponsesFailsWithOneStale(t *testing.T) {
	const (
		queryID  = "deadbeef"
		marketID = uint32(900004)
	)

	origMarketParam := constants.StaticMarketParamsConfig[marketID]
	constants.StaticMarketParamsConfig[marketID] = &pricefeedtypes.MarketParam{
		Id:        marketID,
		Exponent:  -6,
		QueryData: queryID,
	}
	defer func() {
		if origMarketParam != nil {
			constants.StaticMarketParamsConfig[marketID] = origMarketParam
		} else {
			delete(constants.StaticMarketParamsConfig, marketID)
		}
	}()

	origResolverParams := marketParamsForQueryResolver
	SetMarketParamsForQueryResolver([]pricefeedtypes.MarketParam{
		{Id: marketID, QueryData: queryID},
	})
	defer SetMarketParamsForQueryResolver(origResolverParams)

	priceCache := pricefeedservertypes.NewMarketToExchangePrices(1 * time.Minute)
	now := time.Now()
	stale := now.Add(-2 * time.Minute)
	priceCache.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: marketID,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     "coingecko",
					Price:          1_230_000,
					LastUpdateTime: &now,
				},
				{
					ExchangeId:     "coinpaprika",
					Price:          1_240_000,
					LastUpdateTime: &stale,
				},
			},
		},
	})

	query := QueryConfig{
		ID:                queryID,
		AggregationMethod: "median",
		MaxSpreadPercent:  100.0,
		MinResponses:      2,
		ResponseType:      "ufixed256x18",
		RpcReaders: []RpcHandler{
			{
				QueryID:    queryID,
				UseCache:   true,
				Batchable:  true,
				EndpointID: "coingecko",
				MarketId:   "TEST-USD",
				SourceId:   "coingecko",
			},
			{
				QueryID:    queryID,
				UseCache:   true,
				Batchable:  true,
				EndpointID: "coinpaprika",
				MarketId:   "TEST-USD",
				SourceId:   "coinpaprika",
			},
		},
	}

	_, err := FetchPrice(context.Background(), query, priceCache)
	require.Error(t, err)
	require.True(t, strings.Contains(err.Error(), "insufficient successful responses"))
}

