package customquery

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	gometrics "github.com/hashicorp/go-metrics"
	"github.com/tellor-io/layer-daemons/constants"
	"github.com/tellor-io/layer-daemons/custom_query/combined/combined_handler"
	"github.com/tellor-io/layer-daemons/custom_query/contracts/contract_handlers"
	rpc_handler "github.com/tellor-io/layer-daemons/custom_query/rpc/rpc_handler"
	"github.com/tellor-io/layer-daemons/lib/metrics"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"

	"github.com/cosmos/cosmos-sdk/telemetry"
)

// Result holds the value returned from an endpoint
type Result struct {
	Value      float64
	Err        error
	EndpointID string
	MarketId   string
	SourceId   string
}

// FetchPriceResult holds the result of a price fetch operation
type FetchPriceResult struct {
	EncodedValue string
	RawResults   []Result
	QueryID      string
	ResponseType string
	SuccessRate  float64
}

func fetchPriceResultFromCollected(
	allResults []Result,
	successfulResults []Result,
	query QueryConfig,
	totalEndpoints int,
	encodedValue string,
) *FetchPriceResult {
	successRate := 0.0
	if totalEndpoints > 0 {
		successRate = float64(len(successfulResults)) / float64(totalEndpoints)
	}
	return &FetchPriceResult{
		EncodedValue: encodedValue,
		RawResults:   allResults,
		QueryID:      query.ID,
		ResponseType: query.ResponseType,
		SuccessRate:  successRate,
	}
}

// FetchPrice fetches price data for the given query ID.
// On error (e.g. insufficient endpoints or median spread exceeded), the returned *FetchPriceResult
// may still be non-nil with RawResults filled for per-source diagnostics.
func FetchPrice(
	ctx context.Context,
	query QueryConfig,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
) (*FetchPriceResult, error) {
	// Keep custom-query fan-out bounded by the reporter deadline and a local cap.
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	totalEndpoints := len(query.RpcReaders) + len(query.ContractReaders) + len(query.MarketCacheReaders) + len(query.CombinedReaders)
	results := make(chan Result, totalEndpoints)
	var wg sync.WaitGroup

	// Launch goroutines for contract endpoints
	for _, contractEndpoint := range query.ContractReaders {
		wg.Add(1)
		go func(ep ContractHandler) {
			defer wg.Done()
			result := fetchFromContractEndpoint(ctx, ep, priceCache)
			results <- result
		}(contractEndpoint)
	}
	// Launch goroutines for REST API endpoints
	for _, rpchandler := range query.RpcReaders {
		wg.Add(1)
		go func(ep RpcHandler) {
			defer wg.Done()
			result := fetchFromRpcEndpoint(ctx, ep, priceCache)
			results <- result
		}(rpchandler)

	}
	// Launch goroutines for cached market endpoints
	for _, marketCacheHandler := range query.MarketCacheReaders {
		wg.Add(1)
		go func(ep MarketCacheHandler) {
			defer wg.Done()
			result := fetchFromMarketCacheEndpoint(ep, priceCache)
			results <- result
		}(marketCacheHandler)
	}
	// Launch goroutines for combined endpoints
	for _, combinedHandler := range query.CombinedReaders {
		wg.Add(1)
		go func(ep CombinedHandler) {
			defer wg.Done()
			result := fetchFromCombinedEndpoint(ctx, ep, priceCache)
			results <- result
		}(combinedHandler)
	}
	// Close results channel when all goroutines complete
	go func() {
		wg.Wait()
		close(results)
	}()

	var allResults []Result
	var successfulResults []Result
	var aggregateErr error

	for results != nil {
		var result Result
		var ok bool

		select {
		case result, ok = <-results:
			if !ok {
				results = nil
				continue
			}
		case <-ctx.Done():
			results = nil
			continue
		}

		allResults = append(allResults, result)
		if result.Err == nil {
			successfulResults = append(successfulResults, result)
			emitPriceForTelemetry(result, query)
			emitSuccessForTelemetry(result, query)
		} else {
			emitErrorForTelemetry(result, query)
			continue
		}

		if len(successfulResults) >= query.MinResponses {
			aggregatedValue, err := aggregateResults(successfulResults, query.AggregationMethod, query.ResponseType, query.MaxSpreadPercent)
			if err == nil {
				cancel()
				return fetchPriceResultFromCollected(allResults, successfulResults, query, totalEndpoints, aggregatedValue), nil
			}
			aggregateErr = err
		}
	}

	// Check if we have enough successful responses
	if len(successfulResults) < query.MinResponses {
		return fetchPriceResultFromCollected(allResults, successfulResults, query, totalEndpoints, ""),
			fmt.Errorf("insufficient successful responses: got %d, need %d",
				len(successfulResults), query.MinResponses)
	}

	// Aggregate results
	aggregatedValue, err := aggregateResults(successfulResults, query.AggregationMethod, query.ResponseType, query.MaxSpreadPercent)
	if err != nil {
		if aggregateErr != nil {
			err = aggregateErr
		}
		return fetchPriceResultFromCollected(allResults, successfulResults, query, totalEndpoints, ""), err
	}

	return fetchPriceResultFromCollected(allResults, successfulResults, query, totalEndpoints, aggregatedValue), nil
}

func emitPriceForTelemetry(result Result, query QueryConfig) {
	telemetry.SetGaugeWithLabels(
		[]string{metrics.PricefeedDaemon, metrics.PriceEncoderUpdatePrice},
		float32(result.Value),
		[]gometrics.Label{
			metrics.GetLabelForStringValue(metrics.MarketId, result.MarketId),
			metrics.GetLabelForStringValue(metrics.ExchangeId, result.SourceId),
		},
	)
}

func emitSuccessForTelemetry(result Result, query QueryConfig) {
	telemetry.IncrCounterWithLabels(
		[]string{metrics.PricefeedDaemon, metrics.PriceEncoderPriceConversion, metrics.Success},
		1.0,
		[]gometrics.Label{
			metrics.GetLabelForStringValue(metrics.MarketId, result.MarketId),
			metrics.GetLabelForStringValue(metrics.ExchangeId, result.SourceId),
		},
	)
}

func emitErrorForTelemetry(result Result, query QueryConfig) {
	telemetry.IncrCounterWithLabels(
		[]string{metrics.PricefeedDaemon, metrics.PriceEncoderPriceConversion, metrics.Error},
		1.0,
		[]gometrics.Label{
			metrics.GetLabelForStringValue(metrics.MarketId, result.MarketId),
			metrics.GetLabelForStringValue(metrics.ExchangeId, result.SourceId),
			metrics.GetLabelForStringValue(metrics.Reason, normalizedErrorReason(result.Err)),
		},
	)
}

func normalizedErrorReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
		return "timeout"
	}

	errText := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errText, "context deadline exceeded"),
		strings.Contains(errText, "timeout"),
		strings.Contains(errText, "client.timeout exceeded"):
		return "timeout"
	case strings.Contains(errText, "429"),
		strings.Contains(errText, "rate limit"),
		strings.Contains(errText, "too many requests"):
		return metrics.RateLimit
	case containsHTTPStatus(errText, http.StatusInternalServerError, 599):
		return metrics.HttpGet5xx
	case containsHTTPStatus(errText, http.StatusBadRequest, 499):
		return "http_4xx"
	case strings.Contains(errText, "unmarshal"),
		strings.Contains(errText, "decode"),
		strings.Contains(errText, "parse"):
		return "decode_error"
	default:
		return "unknown"
	}
}

func containsHTTPStatus(errText string, minStatus, maxStatus int) bool {
	for status := minStatus; status <= maxStatus; status++ {
		if strings.Contains(errText, strconv.Itoa(status)) {
			return true
		}
	}
	return false
}

// fetchFromContractEndpoint fetches data from a smart contract
func fetchFromContractEndpoint(
	ctx context.Context,
	contractReader ContractHandler,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
) Result {
	handler, err := contract_handlers.GetHandler(contractReader.Handler)
	if err != nil {
		return Result{
			Err:        fmt.Errorf("failed to get contract handler: %w", err),
			EndpointID: contractReader.Handler,
			MarketId:   contractReader.MarketId,
			SourceId:   contractReader.SourceId,
		}
	}
	value, err := handler.FetchValue(ctx, contractReader.Reader, priceCache, contractReader.MaxDataAge)
	if err != nil {
		return Result{
			Err:        fmt.Errorf("failed to fetch contract value: %w", err),
			EndpointID: contractReader.Handler,
			MarketId:   contractReader.MarketId,
			SourceId:   contractReader.SourceId,
		}
	}

	defer contractReader.Reader.Close()

	return Result{
		Value:      value,
		EndpointID: "contract:" + contractReader.Handler,
		MarketId:   contractReader.MarketId,
		SourceId:   contractReader.SourceId,
	}
}

func fetchFromRpcEndpoint(
	ctx context.Context,
	rpchandler RpcHandler,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
) Result {
	handlerStr := rpchandler.Handler
	if handlerStr == "" {
		handlerStr = "generic"
	}

	handler, err := rpc_handler.GetHandler(handlerStr)
	if err != nil {
		return Result{
			Err:        fmt.Errorf("failed to get RPC handler: %w", err),
			EndpointID: rpchandler.EndpointID,
			MarketId:   rpchandler.MarketId,
			SourceId:   rpchandler.SourceId,
		}
	}

	value, err := handler.FetchValue(ctx, rpchandler.Reader, rpchandler.Invert, rpchandler.UsdViaID, priceCache, rpchandler.MaxDataAge)
	if err != nil {
		return Result{
			Err:        fmt.Errorf("failed to fetch value: %w", err),
			EndpointID: rpchandler.EndpointID,
			MarketId:   rpchandler.MarketId,
			SourceId:   rpchandler.SourceId,
		}
	}

	return Result{
		Value:      value,
		EndpointID: rpchandler.EndpointID,
		MarketId:   rpchandler.MarketId,
		SourceId:   rpchandler.SourceId,
	}
}

func fetchFromMarketCacheEndpoint(
	marketCacheHandler MarketCacheHandler,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
) Result {
	if priceCache == nil {
		return Result{
			Err:        fmt.Errorf("price cache is nil"),
			EndpointID: marketCacheHandler.EndpointID,
			MarketId:   marketCacheHandler.MarketId,
			SourceId:   marketCacheHandler.ExchangeId,
		}
	}

	marketParam, found := constants.StaticMarketParamsConfig[marketCacheHandler.CacheMarketId]
	if !found {
		return Result{
			Err:        fmt.Errorf("market param not found for cache market ID %d", marketCacheHandler.CacheMarketId),
			EndpointID: marketCacheHandler.EndpointID,
			MarketId:   marketCacheHandler.MarketId,
			SourceId:   marketCacheHandler.ExchangeId,
		}
	}

	rawPrice, found := priceCache.GetValidExchangePrice(
		marketCacheHandler.CacheMarketId,
		marketCacheHandler.ExchangeId,
		time.Now(),
	)
	if !found {
		return Result{
			Err:        fmt.Errorf("no valid cached price found for market ID %d on exchange %s", marketCacheHandler.CacheMarketId, marketCacheHandler.ExchangeId),
			EndpointID: marketCacheHandler.EndpointID,
			MarketId:   marketCacheHandler.MarketId,
			SourceId:   marketCacheHandler.ExchangeId,
		}
	}

	return Result{
		Value:      float64(rawPrice) * math.Pow10(int(marketParam.Exponent)),
		EndpointID: marketCacheHandler.EndpointID,
		MarketId:   marketCacheHandler.MarketId,
		SourceId:   marketCacheHandler.ExchangeId,
	}
}

// aggregateResults aggregates results using the specified method
func aggregateResults(results []Result, method, responseType string, maxSpreadPercent float64) (string, error) {
	if len(results) == 0 {
		return "", fmt.Errorf("no results to aggregate")
	}

	// Extract values
	values := make([]float64, len(results))
	for i, result := range results {
		values[i] = result.Value
	}

	switch strings.ToLower(method) {
	case "median":
		return MedianInHex(values, responseType, maxSpreadPercent)
	// case "mode":
	// return ModeInHex(values, responseType)
	default:
		return "", fmt.Errorf("unsupported aggregation method: %s", method)
	}
}

// fetchFromCombinedEndpoint fetches data using both contract and RPC sources
func fetchFromCombinedEndpoint(
	ctx context.Context,
	combinedReader CombinedHandler,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
) Result {
	handler, err := combined_handler.GetHandler(combinedReader.Handler)
	if err != nil {
		return Result{
			Err:        fmt.Errorf("failed to get combined handler: %w", err),
			EndpointID: combinedReader.Handler,
		}
	}

	value, err := handler.FetchValue(ctx, combinedReader.ContractReaders, combinedReader.RpcReaders, priceCache, combinedReader.Config, combinedReader.MinResponses, combinedReader.MaxSpreadPercent, combinedReader.MaxDataAge)
	if err != nil {
		return Result{
			Err:        fmt.Errorf("failed to fetch combined value: %w", err),
			EndpointID: combinedReader.Handler,
		}
	}

	// Clean up readers
	for _, reader := range combinedReader.ContractReaders {
		defer reader.Close()
	}

	return Result{
		Value:      value,
		EndpointID: "combined:" + combinedReader.Handler,
	}
}

func ConvertFloat64ToString(num float64) string {
	return strconv.FormatFloat(num, 'f', -1, 64)
}
