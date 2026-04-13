package customquery

import (
	"context"
	"net/http"
	"time"

	"cosmossdk.io/log"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

func StartExchangeRefresher(
	ctx context.Context,
	logger log.Logger,
	queries map[string]QueryConfig,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
	interval time.Duration,
) error {
	if interval <= 0 {
		return nil
	}
	handlers := exchangeCacheHandlers(queries)
	if len(handlers) == 0 {
		logger.Info("No cache-backed exchange endpoints configured; exchange refresher not started")
		return nil
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		run := func() {
			runCtx, cancel := context.WithTimeout(ctx, interval)
			defer cancel()
			RefreshExchangeEndpointsOnce(runCtx, logger, handlers, priceCache, nil)
		}
		run()
		for {
			select {
			case <-ctx.Done():
				logger.Info("Exchange refresher stopped", "reason", ctx.Err())
				return
			case <-ticker.C:
				run()
			}
		}
	}()

	venueCount := countDistinctExchangeIDs(handlers)
	logger.Info(
		"Started exchange cache refresher (batched per venue)",
		"interval_ms",
		interval.Milliseconds(),
		"endpoint_count",
		len(handlers),
		"venue_count",
		venueCount,
	)
	return nil
}

func countDistinctExchangeIDs(handlers []ExchangeHandler) int {
	seen := make(map[string]struct{}, len(handlers))
	for _, h := range handlers {
		seen[string(h.ExchangeID)] = struct{}{}
	}
	return len(seen)
}

func exchangeCacheHandlers(queries map[string]QueryConfig) []ExchangeHandler {
	handlers := make([]ExchangeHandler, 0)
	for _, query := range queries {
		for _, h := range query.ExchangeReaders {
			if h.UseCache {
				handlers = append(handlers, h)
			}
		}
	}
	return handlers
}

func groupExchangeHandlersByVenue(handlers []ExchangeHandler) map[string][]ExchangeHandler {
	out := make(map[string][]ExchangeHandler)
	for _, h := range handlers {
		k := string(h.ExchangeID)
		out[k] = append(out[k], h)
	}
	return out
}

func RefreshExchangeEndpointsOnce(
	ctx context.Context,
	logger log.Logger,
	handlers []ExchangeHandler,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
	httpClient *http.Client,
) {
	now := time.Now()
	byVenue := groupExchangeHandlersByVenue(handlers)
	for _, group := range byVenue {
		if len(group) == 0 {
			continue
		}
		refreshExchangeVenueCachedPrices(ctx, logger, group[0].ExchangeID, group, priceCache, httpClient, now)
	}
}
