package combined_handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"time"

	log "github.com/sirupsen/logrus"
	"github.com/tellor-io/layer-daemons/constants"
	contract_handlers "github.com/tellor-io/layer-daemons/custom_query/contracts/contract_handlers"
	contractreader "github.com/tellor-io/layer-daemons/custom_query/contracts/contract_reader"
	rpcreader "github.com/tellor-io/layer-daemons/custom_query/rpc/rpc_reader"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

// SFRXUSDPriceHandler calculates sFRXUSD price by multiplying the fundamental rate by FRX/USD spot price
// Note: Uses RPC sources (CoinGecko, CoinPaprika, Curve) because FRX is not available on standard CEX exchanges
type SFRXUSDPriceHandler struct{}

func init() {
	RegisterHandler("sfrxusd_price", &SFRXUSDPriceHandler{})
}

func (h *SFRXUSDPriceHandler) FetchValue(
	ctx context.Context,
	contractReaders map[string]*contractreader.Reader,
	rpcReaders map[string]*rpcreader.Reader,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
	config map[string]any,
	minResponses int,
	maxSpreadPercent float64,
	maxDataAge time.Duration,
) (float64, error) {
	fetchedAt := time.Now()
	// validate eth contract reader
	contractReader, exists := contractReaders["ethereum"]
	if !exists {
		return 0, fmt.Errorf("ethereum contract reader not found")
	}

	fetcher := NewParallelFetcher()

	// get sFrx contract price-per-share ratio
	fetcher.FetchContract(
		ctx,
		"price_per_share",
		contractReader,
		contract_handlers.SFRXUSD_CONTRACT,
		"pricePerShare() returns (uint256)",
		nil,
	)

	// get frx/usd spot price
	if reader, exists := rpcReaders["coingecko"]; exists {
		fetcher.FetchRPC(ctx, "frx_coingecko", reader)
	} else {
		log.Warn("[sFRXUSD] CoinGecko reader not available")
	}
	if reader, exists := rpcReaders["curve"]; exists {
		fetcher.FetchRPC(ctx, "frx_curve", reader)
	} else if config["curve_cache_market_id"] == nil {
		log.Warn("[sFRXUSD] Curve reader not available")
	}
	if reader, exists := rpcReaders["coinpaprika"]; exists {
		fetcher.FetchRPC(ctx, "frx_coinpaprika", reader)
	} else {
		log.Warn("[sFRXUSD] CoinPaprika reader not available")
	}

	fetcher.Wait()

	if err := checkDataAge(fetchedAt, maxDataAge); err != nil {
		return 0, fmt.Errorf("sfrxusd: %w", err)
	}

	// parse contract data (pricePerShare is scaled by 1e18)
	pricePerShareBytes, err := fetcher.GetBytes("price_per_share")
	if err != nil {
		return 0, fmt.Errorf("failed to get pricePerShare: %w", err)
	}

	pricePerShare := new(big.Int).SetBytes(pricePerShareBytes)
	if pricePerShare.Sign() == 0 {
		return 0, fmt.Errorf("invalid pricePerShare: zero")
	}

	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil)
	fundamentalRate := new(big.Float).Quo(
		new(big.Float).SetInt(pricePerShare),
		new(big.Float).SetInt(divisor),
	)
	fundamentalRateFloat, _ := fundamentalRate.Float64()
	log.Infof("[sFRXUSD] Fundamental rate (pricePerShare): %f", fundamentalRateFloat)

	var frxPrices []float64

	// Parse CoinGecko response
	if result, err := fetcher.GetBytes("frx_coingecko"); err == nil {
		var cgResponse map[string]map[string]float64
		if err := json.Unmarshal(result, &cgResponse); err == nil {
			if fraxData, exists := cgResponse["frax"]; exists {
				if price, ok := fraxData["usd"]; ok {
					frxPrices = append(frxPrices, price)
				} else {
					log.Warn("[sFRXUSD] CoinGecko response missing frax.usd field")
				}
			} else {
				log.Warn("[sFRXUSD] CoinGecko response missing frax key")
			}
		} else {
			log.Warnf("[sFRXUSD] Failed to parse CoinGecko JSON: %v", err)
		}
	} else {
		log.Warnf("[sFRXUSD] Failed to fetch CoinGecko data: %v", err)
	}

	// Parse Curve response when configured as a live RPC source.
	if _, exists := rpcReaders["curve"]; exists {
		if result, err := fetcher.GetBytes("frx_curve"); err == nil {
			var curveResponse struct {
				Data struct {
					UsdPrice float64 `json:"usd_price"`
				} `json:"data"`
			}
			if err := json.Unmarshal(result, &curveResponse); err == nil {
				if curveResponse.Data.UsdPrice > 0 {
					frxPrices = append(frxPrices, curveResponse.Data.UsdPrice)
				} else {
					log.Warn("[sFRXUSD] Curve response has zero or missing price")
				}
			} else {
				log.Warnf("[sFRXUSD] Failed to parse Curve JSON: %v", err)
			}
		} else {
			log.Warnf("[sFRXUSD] Failed to fetch Curve data: %v", err)
		}
	}

	if price, err := cachedCurvePrice(priceCache, config); err == nil {
		frxPrices = append(frxPrices, price)
	} else if config["curve_cache_market_id"] != nil {
		log.Warnf("[sFRXUSD] Failed to read cached Curve price: %v", err)
	}

	// Parse CoinPaprika response
	if result, err := fetcher.GetBytes("frx_coinpaprika"); err == nil {
		var cpResponse struct {
			Quotes struct {
				USD struct {
					Price float64 `json:"price"`
				} `json:"USD"`
			} `json:"quotes"`
		}
		if err := json.Unmarshal(result, &cpResponse); err == nil {
			if cpResponse.Quotes.USD.Price > 0 {
				frxPrices = append(frxPrices, cpResponse.Quotes.USD.Price)
			} else {
				log.Warn("[sFRXUSD] CoinPaprika response has zero or missing price")
			}
		} else {
			log.Warnf("[sFRXUSD] Failed to parse CoinPaprika JSON: %v", err)
		}
	} else {
		log.Warnf("[sFRXUSD] Failed to fetch CoinPaprika data: %v", err)
	}

	if len(frxPrices) < minResponses {
		return 0, fmt.Errorf("insufficient FRX/USD prices: got %d, need at least %d", len(frxPrices), minResponses)
	}

	// pick out the min and max to calculate spread
	minPrice := frxPrices[0]
	maxPrice := frxPrices[0]
	for _, p := range frxPrices {
		if p < minPrice {
			minPrice = p
		}
		if p > maxPrice {
			maxPrice = p
		}
	}
	spreadPercent := ((maxPrice - minPrice) / minPrice) * 100

	if spreadPercent > maxSpreadPercent {
		log.Warnf("[sFRXUSD] FRX/USD prices show excessive spread: %.2f%% (max: %.2f%%), prices: %v",
			spreadPercent, maxSpreadPercent, frxPrices)
		return 0, fmt.Errorf("FRX/USD price spread of %.2f%% exceeds maximum allowed %.2f%%",
			spreadPercent, maxSpreadPercent)
	}

	log.Infof("[sFRXUSD] FRX/USD price spread: %.2f%%, prices: %v", spreadPercent, frxPrices)

	medianFrxUsdPrice := fetcher.CalculateMedian(frxPrices)

	if medianFrxUsdPrice <= 0 {
		return 0, fmt.Errorf("invalid median FRX/USD price: %.6f", medianFrxUsdPrice)
	}

	log.Infof("[sFRXUSD] Median FRX/USD price: $%.6f", medianFrxUsdPrice)

	// final result = fundamental rate * frx/usd spot price
	result := fundamentalRateFloat * medianFrxUsdPrice

	return result, nil
}

func cachedCurvePrice(
	priceCache *pricefeedservertypes.MarketToExchangePrices,
	config map[string]any,
) (float64, error) {
	if priceCache == nil {
		return 0, fmt.Errorf("price cache is nil")
	}

	cacheMarketId, ok := uint32ConfigValue(config["curve_cache_market_id"])
	if !ok || cacheMarketId == 0 {
		return 0, fmt.Errorf("curve_cache_market_id is not configured")
	}

	exchangeId, ok := config["curve_cache_exchange_id"].(string)
	if !ok || exchangeId == "" {
		return 0, fmt.Errorf("curve_cache_exchange_id is not configured")
	}

	marketParam, found := constants.StaticMarketParamsConfig[cacheMarketId]
	if !found {
		return 0, fmt.Errorf("market param not found for cache market ID %d", cacheMarketId)
	}

	rawPrice, found := priceCache.GetValidExchangePrice(cacheMarketId, exchangeId, time.Now())
	if !found {
		return 0, fmt.Errorf("no valid cached price found for market ID %d on exchange %s", cacheMarketId, exchangeId)
	}

	return float64(rawPrice) * math.Pow10(int(marketParam.Exponent)), nil
}

func uint32ConfigValue(v any) (uint32, bool) {
	switch typed := v.(type) {
	case uint32:
		return typed, true
	case uint64:
		return uint32(typed), true
	case int:
		if typed < 0 {
			return 0, false
		}
		return uint32(typed), true
	case int64:
		if typed < 0 {
			return 0, false
		}
		return uint32(typed), true
	case float64:
		if typed < 0 {
			return 0, false
		}
		return uint32(typed), true
	default:
		return 0, false
	}
}
