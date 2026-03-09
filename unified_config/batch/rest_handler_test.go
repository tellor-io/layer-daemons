package batch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/tellor-io/layer-daemons/unified_config"
)

func TestRESTBatchHandler_NewRESTBatchHandler_QueryParamStrategy(t *testing.T) {
	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "rest",
		Batchable:     true,
		BatchStrategy: "query_param",
		BaseURL:       "https://api.example.com",
	}

	handler, err := NewRESTBatchHandler(sourceConfig)
	if err != nil {
		t.Fatalf("NewRESTBatchHandler failed: %v", err)
	}

	if handler == nil {
		t.Fatal("NewRESTBatchHandler returned nil")
	}

	if handler.strategy != "query_param" {
		t.Errorf("expected strategy query_param, got %q", handler.strategy)
	}

	if handler.queryParamBatcher == nil {
		t.Error("expected queryParamBatcher to be initialized")
	}

	if handler.bodyBatcher != nil {
		t.Error("expected bodyBatcher to be nil for query_param strategy")
	}
}

func TestRESTBatchHandler_NewRESTBatchHandler_BodyStrategy(t *testing.T) {
	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "rest",
		Batchable:     true,
		BatchStrategy: "body",
		BaseURL:       "https://api.example.com",
	}

	handler, err := NewRESTBatchHandler(sourceConfig)
	if err != nil {
		t.Fatalf("NewRESTBatchHandler failed: %v", err)
	}

	if handler == nil {
		t.Fatal("NewRESTBatchHandler returned nil")
	}

	if handler.strategy != "body" {
		t.Errorf("expected strategy body, got %q", handler.strategy)
	}

	if handler.bodyBatcher == nil {
		t.Error("expected bodyBatcher to be initialized")
	}

	if handler.queryParamBatcher != nil {
		t.Error("expected queryParamBatcher to be nil for body strategy")
	}
}

func TestRESTBatchHandler_NewRESTBatchHandler_InvalidStrategy(t *testing.T) {
	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "rest",
		Batchable:     true,
		BatchStrategy: "invalid_strategy",
		BaseURL:       "https://api.example.com",
	}

	_, err := NewRESTBatchHandler(sourceConfig)
	if err == nil {
		t.Fatal("expected error for invalid strategy, got nil")
	}
}

func TestRESTBatchHandler_NewRESTBatchHandler_NonRESTSource(t *testing.T) {
	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "contract",
		Batchable:     true,
		BatchStrategy: "query_param",
		BaseURL:       "https://api.example.com",
	}

	_, err := NewRESTBatchHandler(sourceConfig)
	if err == nil {
		t.Fatal("expected error for non-REST source, got nil")
	}
}

func TestRESTBatchHandler_BatchFetch_QueryParamStrategy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a GET request with query parameter
		if r.Method != http.MethodGet {
			t.Errorf("expected GET request, got %s", r.Method)
		}

		ids := r.URL.Query().Get("ids")
		if ids != "query1,query2" {
			t.Errorf("expected ids=query1,query2, got %q", ids)
		}

		response := map[string]interface{}{
			"query1": map[string]interface{}{"price": 100.5},
			"query2": map[string]interface{}{"price": 200.5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "rest",
		Batchable:     true,
		BatchStrategy: "query_param",
		BaseURL:       server.URL,
	}

	handler, err := NewRESTBatchHandler(sourceConfig)
	if err != nil {
		t.Fatalf("NewRESTBatchHandler failed: %v", err)
	}

	results, err := handler.BatchFetch("test_source", []string{"query1", "query2"})
	if err != nil {
		t.Fatalf("BatchFetch failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results["query1"] != 100.5 {
		t.Errorf("expected price 100.5 for query1, got %f", results["query1"])
	}
	if results["query2"] != 200.5 {
		t.Errorf("expected price 200.5 for query2, got %f", results["query2"])
	}
}

func TestRESTBatchHandler_BatchFetch_BodyStrategy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify it's a POST request
		if r.Method != http.MethodPost {
			t.Errorf("expected POST request, got %s", r.Method)
		}

		response := map[string]interface{}{
			"query1": map[string]interface{}{"price": 100.5},
			"query2": map[string]interface{}{"price": 200.5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "rest",
		Batchable:     true,
		BatchStrategy: "body",
		BaseURL:       server.URL,
	}

	handler, err := NewRESTBatchHandler(sourceConfig)
	if err != nil {
		t.Fatalf("NewRESTBatchHandler failed: %v", err)
	}

	results, err := handler.BatchFetch("test_source", []string{"query1", "query2"})
	if err != nil {
		t.Fatalf("BatchFetch failed: %v", err)
	}

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	if results["query1"] != 100.5 {
		t.Errorf("expected price 100.5 for query1, got %f", results["query1"])
	}
	if results["query2"] != 200.5 {
		t.Errorf("expected price 200.5 for query2, got %f", results["query2"])
	}
}

func TestRESTBatchHandler_BatchFetch_ErrorHandling(t *testing.T) {
	// Create a server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "rest",
		Batchable:     true,
		BatchStrategy: "query_param",
		BaseURL:       server.URL,
	}

	handler, err := NewRESTBatchHandler(sourceConfig)
	if err != nil {
		t.Fatalf("NewRESTBatchHandler failed: %v", err)
	}

	_, err = handler.BatchFetch("test_source", []string{"query1"})
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
}

func TestRESTBatchHandler_BatchFetch_EmptyQueries(t *testing.T) {
	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "rest",
		Batchable:     true,
		BatchStrategy: "query_param",
		BaseURL:       "https://api.example.com",
	}

	handler, err := NewRESTBatchHandler(sourceConfig)
	if err != nil {
		t.Fatalf("NewRESTBatchHandler failed: %v", err)
	}

	results, err := handler.BatchFetch("test_source", []string{})
	if err != nil {
		t.Fatalf("BatchFetch should handle empty queries, got error: %v", err)
	}

	if len(results) != 0 {
		t.Errorf("expected 0 results for empty queries, got %d", len(results))
	}
}

func TestRESTBatchHandler_BatchFetch_DefaultQueryParamSettings(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify default param name "ids" and separator ","
		ids := r.URL.Query().Get("ids")
		if ids != "query1,query2" {
			t.Errorf("expected ids=query1,query2, got %q", ids)
		}

		response := map[string]interface{}{
			"query1": map[string]interface{}{"price": 100.5},
			"query2": map[string]interface{}{"price": 200.5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "rest",
		Batchable:     true,
		BatchStrategy: "query_param",
		BaseURL:       server.URL,
	}

	handler, err := NewRESTBatchHandler(sourceConfig)
	if err != nil {
		t.Fatalf("NewRESTBatchHandler failed: %v", err)
	}

	results, err := handler.BatchFetch("test_source", []string{"query1", "query2"})
	if err != nil {
		t.Fatalf("BatchFetch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("expected 2 results, got %d", len(results))
	}
}

func TestRESTBatchHandler_BatchFetch_DefaultBodyEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify default endpoint "/batch"
		if r.URL.Path != "/batch" {
			t.Errorf("expected path /batch, got %s", r.URL.Path)
		}

		response := map[string]interface{}{
			"query1": map[string]interface{}{"price": 100.5},
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(response)
	}))
	defer server.Close()

	sourceConfig := unified_config.SourceConfig{
		ID:            "test_source",
		Type:          "rest",
		Batchable:     true,
		BatchStrategy: "body",
		BaseURL:       server.URL,
	}

	handler, err := NewRESTBatchHandler(sourceConfig)
	if err != nil {
		t.Fatalf("NewRESTBatchHandler failed: %v", err)
	}

	results, err := handler.BatchFetch("test_source", []string{"query1"})
	if err != nil {
		t.Fatalf("BatchFetch failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("expected 1 result, got %d", len(results))
	}
}
