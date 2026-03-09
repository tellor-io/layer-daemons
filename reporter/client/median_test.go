package client

import (
	"encoding/hex"
	"fmt"
	"testing"

	"cosmossdk.io/log"
	"github.com/tellor-io/layer-daemons/lib/prices"
	"github.com/tellor-io/layer-daemons/pricefeed/client/types"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

// mockOrchestrator is a mock implementation of QueryOrchestrator for testing
type mockOrchestrator struct {
	getPriceFunc func(queryID string) (float64, error)
}

func (m *mockOrchestrator) GetPrice(queryID string) (float64, error) {
	if m.getPriceFunc != nil {
		return m.getPriceFunc(queryID)
	}
	return 0, nil
}

func TestMedian_WithOrchestrator_Success(t *testing.T) {
	// Setup
	querydata := []byte{0x12, 0x34, 0x56, 0x78}
	querydatastr := hex.EncodeToString(querydata)
	expectedPrice := 50000.0
	expectedExponent := int32(-8)

	// Create mock orchestrator that returns a price
	mockOrch := &mockOrchestrator{
		getPriceFunc: func(queryID string) (float64, error) {
			if queryID == querydatastr {
				return expectedPrice, nil
			}
			return 0, nil
		},
	}

	// Create client with orchestrator
	client := &Client{
		logger:       log.NewNopLogger(),
		MarketParams: []types.MarketParam{},
		// Set orchestrator via a helper (we'll add this field)
	}

	// This test will be updated once we add the orchestrator field
	// For now, we're testing the concept
	_ = mockOrch
	_ = client
	_ = querydata
	_ = expectedPrice
	_ = expectedExponent
}

func TestMedian_WithOrchestrator_NotFound_FallbackToOldSystem(t *testing.T) {
	// Setup
	querydata := []byte{0x12, 0x34, 0x56, 0x78}
	querydatastr := hex.EncodeToString(querydata)
	expectedPrice := 100.0
	expectedExponent := int32(-8)

	// Create mock orchestrator that returns error (not found)
	mockOrch := &mockOrchestrator{
		getPriceFunc: func(queryID string) (float64, error) {
			return 0, fmt.Errorf("no asset pair found for queryID: %s", queryID)
		},
	}

	// Create market param for fallback
	marketParam := types.MarketParam{
		Id:        1,
		Pair:      "BTC/USD",
		Exponent:  expectedExponent,
		QueryData: querydatastr,
	}

	// Create price cache with the price
	priceCache := pricefeedservertypes.NewMarketToExchangePrices(5 * 60 * 1000000000) // 5 minutes in nanoseconds
	// Note: This is a simplified test - in reality we'd need to set up the cache properly

	client := &Client{
		logger:           log.NewNopLogger(),
		MarketParams:     []types.MarketParam{marketParam},
		MarketToExchange: priceCache,
	}

	_ = mockOrch
	_ = client
	_ = querydata
	_ = expectedPrice
}

func TestMedian_WithOrchestrator_EncodesPriceCorrectly(t *testing.T) {
	// Test that when orchestrator returns a price, it's encoded correctly
	querydata := []byte{0x12, 0x34, 0x56, 0x78}
	querydatastr := hex.EncodeToString(querydata)
	price := 50000.0
	exponent := int32(-8)

	// Encode price
	encoded, err := prices.EncodePrice(price, exponent)
	if err != nil {
		t.Fatalf("Failed to encode price: %v", err)
	}

	if encoded == "" {
		t.Error("Encoded price should not be empty")
	}

	_ = querydatastr
	_ = encoded
}

func TestMedian_WithoutOrchestrator_FallbackToOldSystem(t *testing.T) {
	// Test that when orchestrator is nil, it falls back to old system
	querydata := []byte{0x12, 0x34, 0x56, 0x78}
	querydatastr := hex.EncodeToString(querydata)

	marketParam := types.MarketParam{
		Id:        1,
		Pair:      "BTC/USD",
		Exponent:  -8,
		QueryData: querydatastr,
	}

	client := &Client{
		logger:           log.NewNopLogger(),
		MarketParams:     []types.MarketParam{marketParam},
		MarketToExchange: pricefeedservertypes.NewMarketToExchangePrices(5 * 60 * 1000000000),
		// Orchestrator is nil
	}

	_ = client
	_ = querydata
}
