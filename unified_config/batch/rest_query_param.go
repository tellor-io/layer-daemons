package batch

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// QueryParamBatcher batches multiple queries in URL query parameters.
// It makes HTTP GET requests with query IDs in a query parameter.
type QueryParamBatcher struct {
	baseURL   string
	paramName string
	separator string
	client    *http.Client
}

// NewQueryParamBatcher creates a new QueryParamBatcher.
// baseURL is the base URL for the API endpoint.
// paramName is the query parameter name (e.g., "ids", "symbols").
// separator is the separator for values (e.g., ",", "|").
func NewQueryParamBatcher(baseURL, paramName, separator string) *QueryParamBatcher {
	return &QueryParamBatcher{
		baseURL:   baseURL,
		paramName: paramName,
		separator: separator,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// BatchFetch fetches prices for multiple queryIDs by batching them in a query parameter.
// It builds a URL with all query IDs in the query parameter, makes an HTTP GET request,
// parses the JSON response, and extracts individual prices by query ID.
// Returns a map of queryID -> price, or an error if the batch fetch fails.
func (b *QueryParamBatcher) BatchFetch(queryIDs []string) (map[string]float64, error) {
	if len(queryIDs) == 0 {
		return make(map[string]float64), nil
	}

	// Build URL with query parameters
	u, err := url.Parse(b.baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}

	// Join query IDs with separator
	paramValue := strings.Join(queryIDs, b.separator)
	q := u.Query()
	q.Set(b.paramName, paramValue)
	u.RawQuery = q.Encode()

	// Make HTTP GET request
	resp, err := b.client.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	// Check status code
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP request failed with status %d", resp.StatusCode)
	}

	// Read response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	// Parse JSON response
	var jsonData interface{}
	if err := json.Unmarshal(body, &jsonData); err != nil {
		return nil, fmt.Errorf("failed to parse JSON response: %w", err)
	}

	// Extract prices by query ID
	results := make(map[string]float64)
	if err := b.extractPrices(jsonData, queryIDs, results); err != nil {
		return nil, fmt.Errorf("failed to extract prices: %w", err)
	}

	return results, nil
}

// extractPrices extracts prices from the JSON response.
// It handles map-based responses where queryID is the key.
// The value can be a map with a "price" field, or a direct number.
func (b *QueryParamBatcher) extractPrices(data interface{}, queryIDs []string, results map[string]float64) error {
	// Handle map-based responses
	if dataMap, ok := data.(map[string]interface{}); ok {
		for _, queryID := range queryIDs {
			value, exists := dataMap[queryID]
			if !exists {
				// Missing price is not an error - just skip it
				continue
			}

			price, err := b.extractPriceFromValue(value)
			if err != nil {
				// Log error but continue with other queries
				continue
			}
			results[queryID] = price
		}
		return nil
	}

	// Handle array-based responses (for future support)
	if dataArray, ok := data.([]interface{}); ok {
		// Create a map of queryID -> price from array
		queryIDMap := make(map[string]bool)
		for _, qid := range queryIDs {
			queryIDMap[qid] = true
		}

		for _, item := range dataArray {
			if itemMap, ok := item.(map[string]interface{}); ok {
				// Try to find "id" or "queryID" field
				var id string
				if idVal, exists := itemMap["id"]; exists {
					if idStr, ok := idVal.(string); ok {
						id = idStr
					}
				} else if idVal, exists := itemMap["queryID"]; exists {
					if idStr, ok := idVal.(string); ok {
						id = idStr
					}
				}

				if id != "" && queryIDMap[id] {
					price, err := b.extractPriceFromValue(itemMap)
					if err == nil {
						results[id] = price
					}
				}
			}
		}
		return nil
	}

	return fmt.Errorf("unsupported response format: expected map or array, got %T", data)
}

// extractPriceFromValue extracts a price from a value.
// It handles:
// - Direct number values
// - Maps with "price" field
// - Maps with other common price field names
func (b *QueryParamBatcher) extractPriceFromValue(value interface{}) (float64, error) {
	// Direct number
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	}

	// Map with price field
	if valueMap, ok := value.(map[string]interface{}); ok {
		// Try common price field names
		priceFields := []string{"price", "Price", "PRICE", "value", "Value", "last", "Last"}
		for _, field := range priceFields {
			if priceVal, exists := valueMap[field]; exists {
				switch p := priceVal.(type) {
				case float64:
					return p, nil
				case float32:
					return float64(p), nil
				case int:
					return float64(p), nil
				case int64:
					return float64(p), nil
				case string:
					// Try to parse string as float
					var f float64
					if _, err := fmt.Sscanf(p, "%f", &f); err == nil {
						return f, nil
					}
				}
			}
		}
	}

	return 0, fmt.Errorf("could not extract price from value of type %T", value)
}
