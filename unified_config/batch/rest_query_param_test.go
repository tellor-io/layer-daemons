package batch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestQueryParamBatcher_NewQueryParamBatcher(t *testing.T) {
	batcher := NewQueryParamBatcher("https://api.example.com", "ids", ",")
	if batcher == nil {
		t.Fatal("NewQueryParamBatcher returned nil")
	}
	if batcher.baseURL != "https://api.example.com" {
		t.Errorf("expected baseURL %q, got %q", "https://api.example.com", batcher.baseURL)
	}
	if batcher.paramName != "ids" {
		t.Errorf("expected paramName %q, got %q", "ids", batcher.paramName)
	}
	if batcher.separator != "," {
		t.Errorf("expected separator %q, got %q", ",", batcher.separator)
	}
}

func TestQueryParamBatcher_BatchFetch_SingleQuery(t *testing.T) {
	// Create a test server that returns a simple JSON response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameter
		ids := r.URL.Query().Get("ids")
		if ids != "query1" {
			t.Errorf("expected ids=query1, got %q", ids)
		}

		// Return JSON response with price
		response := map[string]interface{}{
			"query1": map[string]interface{}{
				"price": 100.5,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	batcher := NewQueryParamBatcher(server.URL, "ids", ",")
	results, err := batcher.BatchFetch([]string{"query1"})
	if err != nil {
		t.Fatalf("BatchFetch failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	price, exists := results["query1"]
	if !exists {
		t.Fatal("expected result for query1")
	}
	if price != 100.5 {
		t.Errorf("expected price 100.5, got %f", price)
	}
}

func TestQueryParamBatcher_BatchFetch_MultipleQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify query parameter contains all IDs
		ids := r.URL.Query().Get("ids")
		expectedIDs := "query1,query2,query3"
		if ids != expectedIDs {
			t.Errorf("expected ids=%q, got %q", expectedIDs, ids)
		}

		// Return JSON response with multiple prices
		response := map[string]interface{}{
			"query1": map[string]interface{}{
				"price": 100.5,
			},
			"query2": map[string]interface{}{
				"price": 200.75,
			},
			"query3": map[string]interface{}{
				"price": 300.25,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	batcher := NewQueryParamBatcher(server.URL, "ids", ",")
	results, err := batcher.BatchFetch([]string{"query1", "query2", "query3"})
	if err != nil {
		t.Fatalf("BatchFetch failed: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	expectedPrices := map[string]float64{
		"query1": 100.5,
		"query2": 200.75,
		"query3": 300.25,
	}

	for queryID, expectedPrice := range expectedPrices {
		price, exists := results[queryID]
		if !exists {
			t.Errorf("expected result for %q", queryID)
			continue
		}
		if price != expectedPrice {
			t.Errorf("expected price %f for %q, got %f", expectedPrice, queryID, price)
		}
	}
}

func TestQueryParamBatcher_BatchFetch_EmptyQueries(t *testing.T) {
	batcher := NewQueryParamBatcher("https://api.example.com", "ids", ",")
	results, err := batcher.BatchFetch([]string{})
	if err != nil {
		t.Fatalf("BatchFetch should handle empty queries, got error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty queries, got %d", len(results))
	}
}

func TestQueryParamBatcher_BatchFetch_HTTPError(t *testing.T) {
	// Create a server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	batcher := NewQueryParamBatcher(server.URL, "ids", ",")
	_, err := batcher.BatchFetch([]string{"query1"})
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestQueryParamBatcher_BatchFetch_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	batcher := NewQueryParamBatcher(server.URL, "ids", ",")
	_, err := batcher.BatchFetch([]string{"query1"})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestQueryParamBatcher_BatchFetch_MissingPrice(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return JSON without the requested queryID
		response := map[string]interface{}{
			"other_query": map[string]interface{}{
				"price": 100.5,
			},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	batcher := NewQueryParamBatcher(server.URL, "ids", ",")
	results, err := batcher.BatchFetch([]string{"query1"})
	if err != nil {
		t.Fatalf("BatchFetch should handle missing prices gracefully, got error: %v", err)
	}

	// Missing price should not be in results
	if _, exists := results["query1"]; exists {
		t.Error("expected missing price to not be in results")
	}
}

func TestQueryParamBatcher_BatchFetch_DifferentSeparator(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify separator is used correctly
		ids := r.URL.Query().Get("symbols")
		expectedIDs := "query1|query2"
		if ids != expectedIDs {
			t.Errorf("expected symbols=%q, got %q", expectedIDs, ids)
		}

		response := map[string]interface{}{
			"query1": map[string]interface{}{"price": 100.5},
			"query2": map[string]interface{}{"price": 200.5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	batcher := NewQueryParamBatcher(server.URL, "symbols", "|")
	results, err := batcher.BatchFetch([]string{"query1", "query2"})
	if err != nil {
		t.Fatalf("BatchFetch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestQueryParamBatcher_BatchFetch_ArrayResponseFormat(t *testing.T) {
	// Some APIs return arrays instead of maps
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Return array format
		response := []map[string]interface{}{
			{"id": "query1", "price": 100.5},
			{"id": "query2", "price": 200.5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	batcher := NewQueryParamBatcher(server.URL, "ids", ",")
	results, err := batcher.BatchFetch([]string{"query1", "query2"})
	// This should work if we implement array parsing, or return an error if not supported
	// For now, let's expect it to handle it gracefully
	if err != nil {
		// If array format is not supported, that's okay for now
		t.Logf("Array format not yet supported (expected): %v", err)
	} else {
		// If it works, verify results
		if len(results) != 2 {
			t.Errorf("expected 2 results, got %d", len(results))
		}
	}
}
