package client

import (
	"context"
	"fmt"
	"strings"
	"sync"

	daemontypes "github.com/tellor-io/layer-daemons/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"cosmossdk.io/log"
)

type grpcConnectionFactory func(ctx context.Context, endpoint string) (*grpc.ClientConn, error)

type grpcEndpointManager struct {
	mu        sync.RWMutex
	endpoints []string
	current   int
	factory   grpcConnectionFactory
	logger    log.Logger
}

func newGRPCEndpointManager(endpoints []string, logger log.Logger, grpcClient daemontypes.GrpcClient) (*grpcEndpointManager, error) {
	if grpcClient == nil {
		return nil, fmt.Errorf("gRPC client is required")
	}
	return newGRPCEndpointManagerWithFactory(endpoints, logger, grpcClient.NewTcpConnection)
}

func newGRPCEndpointManagerWithFactory(endpoints []string, logger log.Logger, factory grpcConnectionFactory) (*grpcEndpointManager, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no gRPC endpoints provided")
	}
	if factory == nil {
		return nil, fmt.Errorf("gRPC connection factory is required")
	}
	return &grpcEndpointManager{
		endpoints: append([]string(nil), endpoints...),
		factory:   factory,
		logger:    logger,
	}, nil
}

func (m *grpcEndpointManager) currentEndpoint() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.endpoints[m.current]
}

func (m *grpcEndpointManager) endpointCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.endpoints)
}

func (m *grpcEndpointManager) usingPrimary() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current == 0
}

func (m *grpcEndpointManager) primaryEndpoint() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.endpoints[0]
}

func (m *grpcEndpointManager) currentConnection(ctx context.Context) (*grpc.ClientConn, string, error) {
	endpoint := m.currentEndpoint()
	conn, err := m.factory(ctx, endpoint)
	if err != nil {
		return nil, endpoint, err
	}
	return conn, endpoint, nil
}

func (m *grpcEndpointManager) primaryConnection(ctx context.Context) (*grpc.ClientConn, string, error) {
	endpoint := m.primaryEndpoint()
	conn, err := m.factory(ctx, endpoint)
	if err != nil {
		return nil, endpoint, err
	}
	return conn, endpoint, nil
}

func (m *grpcEndpointManager) switchToPrimary() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current == 0 {
		return
	}
	m.current = 0
	m.logger.Info("Switched Cosmos gRPC endpoint back to primary", "endpoint", m.endpoints[0])
}

func (m *grpcEndpointManager) nextConnection(ctx context.Context) (*grpc.ClientConn, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	for offset := 1; offset < len(m.endpoints); offset++ {
		idx := (m.current + offset) % len(m.endpoints)
		endpoint := m.endpoints[idx]
		conn, err := m.factory(ctx, endpoint)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", endpoint, err))
			continue
		}
		m.current = idx
		m.logger.Warn("Switched Cosmos gRPC endpoint", "endpoint", endpoint)
		return conn, endpoint, nil
	}

	if len(errs) == 0 {
		return nil, "", fmt.Errorf("no fallback gRPC endpoints configured")
	}
	return nil, "", fmt.Errorf("failed to create fallback gRPC connections: %s", strings.Join(errs, "; "))
}

func shouldFallbackGRPCError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}

	switch status.Code(err) {
	case codes.Unavailable, codes.DeadlineExceeded:
		return true
	case codes.OK, codes.Canceled, codes.InvalidArgument, codes.NotFound, codes.AlreadyExists,
		codes.PermissionDenied, codes.Unauthenticated, codes.FailedPrecondition, codes.OutOfRange,
		codes.Unimplemented:
		return false
	}

	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "connection closed") ||
		strings.Contains(errStr, "transport is closing") ||
		strings.Contains(errStr, "transport: error while dialing") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "bad gateway") ||
		strings.Contains(errStr, "service unavailable") ||
		strings.Contains(errStr, "gateway timeout")
}
