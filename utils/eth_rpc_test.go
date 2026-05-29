package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseEndpointList(t *testing.T) {
	endpoints, err := ParseEndpointList(" https://primary.example ,https://fallback.example ")
	require.NoError(t, err)
	require.Equal(t, []string{"https://primary.example", "https://fallback.example"}, endpoints)
}

func TestParseEndpointListRejectsEmptyEntries(t *testing.T) {
	_, err := ParseEndpointList("https://primary.example,,https://fallback.example")
	require.ErrorContains(t, err, "empty entry")
}

func TestBridgeChainRPCNodesFromEnv(t *testing.T) {
	t.Setenv(EnvBridgeChainRPCNodes, "https://primary.example, https://fallback.example")

	endpoints, err := BridgeChainRPCNodesFromEnv()
	require.NoError(t, err)
	require.Equal(t, []string{"https://primary.example", "https://fallback.example"}, endpoints)
}

func TestBridgeChainRPCNodesFromEnvRequiresValue(t *testing.T) {
	t.Setenv(EnvBridgeChainRPCNodes, "")

	_, err := BridgeChainRPCNodesFromEnv()
	require.ErrorContains(t, err, EnvBridgeChainRPCNodes+" not set")
}

func TestETHMainnetRPCNodesFromEnv(t *testing.T) {
	t.Setenv(EnvETHMainnetRPCNodes, "https://mainnet-primary.example, https://mainnet-fallback.example")

	endpoints, err := ETHMainnetRPCNodesFromEnv()
	require.NoError(t, err)
	require.Equal(t, []string{"https://mainnet-primary.example", "https://mainnet-fallback.example"}, endpoints)
}

func TestETHMainnetRPCNodesFromEnvRequiresValue(t *testing.T) {
	t.Setenv(EnvETHMainnetRPCNodes, "")

	_, err := ETHMainnetRPCNodesFromEnv()
	require.ErrorContains(t, err, EnvETHMainnetRPCNodes+" not set")
}
