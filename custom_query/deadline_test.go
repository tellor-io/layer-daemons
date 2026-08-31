package customquery

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestResolveQueryTimeoutsSpotPriceDefaults(t *testing.T) {
	resolved := ResolveQueryTimeouts(QueryConfig{}, mergedConfigDefaults(nil))
	require.Equal(t, 1500, resolved.FetchTimeoutMs)
	require.Equal(t, 700, resolved.PerSourceTimeoutMs)
	require.Equal(t, 1, resolved.MaxSourceRetries)
	require.Equal(t, 2000, resolved.PostFetchReserveMs)
}

func TestResolveReaderTimeoutsBridgeDepositDefaults(t *testing.T) {
	resolved := ResolveReaderTimeouts(QueryConfig{QueryType: QueryTypeBridgeDeposit}, mergedConfigDefaults(nil))
	require.Equal(t, 5000, resolved.PerSourceTimeoutMs)
	require.Equal(t, 3, resolved.MaxSourceRetries)
	require.Equal(t, 0, resolved.FetchTimeoutMs)
}

func TestResolveFetchTimeoutsBridgeDepositDefaults(t *testing.T) {
	resolved := ResolveFetchTimeouts(QueryConfig{QueryType: QueryTypeBridgeDeposit}, mergedConfigDefaults(nil))
	require.Equal(t, 15000, resolved.FetchTimeoutMs)
	require.Equal(t, 10000, resolved.PostFetchReserveMs)
	require.Equal(t, 0, resolved.PerSourceTimeoutMs)
}

func TestResolveQueryTimeoutsBridgeDepositDefaults(t *testing.T) {
	resolved := ResolveQueryTimeouts(QueryConfig{QueryType: QueryTypeBridgeDeposit}, mergedConfigDefaults(nil))
	require.Equal(t, 15000, resolved.FetchTimeoutMs)
	require.Equal(t, 5000, resolved.PerSourceTimeoutMs)
	require.Equal(t, 3, resolved.MaxSourceRetries)
	require.Equal(t, 10000, resolved.PostFetchReserveMs)
}

func TestResolveQueryTimeoutsPerQueryOverride(t *testing.T) {
	resolved := ResolveQueryTimeouts(QueryConfig{
		QueryType:      QueryTypeBridgeDeposit,
		FetchTimeoutMs: 8000,
	}, mergedConfigDefaults(nil))
	require.Equal(t, 8000, resolved.FetchTimeoutMs)
	require.Equal(t, 5000, resolved.PerSourceTimeoutMs)
}

func TestResolveQueryTimeoutsConfigOverride(t *testing.T) {
	defaults := mergedConfigDefaults(map[string]TimeoutDefaults{
		QueryTypeSpotPrice: {FetchTimeoutMs: 2000, PerSourceTimeoutMs: 900},
	})
	resolved := ResolveQueryTimeouts(QueryConfig{}, defaults)
	require.Equal(t, 2000, resolved.FetchTimeoutMs)
	require.Equal(t, 900, resolved.PerSourceTimeoutMs)
}

func TestMapOnChainQueryType(t *testing.T) {
	require.Equal(t, QueryTypeSpotPrice, MapOnChainQueryType("SpotPrice"))
	require.Equal(t, QueryTypeBridgeDeposit, MapOnChainQueryType("TRBBridgeV2"))
}

func TestCollectionDeadlineUsesQueryTypePostFetchReserve(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	spot := collectionDeadline(ctx, ResolveQueryTimeouts(QueryConfig{}, mergedConfigDefaults(nil)))
	bridge := collectionDeadline(ctx, ResolveQueryTimeouts(QueryConfig{QueryType: QueryTypeBridgeDeposit}, mergedConfigDefaults(nil)))

	require.True(t, bridge.After(spot))
}

func TestWithRuntimeQueryType(t *testing.T) {
	query := WithRuntimeQueryType(QueryConfig{}, "SpotPrice")
	require.Equal(t, QueryTypeSpotPrice, query.QueryType)

	explicit := WithRuntimeQueryType(QueryConfig{QueryType: QueryTypeBridgeDeposit}, "SpotPrice")
	require.Equal(t, QueryTypeBridgeDeposit, explicit.QueryType)
}
