package customquery

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"cosmossdk.io/log"
	"github.com/tellor-io/layer-daemons/constants"
	"github.com/tellor-io/layer-daemons/lib/prices"
	libtime "github.com/tellor-io/layer-daemons/lib/time"
	queryhandler "github.com/tellor-io/layer-daemons/pricefeed/client/queryhandler"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
	servertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
	daemontypes "github.com/tellor-io/layer-daemons/types"
)

var unifiedExponentOverrides map[pricefeedtypes.MarketId]pricefeedtypes.Exponent

// SetUnifiedExponentOverrides configures per-market exponents from the oracle TOML ([[markets]] / query overrides).
// Nil or empty entries fall back to constants.StaticMarketParamsConfig in exponentForMarketID.
func SetUnifiedExponentOverrides(m map[pricefeedtypes.MarketId]pricefeedtypes.Exponent) {
	unifiedExponentOverrides = m
}

func exponentForMarketID(id pricefeedtypes.MarketId) (pricefeedtypes.Exponent, error) {
	if unifiedExponentOverrides != nil {
		if e, ok := unifiedExponentOverrides[id]; ok {
			return e, nil
		}
	}
	p := constants.StaticMarketParamsConfig[id]
	if p == nil {
		return 0, fmt.Errorf("no exponent for market_id=%d (add [[markets]] or keep static market params in sync)", id)
	}
	return p.Exponent, nil
}

func buildMinimalMutableExchangeMarketConfig(h ExchangeHandler) (*pricefeedtypes.MutableExchangeMarketConfig, error) {
	m := map[pricefeedtypes.MarketId]pricefeedtypes.MarketConfig{
		h.ChainMarketID: h.MarketConfig,
	}
	if h.MarketConfig.AdjustByMarket != nil {
		if h.AdjustMarketConfig == nil {
			return nil, fmt.Errorf("internal: exchange handler missing AdjustMarketConfig for adjust-by market")
		}
		m[*h.MarketConfig.AdjustByMarket] = *h.AdjustMarketConfig
	}
	return &pricefeedtypes.MutableExchangeMarketConfig{
		Id:                   h.ExchangeID,
		MarketToMarketConfig: m,
	}, nil
}

func sortedExchangeQueryMarketIds(chainID pricefeedtypes.MarketId, mc pricefeedtypes.MarketConfig) []pricefeedtypes.MarketId {
	if mc.AdjustByMarket == nil {
		return []pricefeedtypes.MarketId{chainID}
	}
	ids := []pricefeedtypes.MarketId{chainID, *mc.AdjustByMarket}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func marketExponentMapForIds(ids []pricefeedtypes.MarketId) (map[pricefeedtypes.MarketId]pricefeedtypes.Exponent, error) {
	out := make(map[pricefeedtypes.MarketId]pricefeedtypes.Exponent, len(ids))
	for _, id := range ids {
		e, err := exponentForMarketID(id)
		if err != nil {
			return nil, err
		}
		out[id] = e
	}
	return out, nil
}

func priceUint64ByMarketFromQueryResults(pts []*pricefeedtypes.MarketPriceTimestamp) map[pricefeedtypes.MarketId]uint64 {
	m := make(map[pricefeedtypes.MarketId]uint64)
	for _, p := range pts {
		if p != nil {
			m[p.MarketId] = p.Price
		}
	}
	return m
}

// convertedUsdFloatFromExchangeRawPrices applies the same invert / multiply / divide rules as price_encoder
// convertPriceUpdate, but uses adjust-by prices from this exchange response (not cross-exchange index).
func convertedUsdRawPriceFromExchangeRawPrices(
	targetMarketID pricefeedtypes.MarketId,
	mc pricefeedtypes.MarketConfig,
	priceByMarket map[pricefeedtypes.MarketId]uint64,
) (uint64, error) {
	targetExp, err := exponentForMarketID(targetMarketID)
	if err != nil {
		return 0, err
	}

	rawTarget, ok := priceByMarket[targetMarketID]
	if !ok || rawTarget == 0 {
		return 0, fmt.Errorf("missing or zero price for market_id=%d", targetMarketID)
	}

	var converted uint64
	if mc.AdjustByMarket == nil {
		if mc.Invert {
			converted = prices.Invert(rawTarget, targetExp)
		} else {
			converted = rawTarget
		}
	} else {
		adjID := *mc.AdjustByMarket
		adjExp, err := exponentForMarketID(adjID)
		if err != nil {
			return 0, fmt.Errorf("adjust_market_id=%d: %w", adjID, err)
		}
		rawAdj, ok := priceByMarket[adjID]
		if !ok || rawAdj == 0 {
			return 0, fmt.Errorf("missing or zero adjust-by price for market_id=%d", adjID)
		}
		if mc.Invert {
			converted = prices.Divide(rawAdj, adjExp, rawTarget, targetExp)
		} else {
			converted = prices.Multiply(rawTarget, targetExp, rawAdj, adjExp)
		}
	}

	return converted, nil
}

func convertedUsdFloatFromExchangeRawPrices(
	targetMarketID pricefeedtypes.MarketId,
	mc pricefeedtypes.MarketConfig,
	priceByMarket map[pricefeedtypes.MarketId]uint64,
) (float64, error) {
	targetExp, err := exponentForMarketID(targetMarketID)
	if err != nil {
		return 0, err
	}
	raw, err := convertedUsdRawPriceFromExchangeRawPrices(targetMarketID, mc, priceByMarket)
	if err != nil {
		return 0, err
	}
	return float64(raw) * math.Pow10(int(targetExp)), nil
}

func exchangeQueryTimeoutMs(exchangeID pricefeedtypes.ExchangeId) uint32 {
	if cfg := constants.StaticExchangeQueryConfig[exchangeID]; cfg != nil && cfg.TimeoutMs > 0 {
		return cfg.TimeoutMs
	}
	return 3000
}

func fetchFromExchangeCacheEndpoint(handler ExchangeHandler, priceCache *pricefeedservertypes.MarketToExchangePrices) Result {
	res := Result{
		EndpointID: "exchange:" + handler.ExchangeID,
		MarketId:   handler.MarketId,
		SourceId:   handler.ExchangeID,
	}
	if priceCache == nil {
		emitExchangeCacheReadTelemetry(handler, false)
		res.Err = fmt.Errorf("exchange cache read failed: price cache is nil")
		return res
	}
	rawPrice, ok := priceCache.GetValidPriceForExchange(handler.ChainMarketID, handler.ExchangeID, time.Now())
	if !ok {
		emitExchangeCacheReadTelemetry(handler, false)
		res.Err = fmt.Errorf(
			"no fresh cached price for market_id=%d exchange_id=%s",
			handler.ChainMarketID,
			handler.ExchangeID,
		)
		return res
	}
	emitExchangeCacheReadTelemetry(handler, true)
	exp, err := exponentForMarketID(handler.ChainMarketID)
	if err != nil {
		res.Err = err
		return res
	}
	res.Value = float64(rawPrice) * math.Pow10(int(exp))
	return res
}

func fetchLiveExchangeRawPrice(ctx context.Context, handler ExchangeHandler, httpClient *http.Client) (rawPrice uint64, err error) {
	defer func() {
		emitExchangeLiveFetchTelemetry(handler, err)
	}()
	exchangeDetails, ok := constants.StaticExchangeDetails[handler.ExchangeID]
	if !ok {
		return 0, fmt.Errorf("unknown exchange_id %q", handler.ExchangeID)
	}

	memc, err := buildMinimalMutableExchangeMarketConfig(handler)
	if err != nil {
		return 0, err
	}

	marketIds := sortedExchangeQueryMarketIds(handler.ChainMarketID, handler.MarketConfig)
	expMap, err := marketExponentMapForIds(marketIds)
	if err != nil {
		return 0, err
	}

	if httpClient == nil {
		httpClient = &http.Client{}
	}
	requestHandler := daemontypes.NewRequestHandlerImpl(httpClient)
	eqh := &queryhandler.ExchangeQueryHandlerImpl{TimeProvider: &libtime.TimeProviderImpl{}}

	qctx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		qctx, cancel = context.WithTimeout(ctx, time.Duration(exchangeQueryTimeoutMs(handler.ExchangeID))*time.Millisecond)
		defer cancel()
	}

	priceRows, unavailable, err := eqh.Query(qctx, &exchangeDetails, memc, marketIds, requestHandler, expMap)
	if err != nil {
		return 0, fmt.Errorf("exchange query %s: %w", handler.ExchangeID, err)
	}
	if u, ok := unavailable[handler.ChainMarketID]; ok {
		return 0, fmt.Errorf("exchange price unavailable for market_id=%d on %s: %w", handler.ChainMarketID, handler.ExchangeID, u)
	}

	byMarket := priceUint64ByMarketFromQueryResults(priceRows)
	raw, err := convertedUsdRawPriceFromExchangeRawPrices(handler.ChainMarketID, handler.MarketConfig, byMarket)
	if err != nil {
		return 0, fmt.Errorf("exchange price conversion: %w", err)
	}
	return raw, nil
}

// fetchFromExchangeEndpoint supports both live fetch (use_cache=false) and cache-backed reads (use_cache=true).
// Live mode issues one exchange HTTP request per source per call (not merged with the cache refresher batch).
func fetchFromExchangeEndpoint(
	ctx context.Context,
	handler ExchangeHandler,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
	httpClient *http.Client,
) Result {
	if handler.UseCache {
		return fetchFromExchangeCacheEndpoint(handler, priceCache)
	}
	raw, err := fetchLiveExchangeRawPrice(ctx, handler, httpClient)
	res := Result{
		EndpointID: "exchange:" + handler.ExchangeID,
		MarketId:   handler.MarketId,
		SourceId:   handler.ExchangeID,
	}
	if err != nil {
		res.Err = err
		return res
	}
	exp, err := exponentForMarketID(handler.ChainMarketID)
	if err != nil {
		res.Err = err
		return res
	}
	res.Value = float64(raw) * math.Pow10(int(exp))
	return res
}

func mergeCachedExchangeHandlersToMemc(
	exchangeID pricefeedtypes.ExchangeId,
	handlers []ExchangeHandler,
) (*pricefeedtypes.MutableExchangeMarketConfig, []pricefeedtypes.MarketId, error) {
	if len(handlers) == 0 {
		return nil, nil, fmt.Errorf("empty handler batch for %s", exchangeID)
	}
	marketMap := make(map[pricefeedtypes.MarketId]pricefeedtypes.MarketConfig)
	for _, h := range handlers {
		if h.ExchangeID != exchangeID {
			return nil, nil, fmt.Errorf("batch contains exchange %s, expected %s", h.ExchangeID, exchangeID)
		}
		if prev, ok := marketMap[h.ChainMarketID]; ok && !prev.Equal(h.MarketConfig) {
			return nil, nil, fmt.Errorf(
				"conflicting MarketConfig for exchange %s primary market_id=%d",
				exchangeID,
				h.ChainMarketID,
			)
		}
		marketMap[h.ChainMarketID] = h.MarketConfig
		if h.MarketConfig.AdjustByMarket != nil {
			if h.AdjustMarketConfig == nil {
				return nil, nil, fmt.Errorf(
					"exchange %s market_id=%d: missing adjust market config",
					exchangeID,
					h.ChainMarketID,
				)
			}
			adj := *h.MarketConfig.AdjustByMarket
			if prev, ok := marketMap[adj]; ok && !prev.Equal(*h.AdjustMarketConfig) {
				return nil, nil, fmt.Errorf(
					"conflicting adjust MarketConfig for exchange %s adjust market_id=%d",
					exchangeID,
					adj,
				)
			}
			marketMap[adj] = *h.AdjustMarketConfig
		}
	}
	ids := make([]pricefeedtypes.MarketId, 0, len(marketMap))
	for id := range marketMap {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	memc := &pricefeedtypes.MutableExchangeMarketConfig{
		Id:                   exchangeID,
		MarketToMarketConfig: marketMap,
	}
	return memc, ids, nil
}

// refreshExchangeVenueCachedPrices runs one ExchangeQueryHandler.Query for the venue, then writes
// cache entries for each handler (use_cache=true refresher path only).
func refreshExchangeVenueCachedPrices(
	ctx context.Context,
	logger log.Logger,
	exchangeID pricefeedtypes.ExchangeId,
	handlers []ExchangeHandler,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
	httpClient *http.Client,
	now time.Time,
) {
	memc, marketIds, err := mergeCachedExchangeHandlersToMemc(exchangeID, handlers)
	if err != nil {
		for _, h := range handlers {
			emitExchangeRefresherTelemetry(h, err)
		}
		logger.Error("Exchange batch prepare failed", "exchange_id", exchangeID, "error", err)
		return
	}
	exchangeDetails, ok := constants.StaticExchangeDetails[exchangeID]
	if !ok {
		err := fmt.Errorf("unknown exchange_id %q", exchangeID)
		for _, h := range handlers {
			emitExchangeRefresherTelemetry(h, err)
		}
		logger.Error("Exchange refresh failed", "exchange_id", exchangeID, "error", err)
		return
	}
	expMap, err := marketExponentMapForIds(marketIds)
	if err != nil {
		for _, h := range handlers {
			emitExchangeRefresherTelemetry(h, err)
		}
		logger.Error("Exchange refresh failed", "exchange_id", exchangeID, "error", err)
		return
	}
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	requestHandler := daemontypes.NewRequestHandlerImpl(httpClient)
	eqh := &queryhandler.ExchangeQueryHandlerImpl{TimeProvider: &libtime.TimeProviderImpl{}}
	qctx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		qctx, cancel = context.WithTimeout(ctx, time.Duration(exchangeQueryTimeoutMs(exchangeID))*time.Millisecond)
		defer cancel()
	}
	priceRows, unavailable, err := eqh.Query(qctx, &exchangeDetails, memc, marketIds, requestHandler, expMap)
	if err != nil {
		for _, h := range handlers {
			emitExchangeRefresherTelemetry(h, err)
		}
		logger.Error("Exchange refresh failed", "exchange_id", exchangeID, "error", err)
		return
	}
	byMarket := priceUint64ByMarketFromQueryResults(priceRows)
	updates := make([]*servertypes.MarketPriceUpdate, 0, len(handlers))
	for _, h := range handlers {
		if u, ok := unavailable[h.ChainMarketID]; ok {
			emitExchangeRefresherTelemetry(h, u)
			logger.Error(
				"Exchange refresh unavailable",
				"exchange_id",
				h.ExchangeID,
				"market_id",
				h.ChainMarketID,
				"reason",
				u,
			)
			continue
		}
		rawPrice, convErr := convertedUsdRawPriceFromExchangeRawPrices(h.ChainMarketID, h.MarketConfig, byMarket)
		if convErr != nil {
			emitExchangeRefresherTelemetry(h, convErr)
			logger.Error(
				"Exchange refresh conversion failed",
				"exchange_id",
				h.ExchangeID,
				"market_id",
				h.ChainMarketID,
				"error",
				convErr,
			)
			continue
		}
		updates = append(updates, &servertypes.MarketPriceUpdate{
			MarketId: h.ChainMarketID,
			ExchangePrices: []*servertypes.ExchangePrice{
				{
					ExchangeId:     h.ExchangeID,
					Price:          rawPrice,
					LastUpdateTime: &now,
				},
			},
		})
		emitExchangeRefresherTelemetry(h, nil)
	}
	if len(updates) > 0 {
		priceCache.UpdatePrices(updates)
	}
}
