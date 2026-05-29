package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseGRPCEndpoints(t *testing.T) {
	endpoints, err := parseGRPCEndpoints(" node1:9090,node2:9090 ")
	require.NoError(t, err)
	require.Equal(t, []string{"node1:9090", "node2:9090"}, endpoints)
}

func TestParseRPCEndpoints(t *testing.T) {
	endpoints, err := parseRPCEndpoints(" https://node1:26657,https://node2:26657 ")
	require.NoError(t, err)
	require.Equal(t, []string{"https://node1:26657", "https://node2:26657"}, endpoints)
}

func TestParseGRPCEndpointsRejectsEmptyEntries(t *testing.T) {
	_, err := parseGRPCEndpoints("node1,,node2")
	require.Error(t, err)

	_, err = parseGRPCEndpoints(" ")
	require.Error(t, err)
}

func TestRPCEndpointsFromEnvOrFlagPrefersEnv(t *testing.T) {
	t.Setenv(envRPCNodes, "env-node1,env-node2")

	cfg, err := rpcEndpointsFromEnvOrFlag("flag-node")
	require.NoError(t, err)
	require.Equal(t, envRPCNodes, cfg.Source)
	require.Equal(t, []string{"env-node1", "env-node2"}, cfg.Endpoints)
}

func TestGRPCEndpointsFromEnvOrFlagFallsBackToFlag(t *testing.T) {
	cfg, err := grpcEndpointsFromEnvOrFlag("flag-node")
	require.NoError(t, err)
	require.Equal(t, "--grpc", cfg.Source)
	require.Equal(t, []string{"flag-node"}, cfg.Endpoints)
}

func TestMoveEndpointToFront(t *testing.T) {
	endpoints := []string{"node1", "node2", "node3"}

	reordered := moveEndpointToFront(endpoints, "node2")

	require.Equal(t, []string{"node2", "node1", "node3"}, reordered)
	require.Equal(t, []string{"node1", "node2", "node3"}, endpoints)
}
