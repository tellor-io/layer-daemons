package client

import (
	"context"
	"fmt"

	"github.com/tellor-io/layer/utils"
	oracletypes "github.com/tellor-io/layer/x/oracle/types"
)

func (c *Client) CurrentQuery(ctx context.Context) ([]byte, *oracletypes.QueryMeta, error) {
	var response *oracletypes.QueryCurrentCyclelistQueryResponse
	if err := c.withGRPCFallback(ctx, "current cyclelist query", func() error {
		var err error
		response, err = c.OracleQueryClient.CurrentCyclelistQuery(ctx, &oracletypes.QueryCurrentCyclelistQueryRequest{})
		return err
	}); err != nil {
		return nil, nil, fmt.Errorf("error calling 'CurrentCyclelistQuery': %w", err)
	}
	querydata, err := utils.QueryBytesFromString(response.QueryData)
	if err != nil {
		return nil, nil, fmt.Errorf("error parsing query id from response: %w", err)
	}

	return querydata, response.QueryMeta, nil
}
