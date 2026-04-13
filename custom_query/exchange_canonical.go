package customquery

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/tellor-io/layer-daemons/constants"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
)

// BuildCanonicalExchangeMarketRegistry parses market params the same way the pricefeed daemon does
// and returns mutable exchange configs keyed by exchange id. Used by tests and static fallback.
func BuildCanonicalExchangeMarketRegistry(marketParams []pricefeedtypes.MarketParam) (
	map[pricefeedtypes.ExchangeId]*pricefeedtypes.MutableExchangeMarketConfig,
	error,
) {
	if len(marketParams) == 0 {
		return nil, fmt.Errorf("market params are empty")
	}

	params := make([]pricefeedtypes.MarketParam, len(marketParams))
	copy(params, marketParams)
	normalizeMarketParamsLikeReadFromFile(params)

	canonicalExchangeIDs := make([]pricefeedtypes.ExchangeId, 0, len(constants.StaticExchangeDetails))
	for id := range constants.StaticExchangeDetails {
		canonicalExchangeIDs = append(canonicalExchangeIDs, id)
	}

	pfmmc := pricefeedtypes.NewPriceFeedMutableMarketConfigs(canonicalExchangeIDs)
	mutableExchangeConfigs, _, marketParamErrors, err := pfmmc.ValidateAndTransformParams(params)
	if err != nil {
		return nil, err
	}
	if len(marketParamErrors) > 0 {
		errs := make([]error, 0, len(marketParamErrors))
		for id, e := range marketParamErrors {
			errs = append(errs, fmt.Errorf("market id %d: %w", id, e))
		}
		return nil, errors.Join(errs...)
	}

	return mutableExchangeConfigs, nil
}

// normalizeMarketParamsLikeReadFromFile applies the same ExchangeConfigJson / QueryData cleanup as
// configs.ReadMarketParamsConfigFile so in-memory constants match file-loaded market params.
func normalizeMarketParamsLikeReadFromFile(params []pricefeedtypes.MarketParam) {
	for i := range params {
		jsonStr := params[i].ExchangeConfigJson
		if strings.Contains(jsonStr, "\\\"") {
			if unquoted, err := strconv.Unquote(`"` + jsonStr + `"`); err == nil {
				params[i].ExchangeConfigJson = unquoted
			}
		}
		if len(params[i].QueryData) > 0 && params[i].QueryData[0] == '"' {
			params[i].QueryData = strings.Trim(params[i].QueryData, `"`)
		}
	}
}

// CanonicalExchangeMarketConfig looks up a single (exchangeID, marketID) in a registry returned by
// BuildCanonicalExchangeMarketRegistry.
func CanonicalExchangeMarketConfig(
	registry map[pricefeedtypes.ExchangeId]*pricefeedtypes.MutableExchangeMarketConfig,
	exchangeID pricefeedtypes.ExchangeId,
	marketID pricefeedtypes.MarketId,
) (cfg pricefeedtypes.MarketConfig, ok bool) {
	if registry == nil {
		return pricefeedtypes.MarketConfig{}, false
	}
	mutable, exists := registry[exchangeID]
	if !exists || mutable == nil {
		return pricefeedtypes.MarketConfig{}, false
	}
	cfg, ok = mutable.MarketToMarketConfig[marketID]
	return cfg, ok
}

type exchangeLegKey struct {
	ex  pricefeedtypes.ExchangeId
	mid pricefeedtypes.MarketId
}

func registerExchangeLeg(
	m map[exchangeLegKey]pricefeedtypes.MarketConfig,
	ex pricefeedtypes.ExchangeId,
	mid pricefeedtypes.MarketId,
	cfg pricefeedtypes.MarketConfig,
) error {
	k := exchangeLegKey{ex: ex, mid: mid}
	if prev, ok := m[k]; ok && !prev.Equal(cfg) {
		return fmt.Errorf("conflicting exchange market config for exchange_id=%s market_id=%d", ex, mid)
	}
	m[k] = cfg
	return nil
}

var (
	staticExchangeRegistryMu   sync.Mutex
	staticExchangeRegistry     map[pricefeedtypes.ExchangeId]*pricefeedtypes.MutableExchangeMarketConfig
	staticExchangeRegistryErr  error
	staticExchangeRegistryInit bool
)

func cachedStaticExchangeRegistry() (map[pricefeedtypes.ExchangeId]*pricefeedtypes.MutableExchangeMarketConfig, error) {
	staticExchangeRegistryMu.Lock()
	defer staticExchangeRegistryMu.Unlock()
	if staticExchangeRegistryInit {
		return staticExchangeRegistry, staticExchangeRegistryErr
	}
	params := make([]pricefeedtypes.MarketParam, 0, len(constants.StaticMarketParamsConfig))
	for _, p := range constants.StaticMarketParamsConfig {
		if p == nil {
			continue
		}
		params = append(params, *p)
	}
	staticExchangeRegistry, staticExchangeRegistryErr = BuildCanonicalExchangeMarketRegistry(params)
	staticExchangeRegistryInit = true
	return staticExchangeRegistry, staticExchangeRegistryErr
}

func buildExchangeHandler(
	query QueryConfig,
	endpoint EndpointConfig,
	legRegistry map[exchangeLegKey]pricefeedtypes.MarketConfig,
) (ExchangeHandler, error) {
	var zero ExchangeHandler
	if endpoint.ExchangeID == "" {
		return zero, fmt.Errorf("query %s: exchange endpoint missing exchange_id", query.ID)
	}
	if _, exists := constants.StaticExchangeDetails[endpoint.ExchangeID]; !exists {
		return zero, fmt.Errorf("query %s: unknown exchange_id %q (not in StaticExchangeDetails)", query.ID, endpoint.ExchangeID)
	}
	if len(endpoint.ResponsePath) > 0 {
		return zero, fmt.Errorf("query %s: exchange endpoint must not set response_path", query.ID)
	}
	if len(endpoint.Params) > 0 {
		return zero, fmt.Errorf("query %s: exchange endpoint must not set params", query.ID)
	}
	if endpoint.Handler != "" || endpoint.Chain != "" {
		return zero, fmt.Errorf("query %s: exchange endpoint must not set handler or chain", query.ID)
	}
	if len(endpoint.CombinedSources) > 0 || len(endpoint.CombinedConfig) > 0 {
		return zero, fmt.Errorf("query %s: exchange endpoint must not set combined_sources or combined_config", query.ID)
	}
	if endpoint.UsdViaID != 0 {
		return zero, fmt.Errorf("query %s: exchange endpoint must not set usd_via_id", query.ID)
	}

	marketID, err := ResolveMarketIDForQueryConfig(query)
	if err != nil {
		return zero, fmt.Errorf("query %s: resolve market id for exchange endpoint: %w", query.ID, err)
	}

	var mc pricefeedtypes.MarketConfig
	var adjustCopy *pricefeedtypes.MarketConfig

	if strings.TrimSpace(endpoint.Ticker) != "" {
		mc, adjustCopy, err = exchangeMarketConfigFromTomlEndpoint(query, endpoint)
		if err != nil {
			return zero, err
		}
	} else {
		if endpoint.AdjustByMarketID != 0 || strings.TrimSpace(endpoint.AdjustTicker) != "" {
			return zero, fmt.Errorf(
				"query %s: exchange endpoint has adjust_by_market_id/adjust_ticker but ticker is empty (set ticker to use TOML legs, or omit adjust fields to use static defaults)",
				query.ID,
			)
		}
		reg, rerr := cachedStaticExchangeRegistry()
		if rerr != nil {
			return zero, fmt.Errorf("query %s: static exchange registry: %w", query.ID, rerr)
		}
		var ok bool
		mc, ok = CanonicalExchangeMarketConfig(reg, endpoint.ExchangeID, marketID)
		if !ok {
			return zero, fmt.Errorf(
				"query %s: no static exchange config for exchange_id=%q and market_id=%d (set ticker in TOML or extend static market params)",
				query.ID,
				endpoint.ExchangeID,
				marketID,
			)
		}
		mc = mc.Copy()
		if mc.AdjustByMarket != nil {
			adjCfg, ok := CanonicalExchangeMarketConfig(reg, endpoint.ExchangeID, *mc.AdjustByMarket)
			if !ok {
				return zero, fmt.Errorf(
					"query %s: no static exchange config for exchange_id=%q and adjust-by market_id=%d",
					query.ID,
					endpoint.ExchangeID,
					*mc.AdjustByMarket,
				)
			}
			c := adjCfg.Copy()
			adjustCopy = &c
		}
	}

	if legRegistry != nil {
		if err := registerExchangeLeg(legRegistry, endpoint.ExchangeID, marketID, mc); err != nil {
			return zero, fmt.Errorf("query %s: %w", query.ID, err)
		}
		if mc.AdjustByMarket != nil {
			if adjustCopy == nil {
				return zero, fmt.Errorf("query %s: internal: missing adjust market config", query.ID)
			}
			if err := registerExchangeLeg(legRegistry, endpoint.ExchangeID, *mc.AdjustByMarket, *adjustCopy); err != nil {
				return zero, fmt.Errorf("query %s: %w", query.ID, err)
			}
		}
	}

	marketLabel := strings.TrimSpace(endpoint.MarketId)
	if marketLabel == "" {
		marketLabel = strconv.FormatUint(uint64(marketID), 10)
	}

	return ExchangeHandler{
		ExchangeID:         endpoint.ExchangeID,
		QueryID:            query.ID,
		MarketId:           marketLabel,
		ChainMarketID:      marketID,
		SourceId:           "exchange",
		UseCache:           endpoint.UseCache,
		MarketConfig:       mc,
		AdjustMarketConfig: adjustCopy,
	}, nil
}

func exchangeMarketConfigFromTomlEndpoint(
	query QueryConfig,
	endpoint EndpointConfig,
) (pricefeedtypes.MarketConfig, *pricefeedtypes.MarketConfig, error) {
	ticker := strings.TrimSpace(endpoint.Ticker)
	if ticker == "" {
		return pricefeedtypes.MarketConfig{}, nil, fmt.Errorf("query %s: exchange ticker is empty", query.ID)
	}
	mc := pricefeedtypes.MarketConfig{
		Ticker: ticker,
		Invert: endpoint.Invert,
	}
	var adjustCopy *pricefeedtypes.MarketConfig
	if endpoint.AdjustByMarketID != 0 {
		if strings.TrimSpace(endpoint.AdjustTicker) == "" {
			return pricefeedtypes.MarketConfig{}, nil, fmt.Errorf(
				"query %s: adjust_by_market_id set but adjust_ticker is empty",
				query.ID,
			)
		}
		adjID := endpoint.AdjustByMarketID
		mc.AdjustByMarket = &adjID
		adj := pricefeedtypes.MarketConfig{Ticker: strings.TrimSpace(endpoint.AdjustTicker)}
		c := adj.Copy()
		adjustCopy = &c
	} else if strings.TrimSpace(endpoint.AdjustTicker) != "" {
		return pricefeedtypes.MarketConfig{}, nil, fmt.Errorf(
			"query %s: adjust_ticker set without adjust_by_market_id",
			query.ID,
		)
	}

	return mc, adjustCopy, nil
}
