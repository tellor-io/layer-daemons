package daemons

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/tellor-io/layer-daemons/unified_config"
	"github.com/tellor-io/layer-daemons/unified_config/batch"
	"github.com/tellor-io/layer-daemons/unified_config/cache"
	"github.com/tellor-io/layer-daemons/unified_config/orchestrator"
)

// TestUnifiedConfigLoading tests that unified config can be loaded successfully
func TestUnifiedConfigLoading(t *testing.T) {
	// Create a temporary directory for test configs
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, "config")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatalf("Failed to create config directory: %v", err)
	}

	// Create test sources.toml
	sourcesPath := filepath.Join(configDir, "sources.toml")
	sourcesContent := `[sources.source1]
id = "source1"
type = "rest"
batchable = true
batch_strategy = "query_param"
batch_group = "group1"
update_interval_seconds = 60
cache_ttl_seconds = 30
base_url = "https://api.example.com"
`
	if err := os.WriteFile(sourcesPath, []byte(sourcesContent), 0644); err != nil {
		t.Fatalf("Failed to write sources.toml: %v", err)
	}

	// Create test asset_pairs.toml
	assetPairsPath := filepath.Join(configDir, "asset_pairs.toml")
	assetPairsContent := `[[asset_pairs]]
id = 1
pair = "BTC/USD"
query_data = "0x1234"
exponent = -8
min_sources = 1
aggregation_method = "median"

[[asset_pairs.sources]]
source_id = "source1"
`
	if err := os.WriteFile(assetPairsPath, []byte(assetPairsContent), 0644); err != nil {
		t.Fatalf("Failed to write asset_pairs.toml: %v", err)
	}

	// Test loading the config
	config, err := unified_config.LoadConfig(sourcesPath, assetPairsPath)
	if err != nil {
		t.Fatalf("Failed to load unified config: %v", err)
	}

	// Verify config was loaded correctly
	if len(config.Sources) != 1 {
		t.Errorf("Expected 1 source, got %d", len(config.Sources))
	}
	if len(config.AssetPairs) != 1 {
		t.Errorf("Expected 1 asset pair, got %d", len(config.AssetPairs))
	}

	source, exists := config.Sources["source1"]
	if !exists {
		t.Fatal("Source 'source1' not found")
	}
	if source.ID != "source1" {
		t.Errorf("Expected source ID 'source1', got %q", source.ID)
	}
	if source.Type != "rest" {
		t.Errorf("Expected source type 'rest', got %q", source.Type)
	}
	if !source.Batchable {
		t.Error("Expected source to be batchable")
	}

	pair := config.AssetPairs[0]
	if pair.Pair != "BTC/USD" {
		t.Errorf("Expected pair 'BTC/USD', got %q", pair.Pair)
	}
	if len(pair.Sources) != 1 {
		t.Errorf("Expected 1 source in pair, got %d", len(pair.Sources))
	}
}

// TestUnifiedConfigInitialization tests that all unified config components can be initialized
func TestUnifiedConfigInitialization(t *testing.T) {
	// Create a minimal valid config
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchStrategy:         "query_param",
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
				CacheTTLSeconds:       30,
				BaseURL:               "https://api.example.com",
			},
		},
		AssetPairs: []unified_config.AssetPairConfig{
			{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x1234",
				Exponent:          -8,
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []unified_config.AssetPairSource{
					{SourceID: "source1"},
				},
			},
		},
		GlobalStalenessThresholdSeconds: 300,
	}

	// Initialize PriceCache
	globalStalenessThreshold := time.Duration(config.GlobalStalenessThresholdSeconds) * time.Second
	sourceTTLs := make(map[string]time.Duration)
	for id, source := range config.Sources {
		if source.CacheTTLSeconds > 0 {
			sourceTTLs[id] = time.Duration(source.CacheTTLSeconds) * time.Second
		}
	}
	priceCache := cache.NewPriceCache(globalStalenessThreshold, sourceTTLs)
	if priceCache == nil {
		t.Fatal("Failed to create PriceCache")
	}

	// Initialize BatchCollector
	collector := batch.NewBatchCollector()
	if collector == nil {
		t.Fatal("Failed to create BatchCollector")
	}

	// Initialize BatchScheduler
	scheduler := batch.NewBatchScheduler(config, collector, priceCache)
	if scheduler == nil {
		t.Fatal("Failed to create BatchScheduler")
	}

	// Initialize QueryOrchestrator
	orchestrator := orchestrator.NewQueryOrchestrator(config, priceCache)
	if orchestrator == nil {
		t.Fatal("Failed to create QueryOrchestrator")
	}

	// Wire batching into orchestrator
	orchestrator.WithBatching(scheduler, collector)

	// Verify components are properly initialized
	if orchestrator == nil {
		t.Fatal("Orchestrator is nil after initialization")
	}
}

// TestUnifiedConfigLoadingWithInvalidPaths tests error handling for invalid config paths
func TestUnifiedConfigLoadingWithInvalidPaths(t *testing.T) {
	// Test with non-existent sources file
	_, err := unified_config.LoadConfig("/nonexistent/sources.toml", "/nonexistent/asset_pairs.toml")
	if err == nil {
		t.Error("Expected error when loading non-existent config files")
	}

	// Test with invalid TOML
	tmpDir := t.TempDir()
	invalidPath := filepath.Join(tmpDir, "invalid.toml")
	if err := os.WriteFile(invalidPath, []byte("invalid toml content !!!"), 0644); err != nil {
		t.Fatalf("Failed to write invalid TOML: %v", err)
	}

	_, err = unified_config.LoadConfig(invalidPath, invalidPath)
	if err == nil {
		t.Error("Expected error when loading invalid TOML")
	}
}

// TestUnifiedConfigInitializationWithEmptyConfig tests initialization with empty config
func TestUnifiedConfigInitializationWithEmptyConfig(t *testing.T) {
	config := &unified_config.Config{
		Sources:                         make(map[string]unified_config.SourceConfig),
		AssetPairs:                      []unified_config.AssetPairConfig{},
		GlobalStalenessThresholdSeconds: 300,
	}

	globalStalenessThreshold := time.Duration(config.GlobalStalenessThresholdSeconds) * time.Second
	priceCache := cache.NewPriceCache(globalStalenessThreshold, nil)
	collector := batch.NewBatchCollector()
	scheduler := batch.NewBatchScheduler(config, collector, priceCache)
	orchestrator := orchestrator.NewQueryOrchestrator(config, priceCache)

	// Should initialize successfully even with empty config
	if priceCache == nil || collector == nil || scheduler == nil || orchestrator == nil {
		t.Fatal("Components should initialize successfully with empty config")
	}

	// Starting scheduler with empty config should not error
	if err := scheduler.Start(); err != nil {
		t.Errorf("Starting scheduler with empty config should not error: %v", err)
	}

	// Cleanup
	scheduler.Stop()
}

// TestBatchSchedulerStart tests that BatchScheduler can be started successfully
func TestBatchSchedulerStart(t *testing.T) {
	config := &unified_config.Config{
		Sources: map[string]unified_config.SourceConfig{
			"source1": {
				ID:                    "source1",
				Type:                  "rest",
				Batchable:             true,
				BatchStrategy:         "query_param",
				BatchGroup:            "group1",
				UpdateIntervalSeconds: 60,
				CacheTTLSeconds:       30,
				BaseURL:               "https://api.example.com",
			},
		},
		AssetPairs:                      []unified_config.AssetPairConfig{},
		GlobalStalenessThresholdSeconds: 300,
	}

	globalStalenessThreshold := time.Duration(config.GlobalStalenessThresholdSeconds) * time.Second
	priceCache := cache.NewPriceCache(globalStalenessThreshold, nil)
	collector := batch.NewBatchCollector()
	scheduler := batch.NewBatchScheduler(config, collector, priceCache)

	// Start should succeed even without batch handlers registered
	// (handlers are registered separately)
	if err := scheduler.Start(); err != nil {
		t.Fatalf("Failed to start BatchScheduler: %v", err)
	}

	// Cleanup
	if err := scheduler.Stop(); err != nil {
		t.Errorf("Failed to stop BatchScheduler: %v", err)
	}
}
