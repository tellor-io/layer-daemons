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

func TestETHRPCNodesFromEnv(t *testing.T) {
	t.Setenv(EnvETHRPCNodes, "https://primary.example, https://fallback.example")

	endpoints, err := ETHRPCNodesFromEnv()
	require.NoError(t, err)
	require.Equal(t, []string{"https://primary.example", "https://fallback.example"}, endpoints)
}

func TestETHRPCNodesFromEnvRequiresValue(t *testing.T) {
	t.Setenv(EnvETHRPCNodes, "")

	_, err := ETHRPCNodesFromEnv()
	require.ErrorContains(t, err, EnvETHRPCNodes+" not set")
}
