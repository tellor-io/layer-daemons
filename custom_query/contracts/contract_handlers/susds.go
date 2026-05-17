package contract_handlers

import (
	"context"
	"fmt"
	"math/big"
	"time"

	reader "github.com/tellor-io/layer-daemons/custom_query/contracts/contract_reader"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

var _ ContractHandler = (*SUSDSHandler)(nil)

type SUSDSHandler struct{}

func (s *SUSDSHandler) FetchValue(
	ctx context.Context, reader *reader.Reader,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
	maxDataAge time.Duration,
) (float64, error) {
	fetchedAt := time.Now()
	result, err := reader.ReadContract(ctx, SUSDS_CONTRACT, "convertToAssets(uint256) returns (uint256)", []string{"1000000000000000000"})
	if err != nil {
		return 0, err
	}
	if err := checkDataAge(fetchedAt, maxDataAge); err != nil {
		return 0, fmt.Errorf("susds: %w", err)
	}

	valueInUsdWei := new(big.Int).SetBytes(result)

	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

	divisorFloat := new(big.Float).SetInt(divisor)

	valueInUsdFloat := new(big.Float).SetInt(valueInUsdWei)
	usdValue := new(big.Float).Quo(valueInUsdFloat, divisorFloat)

	value, _ := usdValue.Float64()

	return value, nil
}
