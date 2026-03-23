package client

import (
	"context"
	"encoding/hex"
	"fmt"

	customquery "github.com/tellor-io/layer-daemons/custom_query"
	"github.com/tellor-io/layer/utils"
)

func (c *Client) median(querydata []byte) (encodedValue string, rawPrice float64, err error) {
	querydatastr := hex.EncodeToString(querydata)
	// Phase 6: route all query IDs through custom_query.FetchPrice.
	queryId := utils.QueryIDFromData(querydata)
	queryIdStr := hex.EncodeToString(queryId)
	queryConfig, ok := c.Custom_query[queryIdStr]
	if !ok {
		return "", 0, fmt.Errorf("no config found for query data: %s", querydatastr)
	}
	results, err := customquery.FetchPrice(context.Background(), queryConfig, c.MarketToExchange)
	if err != nil {
		return "", 0, fmt.Errorf("failed to fetch price: %w", err)
	}
	// For custom queries, we return 0 for raw price (price guard won't apply)
	return results.EncodedValue, 0, nil
}
