package customquery

import (
	"context"
	"strings"
	"time"
)

const (
	QueryTypeSpotPrice     = "spot_price"
	QueryTypeBridgeDeposit = "bridge_deposit"

	DefaultFetchTimeout        = 1500 * time.Millisecond
	DefaultAggregationBuffer     = 100 * time.Millisecond
	DefaultPostFetchReserve      = 2000 * time.Millisecond
	DefaultPerSourceTimeout      = 700 * time.Millisecond
	DefaultPerSourceRetryTimeout = 500 * time.Millisecond
	DefaultRetryDelay            = 75 * time.Millisecond
	DefaultMaxSourceRetries      = 1
	MinRetryBudget               = 400 * time.Millisecond
)

// TimeoutDefaults holds fetch timeout settings for a query category.
type TimeoutDefaults struct {
	FetchTimeoutMs      int `toml:"fetch_timeout_ms"`
	PerSourceTimeoutMs  int `toml:"per_source_timeout_ms"`
	MaxSourceRetries    int `toml:"max_source_retries"`
	PostFetchReserveMs  int `toml:"post_fetch_reserve_ms"`
	AggregationBufferMs int `toml:"aggregation_buffer_ms"`
}

var StaticTimeoutDefaultsByQueryType = map[string]TimeoutDefaults{
	QueryTypeSpotPrice: {
		FetchTimeoutMs:      1500,
		PerSourceTimeoutMs:  700,
		MaxSourceRetries:    1,
		PostFetchReserveMs:  2000,
		AggregationBufferMs: 100,
	},
	QueryTypeBridgeDeposit: {
		FetchTimeoutMs:      15000,
		PerSourceTimeoutMs:  5000,
		MaxSourceRetries:    3,
		PostFetchReserveMs:  10000,
		AggregationBufferMs: 100,
	},
}

// MapOnChainQueryType maps Tellor query-type strings to custom_query default keys.
func MapOnChainQueryType(onChainType string) string {
	switch strings.ToLower(onChainType) {
	case "spotprice":
		return QueryTypeSpotPrice
	case "trbbridgev2", "bridge_deposit", "bridgedeposit":
		return QueryTypeBridgeDeposit
	default:
		return QueryTypeSpotPrice
	}
}

func mergeTimeoutDefaults(base, override TimeoutDefaults) TimeoutDefaults {
	if override.FetchTimeoutMs > 0 {
		base.FetchTimeoutMs = override.FetchTimeoutMs
	}
	if override.PerSourceTimeoutMs > 0 {
		base.PerSourceTimeoutMs = override.PerSourceTimeoutMs
	}
	if override.MaxSourceRetries > 0 {
		base.MaxSourceRetries = override.MaxSourceRetries
	}
	if override.PostFetchReserveMs > 0 {
		base.PostFetchReserveMs = override.PostFetchReserveMs
	}
	if override.AggregationBufferMs > 0 {
		base.AggregationBufferMs = override.AggregationBufferMs
	}
	return base
}

func mergedConfigDefaults(configDefaults map[string]TimeoutDefaults) map[string]TimeoutDefaults {
	merged := make(map[string]TimeoutDefaults, len(StaticTimeoutDefaultsByQueryType))
	for queryType, defaults := range StaticTimeoutDefaultsByQueryType {
		merged[queryType] = defaults
	}
	for queryType, overrides := range configDefaults {
		base := merged[queryType]
		merged[queryType] = mergeTimeoutDefaults(base, overrides)
	}
	return merged
}

func queryTypeOf(query QueryConfig) string {
	if query.QueryType != "" {
		return query.QueryType
	}
	return QueryTypeSpotPrice
}

func typeDefaults(query QueryConfig, defaults map[string]TimeoutDefaults) TimeoutDefaults {
	if td, ok := defaults[queryTypeOf(query)]; ok {
		return td
	}
	return StaticTimeoutDefaultsByQueryType[QueryTypeSpotPrice]
}

// ResolveReaderTimeouts fills per-source timeout fields used when constructing readers.
func ResolveReaderTimeouts(query QueryConfig, defaults map[string]TimeoutDefaults) QueryConfig {
	td := typeDefaults(query, defaults)

	if query.PerSourceTimeoutMs == 0 {
		query.PerSourceTimeoutMs = td.PerSourceTimeoutMs
	}
	if query.MaxSourceRetries == 0 {
		query.MaxSourceRetries = td.MaxSourceRetries
	}
	return query
}

// ResolveFetchTimeouts fills fetch-collection timeout fields at request time.
func ResolveFetchTimeouts(query QueryConfig, defaults map[string]TimeoutDefaults) QueryConfig {
	td := typeDefaults(query, defaults)

	if query.FetchTimeoutMs == 0 {
		query.FetchTimeoutMs = td.FetchTimeoutMs
	}
	if query.PostFetchReserveMs == 0 {
		query.PostFetchReserveMs = td.PostFetchReserveMs
	}
	if query.AggregationBufferMs == 0 {
		query.AggregationBufferMs = td.AggregationBufferMs
	}
	return query
}

// ResolveQueryTimeouts fills all zero timeout fields from query-type defaults.
func ResolveQueryTimeouts(query QueryConfig, defaults map[string]TimeoutDefaults) QueryConfig {
	query = ResolveReaderTimeouts(query, defaults)
	query = ResolveFetchTimeouts(query, defaults)
	return query
}

// WithRuntimeQueryType returns a copy of query with QueryType set when unset.
func WithRuntimeQueryType(query QueryConfig, onChainType string) QueryConfig {
	if query.QueryType != "" {
		return query
	}
	query.QueryType = MapOnChainQueryType(onChainType)
	return query
}

func fetchTimeout(query QueryConfig) time.Duration {
	if query.FetchTimeoutMs > 0 {
		return time.Duration(query.FetchTimeoutMs) * time.Millisecond
	}
	return DefaultFetchTimeout
}

func perSourceTimeout(query QueryConfig) time.Duration {
	if query.PerSourceTimeoutMs > 0 {
		return time.Duration(query.PerSourceTimeoutMs) * time.Millisecond
	}
	return DefaultPerSourceTimeout
}

func maxSourceRetries(query QueryConfig) int {
	if query.MaxSourceRetries > 0 {
		return query.MaxSourceRetries
	}
	return DefaultMaxSourceRetries
}

func perSourceTimeoutMs(query QueryConfig) int {
	return int(perSourceTimeout(query).Milliseconds())
}

func aggregationBuffer(query QueryConfig) time.Duration {
	if query.AggregationBufferMs > 0 {
		return time.Duration(query.AggregationBufferMs) * time.Millisecond
	}
	return DefaultAggregationBuffer
}

func postFetchReserve(query QueryConfig) time.Duration {
	if query.PostFetchReserveMs > 0 {
		return time.Duration(query.PostFetchReserveMs) * time.Millisecond
	}
	return DefaultPostFetchReserve
}

func collectionDeadline(ctx context.Context, query QueryConfig) time.Time {
	deadline := time.Now().Add(fetchTimeout(query))

	if ctxDeadline, ok := ctx.Deadline(); ok {
		safeDeadline := ctxDeadline.Add(-aggregationBuffer(query) - postFetchReserve(query))
		if safeDeadline.Before(deadline) {
			deadline = safeDeadline
		}
	}
	return deadline
}

func hasRetryBudget(ctx context.Context) bool {
	if deadline, ok := ctx.Deadline(); ok {
		return time.Until(deadline) >= MinRetryBudget
	}
	return true
}
