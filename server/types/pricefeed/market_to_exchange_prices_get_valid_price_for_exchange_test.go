package types

import (
	"testing"

	appconstants "github.com/tellor-io/layer-daemons/constants"
	servertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	"github.com/tellor-io/layer-daemons/testutil/constants"

	"github.com/stretchr/testify/require"
)

func TestMarketToExchangePrices_GetValidPriceForExchange_MissingMarket(t *testing.T) {
	mte := NewMarketToExchangePrices(appconstants.MaxPriceAge)

	price, ok := mte.GetValidPriceForExchange(
		constants.MarketId1,
		constants.ExchangeId1,
		constants.TimeT,
	)

	require.False(t, ok)
	require.Equal(t, uint64(0), price)
}

func TestMarketToExchangePrices_GetValidPriceForExchange_MissingExchange(t *testing.T) {
	mte := NewMarketToExchangePrices(appconstants.MaxPriceAge)

	cutoffTime := constants.TimeT.Add(-appconstants.MaxPriceAge)
	lastUpdateTime := cutoffTime
	mte.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: constants.MarketId1,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     constants.ExchangeId1,
					Price:          constants.Price1,
					LastUpdateTime: &lastUpdateTime,
				},
			},
		},
	})

	price, ok := mte.GetValidPriceForExchange(
		constants.MarketId1,
		constants.ExchangeId2,
		constants.TimeT,
	)

	require.False(t, ok)
	require.Equal(t, uint64(0), price)
}

func TestMarketToExchangePrices_GetValidPriceForExchange_StalePrice(t *testing.T) {
	mte := NewMarketToExchangePrices(appconstants.MaxPriceAge)

	// One nanosecond before the cutoff: should be considered stale.
	staleTime := constants.TimeTMinusThreshold
	mte.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: constants.MarketId1,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     constants.ExchangeId1,
					Price:          constants.Price1,
					LastUpdateTime: &staleTime,
				},
			},
		},
	})

	price, ok := mte.GetValidPriceForExchange(
		constants.MarketId1,
		constants.ExchangeId1,
		constants.TimeT,
	)

	require.False(t, ok)
	require.Equal(t, uint64(0), price)
}

func TestMarketToExchangePrices_GetValidPriceForExchange_ExactCutoffIsValid(t *testing.T) {
	mte := NewMarketToExchangePrices(appconstants.MaxPriceAge)

	cutoffTime := constants.TimeT.Add(-appconstants.MaxPriceAge)
	mte.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: constants.MarketId1,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     constants.ExchangeId1,
					Price:          constants.Price1,
					LastUpdateTime: &cutoffTime,
				},
			},
		},
	})

	price, ok := mte.GetValidPriceForExchange(
		constants.MarketId1,
		constants.ExchangeId1,
		constants.TimeT,
	)

	require.True(t, ok)
	require.Equal(t, constants.Price1, price)
}

