package unified_config

import (
	"fmt"
	"os"

	"github.com/pelletier/go-toml"
)

// Config represents the complete configuration for the unified pricefeed system.
// It contains sources, asset pairs, and global settings.
type Config struct {
	// Sources maps source IDs to their configurations
	Sources map[string]SourceConfig `toml:"sources" json:"sources"`

	// AssetPairs is a list of asset pair configurations
	AssetPairs []AssetPairConfig `toml:"asset_pairs" json:"asset_pairs"`

	// GlobalStalenessThresholdSeconds is the global staleness threshold in seconds
	GlobalStalenessThresholdSeconds int `toml:"global_staleness_threshold_seconds" json:"global_staleness_threshold_seconds"`
}

// LoadConfig loads and parses the configuration from TOML files.
// It reads sources.toml and asset_pairs.toml, parses them, and validates the configuration.
func LoadConfig(sourcesPath, assetPairsPath string) (*Config, error) {
	config := &Config{
		Sources: make(map[string]SourceConfig),
	}

	// Read and parse sources.toml
	sourcesData, err := os.ReadFile(sourcesPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read sources file %q: %w", sourcesPath, err)
	}

	// Try parsing as a wrapper struct first (in case sources are nested under a "sources" key)
	var sourcesWrapper struct {
		Sources map[string]SourceConfig `toml:"sources"`
	}
	if err := toml.Unmarshal(sourcesData, &sourcesWrapper); err == nil && len(sourcesWrapper.Sources) > 0 {
		// Successfully parsed with wrapper structure
		for id, sourceConfig := range sourcesWrapper.Sources {
			sourceConfig.ID = id
			config.Sources[id] = sourceConfig
		}
	} else {
		// Try parsing as a direct map (top-level keys are source IDs)
		var sourcesMap map[string]SourceConfig
		if err := toml.Unmarshal(sourcesData, &sourcesMap); err != nil {
			return nil, fmt.Errorf("failed to parse sources file %q: %w", sourcesPath, err)
		}

		// Set the ID field for each source config based on the map key
		for id, sourceConfig := range sourcesMap {
			sourceConfig.ID = id
			config.Sources[id] = sourceConfig
		}
	}

	// Read and parse asset_pairs.toml
	assetPairsData, err := os.ReadFile(assetPairsPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read asset pairs file %q: %w", assetPairsPath, err)
	}

	// Parse asset_pairs.toml - try wrapper struct first (in case asset_pairs are nested)
	var assetPairsWrapper struct {
		AssetPairs []AssetPairConfig `toml:"asset_pairs"`
	}
	if err := toml.Unmarshal(assetPairsData, &assetPairsWrapper); err == nil && len(assetPairsWrapper.AssetPairs) > 0 {
		// Successfully parsed with wrapper structure
		config.AssetPairs = assetPairsWrapper.AssetPairs
	} else {
		// Try parsing as a direct array of tables
		if err := toml.Unmarshal(assetPairsData, &config.AssetPairs); err != nil {
			return nil, fmt.Errorf("failed to parse asset pairs file %q: %w", assetPairsPath, err)
		}
	}

	// Validate the configuration
	if err := config.Validate(); err != nil {
		return nil, fmt.Errorf("configuration validation failed: %w", err)
	}

	return config, nil
}

// Validate checks that the Config is valid and returns an error if not.
// It validates:
// - All source configurations
// - All asset pair configurations
// - That all asset pair sources reference valid source IDs
func (c *Config) Validate() error {
	// Validate all sources
	for id, source := range c.Sources {
		if err := source.Validate(); err != nil {
			return fmt.Errorf("source %q: %w", id, err)
		}
		// Ensure the ID in the config matches the map key
		if source.ID != id {
			return fmt.Errorf("source %q: ID mismatch (config has %q)", id, source.ID)
		}
	}

	// Validate all asset pairs
	for i, pair := range c.AssetPairs {
		if err := pair.Validate(); err != nil {
			return fmt.Errorf("asset pair %d (%q): %w", i, pair.Pair, err)
		}

		// Check that all sources in the asset pair reference valid source IDs
		for j, source := range pair.Sources {
			if _, exists := c.Sources[source.SourceID]; !exists {
				return fmt.Errorf("asset pair %d (%q): source %d references unknown source ID %q", i, pair.Pair, j, source.SourceID)
			}
		}
	}

	return nil
}
