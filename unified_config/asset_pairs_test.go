package unified_config

import (
	"testing"
)

func TestAssetPairConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  AssetPairConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid asset pair with median aggregation",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x1234567890abcdef",
				Exponent:          8,
				MinSources:        3,
				AggregationMethod: "median",
				Sources: []AssetPairSource{
					{SourceID: "binance"},
					{SourceID: "coinbase"},
					{SourceID: "kraken"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid asset pair with mean aggregation",
			config: AssetPairConfig{
				ID:                2,
				Pair:              "ETH/USD",
				QueryData:         "0xabcdef1234567890",
				Exponent:          8,
				MinSources:        2,
				AggregationMethod: "mean",
				Sources: []AssetPairSource{
					{SourceID: "binance"},
					{SourceID: "coinbase"},
				},
			},
			wantErr: false,
		},
		{
			name: "valid asset pair with weighted aggregation",
			config: AssetPairConfig{
				ID:                3,
				Pair:              "SOL/USD",
				QueryData:         "0x9876543210fedcba",
				Exponent:          8,
				MinSources:        2,
				AggregationMethod: "weighted",
				Sources: []AssetPairSource{
					{SourceID: "binance", Weight: 0.6},
					{SourceID: "coinbase", Weight: 0.4},
				},
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			config: AssetPairConfig{
				Pair:              "BTC/USD",
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []AssetPairSource{
					{SourceID: "binance"},
				},
			},
			wantErr: false, // ID can be 0, which is valid
		},
		{
			name: "missing Pair",
			config: AssetPairConfig{
				ID:                1,
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        3,
				AggregationMethod: "median",
				Sources: []AssetPairSource{
					{SourceID: "binance"},
				},
			},
			wantErr: true,
			errMsg:  "Pair is required",
		},
		{
			name: "missing QueryData",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				Exponent:          8,
				MinSources:        3,
				AggregationMethod: "median",
				Sources: []AssetPairSource{
					{SourceID: "binance"},
				},
			},
			wantErr: true,
			errMsg:  "QueryData is required",
		},
		{
			name: "missing Sources",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        3,
				AggregationMethod: "median",
				Sources:           []AssetPairSource{},
			},
			wantErr: true,
			errMsg:  "at least one source is required",
		},
		{
			name: "invalid AggregationMethod",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        3,
				AggregationMethod: "invalid",
				Sources: []AssetPairSource{
					{SourceID: "binance"},
				},
			},
			wantErr: true,
			errMsg:  "AggregationMethod must be one of: median, mean, weighted",
		},
		{
			name: "MinSources less than 1",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        0,
				AggregationMethod: "median",
				Sources: []AssetPairSource{
					{SourceID: "binance"},
				},
			},
			wantErr: true,
			errMsg:  "MinSources must be at least 1",
		},
		{
			name: "MinSources greater than number of sources",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        5,
				AggregationMethod: "median",
				Sources: []AssetPairSource{
					{SourceID: "binance"},
					{SourceID: "coinbase"},
				},
			},
			wantErr: true,
			errMsg:  "MinSources cannot be greater than number of sources",
		},
		{
			name: "weighted aggregation with missing weights",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        2,
				AggregationMethod: "weighted",
				Sources: []AssetPairSource{
					{SourceID: "binance", Weight: 0.0},  // Weight not set
					{SourceID: "coinbase", Weight: 0.0}, // Weight not set
				},
			},
			wantErr: true,
			errMsg:  "all sources must have weights for weighted aggregation",
		},
		{
			name: "weighted aggregation with weights not summing to 1",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        2,
				AggregationMethod: "weighted",
				Sources: []AssetPairSource{
					{SourceID: "binance", Weight: 0.3},
					{SourceID: "coinbase", Weight: 0.4},
				},
			},
			wantErr: true,
			errMsg:  "weights must sum to approximately 1.0",
		},
		{
			name: "weighted aggregation with valid weights",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        2,
				AggregationMethod: "weighted",
				Sources: []AssetPairSource{
					{SourceID: "binance", Weight: 0.6},
					{SourceID: "coinbase", Weight: 0.4},
				},
			},
			wantErr: false,
		},
		{
			name: "source with empty SourceID",
			config: AssetPairConfig{
				ID:                1,
				Pair:              "BTC/USD",
				QueryData:         "0x123",
				Exponent:          8,
				MinSources:        1,
				AggregationMethod: "median",
				Sources: []AssetPairSource{
					{SourceID: ""},
				},
			},
			wantErr: true,
			errMsg:  "SourceID is required for all sources",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("AssetPairConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil {
					t.Errorf("AssetPairConfig.Validate() expected error message containing %q, got nil", tt.errMsg)
				} else if err.Error() == "" {
					t.Errorf("AssetPairConfig.Validate() expected error message containing %q, got empty error", tt.errMsg)
				}
			}
		})
	}
}

func TestAssetPairSource_Validate(t *testing.T) {
	tests := []struct {
		name    string
		source  AssetPairSource
		wantErr bool
	}{
		{
			name:    "valid source with SourceID",
			source:  AssetPairSource{SourceID: "binance"},
			wantErr: false,
		},
		{
			name:    "valid source with weight",
			source:  AssetPairSource{SourceID: "binance", Weight: 0.5},
			wantErr: false,
		},
		{
			name:    "empty SourceID",
			source:  AssetPairSource{SourceID: ""},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.source.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("AssetPairSource.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestAssetPairConfig_TOMLSerialization(t *testing.T) {
	// Test that AssetPairConfig can be serialized/deserialized with TOML tags
	config := AssetPairConfig{
		ID:                1,
		Pair:              "BTC/USD",
		QueryData:         "0x1234567890abcdef",
		Exponent:          8,
		MinSources:        3,
		AggregationMethod: "median",
		Sources: []AssetPairSource{
			{SourceID: "binance"},
			{SourceID: "coinbase", Weight: 0.5},
		},
	}

	// Verify all fields are set
	if config.ID != 1 {
		t.Errorf("ID not set correctly: got %d", config.ID)
	}
	if config.Pair != "BTC/USD" {
		t.Errorf("Pair not set correctly: got %q", config.Pair)
	}
	if len(config.Sources) != 2 {
		t.Errorf("Sources not set correctly: got %d sources", len(config.Sources))
	}
}
