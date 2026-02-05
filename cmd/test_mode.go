package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tellor-io/layer-daemons/configs"
	"github.com/tellor-io/layer-daemons/constants"
	customquery "github.com/tellor-io/layer-daemons/custom_query"
	daemonflags "github.com/tellor-io/layer-daemons/flags"
	"github.com/tellor-io/layer-daemons/lib"
	libtime "github.com/tellor-io/layer-daemons/lib/time"
	pricefeedclient "github.com/tellor-io/layer-daemons/pricefeed/client"
	"github.com/tellor-io/layer-daemons/pricefeed/client/types"
	handler "github.com/tellor-io/layer-daemons/pricefeed/client/queryhandler"
	daemonserver "github.com/tellor-io/layer-daemons/server"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
	daemontypes "github.com/tellor-io/layer-daemons/types"
	"github.com/tellor-io/layer/utils"
	"google.golang.org/grpc"

	"cosmossdk.io/log"
)

// exchangeTestResult represents the result of testing an exchange for a market
type exchangeTestResult struct {
	Success bool
	Price   uint64
	Error   string
}

// pricefeedDaemonResult holds the results from starting the pricefeed daemon
type pricefeedDaemonResult struct {
	cache   *pricefeedservertypes.MarketToExchangePrices
	cleanup func()
}

// startPricefeedDaemon starts the gRPC server and pricefeed client to populate the price cache.
// It returns the price cache and a cleanup function that should be called when done.
func startPricefeedDaemon(
	homePath string,
	logger log.Logger,
	marketParams []types.MarketParam,
	exchangeConfigs map[types.ExchangeId]*types.ExchangeQueryConfig,
) (*pricefeedDaemonResult, error) {
	// Get default daemon flags
	daemonFlags := daemonflags.GetDefaultDaemonFlags()

	// Use a unique socket address for test mode to avoid conflicts
	daemonFlags.Shared.SocketAddress = "/tmp/daemons_test.sock"

	// Create the price cache
	indexPriceCache := pricefeedservertypes.NewMarketToExchangePrices(constants.MaxPriceAge)

	// Create server that will ingest gRPC messages from daemon clients
	server := daemonserver.NewServer(
		logger,
		grpc.NewServer(),
		&daemontypes.FileHandlerImpl{},
		daemonFlags.Shared.SocketAddress,
	)
	server.WithPriceFeedMarketToExchangePrices(indexPriceCache)

	// Start server for handling gRPC messages from daemons
	go server.Start()

	// Give the server a moment to start listening
	time.Sleep(100 * time.Millisecond)

	// Start pricefeed client for sending prices to the server
	// Note: grpcAddress is not used by the pricefeed client for fetching prices,
	// it's only used for the daemon's connection to cosmos gRPC services which we don't need in test mode.
	// The pricefeed client communicates with the server via the unix socket.
	priceFeedClient := pricefeedclient.StartNewClient(
		context.Background(),
		daemonFlags,
		"localhost:9090", // Not used for price fetching, just needs a valid address format
		logger,
		&daemontypes.GrpcClientImpl{},
		marketParams,
		exchangeConfigs,
		constants.StaticExchangeDetails,
		&pricefeedclient.SubTaskRunnerImpl{},
	)

	cleanup := func() {
		priceFeedClient.Stop()
		server.Stop()
	}

	return &pricefeedDaemonResult{
		cache:   indexPriceCache,
		cleanup: cleanup,
	}, nil
}

// waitForCacheWarmup polls the cache until the required market IDs have valid prices or timeout is reached.
func waitForCacheWarmup(
	cache *pricefeedservertypes.MarketToExchangePrices,
	requiredMarketIDs []uint32,
	timeout time.Duration,
	logger log.Logger,
) error {
	if len(requiredMarketIDs) == 0 {
		return nil
	}

	logger.Info("Waiting for price cache to warm up...", "required_markets", requiredMarketIDs, "timeout", timeout)

	// Build market params for the required IDs
	var requiredMarketParams []types.MarketParam
	for _, marketID := range requiredMarketIDs {
		if param, exists := constants.StaticMarketParamsConfig[marketID]; exists {
			requiredMarketParams = append(requiredMarketParams, *param)
		}
	}

	deadline := time.Now().Add(timeout)
	checkInterval := 500 * time.Millisecond

	for time.Now().Before(deadline) {
		prices := cache.GetValidMedianPrices(requiredMarketParams, time.Now())

		// Check if all required markets have prices
		allPresent := true
		var missingMarkets []uint32
		for _, marketID := range requiredMarketIDs {
			if _, exists := prices[marketID]; !exists {
				allPresent = false
				missingMarkets = append(missingMarkets, marketID)
			}
		}

		if allPresent {
			logger.Info("Price cache warmed up successfully", "markets_loaded", len(prices))
			return nil
		}

		logger.Debug("Cache warm-up in progress", "loaded", len(prices), "missing", missingMarkets)
		time.Sleep(checkInterval)
	}

	return fmt.Errorf("timeout waiting for price cache to warm up")
}

// hasCustomQueriesWithUsdVia checks if any custom queries require USD via prices from the cache.
func hasCustomQueriesWithUsdVia(customQueries map[string]customquery.QueryConfig, queryIdFilter string) bool {
	for queryID, queryConfig := range customQueries {
		// If a filter is provided, only check the matching query
		if queryIdFilter != "" && !strings.EqualFold(queryID, queryIdFilter) {
			continue
		}
		for _, rpcHandler := range queryConfig.RpcReaders {
			if rpcHandler.UsdViaID != 0 {
				return true
			}
		}
	}
	return false
}

// getRequiredMarketIDs extracts all unique UsdViaID values from custom queries.
func getRequiredMarketIDs(customQueries map[string]customquery.QueryConfig, queryIdFilter string) []uint32 {
	marketIDSet := make(map[uint32]bool)
	for queryID, queryConfig := range customQueries {
		// If a filter is provided, only check the matching query
		if queryIdFilter != "" && !strings.EqualFold(queryID, queryIdFilter) {
			continue
		}
		for _, rpcHandler := range queryConfig.RpcReaders {
			if rpcHandler.UsdViaID != 0 {
				marketIDSet[rpcHandler.UsdViaID] = true
			}
		}
	}

	var marketIDs []uint32
	for id := range marketIDSet {
		marketIDs = append(marketIDs, id)
	}
	return marketIDs
}

// runTestMode loads all price feed configurations and tests them
// If queryId is provided (non-empty), only that specific query will be tested
func runTestMode(homePath string, logger log.Logger, queryId string) error {
	if queryId != "" {
		logger.Info("Starting test mode - testing specific query ID", "query_id", queryId)
	} else {
		logger.Info("Starting test mode - verifying price feed configurations")
	}

	// Load configurations
	logger.Info("Loading market parameters...")
	marketParams := configs.ReadMarketParamsConfigFile(homePath)
	logger.Info("Loaded market parameters", "count", len(marketParams))

	logger.Info("Loading exchange configurations...")
	exchangeConfigs := configs.ReadExchangeQueryConfigFile(homePath)
	logger.Info("Loaded exchange configurations", "count", len(exchangeConfigs))

	// Load custom queries
	logger.Info("Loading custom query configurations...")
	customQueries, err := customquery.BuildQueryEndpoints(homePath, "config", "custom_query_config.toml")
	if err != nil {
		logger.Warn("Failed to load custom queries (may not exist)", "error", err)
		customQueries = make(map[string]customquery.QueryConfig)
	} else {
		logger.Info("Loaded custom query configurations", "count", len(customQueries))
	}

	// Normalize the query ID filter (remove 0x prefix if present, lowercase)
	queryIdFilter := strings.ToLower(strings.TrimPrefix(queryId, "0x"))

	// Test each market param
	if queryIdFilter == "" {
		logger.Info("Testing price feeds...")
	}
	marketParamsTested := 0
	for _, marketParam := range marketParams {
		// If a query ID filter is provided, compute the query ID for this market param and check if it matches
		if queryIdFilter != "" {
			queryDataBytes, err := hex.DecodeString(marketParam.QueryData)
			if err != nil {
				logger.Warn("Failed to decode query data", "pair", marketParam.Pair, "error", err)
				continue
			}
			marketQueryId := hex.EncodeToString(utils.QueryIDFromData(queryDataBytes))
			if !strings.EqualFold(marketQueryId, queryIdFilter) {
				continue
			}
			logger.Info("Found matching market param for query ID", "pair", marketParam.Pair, "query_id", marketQueryId)
		}
		marketParamsTested++
		if err := testMarketParam(marketParam, exchangeConfigs, logger); err != nil {
			logger.Error("Failed to test market param", "pair", marketParam.Pair, "id", marketParam.Id, "error", err)
		}
	}

	// Test custom queries
	customQueriesTested := 0
	if len(customQueries) > 0 {
		if queryIdFilter == "" {
			logger.Info("Testing custom queries...")
		}

		// Check if any custom queries need USD via prices from the cache
		var priceCache *pricefeedservertypes.MarketToExchangePrices
		var daemonCleanup func()

		if hasCustomQueriesWithUsdVia(customQueries, queryIdFilter) {
			logger.Info("Custom queries require USD via prices, starting pricefeed daemon...")
			daemonResult, err := startPricefeedDaemon(homePath, logger, marketParams, exchangeConfigs)
			if err != nil {
				logger.Error("Failed to start pricefeed daemon", "error", err)
				// Fall back to empty cache
				priceCache = pricefeedservertypes.NewMarketToExchangePrices(5 * time.Minute)
			} else {
				priceCache = daemonResult.cache
				daemonCleanup = daemonResult.cleanup
				defer daemonCleanup()

				// Wait for cache to warm up with required market prices
				requiredMarketIDs := getRequiredMarketIDs(customQueries, queryIdFilter)
				if err := waitForCacheWarmup(priceCache, requiredMarketIDs, 15*time.Second, logger); err != nil {
					logger.Warn("Cache warm-up incomplete, some tests may fail", "error", err)
				}
			}
		} else {
			// No USD via prices needed, use empty cache
			priceCache = pricefeedservertypes.NewMarketToExchangePrices(5 * time.Minute)
		}

		for customQueryId, queryConfig := range customQueries {
			// If a query ID filter is provided, check if it matches this custom query
			if queryIdFilter != "" && !strings.EqualFold(customQueryId, queryIdFilter) {
				continue
			}
			if queryIdFilter != "" {
				logger.Info("Found matching custom query for query ID", "query_id", customQueryId)
			}
			customQueriesTested++
			if err := testCustomQuery(customQueryId, queryConfig, priceCache, logger); err != nil {
				logger.Error("Failed to test custom query", "query_id", customQueryId, "error", err)
			}
		}
	}

	// If a query ID filter was provided but nothing matched, report it
	if queryIdFilter != "" && marketParamsTested == 0 && customQueriesTested == 0 {
		return fmt.Errorf("no market params or custom queries found matching query ID: %s", queryIdFilter)
	}

	logger.Info("Test mode completed successfully")
	return nil
}

// testMarketParam tests a single market param by querying all configured exchanges
func testMarketParam(
	marketParam types.MarketParam,
	exchangeConfigs map[types.ExchangeId]*types.ExchangeQueryConfig,
	logger log.Logger,
) error {
	logger.Info("Testing market", "pair", marketParam.Pair, "id", marketParam.Id)

	// Parse ExchangeConfigJson to get exchange ticker mappings
	var exchangeConfigJson types.ExchangeConfigJson
	if err := json.Unmarshal([]byte(marketParam.ExchangeConfigJson), &exchangeConfigJson); err != nil {
		return fmt.Errorf("failed to parse exchange config JSON: %w", err)
	}

	// Build market name to ID mapping for adjust-by-market support
	// For test mode, we'll skip adjust-by-market validation as it requires all market params
	marketNameToId := make(map[string]types.MarketId)
	marketNameToId[marketParam.Pair] = marketParam.Id

	// Track results for each exchange
	exchangeResults := make(map[types.ExchangeId]exchangeTestResult)
	var validPrices []uint64

	// Test each configured exchange
	for _, exchangeConfigJsonItem := range exchangeConfigJson.Exchanges {
		exchangeId := exchangeConfigJsonItem.ExchangeName

		// Check if exchange details exist
		exchangeDetails, exists := constants.StaticExchangeDetails[exchangeId]
		if !exists {
			exchangeResults[exchangeId] = exchangeTestResult{
				Success: false,
				Error:   "no exchange details found",
			}
			continue
		}

		// Check if exchange query config exists
		exchangeQueryConfig, hasConfig := exchangeConfigs[exchangeId]
		if !hasConfig {
			exchangeResults[exchangeId] = exchangeTestResult{
				Success: false,
				Error:   "exchange query config not found",
			}
			continue
		}

		// Query the exchange
		result := queryExchangeForMarket(
			exchangeId,
			exchangeDetails,
			*exchangeQueryConfig,
			marketParam,
			exchangeConfigJsonItem,
			logger,
		)
		exchangeResults[exchangeId] = result

		if result.Success {
			validPrices = append(validPrices, result.Price)
		}
	}

	// Calculate median
	var medianPrice uint64
	var medianErr error
	if len(validPrices) >= int(marketParam.MinExchanges) {
		medianPrice, medianErr = lib.Median[uint64](validPrices)
	} else {
		medianErr = fmt.Errorf("insufficient valid prices: got %d, need %d", len(validPrices), marketParam.MinExchanges)
	}

	// Log results
	logger.Info("Market test results",
		"pair", marketParam.Pair,
		"id", marketParam.Id,
		"valid_sources", len(validPrices),
		"total_sources", len(exchangeResults),
		"min_required", marketParam.MinExchanges,
		"median_price", medianPrice,
		"median_error", medianErr,
	)

	// Log individual exchange results
	for exchangeId, result := range exchangeResults {
		if result.Success {
			logger.Info("  ✓ Exchange succeeded",
				"exchange", exchangeId,
				"price", result.Price,
			)
		} else {
			logger.Warn("  ✗ Exchange failed",
				"exchange", exchangeId,
				"error", result.Error,
			)
		}
	}

	if medianErr != nil {
		return fmt.Errorf("failed to calculate median: %w", medianErr)
	}

	return nil
}

// queryExchangeForMarket queries a single exchange for a market and returns the result
func queryExchangeForMarket(
	exchangeId types.ExchangeId,
	exchangeDetails types.ExchangeQueryDetails,
	exchangeConfig types.ExchangeQueryConfig,
	marketParam types.MarketParam,
	exchangeConfigJsonItem types.ExchangeMarketConfigJson,
	logger log.Logger,
) exchangeTestResult {
	// Create a mutable exchange market config for this test
	mutableConfig := &types.MutableExchangeMarketConfig{
		Id:                   exchangeId,
		MarketToMarketConfig: make(map[types.MarketId]types.MarketConfig),
	}

	// Create market config from exchange config JSON
	marketConfig := types.MarketConfig{
		Ticker: exchangeConfigJsonItem.Ticker,
		Invert: exchangeConfigJsonItem.Invert,
	}

	// Handle AdjustByMarket if present (for test mode, we'll skip this as it requires
	// all market params to be loaded, which is complex for a simple test)
	if exchangeConfigJsonItem.AdjustByMarket != "" {
		// In a full implementation, we'd look up the market ID here
		// For test mode, we'll log a warning and continue
		logger.Debug("AdjustByMarket specified but not fully supported in test mode",
			"exchange", exchangeId,
			"adjust_by_market", exchangeConfigJsonItem.AdjustByMarket,
		)
	}

	mutableConfig.MarketToMarketConfig[marketParam.Id] = marketConfig

	// Create market price exponent map
	marketExponents := map[types.MarketId]types.Exponent{
		marketParam.Id: marketParam.Exponent,
	}

	// Create query handler
	queryHandler := &handler.ExchangeQueryHandlerImpl{
		TimeProvider: &libtime.TimeProviderImpl{},
	}

	// Create request handler with a new HTTP client
	httpClient := &http.Client{
		Timeout: time.Duration(exchangeConfig.TimeoutMs) * time.Millisecond,
	}
	requestHandler := daemontypes.NewRequestHandlerImpl(httpClient)

	// Query with timeout
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(exchangeConfig.TimeoutMs)*time.Millisecond)
	defer cancel()

	marketIds := []types.MarketId{marketParam.Id}

	prices, unavailableMarkets, err := queryHandler.Query(
		ctx,
		&exchangeDetails,
		mutableConfig,
		marketIds,
		requestHandler,
		marketExponents,
	)
	if err != nil {
		return exchangeTestResult{
			Success: false,
			Error:   err.Error(),
		}
	}

	if len(unavailableMarkets) > 0 {
		if err, ok := unavailableMarkets[marketParam.Id]; ok {
			return exchangeTestResult{
				Success: false,
				Error:   fmt.Sprintf("market unavailable: %v", err),
			}
		}
	}

	if len(prices) == 0 {
		return exchangeTestResult{
			Success: false,
			Error:   "no prices returned",
		}
	}

	return exchangeTestResult{
		Success: true,
		Price:   prices[0].Price,
	}
}

// testCustomQuery tests a single custom query configuration
func testCustomQuery(queryId string, queryConfig customquery.QueryConfig, priceCache *pricefeedservertypes.MarketToExchangePrices, logger log.Logger) error {
	logger.Info("Testing custom query", "query_id", queryId)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	results, err := customquery.FetchPrice(ctx, queryConfig, priceCache, logger)
	if err != nil {
		logger.Warn("  ✗ Custom query failed",
			"query_id", queryId,
			"error", err,
		)
		return err
	}

	logger.Info("  ✓ Custom query succeeded",
		"query_id", queryId,
		"encoded_value", results.EncodedValue,
	)

	return nil
}
