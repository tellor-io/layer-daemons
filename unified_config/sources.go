package unified_config

import (
	"fmt"
	"strings"
)

// SourceConfig represents the configuration for a price data source.
// Sources can be REST APIs, smart contracts, or RPC endpoints.
type SourceConfig struct {
	// ID is a unique identifier for this source
	ID string `toml:"id" json:"id"`

	// Type specifies the source type: "rest", "contract", or "rpc"
	Type string `toml:"type" json:"type"`

	// Batchable indicates whether this source supports batching multiple queries
	Batchable bool `toml:"batchable" json:"batchable"`

	// BatchStrategy specifies how to batch queries: "query_param", "body", or "multicall3"
	// Empty string means no batching strategy (for non-batchable sources)
	BatchStrategy string `toml:"batch_strategy,omitempty" json:"batch_strategy,omitempty"`

	// BatchGroup groups sources together for batching
	BatchGroup string `toml:"batch_group,omitempty" json:"batch_group,omitempty"`

	// UpdateIntervalSeconds is the interval (in seconds) for batchable sources to update
	UpdateIntervalSeconds int `toml:"update_interval_seconds,omitempty" json:"update_interval_seconds,omitempty"`

	// CacheTTLSeconds is the cache TTL (time-to-live) in seconds for this source
	CacheTTLSeconds int `toml:"cache_ttl_seconds,omitempty" json:"cache_ttl_seconds,omitempty"`

	// BaseURL is the base URL for REST sources
	BaseURL string `toml:"base_url,omitempty" json:"base_url,omitempty"`

	// ChainID is the chain ID for contract sources
	ChainID uint64 `toml:"chain_id,omitempty" json:"chain_id,omitempty"`

	// ContractAddress is the contract address for contract sources
	ContractAddress string `toml:"contract_address,omitempty" json:"contract_address,omitempty"`

	// RPCURL is the RPC endpoint URL for RPC sources
	RPCURL string `toml:"rpc_url,omitempty" json:"rpc_url,omitempty"`
}

// Validate checks that the SourceConfig is valid and returns an error if not.
func (c *SourceConfig) Validate() error {
	if c.ID == "" {
		return fmt.Errorf("ID is required")
	}

	if c.Type == "" {
		return fmt.Errorf("Type is required")
	}

	validTypes := map[string]bool{
		"rest":     true,
		"contract": true,
		"rpc":      true,
	}
	if !validTypes[c.Type] {
		return fmt.Errorf("Type must be one of: rest, contract, rpc")
	}

	// Type-specific validations
	switch c.Type {
	case "rest":
		if c.BaseURL == "" {
			return fmt.Errorf("BaseURL is required for REST sources")
		}
	case "contract":
		if c.ChainID == 0 {
			return fmt.Errorf("ChainID is required for contract sources")
		}
		if c.ContractAddress == "" {
			return fmt.Errorf("ContractAddress is required for contract sources")
		}
	case "rpc":
		if c.RPCURL == "" {
			return fmt.Errorf("RPCURL is required for RPC sources")
		}
	}

	// Validate BatchStrategy if provided
	if c.BatchStrategy != "" {
		validStrategies := map[string]bool{
			"query_param": true,
			"body":        true,
			"multicall3":  true,
		}
		if !validStrategies[c.BatchStrategy] {
			return fmt.Errorf("BatchStrategy must be one of: query_param, body, multicall3")
		}

		// Validate strategy matches source type
		if c.BatchStrategy == "multicall3" && c.Type != "contract" {
			return fmt.Errorf("multicall3 strategy can only be used with contract sources")
		}
		if (c.BatchStrategy == "query_param" || c.BatchStrategy == "body") && c.Type != "rest" {
			return fmt.Errorf("%s strategy can only be used with REST sources", c.BatchStrategy)
		}
	}

	return nil
}

// String returns a string representation of the SourceConfig for debugging.
func (c *SourceConfig) String() string {
	var parts []string
	parts = append(parts, fmt.Sprintf("ID=%q", c.ID))
	parts = append(parts, fmt.Sprintf("Type=%q", c.Type))
	if c.Batchable {
		parts = append(parts, fmt.Sprintf("Batchable=%t", c.Batchable))
		if c.BatchStrategy != "" {
			parts = append(parts, fmt.Sprintf("BatchStrategy=%q", c.BatchStrategy))
		}
	}
	return fmt.Sprintf("SourceConfig{%s}", strings.Join(parts, ", "))
}
