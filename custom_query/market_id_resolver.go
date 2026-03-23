package customquery

import (
	"fmt"
	"strings"

	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
)

var marketParamsForQueryResolver []pricefeedtypes.MarketParam

// SetMarketParamsForQueryResolver configures the resolver used by ResolveMarketIdForQuery.
// This is expected to be called once at startup.
func SetMarketParamsForQueryResolver(marketParams []pricefeedtypes.MarketParam) {
	marketParamsForQueryResolver = marketParams
}

func normalizeHex(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"`)
	s = strings.TrimPrefix(strings.ToLower(s), "0x")
	return s
}

// ResolveMarketIdForQuery resolves a custom_query QueryConfig.ID (query ID hex) into the chain market param id.
//
// Semantics (phase 0):
// - Every custom_query query id to be priced is assumed to have a corresponding chain market param
//   where MarketParam.QueryData matches hex(queryId).
func ResolveMarketIdForQuery(queryIDHex string) (marketId uint32, err error) {
	if len(marketParamsForQueryResolver) == 0 {
		return 0, fmt.Errorf("market params not configured for ResolveMarketIdForQuery")
	}

	q := normalizeHex(queryIDHex)
	if q == "" {
		return 0, fmt.Errorf("query id is empty")
	}

	for _, marketParam := range marketParamsForQueryResolver {
		if normalizeHex(marketParam.QueryData) == q {
			return marketParam.Id, nil
		}
	}

	return 0, fmt.Errorf("no market param found for query id %s", q)
}

