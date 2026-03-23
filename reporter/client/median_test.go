package client

import (
	"encoding/hex"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/require"
	customquery "github.com/tellor-io/layer-daemons/custom_query"
	"github.com/tellor-io/layer-daemons/constants"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
	servertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
	"github.com/tellor-io/layer/utils"
)

func TestMedian_RoutesThroughCustomQueryForKnownQueryID(t *testing.T) {
	queryData := []byte("phase6-querydata")
	queryIDHex := hex.EncodeToString(utils.QueryIDFromData(queryData))
	marketID := uint32(920001)

	origMarketParam := constants.StaticMarketParamsConfig[marketID]
	constants.StaticMarketParamsConfig[marketID] = &pricefeedtypes.MarketParam{
		Id:        marketID,
		Exponent:  -6,
		QueryData: queryIDHex,
	}
	defer func() {
		if origMarketParam != nil {
			constants.StaticMarketParamsConfig[marketID] = origMarketParam
		} else {
			delete(constants.StaticMarketParamsConfig, marketID)
		}
	}()

	customquery.SetMarketParamsForQueryResolver([]pricefeedtypes.MarketParam{{Id: marketID, QueryData: queryIDHex}})

	priceCache := pricefeedservertypes.NewMarketToExchangePrices(1 * time.Minute)
	now := time.Now()
	priceCache.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: marketID,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     "coingecko",
					Price:          2_500_000, // 2.5 with exponent -6
					LastUpdateTime: &now,
				},
			},
		},
	})

	c := &Client{
		logger:          log.NewNopLogger(),
		MarketToExchange: priceCache,
		Custom_query: map[string]customquery.QueryConfig{
			queryIDHex: {
				ID:                queryIDHex,
				AggregationMethod: "median",
				MinResponses:      1,
				MaxSpreadPercent:  100.0,
				ResponseType:      "ufixed256x18",
				RpcReaders: []customquery.RpcHandler{
					{
						QueryID:    queryIDHex,
						UseCache:   true,
						Batchable:  true,
						EndpointID: "coingecko",
						MarketId:   "TEST-USD",
						SourceId:   "coingecko",
					},
				},
			},
		},
	}

	encoded, rawPrice, err := c.median(queryData)
	require.NoError(t, err)
	require.NotEmpty(t, encoded)
	require.Equal(t, float64(0), rawPrice)
}

