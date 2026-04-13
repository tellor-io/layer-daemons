package customquery

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/ethereum/go-ethereum/crypto"
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
// Matches in order:
//  1. MarketParam.QueryData hex equals queryIDHex (after normalize) — legacy/direct form used in tests.
//  2. queryIDHex equals hex(keccak256(decoded MarketParam.QueryData)) — matches reporter
//     github.com/tellor-io/layer/utils.QueryIDFromData(chain query bytes) and [[queries.*]] id keys.
func ResolveMarketIdForQuery(queryIDHex string) (marketId uint32, err error) {
	if len(marketParamsForQueryResolver) == 0 {
		return 0, fmt.Errorf("market params not configured for ResolveMarketIdForQuery")
	}

	q := normalizeHex(queryIDHex)
	if q == "" {
		return 0, fmt.Errorf("query id is empty")
	}

	for _, marketParam := range marketParamsForQueryResolver {
		qd := normalizeHex(marketParam.QueryData)
		if qd == q {
			return marketParam.Id, nil
		}
		raw, err := hex.DecodeString(qd)
		if err != nil || len(raw) == 0 {
			continue
		}
		idHex := hex.EncodeToString(crypto.Keccak256(raw))
		if idHex == q {
			return marketParam.Id, nil
		}
	}

	return 0, fmt.Errorf("no market param found for query id %s", q)
}

// ResolveMarketIDForQueryConfig resolves the chain market id for a built query config.
// It prefers query.ChainMarketID when set, otherwise ResolveMarketIdForQuery(query.ID).
func ResolveMarketIDForQueryConfig(q QueryConfig) (uint32, error) {
	if q.ChainMarketID != 0 {
		return q.ChainMarketID, nil
	}
	return ResolveMarketIdForQuery(q.ID)
}
