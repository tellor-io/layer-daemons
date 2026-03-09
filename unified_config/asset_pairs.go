package unified_config

import (
	"fmt"
	"math"
)

// AssetPairConfig represents the configuration for an asset pair (e.g., BTC/USD).
// It defines which sources to use and how to aggregate their prices.
type AssetPairConfig struct {
	// ID is a unique identifier for this asset pair
	ID uint32 `toml:"id" json:"id"`

	// Pair is the human-readable name of the market pair (e.g., "BTC/USD")
	Pair string `toml:"pair" json:"pair"`

	// QueryData is the Layer representation of the market pair (query identifier)
	QueryData string `toml:"query_data" json:"query_data"`

	// Exponent is the price exponent
	Exponent int32 `toml:"exponent" json:"exponent"`

	// MinSources is the minimum number of sources required for a valid price update
	MinSources int `toml:"min_sources" json:"min_sources"`

	// Sources is the list of sources to use for this pair
	Sources []AssetPairSource `toml:"sources" json:"sources"`

	// AggregationMethod specifies how to aggregate prices: "median", "mean", or "weighted"
	AggregationMethod string `toml:"aggregation_method" json:"aggregation_method"`
}

// AssetPairSource represents a single source configuration for an asset pair.
// No individual source is required - only MinSources from AssetPairConfig matters.
// As long as MinSources number of sources respond successfully, a valid price can be computed.
type AssetPairSource struct {
	// SourceID references a source ID from sources.toml
	SourceID string `toml:"source_id" json:"source_id"`

	// Weight is the weight for weighted aggregation (optional, only used for weighted method)
	Weight float64 `toml:"weight,omitempty" json:"weight,omitempty"`
}

// Validate checks that the AssetPairConfig is valid and returns an error if not.
func (c *AssetPairConfig) Validate() error {
	if c.Pair == "" {
		return fmt.Errorf("Pair is required")
	}

	if c.QueryData == "" {
		return fmt.Errorf("QueryData is required")
	}

	if len(c.Sources) == 0 {
		return fmt.Errorf("at least one source is required")
	}

	validMethods := map[string]bool{
		"median":   true,
		"mean":     true,
		"weighted": true,
	}
	if !validMethods[c.AggregationMethod] {
		return fmt.Errorf("AggregationMethod must be one of: median, mean, weighted")
	}

	if c.MinSources < 1 {
		return fmt.Errorf("MinSources must be at least 1")
	}

	if c.MinSources > len(c.Sources) {
		return fmt.Errorf("MinSources cannot be greater than number of sources")
	}

	// Validate all sources
	totalWeight := 0.0
	for i, source := range c.Sources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("source %d: %w", i, err)
		}
		totalWeight += source.Weight
	}

	// Validate weights for weighted aggregation
	if c.AggregationMethod == "weighted" {
		hasWeights := false
		for _, source := range c.Sources {
			if source.Weight > 0 {
				hasWeights = true
				break
			}
		}
		if !hasWeights {
			return fmt.Errorf("all sources must have weights for weighted aggregation")
		}

		// Check if weights sum to approximately 1.0 (allow small floating point errors)
		if math.Abs(totalWeight-1.0) > 0.01 {
			return fmt.Errorf("weights must sum to approximately 1.0, got %f", totalWeight)
		}
	}

	return nil
}

// Validate checks that the AssetPairSource is valid and returns an error if not.
func (s *AssetPairSource) Validate() error {
	if s.SourceID == "" {
		return fmt.Errorf("SourceID is required for all sources")
	}
	return nil
}
