package orchestrator

import (
	"errors"
	"math"
	"sort"
	"time"
)

// PriceResult represents a price result from a single source.
type PriceResult struct {
	// Price is the price value from the source
	Price float64

	// SourceID is the identifier of the source that provided this price
	SourceID string

	// Timestamp is when this price was fetched
	Timestamp time.Time

	// Weight is the weight for weighted aggregation (optional, only used for weighted method)
	Weight float64
}

// AggregateMedian calculates the median of the given price results.
// For an even number of results, it returns the average of the two middle values.
// Returns an error if results are empty or contain invalid prices (NaN, Inf).
func AggregateMedian(results []PriceResult) (float64, error) {
	if len(results) == 0 {
		return 0, errors.New("cannot calculate median of empty results")
	}

	// Extract prices and validate
	prices := make([]float64, 0, len(results))
	for _, result := range results {
		if math.IsNaN(result.Price) {
			return 0, errors.New("cannot calculate median: price contains NaN")
		}
		if math.IsInf(result.Price, 0) {
			return 0, errors.New("cannot calculate median: price contains Inf")
		}
		prices = append(prices, result.Price)
	}

	// Sort prices
	sort.Float64s(prices)

	// Calculate median
	n := len(prices)
	if n == 1 {
		return prices[0], nil
	}

	midIdx := n / 2
	if n%2 == 1 {
		// Odd number of elements: return middle value
		return prices[midIdx], nil
	}

	// Even number of elements: return average of two middle values
	return (prices[midIdx-1] + prices[midIdx]) / 2.0, nil
}

// AggregateMean calculates the arithmetic mean (average) of the given price results.
// Returns an error if results are empty or contain invalid prices (NaN, Inf).
func AggregateMean(results []PriceResult) (float64, error) {
	if len(results) == 0 {
		return 0, errors.New("cannot calculate mean of empty results")
	}

	var sum float64
	for _, result := range results {
		if math.IsNaN(result.Price) {
			return 0, errors.New("cannot calculate mean: price contains NaN")
		}
		if math.IsInf(result.Price, 0) {
			return 0, errors.New("cannot calculate mean: price contains Inf")
		}
		sum += result.Price
	}

	return sum / float64(len(results)), nil
}

// AggregateWeighted calculates the weighted average of the given price results.
// Each result's price is multiplied by its weight, then divided by the sum of weights.
// Returns an error if results are empty, contain invalid prices (NaN, Inf), or all weights are zero.
func AggregateWeighted(results []PriceResult) (float64, error) {
	if len(results) == 0 {
		return 0, errors.New("cannot calculate weighted average of empty results")
	}

	var weightedSum float64
	var totalWeight float64

	for _, result := range results {
		if math.IsNaN(result.Price) {
			return 0, errors.New("cannot calculate weighted average: price contains NaN")
		}
		if math.IsInf(result.Price, 0) {
			return 0, errors.New("cannot calculate weighted average: price contains Inf")
		}
		weightedSum += result.Price * result.Weight
		totalWeight += result.Weight
	}

	if totalWeight == 0 {
		return 0, errors.New("cannot calculate weighted average: sum of weights is zero")
	}

	return weightedSum / totalWeight, nil
}
