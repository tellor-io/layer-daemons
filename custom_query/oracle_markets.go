package customquery

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/tellor-io/layer-daemons/configs"
	"github.com/tellor-io/layer-daemons/constants"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
)

// OracleMarketEntry is an optional [[markets]] row in the unified oracle TOML.
// When present, the daemon can run without market_params.toml on disk.
type OracleMarketEntry struct {
	ID                 uint32 `toml:"id"`
	Pair               string `toml:"pair"`
	Exponent           int32  `toml:"exponent"`
	MinExchanges       uint32 `toml:"min_exchanges"`
	MinPriceChangePpm  uint32 `toml:"min_price_change_ppm"`
	QueryData          string `toml:"query_data"`
	ExchangeConfigJSON string `toml:"exchange_config_json"`
}

func oracleConfigHasMarketsTable(c Config) bool {
	return len(c.Markets) > 0
}

func buildSyntheticMarketParams(c Config) ([]pricefeedtypes.MarketParam, map[pricefeedtypes.MarketId]pricefeedtypes.Exponent, error) {
	if len(c.Markets) == 0 {
		return nil, nil, fmt.Errorf("internal: buildSyntheticMarketParams called with empty markets")
	}
	byID := make(map[uint32]pricefeedtypes.MarketParam, len(c.Markets))
	exponents := make(map[pricefeedtypes.MarketId]pricefeedtypes.Exponent, len(c.Markets))
	for _, row := range c.Markets {
		if row.ID == 0 {
			return nil, nil, fmt.Errorf("oracle markets: invalid id 0")
		}
		qd := strings.TrimSpace(row.QueryData)
		qd = strings.Trim(qd, `"`)
		if qd == "" {
			return nil, nil, fmt.Errorf("oracle markets: id=%d missing query_data", row.ID)
		}
		if _, err := hex.DecodeString(qd); err != nil {
			return nil, nil, fmt.Errorf("oracle markets: id=%d query_data is not hex: %w", row.ID, err)
		}
		pair := strings.TrimSpace(row.Pair)
		if pair == "" {
			return nil, nil, fmt.Errorf("oracle markets: id=%d missing pair", row.ID)
		}
		minEx := row.MinExchanges
		if minEx == 0 {
			minEx = 1
		}
		ppm := row.MinPriceChangePpm
		if ppm == 0 {
			if sp := constants.StaticMarketParamsConfig[row.ID]; sp != nil && sp.MinPriceChangePpm != 0 {
				ppm = sp.MinPriceChangePpm
			} else {
				ppm = 50
			}
		}
		exJSON := strings.TrimSpace(row.ExchangeConfigJSON)
		if exJSON == "" {
			exJSON = "{}"
		}
		mp := pricefeedtypes.MarketParam{
			Id:                 row.ID,
			Pair:               pair,
			Exponent:           row.Exponent,
			MinExchanges:       minEx,
			MinPriceChangePpm:  ppm,
			ExchangeConfigJson: exJSON,
			QueryData:          qd,
		}
		if _, exists := byID[row.ID]; exists {
			return nil, nil, fmt.Errorf("oracle markets: duplicate id %d", row.ID)
		}
		byID[row.ID] = mp
		exponents[row.ID] = row.Exponent
	}

	// Merge per-query exponent / query_data overrides for reporter rows not in [[markets]].
	for _, q := range c.Queries {
		mid := q.ChainMarketID
		if mid == 0 {
			continue
		}
		if _, ok := byID[mid]; ok {
			if q.Exponent != 0 {
				exponents[mid] = q.Exponent
				cur := byID[mid]
				cur.Exponent = q.Exponent
				byID[mid] = cur
			}
			continue
		}
		qd := strings.TrimSpace(q.QueryData)
		qd = strings.Trim(qd, `"`)
		if qd == "" {
			return nil, nil, fmt.Errorf("query %s: chain_market_id=%d not in [[markets]] and query_data missing", q.ID, mid)
		}
		if _, err := hex.DecodeString(qd); err != nil {
			return nil, nil, fmt.Errorf("query %s: query_data is not hex: %w", q.ID, err)
		}
		pair := strings.TrimSpace(q.Pair)
		if pair == "" {
			if sp := constants.StaticMarketParamsConfig[mid]; sp != nil {
				pair = sp.Pair
			} else {
				return nil, nil, fmt.Errorf("query %s: chain_market_id=%d missing pair (in query or [[markets]])", q.ID, mid)
			}
		}
		exp := q.Exponent
		if exp == 0 {
			if sp := constants.StaticMarketParamsConfig[mid]; sp != nil {
				exp = sp.Exponent
			} else {
				return nil, nil, fmt.Errorf("query %s: chain_market_id=%d missing exponent (in query or static)", q.ID, mid)
			}
		}
		minEx := q.MarketMinExchanges
		if minEx == 0 {
			if sp := constants.StaticMarketParamsConfig[mid]; sp != nil && sp.MinExchanges != 0 {
				minEx = sp.MinExchanges
			} else {
				minEx = 1
			}
		}
		ppm := q.MarketMinPriceChangePpm
		if ppm == 0 {
			if sp := constants.StaticMarketParamsConfig[mid]; sp != nil && sp.MinPriceChangePpm != 0 {
				ppm = sp.MinPriceChangePpm
			} else {
				ppm = 50
			}
		}
		exJSON := strings.TrimSpace(q.MarketExchangeConfigJSON)
		if exJSON == "" {
			exJSON = "{}"
		}
		byID[mid] = pricefeedtypes.MarketParam{
			Id:                 mid,
			Pair:               pair,
			Exponent:           exp,
			MinExchanges:       minEx,
			MinPriceChangePpm:  ppm,
			ExchangeConfigJson: exJSON,
			QueryData:          qd,
		}
		exponents[mid] = exp
	}

	ids := make([]uint32, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]pricefeedtypes.MarketParam, 0, len(ids))
	for _, id := range ids {
		out = append(out, byID[id])
	}
	return out, exponents, nil
}

func mergeExponentOverrides(base map[pricefeedtypes.MarketId]pricefeedtypes.Exponent, c Config) map[pricefeedtypes.MarketId]pricefeedtypes.Exponent {
	if base == nil {
		base = make(map[pricefeedtypes.MarketId]pricefeedtypes.Exponent)
	}
	for _, q := range c.Queries {
		if q.ChainMarketID != 0 && q.Exponent != 0 {
			base[q.ChainMarketID] = q.Exponent
		}
	}
	return base
}

// MarketParamsSliceFromConstants builds a slice from constants.StaticMarketParamsConfig (compiled defaults).
func MarketParamsSliceFromConstants() []pricefeedtypes.MarketParam {
	out := make([]pricefeedtypes.MarketParam, 0, len(constants.StaticMarketParamsConfig))
	for _, p := range constants.StaticMarketParamsConfig {
		if p != nil {
			out = append(out, *p)
		}
	}
	return out
}

// PrepareDaemonMarketParams returns market params for the query resolver and reporter.
// When the TOML contains [[markets]], market_params.toml is not read.
// When it does not, and $homeDir/config/market_params.toml is missing, compiled static params are used (tests / minimal setups).
func PrepareDaemonMarketParams(homeDir string, c Config) ([]pricefeedtypes.MarketParam, map[pricefeedtypes.MarketId]pricefeedtypes.Exponent, error) {
	if oracleConfigHasMarketsTable(c) {
		mp, exp, err := buildSyntheticMarketParams(c)
		if err != nil {
			return nil, nil, err
		}
		return mp, mergeExponentOverrides(exp, c), nil
	}
	path := filepath.Join(homeDir, "config", constants.MarketParamsConfigFileName)
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return MarketParamsSliceFromConstants(), mergeExponentOverrides(nil, c), nil
		}
		return nil, nil, err
	}
	mp := configs.ReadMarketParamsConfigFile(homeDir)
	return mp, mergeExponentOverrides(nil, c), nil
}
