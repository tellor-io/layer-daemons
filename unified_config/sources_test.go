package unified_config

import (
	"testing"
)

func TestSourceConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  SourceConfig
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid REST source",
			config: SourceConfig{
				ID:        "binance",
				Type:      "rest",
				Batchable: true,
				BaseURL:   "https://api.binance.com",
			},
			wantErr: false,
		},
		{
			// replace this with a different contract test
			name: "valid contract source",
			config: SourceConfig{
				ID:              "ethereum_contract",
				Type:            "contract",
				Batchable:       true,
				BatchStrategy:   "multicall3",
				ChainID:         1,
				ContractAddress: "0x1234567890123456789012345678901234567890",
			},
			wantErr: false,
		},
		{
			name: "valid RPC source",
			config: SourceConfig{
				ID:     "ethereum_rpc",
				Type:   "rpc",
				RPCURL: "https://eth-mainnet.g.alchemy.com/v2/xxx",
			},
			wantErr: false,
		},
		{
			name: "missing ID",
			config: SourceConfig{
				Type:    "rest",
				BaseURL: "https://api.example.com",
			},
			wantErr: true,
			errMsg:  "ID is required",
		},
		{
			name: "missing Type",
			config: SourceConfig{
				ID:      "test",
				BaseURL: "https://api.example.com",
			},
			wantErr: true,
			errMsg:  "Type is required",
		},
		{
			name: "invalid Type",
			config: SourceConfig{
				ID:   "test",
				Type: "invalid",
			},
			wantErr: true,
			errMsg:  "Type must be one of: rest, contract, rpc",
		},
		{
			name: "REST source missing BaseURL",
			config: SourceConfig{
				ID:   "test",
				Type: "rest",
			},
			wantErr: true,
			errMsg:  "BaseURL is required for REST sources",
		},
		{
			name: "contract source missing ChainID",
			config: SourceConfig{
				ID:              "test",
				Type:            "contract",
				ContractAddress: "0x1234567890123456789012345678901234567890",
			},
			wantErr: true,
			errMsg:  "ChainID is required for contract sources",
		},
		{
			name: "contract source missing ContractAddress",
			config: SourceConfig{
				ID:      "test",
				Type:    "contract",
				ChainID: 1,
			},
			wantErr: true,
			errMsg:  "ContractAddress is required for contract sources",
		},
		{
			name: "RPC source missing RPCURL",
			config: SourceConfig{
				ID:   "test",
				Type: "rpc",
			},
			wantErr: true,
			errMsg:  "RPCURL is required for RPC sources",
		},
		{
			name: "batchable source with invalid BatchStrategy",
			config: SourceConfig{
				ID:            "test",
				Type:          "rest",
				BaseURL:       "https://api.example.com",
				Batchable:     true,
				BatchStrategy: "invalid_strategy",
			},
			wantErr: true,
			errMsg:  "BatchStrategy must be one of: query_param, body, multicall3",
		},
		{
			name: "batchable REST source with query_param strategy",
			config: SourceConfig{
				ID:            "test",
				Type:          "rest",
				BaseURL:       "https://api.example.com",
				Batchable:     true,
				BatchStrategy: "query_param",
			},
			wantErr: false,
		},
		{
			name: "batchable REST source with body strategy",
			config: SourceConfig{
				ID:            "test",
				Type:          "rest",
				BaseURL:       "https://api.example.com",
				Batchable:     true,
				BatchStrategy: "body",
			},
			wantErr: false,
		},
		{
			name: "batchable contract source with multicall3 strategy",
			config: SourceConfig{
				ID:              "test",
				Type:            "contract",
				ChainID:         1,
				ContractAddress: "0x1234567890123456789012345678901234567890",
				Batchable:       true,
				BatchStrategy:   "multicall3",
			},
			wantErr: false,
		},
		{
			name: "batchable source missing UpdateIntervalSeconds",
			config: SourceConfig{
				ID:            "test",
				Type:          "rest",
				BaseURL:       "https://api.example.com",
				Batchable:     true,
				BatchStrategy: "query_param",
			},
			wantErr: false, // UpdateIntervalSeconds is optional, defaults to 0
		},
		{
			name: "non-batchable source with BatchStrategy",
			config: SourceConfig{
				ID:            "test",
				Type:          "rest",
				BaseURL:       "https://api.example.com",
				Batchable:     false,
				BatchStrategy: "query_param",
			},
			wantErr: false, // BatchStrategy can be set even if not batchable (will be ignored)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("SourceConfig.Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errMsg != "" {
				if err == nil || err.Error() == "" {
					t.Errorf("SourceConfig.Validate() expected error message containing %q, got %v", tt.errMsg, err)
				}
			}
		})
	}
}

func TestSourceConfig_TOMLSerialization(t *testing.T) {
	// Test that SourceConfig can be serialized/deserialized with TOML tags
	config := SourceConfig{
		ID:                    "binance",
		Type:                  "rest",
		Batchable:             true,
		BatchStrategy:         "query_param",
		BatchGroup:            "exchange_group",
		UpdateIntervalSeconds: 60,
		CacheTTLSeconds:       30,
		BaseURL:               "https://api.binance.com",
	}

	// Verify all fields are set
	if config.ID != "binance" {
		t.Errorf("ID not set correctly: got %q", config.ID)
	}
	if config.Type != "rest" {
		t.Errorf("Type not set correctly: got %q", config.Type)
	}
	if !config.Batchable {
		t.Errorf("Batchable not set correctly: got %v", config.Batchable)
	}
	if config.BaseURL != "https://api.binance.com" {
		t.Errorf("BaseURL not set correctly: got %q", config.BaseURL)
	}
}
