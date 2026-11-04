package types_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	servertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	pricefeedtypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

func TestGetValidPriceAggregates(t *testing.T) {
	now := time.Now().UTC()
	older := now.Add(-5 * time.Second)
	stale := now.Add(-2 * time.Minute)

	mte := pricefeedtypes.NewMarketToExchangePrices(30 * time.Second)
	mte.UpdatePrices([]*servertypes.MarketPriceUpdate{
		{
			MarketId: 5,
			ExchangePrices: []*servertypes.ExchangePrice{
				{ExchangeId: "Kraken", Price: 998560, LastUpdateTime: &now},
				{ExchangeId: "Okx", Price: 998820, LastUpdateTime: &older},
				{ExchangeId: "Bitstamp", Price: 900000, LastUpdateTime: &stale},
			},
		},
	})

	snapshots := mte.GetValidPriceAggregates([]uint32{5, 999}, now)
	require.Len(t, snapshots, 1)
	require.Equal(t, uint32(5), snapshots[0].MarketId)
	require.Equal(t, 2, snapshots[0].NumSources)
	require.Equal(t, uint64(998690), snapshots[0].Price) // median of 998560 and 998820
	require.True(t, snapshots[0].LatestUpdate.Equal(now))
}
