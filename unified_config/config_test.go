package unified_config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadConfig_ValidConfig(t *testing.T) {
	// Create temporary directory for test files
	tmpDir := t.TempDir()

	// Create valid sources.toml
	sourcesTOML := `
[binance]
type = "rest"
batchable = true
batch_strategy = "query_param"
base_url = "https://api.binance.com"

[coinbase]
type = "rest"
batchable = true
batch_strategy = "body"
base_url = "https://api.coinbase.com"

[ethereum_contract]
type = "contract"
batchable = true
batch_strategy = "multicall3"
chain_id = 1
contract_address = "0x1234567890123456789012345678901234567890"
`
	sourcesPath := filepath.Join(tmpDir, "sources.toml")
	if err := os.WriteFile(sourcesPath, []byte(sourcesTOML), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	// Create valid asset_pairs.toml
	assetPairsTOML := `
[[asset_pairs]]
id = 1
pair = "BTC/USD"
query_data = "0x1234567890abcdef"
exponent = 8
min_sources = 2
aggregation_method = "median"
sources = [
	{ source_id = "binance" },
	{ source_id = "coinbase" }
]

[[asset_pairs]]
id = 2
pair = "ETH/USD"
query_data = "0xabcdef1234567890"
exponent = 8
min_sources = 2
aggregation_method = "mean"
sources = [
	{ source_id = "binance" },
	{ source_id = "coinbase" }
]
`
	assetPairsPath := filepath.Join(tmpDir, "asset_pairs.toml")
	if err := os.WriteFile(assetPairsPath, []byte(assetPairsTOML), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	// Load config
	config, err := LoadConfig(sourcesPath, assetPairsPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	// Verify sources
	if len(config.Sources) != 3 {
		t.Errorf("Expected 3 sources, got %d", len(config.Sources))
	}

	if source, exists := config.Sources["binance"]; !exists {
		t.Error("Source 'binance' not found")
	} else {
		if source.ID != "binance" {
			t.Errorf("Source ID mismatch: got %q, want 'binance'", source.ID)
		}
		if source.Type != "rest" {
			t.Errorf("Source type mismatch: got %q, want 'rest'", source.Type)
		}
		if source.BaseURL != "https://api.binance.com" {
			t.Errorf("BaseURL mismatch: got %q", source.BaseURL)
		}
	}

	// Verify asset pairs
	if len(config.AssetPairs) != 2 {
		t.Errorf("Expected 2 asset pairs, got %d", len(config.AssetPairs))
	}

	if config.AssetPairs[0].Pair != "BTC/USD" {
		t.Errorf("First pair mismatch: got %q, want 'BTC/USD'", config.AssetPairs[0].Pair)
	}
	if config.AssetPairs[1].Pair != "ETH/USD" {
		t.Errorf("Second pair mismatch: got %q, want 'ETH/USD'", config.AssetPairs[1].Pair)
	}
}

func TestLoadConfig_ValidConfigDirectMap(t *testing.T) {
	// Test with direct map structure (no wrapper)
	tmpDir := t.TempDir()

	// Create sources.toml with direct map structure
	sourcesTOML := `
[binance]
type = "rest"
base_url = "https://api.binance.com"
`
	sourcesPath := filepath.Join(tmpDir, "sources.toml")
	if err := os.WriteFile(sourcesPath, []byte(sourcesTOML), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	// Create asset_pairs.toml with direct array structure
	assetPairsTOML := `
[[asset_pairs]]
id = 1
pair = "BTC/USD"
query_data = "0x123"
exponent = 8
min_sources = 1
aggregation_method = "median"
sources = [
	{ source_id = "binance" }
]
`
	assetPairsPath := filepath.Join(tmpDir, "asset_pairs.toml")
	if err := os.WriteFile(assetPairsPath, []byte(assetPairsTOML), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	config, err := LoadConfig(sourcesPath, assetPairsPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	if len(config.Sources) != 1 {
		t.Errorf("Expected 1 source, got %d", len(config.Sources))
	}
	if len(config.AssetPairs) != 1 {
		t.Errorf("Expected 1 asset pair, got %d", len(config.AssetPairs))
	}
}

func TestLoadConfig_InvalidSourceConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create sources.toml with invalid source (missing required field)
	sourcesTOML := `
[binance]
type = "rest"
# Missing base_url
`
	sourcesPath := filepath.Join(tmpDir, "sources.toml")
	if err := os.WriteFile(sourcesPath, []byte(sourcesTOML), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	// Create valid asset_pairs.toml
	assetPairsTOML := `
[[asset_pairs]]
id = 1
pair = "BTC/USD"
query_data = "0x123"
exponent = 8
min_sources = 1
aggregation_method = "median"
sources = [
	{ source_id = "binance" }
]
`
	assetPairsPath := filepath.Join(tmpDir, "asset_pairs.toml")
	if err := os.WriteFile(assetPairsPath, []byte(assetPairsTOML), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	_, err := LoadConfig(sourcesPath, assetPairsPath)
	if err == nil {
		t.Error("LoadConfig() expected error for invalid source config, got nil")
	}
	if err != nil && err.Error() == "" {
		t.Error("LoadConfig() expected descriptive error message")
	}
}

func TestLoadConfig_InvalidAssetPairConfig(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid sources.toml
	sourcesTOML := `
[binance]
type = "rest"
base_url = "https://api.binance.com"
`
	sourcesPath := filepath.Join(tmpDir, "sources.toml")
	if err := os.WriteFile(sourcesPath, []byte(sourcesTOML), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	// Create asset_pairs.toml with invalid pair (missing required field)
	assetPairsTOML := `
[[asset_pairs]]
id = 1
# Missing pair field
query_data = "0x123"
exponent = 8
min_sources = 1
aggregation_method = "median"
sources = [
	{ source_id = "binance" }
]
`
	assetPairsPath := filepath.Join(tmpDir, "asset_pairs.toml")
	if err := os.WriteFile(assetPairsPath, []byte(assetPairsTOML), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	_, err := LoadConfig(sourcesPath, assetPairsPath)
	if err == nil {
		t.Error("LoadConfig() expected error for invalid asset pair config, got nil")
	}
	if err != nil && err.Error() == "" {
		t.Error("LoadConfig() expected descriptive error message")
	}
}

func TestLoadConfig_InvalidSourceReference(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid sources.toml
	sourcesTOML := `
[binance]
type = "rest"
base_url = "https://api.binance.com"
`
	sourcesPath := filepath.Join(tmpDir, "sources.toml")
	if err := os.WriteFile(sourcesPath, []byte(sourcesTOML), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	// Create asset_pairs.toml with reference to non-existent source
	assetPairsTOML := `
[[asset_pairs]]
id = 1
pair = "BTC/USD"
query_data = "0x123"
exponent = 8
min_sources = 1
aggregation_method = "median"
sources = [
	{ source_id = "nonexistent_source" }
]
`
	assetPairsPath := filepath.Join(tmpDir, "asset_pairs.toml")
	if err := os.WriteFile(assetPairsPath, []byte(assetPairsTOML), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	_, err := LoadConfig(sourcesPath, assetPairsPath)
	if err == nil {
		t.Error("LoadConfig() expected error for invalid source reference, got nil")
	}
	if err != nil {
		// Check that error message mentions the invalid source
		if err.Error() == "" {
			t.Error("LoadConfig() expected descriptive error message")
		}
		// Error should mention the unknown source ID
		if err.Error() != "" && !contains(err.Error(), "nonexistent_source") {
			t.Errorf("LoadConfig() error should mention unknown source ID, got: %v", err)
		}
	}
}

func TestLoadConfig_InvalidBatchStrategy(t *testing.T) {
	tmpDir := t.TempDir()

	// Create sources.toml with invalid batch strategy for source type
	sourcesTOML := `
[binance]
type = "rest"
batchable = true
batch_strategy = "multicall3"  # multicall3 can only be used with contract sources
base_url = "https://api.binance.com"
`
	sourcesPath := filepath.Join(tmpDir, "sources.toml")
	if err := os.WriteFile(sourcesPath, []byte(sourcesTOML), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	// Create valid asset_pairs.toml
	assetPairsTOML := `
[[asset_pairs]]
id = 1
pair = "BTC/USD"
query_data = "0x123"
exponent = 8
min_sources = 1
aggregation_method = "median"
sources = [
	{ source_id = "binance" }
]
`
	assetPairsPath := filepath.Join(tmpDir, "asset_pairs.toml")
	if err := os.WriteFile(assetPairsPath, []byte(assetPairsTOML), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	_, err := LoadConfig(sourcesPath, assetPairsPath)
	if err == nil {
		t.Error("LoadConfig() expected error for invalid batch strategy, got nil")
	}
}

func TestLoadConfig_InvalidAggregationMethod(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid sources.toml
	sourcesTOML := `
[binance]
type = "rest"
base_url = "https://api.binance.com"
`
	sourcesPath := filepath.Join(tmpDir, "sources.toml")
	if err := os.WriteFile(sourcesPath, []byte(sourcesTOML), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	// Create asset_pairs.toml with invalid aggregation method
	assetPairsTOML := `
[[asset_pairs]]
id = 1
pair = "BTC/USD"
query_data = "0x123"
exponent = 8
min_sources = 1
aggregation_method = "invalid_method"
sources = [
	{ source_id = "binance" }
]
`
	assetPairsPath := filepath.Join(tmpDir, "asset_pairs.toml")
	if err := os.WriteFile(assetPairsPath, []byte(assetPairsTOML), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	_, err := LoadConfig(sourcesPath, assetPairsPath)
	if err == nil {
		t.Error("LoadConfig() expected error for invalid aggregation method, got nil")
	}
	if err != nil {
		// Check that error message mentions the invalid aggregation method
		if err.Error() == "" {
			t.Error("LoadConfig() expected descriptive error message")
		}
	}
}

func TestLoadConfig_MissingFile(t *testing.T) {
	tmpDir := t.TempDir()

	sourcesPath := filepath.Join(tmpDir, "nonexistent_sources.toml")
	assetPairsPath := filepath.Join(tmpDir, "nonexistent_asset_pairs.toml")

	_, err := LoadConfig(sourcesPath, assetPairsPath)
	if err == nil {
		t.Error("LoadConfig() expected error for missing file, got nil")
	}
}

func TestLoadConfig_InvalidTOML(t *testing.T) {
	tmpDir := t.TempDir()

	// Create invalid TOML file
	sourcesTOML := `invalid toml content {`
	sourcesPath := filepath.Join(tmpDir, "sources.toml")
	if err := os.WriteFile(sourcesPath, []byte(sourcesTOML), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	assetPairsPath := filepath.Join(tmpDir, "asset_pairs.toml")
	if err := os.WriteFile(assetPairsPath, []byte(""), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	_, err := LoadConfig(sourcesPath, assetPairsPath)
	if err == nil {
		t.Error("LoadConfig() expected error for invalid TOML, got nil")
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Sources: map[string]SourceConfig{
					"binance": {
						ID:      "binance",
						Type:    "rest",
						BaseURL: "https://api.binance.com",
					},
				},
				AssetPairs: []AssetPairConfig{
					{
						ID:                1,
						Pair:              "BTC/USD",
						QueryData:         "0x123",
						Exponent:          8,
						MinSources:        1,
						AggregationMethod: "median",
						Sources: []AssetPairSource{
							{SourceID: "binance"},
						},
					},
				},
			},
			wantErr: false,
		},
		{
			name: "invalid source config",
			config: &Config{
				Sources: map[string]SourceConfig{
					"binance": {
						ID:   "binance",
						Type: "rest",
						// Missing BaseURL
					},
				},
				AssetPairs: []AssetPairConfig{},
			},
			wantErr: true,
		},
		{
			name: "invalid asset pair config",
			config: &Config{
				Sources: map[string]SourceConfig{
					"binance": {
						ID:      "binance",
						Type:    "rest",
						BaseURL: "https://api.binance.com",
					},
				},
				AssetPairs: []AssetPairConfig{
					{
						ID:        1,
						QueryData: "0x123",
						// Missing Pair
						Exponent:          8,
						MinSources:        1,
						AggregationMethod: "median",
						Sources: []AssetPairSource{
							{SourceID: "binance"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "invalid source reference",
			config: &Config{
				Sources: map[string]SourceConfig{
					"binance": {
						ID:      "binance",
						Type:    "rest",
						BaseURL: "https://api.binance.com",
					},
				},
				AssetPairs: []AssetPairConfig{
					{
						ID:                1,
						Pair:              "BTC/USD",
						QueryData:         "0x123",
						Exponent:          8,
						MinSources:        1,
						AggregationMethod: "median",
						Sources: []AssetPairSource{
							{SourceID: "nonexistent"},
						},
					},
				},
			},
			wantErr: true,
		},
		{
			name: "source ID mismatch",
			config: &Config{
				Sources: map[string]SourceConfig{
					"binance": {
						ID:      "coinbase", // ID doesn't match map key
						Type:    "rest",
						BaseURL: "https://api.binance.com",
					},
				},
				AssetPairs: []AssetPairConfig{},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Config.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && err != nil && err.Error() == "" {
				t.Error("Config.Validate() expected descriptive error message")
			}
		})
	}
}

func TestLoadConfig_WeightedAggregation(t *testing.T) {
	tmpDir := t.TempDir()

	// Create valid sources.toml
	sourcesTOML := `
[binance]
type = "rest"
base_url = "https://api.binance.com"

[coinbase]
type = "rest"
base_url = "https://api.coinbase.com"
`
	sourcesPath := filepath.Join(tmpDir, "sources.toml")
	if err := os.WriteFile(sourcesPath, []byte(sourcesTOML), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	// Create asset_pairs.toml with weighted aggregation
	assetPairsTOML := `
[[asset_pairs]]
id = 1
pair = "BTC/USD"
query_data = "0x123"
exponent = 8
min_sources = 2
aggregation_method = "weighted"
sources = [
	{ source_id = "binance", weight = 0.6 },
	{ source_id = "coinbase", weight = 0.4 }
]
`
	assetPairsPath := filepath.Join(tmpDir, "asset_pairs.toml")
	if err := os.WriteFile(assetPairsPath, []byte(assetPairsTOML), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	config, err := LoadConfig(sourcesPath, assetPairsPath)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v, want nil", err)
	}

	if len(config.AssetPairs) != 1 {
		t.Fatalf("Expected 1 asset pair, got %d", len(config.AssetPairs))
	}

	pair := config.AssetPairs[0]
	if pair.AggregationMethod != "weighted" {
		t.Errorf("Expected weighted aggregation, got %q", pair.AggregationMethod)
	}
	if len(pair.Sources) != 2 {
		t.Errorf("Expected 2 sources, got %d", len(pair.Sources))
	}
}

// Helper function to check if a string contains a substring
func contains(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
