package combined_handler

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	contractreader "github.com/tellor-io/layer-daemons/custom_query/contracts/contract_reader"
	rpcreader "github.com/tellor-io/layer-daemons/custom_query/rpc/rpc_reader"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

// Context keys for query and source information
type contextKey string

const (
	queryIDKey contextKey = "queryID"
)

// WithQueryID adds queryID to the context
func WithQueryID(ctx context.Context, queryID string) context.Context {
	return context.WithValue(ctx, queryIDKey, queryID)
}

// getQueryID extracts queryID from context
func getQueryID(ctx context.Context) (string, bool) {
	val := ctx.Value(queryIDKey)
	if val == nil {
		return "", false
	}
	queryID, ok := val.(string)
	return queryID, ok
}

// chainNameToChainID maps chain names to chain IDs
// This is a simple mapping - in production, this might come from config
func chainNameToChainID(chainName string) string {
	chainMap := map[string]string{
		"ethereum":  "1",
		"polygon":   "137",
		"arbitrum":  "42161",
		"optimism":  "10",
		"base":      "8453",
		"bsc":       "56",
		"avalanche": "43114",
	}
	if chainID, exists := chainMap[strings.ToLower(chainName)]; exists {
		return chainID
	}
	// Default to "1" (Ethereum mainnet) if chain not found
	return "1"
}

type CombinedHandler interface {
	FetchValue(
		ctx context.Context,
		contractReaders map[string]*contractreader.Reader,
		rpcReaders map[string]*rpcreader.Reader,
		priceCache *pricefeedservertypes.MarketToExchangePrices,
		minResponses int,
		maxSpreadPercent float64,
	) (float64, error)
}

type ParallelFetcher struct {
	mu      sync.Mutex
	results map[string]any
	errors  map[string]error
	wg      sync.WaitGroup
}

func NewParallelFetcher() *ParallelFetcher {
	return &ParallelFetcher{
		results: make(map[string]any),
		errors:  make(map[string]error),
	}
}

// FetchContract fetches data from a contract and adds call information to context for batching
// sourceID is the identifier for the source (e.g., "ethereum", "polygon")
// key is the call key used to identify this specific call (e.g., "total_assets", "conversion_rate")
func (p *ParallelFetcher) FetchContract(
	ctx context.Context,
	key string,
	sourceID string,
	reader *contractreader.Reader,
	address string,
	functionSig string,
	args []string,
) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		// Extract queryID from context
		queryID, hasQueryID := getQueryID(ctx)
		if !hasQueryID {
			queryID = "unknown"
		}

		// Generate callID in format: {queryID}-{sourceID}-{callKey}
		callID := fmt.Sprintf("%s-%s-%s", queryID, sourceID, key)

		// Determine chainID from sourceID (sourceID is typically the chain name)
		chainID := chainNameToChainID(sourceID)

		// Use sourceID as batchGroup (sources with same ID will be batched together)
		batchGroup := sourceID

		// Add call information to context using WithCallInfo from contract_reader package
		ctx = contractreader.WithCallInfo(ctx, chainID, batchGroup, callID)

		result, err := reader.ReadContract(ctx, address, functionSig, args)

		p.mu.Lock()
		defer p.mu.Unlock()
		if err != nil {
			p.errors[key] = err
		} else {
			p.results[key] = result
		}
	}()
}

func (p *ParallelFetcher) FetchRPC(
	ctx context.Context,
	key string,
	reader *rpcreader.Reader,
) {
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()

		result, err := reader.FetchJSON(ctx)

		p.mu.Lock()
		defer p.mu.Unlock()
		if err != nil {
			p.errors[key] = err
		} else {
			p.results[key] = result
		}
	}()
}

func (p *ParallelFetcher) Wait() {
	p.wg.Wait()
}

func (p *ParallelFetcher) GetResult(key string) (any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err, exists := p.errors[key]; exists {
		return nil, err
	}

	result, exists := p.results[key]
	if !exists {
		return nil, fmt.Errorf("no result found for key: %s", key)
	}

	return result, nil
}

func (p *ParallelFetcher) GetBytes(key string) ([]byte, error) {
	result, err := p.GetResult(key)
	if err != nil {
		return nil, err
	}

	bytes, ok := result.([]byte)
	if !ok {
		return nil, fmt.Errorf("result for key %s is not []byte", key)
	}

	return bytes, nil
}

func (p *ParallelFetcher) GetContractBytes(key string) ([]byte, error) {
	return p.GetBytes(key)
}

// calculate median of a list of floats
func (p *ParallelFetcher) CalculateMedian(prices []float64) float64 {
	if len(prices) == 0 {
		return 0
	}
	if len(prices) == 1 {
		return prices[0]
	}

	// Sort the prices
	sort.Float64s(prices)

	// Calculate median
	n := len(prices)
	if n%2 == 0 {
		return (prices[n/2-1] + prices[n/2]) / 2.0
	}
	// Odd number of elements: middle value
	return prices[n/2]
}
