package client

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"

	cosmosclient "github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

func TestRPCEndpointManagerAllClientsReturnsEveryEndpoint(t *testing.T) {
	manager, err := newRPCEndpointManagerWithFactory([]string{"node1", "node2", "node3"}, log.NewNopLogger(), func(endpoint string) (cosmosclient.CometRPC, error) {
		return nil, nil
	})
	require.NoError(t, err)

	clients, errs := manager.allClients()
	require.Empty(t, errs)
	require.Len(t, clients, 3)

	got := []string{clients[0].endpoint, clients[1].endpoint, clients[2].endpoint}
	require.Equal(t, []string{"node1", "node2", "node3"}, got)
}

func TestRPCEndpointManagerAllClientsSkipsFactoryFailures(t *testing.T) {
	manager, err := newRPCEndpointManagerWithFactory([]string{"node1", "bad-node", "node3"}, log.NewNopLogger(), func(endpoint string) (cosmosclient.CometRPC, error) {
		if endpoint == "bad-node" {
			return nil, fmt.Errorf("bad endpoint")
		}
		return nil, nil
	})
	require.NoError(t, err)

	clients, errs := manager.allClients()
	require.Len(t, clients, 2)
	require.Len(t, errs, 1)
	require.Contains(t, errs[0], "bad-node")
	require.Equal(t, "node1", clients[0].endpoint)
	require.Equal(t, "node3", clients[1].endpoint)
}

func TestIsAlreadyBroadcastErr(t *testing.T) {
	require.True(t, isAlreadyBroadcastErr(fmt.Errorf("tx already exists in cache")))
	require.True(t, isAlreadyBroadcastErr(fmt.Errorf("RPC error: tx already in mempool")))
	require.True(t, isAlreadyBroadcastErr(fmt.Errorf("Tx Already Exists")))
	require.False(t, isAlreadyBroadcastErr(fmt.Errorf("connection refused")))
	require.False(t, isAlreadyBroadcastErr(nil))
}

func TestIsAlreadyBroadcastCode(t *testing.T) {
	require.True(t, isAlreadyBroadcastCode(&sdk.TxResponse{Code: 19, RawLog: "tx already exists in cache"}))
	require.False(t, isAlreadyBroadcastCode(&sdk.TxResponse{Code: 11, RawLog: "out of gas"}))
	require.False(t, isAlreadyBroadcastCode(nil))
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
