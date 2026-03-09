package orchestrator

import (
	"math"
	"testing"
	"time"
)

func TestAggregateMedian_OddNumberOfResults(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Timestamp: time.Now()},
		{Price: 200.0, SourceID: "source2", Timestamp: time.Now()},
		{Price: 150.0, SourceID: "source3", Timestamp: time.Now()},
	}

	median, err := AggregateMedian(results)
	if err != nil {
		t.Fatalf("AggregateMedian() returned error: %v", err)
	}
	if median != 150.0 {
		t.Errorf("AggregateMedian() = %v, want %v", median, 150.0)
	}
}

func TestAggregateMedian_EvenNumberOfResults(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Timestamp: time.Now()},
		{Price: 200.0, SourceID: "source2", Timestamp: time.Now()},
		{Price: 150.0, SourceID: "source3", Timestamp: time.Now()},
		{Price: 250.0, SourceID: "source4", Timestamp: time.Now()},
	}

	median, err := AggregateMedian(results)
	if err != nil {
		t.Fatalf("AggregateMedian() returned error: %v", err)
	}
	// For even number, median is average of two middle values: (150 + 200) / 2 = 175
	expected := 175.0
	if median != expected {
		t.Errorf("AggregateMedian() = %v, want %v", median, expected)
	}
}

func TestAggregateMedian_UnsortedResults(t *testing.T) {
	// Test that median works correctly even when results are not sorted
	results := []PriceResult{
		{Price: 300.0, SourceID: "source1", Timestamp: time.Now()},
		{Price: 100.0, SourceID: "source2", Timestamp: time.Now()},
		{Price: 200.0, SourceID: "source3", Timestamp: time.Now()},
		{Price: 50.0, SourceID: "source4", Timestamp: time.Now()},
		{Price: 250.0, SourceID: "source5", Timestamp: time.Now()},
	}

	median, err := AggregateMedian(results)
	if err != nil {
		t.Fatalf("AggregateMedian() returned error: %v", err)
	}
	// Sorted: 50, 100, 200, 250, 300 -> median is 200
	if median != 200.0 {
		t.Errorf("AggregateMedian() = %v, want %v", median, 200.0)
	}
}

func TestAggregateMedian_SingleResult(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Timestamp: time.Now()},
	}

	median, err := AggregateMedian(results)
	if err != nil {
		t.Fatalf("AggregateMedian() returned error: %v", err)
	}
	if median != 100.0 {
		t.Errorf("AggregateMedian() = %v, want %v", median, 100.0)
	}
}

func TestAggregateMedian_EmptyResults(t *testing.T) {
	results := []PriceResult{}

	_, err := AggregateMedian(results)
	if err == nil {
		t.Error("AggregateMedian() expected error for empty results, got nil")
	}
}

func TestAggregateMedian_WithNaN(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Timestamp: time.Now()},
		{Price: math.NaN(), SourceID: "source2", Timestamp: time.Now()},
		{Price: 200.0, SourceID: "source3", Timestamp: time.Now()},
	}

	_, err := AggregateMedian(results)
	if err == nil {
		t.Error("AggregateMedian() expected error for NaN value, got nil")
	}
}

func TestAggregateMedian_WithInf(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Timestamp: time.Now()},
		{Price: math.Inf(1), SourceID: "source2", Timestamp: time.Now()},
		{Price: 200.0, SourceID: "source3", Timestamp: time.Now()},
	}

	_, err := AggregateMedian(results)
	if err == nil {
		t.Error("AggregateMedian() expected error for Inf value, got nil")
	}
}

func TestAggregateMean(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Timestamp: time.Now()},
		{Price: 200.0, SourceID: "source2", Timestamp: time.Now()},
		{Price: 300.0, SourceID: "source3", Timestamp: time.Now()},
	}

	mean, err := AggregateMean(results)
	if err != nil {
		t.Fatalf("AggregateMean() returned error: %v", err)
	}
	expected := 200.0 // (100 + 200 + 300) / 3
	if mean != expected {
		t.Errorf("AggregateMean() = %v, want %v", mean, expected)
	}
}

func TestAggregateMean_SingleResult(t *testing.T) {
	results := []PriceResult{
		{Price: 150.0, SourceID: "source1", Timestamp: time.Now()},
	}

	mean, err := AggregateMean(results)
	if err != nil {
		t.Fatalf("AggregateMean() returned error: %v", err)
	}
	if mean != 150.0 {
		t.Errorf("AggregateMean() = %v, want %v", mean, 150.0)
	}
}

func TestAggregateMean_EmptyResults(t *testing.T) {
	results := []PriceResult{}

	_, err := AggregateMean(results)
	if err == nil {
		t.Error("AggregateMean() expected error for empty results, got nil")
	}
}

func TestAggregateMean_WithNaN(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Timestamp: time.Now()},
		{Price: math.NaN(), SourceID: "source2", Timestamp: time.Now()},
	}

	_, err := AggregateMean(results)
	if err == nil {
		t.Error("AggregateMean() expected error for NaN value, got nil")
	}
}

func TestAggregateMean_WithInf(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Timestamp: time.Now()},
		{Price: math.Inf(1), SourceID: "source2", Timestamp: time.Now()},
	}

	_, err := AggregateMean(results)
	if err == nil {
		t.Error("AggregateMean() expected error for Inf value, got nil")
	}
}

func TestAggregateWeighted(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Weight: 0.3, Timestamp: time.Now()},
		{Price: 200.0, SourceID: "source2", Weight: 0.5, Timestamp: time.Now()},
		{Price: 300.0, SourceID: "source3", Weight: 0.2, Timestamp: time.Now()},
	}

	weighted, err := AggregateWeighted(results)
	if err != nil {
		t.Fatalf("AggregateWeighted() returned error: %v", err)
	}
	// (100 * 0.3) + (200 * 0.5) + (300 * 0.2) = 30 + 100 + 60 = 190
	expected := 190.0
	if weighted != expected {
		t.Errorf("AggregateWeighted() = %v, want %v", weighted, expected)
	}
}

func TestAggregateWeighted_SingleResult(t *testing.T) {
	results := []PriceResult{
		{Price: 150.0, SourceID: "source1", Weight: 1.0, Timestamp: time.Now()},
	}

	weighted, err := AggregateWeighted(results)
	if err != nil {
		t.Fatalf("AggregateWeighted() returned error: %v", err)
	}
	if weighted != 150.0 {
		t.Errorf("AggregateWeighted() = %v, want %v", weighted, 150.0)
	}
}

func TestAggregateWeighted_EmptyResults(t *testing.T) {
	results := []PriceResult{}

	_, err := AggregateWeighted(results)
	if err == nil {
		t.Error("AggregateWeighted() expected error for empty results, got nil")
	}
}

func TestAggregateWeighted_WithNaN(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Weight: 0.5, Timestamp: time.Now()},
		{Price: math.NaN(), SourceID: "source2", Weight: 0.5, Timestamp: time.Now()},
	}

	_, err := AggregateWeighted(results)
	if err == nil {
		t.Error("AggregateWeighted() expected error for NaN value, got nil")
	}
}

func TestAggregateWeighted_WithInf(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Weight: 0.5, Timestamp: time.Now()},
		{Price: math.Inf(1), SourceID: "source2", Weight: 0.5, Timestamp: time.Now()},
	}

	_, err := AggregateWeighted(results)
	if err == nil {
		t.Error("AggregateWeighted() expected error for Inf value, got nil")
	}
}

func TestAggregateWeighted_ZeroWeights(t *testing.T) {
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Weight: 0.0, Timestamp: time.Now()},
		{Price: 200.0, SourceID: "source2", Weight: 0.0, Timestamp: time.Now()},
	}

	_, err := AggregateWeighted(results)
	if err == nil {
		t.Error("AggregateWeighted() expected error for zero weights, got nil")
	}
}

func TestAggregateWeighted_MissingWeights(t *testing.T) {
	// Test that missing weights (default 0.0) are handled
	results := []PriceResult{
		{Price: 100.0, SourceID: "source1", Timestamp: time.Now()}, // Weight defaults to 0.0
		{Price: 200.0, SourceID: "source2", Weight: 1.0, Timestamp: time.Now()},
	}

	weighted, err := AggregateWeighted(results)
	if err != nil {
		t.Fatalf("AggregateWeighted() returned error: %v", err)
	}
	// Only source2 contributes: 200 * 1.0 = 200
	if weighted != 200.0 {
		t.Errorf("AggregateWeighted() = %v, want %v", weighted, 200.0)
	}
}
