package client

import (
	"context"
	"encoding/hex"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/require"
	"github.com/tellor-io/layer-daemons/constants"
	customquery "github.com/tellor-io/layer-daemons/custom_query"
	"github.com/tellor-io/layer-daemons/exchange_common"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	pricefeed "github.com/tellor-io/layer-daemons/server/types/pricefeed"
	"github.com/tellor-io/layer/utils"
)

func TestMedianFallsBackToCustomQueryForCacheOnlyMarketParam(t *testing.T) {
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

	queryData := []byte("custom-query-data")
	queryId := hex.EncodeToString(utils.QueryIDFromData(queryData))
	cacheOnlyMarketParam := *constants.StaticMarketParamsConfig[exchange_common.SUSDSUSD_ID]
	cacheOnlyMarketParam.QueryData = "63616368653a53555344532d5553443a4375727665"

	c := NewClient(log.NewNopLogger(), "")
	c.MarketParams = []pricefeedtypes.MarketParam{cacheOnlyMarketParam}
	c.MarketToExchange = priceCache
	c.Custom_query = map[string]customquery.QueryConfig{
		queryId: {
			ID:                queryId,
			AggregationMethod: "median",
			MinResponses:      1,
			ResponseType:      "ufixed256x18",
			MaxSpreadPercent:  100,
			MarketCacheReaders: []customquery.MarketCacheHandler{
				{
					EndpointID:    "market_cache",
					MarketId:      "SUSDS-USD",
					CacheMarketId: exchange_common.SUSDSUSD_ID,
					ExchangeId:    string(exchange_common.EXCHANGE_ID_CURVE),
				},
			},
		},
	}

	encodedValue, rawPrice, err := c.median(context.Background(), queryData)

	require.NoError(t, err)
	require.NotEmpty(t, encodedValue)
	require.Equal(t, 0.0, rawPrice)
}

func TestMedianUsesMarketParamOnExactQueryDataMatch(t *testing.T) {
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

	queryData := []byte("cache:SUSDS-USD:Curve")
	queryDataHex := hex.EncodeToString(queryData)
	queryId := hex.EncodeToString(utils.QueryIDFromData(queryData))
	cacheOnlyMarketParam := *constants.StaticMarketParamsConfig[exchange_common.SUSDSUSD_ID]
	cacheOnlyMarketParam.QueryData = queryDataHex

	c := NewClient(log.NewNopLogger(), "")
	c.MarketParams = []pricefeedtypes.MarketParam{cacheOnlyMarketParam}
	c.MarketToExchange = priceCache
	c.Custom_query = map[string]customquery.QueryConfig{
		queryId: {
			ID:                queryId,
			AggregationMethod: "median",
			MinResponses:      1,
			ResponseType:      "ufixed256x18",
			MaxSpreadPercent:  100,
		},
	}

	encodedValue, rawPrice, err := c.median(context.Background(), queryData)

	require.NoError(t, err)
	require.NotEmpty(t, encodedValue)
	require.Equal(t, 1_250_000.0, rawPrice)
}
