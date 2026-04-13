package customquery

import (
	"encoding/hex"
	"testing"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
)

func TestResolveMarketIdForQuery_directQueryDataHex(t *testing.T) {
	orig := marketParamsForQueryResolver
	t.Cleanup(func() { marketParamsForQueryResolver = orig })

	SetMarketParamsForQueryResolver([]pricefeedtypes.MarketParam{
		{Id: 42, QueryData: "abcd01"},
	})
	mid, err := ResolveMarketIdForQuery("abcd01")
	require.NoError(t, err)
	require.Equal(t, uint32(42), mid)
}

func TestResolveMarketIdForQuery_keccakOfQueryData(t *testing.T) {
	orig := marketParamsForQueryResolver
	t.Cleanup(func() { marketParamsForQueryResolver = orig })

	qdHex := "00000000000000000000000000000000000000000000000000000000000000400000000000000000000000000000000000000000000000000000000000000080000000000000000000000000000000000000000000000000000000000000000953706F745072696365000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000C0000000000000000000000000000000000000000000000000000000000000004000000000000000000000000000000000000000000000000000000000000000800000000000000000000000000000000000000000000000000000000000000003747262000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000000037573640000000000000000000000000000000000000000000000000000000000"
	raw, err := hex.DecodeString(qdHex)
	require.NoError(t, err)
	queryID := hex.EncodeToString(crypto.Keccak256(raw))

	SetMarketParamsForQueryResolver([]pricefeedtypes.MarketParam{
		{Id: 69, QueryData: qdHex},
	})
	mid, err := ResolveMarketIdForQuery(queryID)
	require.NoError(t, err)
	require.Equal(t, uint32(69), mid)
}
