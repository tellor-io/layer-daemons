package customquery

import (
	"fmt"
	"strings"
)

const (
	FeedTypeMarket      = "market"
	FeedTypeFundamental = "fundamental"
)

// contractHandlerCollateral maps contract handlers to the collateral asset whose
// market median USD price is multiplied by the on-chain conversion ratio.
var contractHandlerCollateral = map[string]string{
	"yieldfi_yusd_handler":  "USDC",
	"yieldfi_vyusd_handler": "USDC",
	"yieldfi_yeth_handler":  "ETH",
	"susdeusd_handler":      "USDE",
	"reth_handler":          "ETH",
	"wsteth_handler":        "stETH",
}

// contractHandlerLabel maps contract handlers to a short label used in TOML comments.
var contractHandlerLabel = map[string]string{
	"yieldfi_yusd_handler":  "yieldfi-yusd contract",
	"yieldfi_vyusd_handler": "yieldfi-vyusd contract",
	"yieldfi_yeth_handler":  "yieldfi-yeth contract",
	"susdeusd_handler":      "susde contract",
	"reth_handler":          "reth contract",
	"wsteth_handler":        "wsteth contract",
}

// combinedHandlerCollateral maps combined handlers to their collateral asset.
var combinedHandlerCollateral = map[string]string{
	"susn_price":    "USN",
	"sfrxusd_price": "FRX",
}

// combinedHandlerLabel maps combined handlers to a short label used in TOML comments.
var combinedHandlerLabel = map[string]string{
	"susn_price":    "susn contract",
	"sfrxusd_price": "sfrxusd contract",
}

// queryTargetAsset overrides inferred display names for specific query IDs.
var queryTargetAsset = map[string]string{
	"187f74d310dc494e6efd928107713d4229cd319c2cf300224de02776090809f1": "SUSN",
}

// ClassifyQueryFeed returns the feed type, target asset symbol, and collateral asset
// (empty for market feeds).
func ClassifyQueryFeed(q *QueryConfig) (feedType, targetAsset, collateral string) {
	targetAsset = inferTargetAsset(q)

	if len(q.Endpoints) == 1 {
		ep := q.Endpoints[0]
		switch ep.EndpointType {
		case "contract":
			if collateral, ok := contractHandlerCollateral[ep.Handler]; ok {
				return FeedTypeFundamental, targetAsset, collateral
			}
		case "combined":
			if collateral, ok := combinedHandlerCollateral[ep.Handler]; ok {
				return FeedTypeFundamental, targetAsset, collateral
			}
		}
	}

	return FeedTypeMarket, targetAsset, ""
}

// GenerateFeedComment returns a concise TOML comment describing the feed.
func GenerateFeedComment(q *QueryConfig) string {
	targetAsset := inferTargetAsset(q)
	if targetAsset == "" {
		targetAsset = "asset"
	}
	pair := targetAsset + "/USD"

	feedType, _, collateral := ClassifyQueryFeed(q)
	switch feedType {
	case FeedTypeFundamental:
		return fundamentalFeedComment(q, pair, collateral)
	default:
		return fmt.Sprintf("%s: (market) median of %d sources.", pair, len(q.Endpoints))
	}
}

func fundamentalFeedComment(q *QueryConfig, pair, collateral string) string {
	ep := q.Endpoints[0]

	switch ep.EndpointType {
	case "contract":
		contractLabel := contractHandlerLabel[ep.Handler]
		if contractLabel == "" {
			contractLabel = ep.Handler
		}
		return fmt.Sprintf(
			"%s: (fundamental) ratio from %s × %s/USD pricefeed cache.",
			pair, contractLabel, collateral,
		)
	case "combined":
		contractLabel := combinedHandlerLabel[ep.Handler]
		if contractLabel == "" {
			contractLabel = ep.Handler
		}
		return fmt.Sprintf(
			"%s: (fundamental) ratio from %s × median %s/USD from %d sources.",
			pair, contractLabel, collateral, countCombinedRPCSources(ep),
		)
	default:
		return fmt.Sprintf("%s: (fundamental).", pair)
	}
}

func countCombinedRPCSources(ep EndpointConfig) int {
	n := 0
	for _, sourceType := range ep.CombinedSources {
		if strings.HasPrefix(sourceType, "rpc:") {
			n++
		}
	}
	return n
}

func inferTargetAsset(q *QueryConfig) string {
	if asset, ok := queryTargetAsset[q.ID]; ok {
		return asset
	}

	for _, ep := range q.Endpoints {
		if ep.MarketId != "" {
			return marketIDToAsset(ep.MarketId)
		}
	}

	if len(q.Endpoints) == 1 {
		ep := q.Endpoints[0]
		switch ep.Handler {
		case "yieldfi_yusd_handler", "yieldfi_vyusd_handler":
			return "yUSD"
		case "yieldfi_yeth_handler":
			return "yETH"
		case "susn_price":
			return "SUSN"
		case "sfrxusd_price":
			return "sFRXUSD"
		}
	}

	return ""
}

func marketIDToAsset(marketID string) string {
	asset, _, ok := strings.Cut(marketID, "-")
	if !ok {
		return marketID
	}
	return asset
}
