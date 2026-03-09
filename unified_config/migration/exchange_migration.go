package migration

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml"
	customquery "github.com/tellor-io/layer-daemons/custom_query"
	"github.com/tellor-io/layer-daemons/pricefeed/client/types"
	"github.com/tellor-io/layer-daemons/unified_config"
)

// DefaultExchangeSourceConfigs is a thin wrapper around the shared
// unified_config.DefaultExchangeSourceConfigs helper so that migration
// code and future default-config generators can share the same mapping
// without duplicating definitions.
func DefaultExchangeSourceConfigs() []unified_config.SourceConfig {
	return unified_config.DefaultExchangeSourceConfigs()
}

// legacyExchangeMapping mirrors the structure of the legacy
// ExchangeConfigJson blob embedded in StaticMarketParamsConfig.
//
// Example (pretty‑printed) payload:
//
//	{
//	  "exchanges": [
//	    {"exchangeName": "Binance", "ticker": "\"BTCUSDT\""},
//	    {"exchangeName": "Kraken",  "ticker": "XXBTZUSD"}
//	  ]
//	}
type legacyExchangeMapping struct {
	Exchanges []struct {
		ExchangeName string `json:"exchangeName"`
		Ticker       string `json:"ticker"`
	} `json:"exchanges"`
}

// normalizeQuotedString trims a single leading/trailing double quote from
// strings that are stored with extra quotes in the legacy config (e.g.
// `"BTC-USD"`), while leaving already-normal strings untouched.
func normalizeQuotedString(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && strings.HasPrefix(s, "\"") && strings.HasSuffix(s, "\"") {
		return s[1 : len(s)-1]
	}
	return s
}

// MigrateMarketParamsFromStatic converts the in-memory StaticMarketParamsConfig
// into unified SourceConfig and AssetPairConfig slices.
//
// This helper is used by MigrateMarketParams (which will eventually read a
// market_params.toml file) and is the primary unit under test for Step 3.4.
func MigrateMarketParamsFromStatic(
	static map[uint32]*types.MarketParam,
) ([]unified_config.SourceConfig, []unified_config.AssetPairConfig, error) {
	if len(static) == 0 {
		return nil, nil, fmt.Errorf("no market params provided for migration")
	}

	// Start from the shared default exchange source configs. These represent
	// the REST sources that replace the old ticker-style exchange layer.
	sourceConfigs := DefaultExchangeSourceConfigs()
	sourceByID := make(map[string]unified_config.SourceConfig, len(sourceConfigs))
	for _, cfg := range sourceConfigs {
		sourceByID[cfg.ID] = cfg
	}

	var pairs []unified_config.AssetPairConfig

	for id, mp := range static {
		if mp == nil {
			return nil, nil, fmt.Errorf("market param %d is nil", id)
		}

		var legacyCfg legacyExchangeMapping

		rawJSON := strings.TrimSpace(mp.ExchangeConfigJson)
		// First try to parse as-is (useful for tests and any already-normal JSON).
		if err := json.Unmarshal([]byte(rawJSON), &legacyCfg); err != nil {
			// If that fails, fall back to handling the legacy raw-literal form which
			// includes backslash-escaped quotes (e.g. `{\"exchanges\":[...]}`).
			unescaped, uerr := strconv.Unquote(`"` + rawJSON + `"`)
			if uerr != nil {
				return nil, nil, fmt.Errorf("market param %d (%s): normalise ExchangeConfigJson: %w", id, mp.Pair, uerr)
			}

			if err2 := json.Unmarshal([]byte(unescaped), &legacyCfg); err2 != nil {
				return nil, nil, fmt.Errorf("market param %d (%s): decode ExchangeConfigJson: %w", id, mp.Pair, err2)
			}
		}
		if len(legacyCfg.Exchanges) == 0 {
			return nil, nil, fmt.Errorf("market param %d (%s): no exchanges defined in ExchangeConfigJson", id, mp.Pair)
		}

		sources := make([]unified_config.AssetPairSource, 0, len(legacyCfg.Exchanges))
		for _, ex := range legacyCfg.Exchanges {
			if ex.ExchangeName == "" {
				return nil, nil, fmt.Errorf("market param %d (%s): exchange with empty name", id, mp.Pair)
			}
			if _, ok := sourceByID[ex.ExchangeName]; !ok {
				return nil, nil, fmt.Errorf("market param %d (%s): no unified SourceConfig for exchange %q", id, mp.Pair, ex.ExchangeName)
			}

			sources = append(sources, unified_config.AssetPairSource{
				SourceID: ex.ExchangeName,
				// Symbol/ticker weights are not explicitly modeled in the
				// legacy config; we default to equal weighting and rely on
				// MinSources for robustness.
				Weight: 0,
			})
		}

		if mp.MinExchanges == 0 {
			return nil, nil, fmt.Errorf("market param %d (%s): MinExchanges must be > 0", id, mp.Pair)
		}
		if int(mp.MinExchanges) > len(sources) {
			return nil, nil, fmt.Errorf("market param %d (%s): MinExchanges (%d) > number of exchanges (%d)", id, mp.Pair, mp.MinExchanges, len(sources))
		}

		pairCfg := unified_config.AssetPairConfig{
			ID:                id,
			Pair:              normalizeQuotedString(mp.Pair),
			QueryData:         normalizeQuotedString(mp.QueryData),
			Exponent:          mp.Exponent,
			MinSources:        int(mp.MinExchanges),
			Sources:           sources,
			AggregationMethod: "median",
		}

		if err := pairCfg.Validate(); err != nil {
			return nil, nil, fmt.Errorf("market param %d (%s): invalid migrated AssetPairConfig: %w", id, mp.Pair, err)
		}

		pairs = append(pairs, pairCfg)
	}

	return sourceConfigs, pairs, nil
}

// MigrateMarketParams is the public entry point described in the design
// document. For now (Step 3.4) it delegates to the in-memory static config,
// ignoring the provided path. A later Step 3.5 implementation can extend this
// to read an actual market_params.toml file while keeping tests intact.
func MigrateMarketParams(marketParamsPath string) ([]unified_config.SourceConfig, []unified_config.AssetPairConfig, error) {
	if marketParamsPath == "" {
		return nil, nil, fmt.Errorf("market params path is required")
	}

	type marketEntry struct {
		ID                uint32 `toml:"id"`
		Pair              string `toml:"pair"`
		Exponent          int32  `toml:"exponent"`
		MinExchanges      uint32 `toml:"min_exchanges"`
		ExchangeConfigRaw string `toml:"exchange_config_json"`
		QueryData         string `toml:"query_data"`
	}

	type fileConfig struct {
		Markets []marketEntry `toml:"markets"`
	}

	data, err := os.ReadFile(marketParamsPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read market params file: %w", err)
	}

	var fc fileConfig
	if err := tomlUnmarshal(data, &fc); err != nil {
		return nil, nil, fmt.Errorf("decode market params toml: %w", err)
	}

	if len(fc.Markets) == 0 {
		return nil, nil, fmt.Errorf("no markets found in market params file")
	}

	static := make(map[uint32]*types.MarketParam, len(fc.Markets))
	for _, m := range fc.Markets {
		static[m.ID] = &types.MarketParam{
			Id:                 m.ID,
			Pair:               m.Pair,
			Exponent:           m.Exponent,
			MinExchanges:       m.MinExchanges,
			ExchangeConfigJson: m.ExchangeConfigRaw,
			QueryData:          m.QueryData,
		}
	}

	return MigrateMarketParamsFromStatic(static)
}

// tomlUnmarshal is a tiny indirection so tests don't need a real TOML
// dependency injected; it is implemented via go-toml in this package.
var tomlUnmarshal = func(b []byte, v any) error {
	return toml.Unmarshal(b, v)
}

// MigrateCustomQueryConfig converts a custom_query_config.toml file into
// unified SourceConfig and AssetPairConfig slices.
func MigrateCustomQueryConfig(customQueryPath string) ([]unified_config.SourceConfig, []unified_config.AssetPairConfig, error) {
	if customQueryPath == "" {
		return nil, nil, fmt.Errorf("custom query config path is required")
	}

	data, err := os.ReadFile(customQueryPath)
	if err != nil {
		return nil, nil, fmt.Errorf("read custom query config file: %w", err)
	}

	var cfg customquery.Config
	if err := tomlUnmarshal(data, &cfg); err != nil {
		return nil, nil, fmt.Errorf("decode custom query toml: %w", err)
	}

	var sources []unified_config.SourceConfig
	for id, ep := range cfg.Endpoints {
		src := unified_config.SourceConfig{
			ID:              id,
			Type:            "rest",
			BaseURL:         ep.URLTemplate,
			Batchable:       false,
			BatchStrategy:   "",
			BatchGroup:      "custom_query_rest",
			CacheTTLSeconds: 15,
		}
		if err := src.Validate(); err != nil {
			return nil, nil, fmt.Errorf("invalid REST source %q derived from endpoints: %w", id, err)
		}
		sources = append(sources, src)
	}

	// RPC endpoints are connection details for contract/RPC readers; in the
	// unified config they will be modeled as separate RPC or contract sources
	// in later steps. For now we focus on REST endpoints and queries.

	var pairs []unified_config.AssetPairConfig
	for id, q := range cfg.Queries {
		assetSources := make([]unified_config.AssetPairSource, 0, len(q.Endpoints))
		for _, ep := range q.Endpoints {
			if ep.EndpointType == "" {
				continue
			}
			assetSources = append(assetSources, unified_config.AssetPairSource{
				SourceID: ep.EndpointType,
			})
		}

		if len(assetSources) == 0 {
			continue
		}

		minSources := q.MinResponses
		if minSources <= 0 {
			minSources = 1
		}

		pair := unified_config.AssetPairConfig{
			ID:                0,
			Pair:              id,
			QueryData:         q.ID,
			Exponent:          0,
			MinSources:        minSources,
			Sources:           assetSources,
			AggregationMethod: q.AggregationMethod,
		}
		if pair.AggregationMethod == "" {
			pair.AggregationMethod = "median"
		}

		if err := pair.Validate(); err != nil {
			return nil, nil, fmt.Errorf("invalid asset pair derived from query %q: %w", id, err)
		}

		pairs = append(pairs, pair)
	}

	return sources, pairs, nil
}
