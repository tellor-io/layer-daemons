package main

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestValidateReachableChainIDsAllowsMatchingEndpoints(t *testing.T) {
	err := validateReachableChainIDs("gRPC", []detectedEndpointChainID{
		{endpoint: "node1:9090", chainID: "tellor-1"},
		{endpoint: "node2:9090", chainID: "tellor-1"},
	})
	require.NoError(t, err)
}

func TestValidateReachableChainIDsRejectsMismatchedEndpoints(t *testing.T) {
	err := validateReachableChainIDs("node RPC", []detectedEndpointChainID{
		{endpoint: "http://node1:26657", chainID: "tellor-1"},
		{endpoint: "http://node2:26657", chainID: "layertest-5"},
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "disagree on chain ID")
}

func TestDetectEndpointChainIDsUsesPerEndpointTimeout(t *testing.T) {
	originalTimeout := chainIDEndpointDetectTimeout
	chainIDEndpointDetectTimeout = time.Millisecond
	t.Cleanup(func() {
		chainIDEndpointDetectTimeout = originalTimeout
	})

	detected, err := detectEndpointChainIDs(context.Background(), "test", []string{"hung", "healthy"}, func(ctx context.Context, endpoint string) (string, error) {
		if endpoint == "hung" {
			<-ctx.Done()
			return "", ctx.Err()
		}
		return "tellor-1", nil
	})

	require.NoError(t, err)
	require.Equal(t, []detectedEndpointChainID{{endpoint: "healthy", chainID: "tellor-1"}}, detected)
}
