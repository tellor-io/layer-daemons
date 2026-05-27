package main

import (
	"testing"

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
