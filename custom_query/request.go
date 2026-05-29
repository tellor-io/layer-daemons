package customquery

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	gometrics "github.com/hashicorp/go-metrics"
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
	// Create a context with timeout
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	totalEndpoints := len(query.RpcReaders) + len(query.ContractReaders) + len(query.CombinedReaders)
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

	// Collect results
	var allResults []Result
	// Count successful results
	var successfulResults []Result
	for result := range results {
		allResults = append(allResults, result)
		if result.Err == nil {
			successfulResults = append(successfulResults, result)
			// Emit metrics for successful results
			emitPriceForTelemetry(result, query)
			emitSuccessForTelemetry(result, query)
		} else {
			// Emit error metrics for failed results
			emitErrorForTelemetry(result, query)
		}
	}
	// Check if we have enough successful responses
	if len(successfulResults) < query.MinResponses {
		return fetchPriceResultFromCollected(allResults, successfulResults, query, totalEndpoints, ""),
			fmt.Errorf("insufficient successful responses: got %d, need %d",
				len(successfulResults), query.MinResponses)
	}
	fmt.Println("Successful results:", successfulResults)
	// Aggregate results
	aggregatedValue, err := aggregateResults(successfulResults, query.AggregationMethod, query.ResponseType, query.MaxSpreadPercent)
	if err != nil {
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
			metrics.GetLabelForStringValue(metrics.Reason, result.Err.Error()),
		},
	)
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
	if err := validatePositiveFiniteValue(value, "contract value"); err != nil {
		return Result{
			Err:        err,
			EndpointID: contractReader.Handler,
			MarketId:   contractReader.MarketId,
			SourceId:   contractReader.SourceId,
		}
	}

	defer contractReader.Reader.Close()

	fmt.Println("Contract value:", value)
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
	if err := validatePositiveFiniteValue(value, "RPC value"); err != nil {
		return Result{
			Err:        err,
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

	value, err := handler.FetchValue(ctx, combinedReader.ContractReaders, combinedReader.RpcReaders, priceCache, combinedReader.MinResponses, combinedReader.MaxSpreadPercent, combinedReader.MaxDataAge)
	if err != nil {
		return Result{
			Err:        fmt.Errorf("failed to fetch combined value: %w", err),
			EndpointID: combinedReader.Handler,
		}
	}
	if err := validatePositiveFiniteValue(value, "combined value"); err != nil {
		return Result{
			Err:        err,
			EndpointID: combinedReader.Handler,
		}
	}

	// Clean up readers
	for _, reader := range combinedReader.ContractReaders {
		defer reader.Close()
	}

	fmt.Println("Combined value:", value)
	return Result{
		Value:      value,
		EndpointID: "combined:" + combinedReader.Handler,
	}
}

func ConvertFloat64ToString(num float64) string {
	return strconv.FormatFloat(num, 'f', -1, 64)
}

func validatePositiveFiniteValue(value float64, label string) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("%s is not finite: %v", label, value)
	}
	if value <= 0 {
		return fmt.Errorf("%s must be positive, got %f", label, value)
	}
	return nil
}
