package client

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	cosmosclient "github.com/cosmos/cosmos-sdk/client"
)

func TestRPCEndpointManagerUsesFirstEndpoint(t *testing.T) {
	manager, err := newRPCEndpointManagerWithFactory([]string{"node1", "node2"}, log.NewNopLogger(), func(endpoint string) (cosmosclient.CometRPC, error) {
		return nil, nil
	})
	require.NoError(t, err)

	_, endpoint, err := manager.currentClient()
	require.NoError(t, err)
	require.Equal(t, "node1", endpoint)
}

func TestRPCEndpointManagerFallbackSkipsFactoryFailures(t *testing.T) {
	manager, err := newRPCEndpointManagerWithFactory([]string{"node1", "bad-node", "node3"}, log.NewNopLogger(), func(endpoint string) (cosmosclient.CometRPC, error) {
		if endpoint == "bad-node" {
			return nil, fmt.Errorf("bad endpoint")
		}
		return nil, nil
	})
	require.NoError(t, err)

	_, endpoint, err := manager.nextClient()
	require.NoError(t, err)
	require.Equal(t, "node3", endpoint)
	require.Equal(t, "node3", manager.currentEndpoint())
}

func TestRPCEndpointManagerSwitchesBackToPrimary(t *testing.T) {
	manager, err := newRPCEndpointManagerWithFactory([]string{"node1", "node2"}, log.NewNopLogger(), func(endpoint string) (cosmosclient.CometRPC, error) {
		return nil, nil
	})
	require.NoError(t, err)

	_, endpoint, err := manager.nextClient()
	require.NoError(t, err)
	require.Equal(t, "node2", endpoint)
	require.False(t, manager.usingPrimary())

	manager.switchToPrimary()

	require.True(t, manager.usingPrimary())
	require.Equal(t, "node1", manager.currentEndpoint())
}

func TestShouldFallbackRPCError(t *testing.T) {
	ctx := context.Background()

	require.True(t, shouldFallbackRPCError(ctx, fmt.Errorf("connection refused")))
	require.True(t, shouldFallbackRPCError(ctx, fmt.Errorf("context deadline exceeded")))
	require.False(t, shouldFallbackRPCError(ctx, fmt.Errorf("tx not found")))
	require.False(t, shouldFallbackRPCError(ctx, fmt.Errorf("error code: '11' msg: 'out of gas'")))
}

func TestShouldFallbackRPCErrorHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.False(t, shouldFallbackRPCError(ctx, fmt.Errorf("connection refused")))
}
