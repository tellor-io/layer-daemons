package batch

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestBodyBatcher_NewBodyBatcher(t *testing.T) {
	batcher := NewBodyBatcher("https://api.example.com", "/batch")
	if batcher == nil {
		t.Fatal("NewBodyBatcher returned nil")
	}
	if batcher.baseURL != "https://api.example.com" {
		t.Errorf("expected baseURL %q, got %q", "https://api.example.com", batcher.baseURL)
	}
	if batcher.endpoint != "/batch" {
		t.Errorf("expected endpoint %q, got %q", "/batch", batcher.endpoint)
	}
}

func TestBodyBatcher_BatchFetch_SingleQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a POST request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}

		// Verify endpoint
		if r.URL.Path != "/batch" {
			t.Errorf("expected path /batch, got %s", r.URL.Path)
		}

		// Read and verify request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		var requestData map[string]interface{}
		if err := json.Unmarshal(body, &requestData); err != nil {
			t.Fatalf("failed to parse request body: %v", err)
		}

		// Verify request contains query IDs
		ids, exists := requestData["ids"]
		if !exists {
			t.Error("expected 'ids' field in request body")
		}

		idsArray, ok := ids.([]interface{})
		if !ok || len(idsArray) != 1 {
			t.Errorf("expected ids array with 1 element, got %v", ids)
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

	batcher := NewBodyBatcher(server.URL, "/batch")
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

func TestBodyBatcher_BatchFetch_MultipleQueries(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		var requestData map[string]interface{}
		if err := json.Unmarshal(body, &requestData); err != nil {
			t.Fatalf("failed to parse request body: %v", err)
		}

		// Verify all query IDs are in request
		ids, exists := requestData["ids"]
		if !exists {
			t.Error("expected 'ids' field in request body")
		}

		idsArray, ok := ids.([]interface{})
		if !ok || len(idsArray) != 3 {
			t.Errorf("expected ids array with 3 elements, got %v", ids)
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

	batcher := NewBodyBatcher(server.URL, "/batch")
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

func TestBodyBatcher_BatchFetch_EmptyQueries(t *testing.T) {
	batcher := NewBodyBatcher("https://api.example.com", "/batch")
	results, err := batcher.BatchFetch([]string{})
	if err != nil {
		t.Fatalf("BatchFetch should handle empty queries, got error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty queries, got %d", len(results))
	}
}

func TestBodyBatcher_BatchFetch_HTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	batcher := NewBodyBatcher(server.URL, "/batch")
	_, err := batcher.BatchFetch([]string{"query1"})
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestBodyBatcher_BatchFetch_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("invalid json"))
	}))
	defer server.Close()

	batcher := NewBodyBatcher(server.URL, "/batch")
	_, err := batcher.BatchFetch([]string{"query1"})
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

func TestBodyBatcher_BatchFetch_MissingPrice(t *testing.T) {
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

	batcher := NewBodyBatcher(server.URL, "/batch")
	results, err := batcher.BatchFetch([]string{"query1"})
	if err != nil {
		t.Fatalf("BatchFetch should handle missing prices gracefully, got error: %v", err)
	}

	// Missing price should not be in results
	if _, exists := results["query1"]; exists {
		t.Error("expected missing price to not be in results")
	}
}

func TestBodyBatcher_BatchFetch_CustomRequestBodyFormat(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("failed to read request body: %v", err)
		}

		// Verify body contains query IDs (could be in different formats)
		bodyStr := string(body)
		if !strings.Contains(bodyStr, "query1") || !strings.Contains(bodyStr, "query2") {
			t.Errorf("expected request body to contain query IDs, got: %s", bodyStr)
		}

		response := map[string]interface{}{
			"query1": map[string]interface{}{"price": 100.5},
			"query2": map[string]interface{}{"price": 200.5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	batcher := NewBodyBatcher(server.URL, "/batch")
	results, err := batcher.BatchFetch([]string{"query1", "query2"})
	if err != nil {
		t.Fatalf("BatchFetch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestBodyBatcher_BatchFetch_DifferentEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify endpoint
		if r.URL.Path != "/api/v1/prices" {
			t.Errorf("expected path /api/v1/prices, got %s", r.URL.Path)
		}

		response := map[string]interface{}{
			"query1": map[string]interface{}{"price": 100.5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	batcher := NewBodyBatcher(server.URL, "/api/v1/prices")
	results, err := batcher.BatchFetch([]string{"query1"})
	if err != nil {
		t.Fatalf("BatchFetch failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}
