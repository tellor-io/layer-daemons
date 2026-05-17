package contract_handlers

import (
	"context"
	"fmt"
	"math/big"
	"time"

	reader "github.com/tellor-io/layer-daemons/custom_query/contracts/contract_reader"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

var _ ContractHandler = (*KingHandler)(nil)

type KingHandler struct{}

func (r *KingHandler) FetchValue(
	ctx context.Context, reader *reader.Reader,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
	maxDataAge time.Duration,
) (float64, error) {
	fetchedAt := time.Now()
	result, err := reader.ReadContract(ctx, KING_CONTRACT, "fairValueOf(uint256)", []string{"1000000000000000000"})
	if err != nil {
		return 0, fmt.Errorf("failed to call fairValueOf: %w", err)
	}
	if err := checkDataAge(fetchedAt, maxDataAge); err != nil {
		return 0, fmt.Errorf("king: %w", err)
	}

	if len(result) < 64 {
		return 0, fmt.Errorf("unexpected result length: got %d bytes, expected 64 bytes for two uint256 values", len(result))
	}

	// Parse the second return value which is in wei
	valueInUsdWei := new(big.Int).SetBytes(result[32:64])

	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)

	divisorFloat := new(big.Float).SetInt(divisor)

	valueInUsdFloat := new(big.Float).SetInt(valueInUsdWei)
	usdValue := new(big.Float).Quo(valueInUsdFloat, divisorFloat)

	value, _ := usdValue.Float64()
	return value, nil
}
