package combined_handler

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	contractreader "github.com/tellor-io/layer-daemons/custom_query/contracts/contract_reader"
	rpcreader "github.com/tellor-io/layer-daemons/custom_query/rpc/rpc_reader"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

type CombinedHandler interface {
	FetchValue(
		ctx context.Context,
		contractReaders map[string]*contractreader.Reader,
		rpcReaders map[string]*rpcreader.Reader,
		priceCache *pricefeedservertypes.MarketToExchangePrices,
		minResponses int,
		maxSpreadPercent float64,
		maxDataAge time.Duration,
	) (float64, error)
}

type ParallelFetcher struct {
	mu       sync.Mutex
	results  map[string]any
	errors   map[string]error
	pending  int
	complete chan struct{}
}

func NewParallelFetcher() *ParallelFetcher {
	return &ParallelFetcher{
		results:  make(map[string]any),
		errors:   make(map[string]error),
		complete: make(chan struct{}, 16),
	}
}

func (p *ParallelFetcher) markDone() {
	p.mu.Lock()
	p.pending--
	p.mu.Unlock()
	p.complete <- struct{}{}
}

func (p *ParallelFetcher) FetchContract(
	ctx context.Context,
	key string,
	reader *contractreader.Reader,
	address string,
	functionSig string,
	args []string,
) {
	p.mu.Lock()
	p.pending++
	p.mu.Unlock()

	go func() {
		defer p.markDone()

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
	p.mu.Lock()
	p.pending++
	p.mu.Unlock()

	go func() {
		defer p.markDone()

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

// Wait blocks until all fetches complete (legacy callers).
func (p *ParallelFetcher) Wait() {
	p.WaitWithContext(context.Background())
}

// WaitWithContext waits until all fetches finish or the context/deadline expires.
func (p *ParallelFetcher) WaitWithContext(ctx context.Context) {
	p.mu.Lock()
	pending := p.pending
	p.mu.Unlock()
	if pending == 0 {
		return
	}

	var timer *time.Timer
	var timerCh <-chan time.Time
	if deadline, ok := ctx.Deadline(); ok {
		timer = time.NewTimer(time.Until(deadline))
		defer timer.Stop()
		timerCh = timer.C
	}

	for pending > 0 {
		select {
		case <-p.complete:
			p.mu.Lock()
			pending = p.pending
			p.mu.Unlock()
		case <-timerCh:
			return
		case <-ctx.Done():
			return
		}
	}
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
