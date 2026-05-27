package client

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log"
)

func TestGRPCEndpointManagerUsesFirstEndpoint(t *testing.T) {
	manager, err := newGRPCEndpointManagerWithFactory([]string{"grpc1", "grpc2"}, log.NewNopLogger(), func(ctx context.Context, endpoint string) (*grpc.ClientConn, error) {
		return nil, nil
	})
	require.NoError(t, err)

	_, endpoint, err := manager.currentConnection(context.Background())
	require.NoError(t, err)
	require.Equal(t, "grpc1", endpoint)
}

func TestGRPCEndpointManagerFallbackSkipsFactoryFailures(t *testing.T) {
	manager, err := newGRPCEndpointManagerWithFactory([]string{"grpc1", "bad-grpc", "grpc3"}, log.NewNopLogger(), func(ctx context.Context, endpoint string) (*grpc.ClientConn, error) {
		if endpoint == "bad-grpc" {
			return nil, fmt.Errorf("bad endpoint")
		}
		return nil, nil
	})
	require.NoError(t, err)

	_, endpoint, err := manager.nextConnection(context.Background())
	require.NoError(t, err)
	require.Equal(t, "grpc3", endpoint)
	require.Equal(t, "grpc3", manager.currentEndpoint())
}

func TestGRPCEndpointManagerSwitchesBackToPrimary(t *testing.T) {
	manager, err := newGRPCEndpointManagerWithFactory([]string{"grpc1", "grpc2"}, log.NewNopLogger(), func(ctx context.Context, endpoint string) (*grpc.ClientConn, error) {
		return nil, nil
	})
	require.NoError(t, err)

	_, endpoint, err := manager.nextConnection(context.Background())
	require.NoError(t, err)
	require.Equal(t, "grpc2", endpoint)
	require.False(t, manager.usingPrimary())

	manager.switchToPrimary()

	require.True(t, manager.usingPrimary())
	require.Equal(t, "grpc1", manager.currentEndpoint())
}

func TestShouldFallbackGRPCError(t *testing.T) {
	ctx := context.Background()

	require.True(t, shouldFallbackGRPCError(ctx, status.Error(codes.Unavailable, "connection refused")))
	require.True(t, shouldFallbackGRPCError(ctx, status.Error(codes.DeadlineExceeded, "deadline exceeded")))
	require.False(t, shouldFallbackGRPCError(ctx, status.Error(codes.NotFound, "query not found")))
	require.False(t, shouldFallbackGRPCError(ctx, status.Error(codes.InvalidArgument, "bad request")))
}

func TestShouldFallbackGRPCErrorHonorsCanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	require.False(t, shouldFallbackGRPCError(ctx, status.Error(codes.Unavailable, "connection refused")))
}
