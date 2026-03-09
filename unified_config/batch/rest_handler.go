package batch

import (
	"fmt"

	"github.com/tellor-io/layer-daemons/unified_config"
)

// RESTBatchHandler routes batch requests to the appropriate REST batching strategy.
// It implements the BatchHandler interface and delegates to either QueryParamBatcher
// or BodyBatcher based on the source configuration.
type RESTBatchHandler struct {
	strategy          string
	queryParamBatcher *QueryParamBatcher
	bodyBatcher       *BodyBatcher
}

// NewRESTBatchHandler creates a new RESTBatchHandler based on the source configuration.
// It creates the appropriate batcher (QueryParamBatcher or BodyBatcher) based on
// the BatchStrategy field in the source configuration.
func NewRESTBatchHandler(sourceConfig unified_config.SourceConfig) (*RESTBatchHandler, error) {
	// Validate source type
	if sourceConfig.Type != "rest" {
		return nil, fmt.Errorf("RESTBatchHandler can only be used with REST sources, got type %q", sourceConfig.Type)
	}

	// Validate batch strategy
	if sourceConfig.BatchStrategy != "query_param" && sourceConfig.BatchStrategy != "body" {
		return nil, fmt.Errorf("invalid batch strategy for REST handler: %q (must be 'query_param' or 'body')", sourceConfig.BatchStrategy)
	}

	handler := &RESTBatchHandler{
		strategy: sourceConfig.BatchStrategy,
	}

	// Create appropriate batcher based on strategy
	switch sourceConfig.BatchStrategy {
	case "query_param":
		// Default values: paramName="ids", separator=","
		// These can be made configurable in the future if needed
		paramName := "ids"
		separator := ","
		handler.queryParamBatcher = NewQueryParamBatcher(sourceConfig.BaseURL, paramName, separator)

	case "body":
		// Default endpoint: "/batch"
		// This can be made configurable in the future if needed
		endpoint := "/batch"
		handler.bodyBatcher = NewBodyBatcher(sourceConfig.BaseURL, endpoint)
	}

	return handler, nil
}

// BatchFetch fetches prices for multiple queryIDs using the configured batching strategy.
// It implements the BatchHandler interface and routes to the appropriate batcher.
func (h *RESTBatchHandler) BatchFetch(sourceID string, queryIDs []string) (map[string]float64, error) {
	switch h.strategy {
	case "query_param":
		if h.queryParamBatcher == nil {
			return nil, fmt.Errorf("query_param batcher not initialized")
		}
		return h.queryParamBatcher.BatchFetch(queryIDs)

	case "body":
		if h.bodyBatcher == nil {
			return nil, fmt.Errorf("body batcher not initialized")
		}
		return h.bodyBatcher.BatchFetch(queryIDs)

	default:
		return nil, fmt.Errorf("unsupported batch strategy: %q", h.strategy)
	}
}
