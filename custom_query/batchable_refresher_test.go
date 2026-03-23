package customquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/require"
	"github.com/tellor-io/layer-daemons/constants"
	"github.com/tellor-io/layer-daemons/pricefeed/client/types"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

func TestRefreshBatchableEndpointsOnce_CoingeckoStyleBatch(t *testing.T) {
	const (
		marketID1 = uint32(91001)
		marketID2 = uint32(91002)
	)

	original1 := constants.StaticMarketParamsConfig[marketID1]
	original2 := constants.StaticMarketParamsConfig[marketID2]
	constants.StaticMarketParamsConfig[marketID1] = &types.MarketParam{Id: marketID1, Exponent: -6}
	constants.StaticMarketParamsConfig[marketID2] = &types.MarketParam{Id: marketID2, Exponent: -6}
	defer func() {
		if original1 != nil {
			constants.StaticMarketParamsConfig[marketID1] = original1
		} else {
			delete(constants.StaticMarketParamsConfig, marketID1)
		}
		if original2 != nil {
			constants.StaticMarketParamsConfig[marketID2] = original2
		} else {
			delete(constants.StaticMarketParamsConfig, marketID2)
		}
	}()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/simple/price", r.URL.Path)
		require.Equal(t, "coin-a,coin-b", r.URL.Query().Get("ids"))
		require.Equal(t, "usd", r.URL.Query().Get("vs_currencies"))
		_, err := w.Write([]byte(`{"coin-a":{"usd":1.23},"coin-b":{"usd":"2.5"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()

	cache := pricefeedservertypes.NewMarketToExchangePrices(5 * time.Minute)
	plans := []batchableEndpointPlan{
		{
			endpointID:  "coingecko",
			urlTemplate: server.URL + "/simple/price?ids={coin_id}&vs_currencies=usd",
			method:      "GET",
			timeoutMs:   2000,
			targets: []batchableTarget{
				{
					marketID:     marketID1,
					responsePath: []string{"coin-a", "usd"},
					params:       map[string]string{"coin_id": "coin-a"},
				},
				{
					marketID:     marketID2,
					responsePath: []string{"coin-b", "usd"},
					params:       map[string]string{"coin_id": "coin-b"},
				},
			},
		},
	}

	RefreshBatchableEndpointsOnce(context.Background(), log.NewNopLogger(), plans, cache)

	price1, ok := cache.GetValidPriceForExchange(marketID1, "coingecko", time.Now())
	require.True(t, ok)
	require.Equal(t, uint64(1_230_000), price1)

	price2, ok := cache.GetValidPriceForExchange(marketID2, "coingecko", time.Now())
	require.True(t, ok)
	require.Equal(t, uint64(2_500_000), price2)
}

