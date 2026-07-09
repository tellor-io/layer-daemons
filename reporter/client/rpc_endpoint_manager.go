package client

import (
	"context"
	"fmt"
	"strings"
	"sync"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"

	"cosmossdk.io/log"

	cosmosclient "github.com/cosmos/cosmos-sdk/client"
)

type rpcClientFactory func(endpoint string) (cosmosclient.CometRPC, error)

type rpcEndpointManager struct {
	mu        sync.RWMutex
	endpoints []string
	current   int
	factory   rpcClientFactory
	logger    log.Logger
}

func newRPCEndpointManager(endpoints []string, logger log.Logger) (*rpcEndpointManager, error) {
	return newRPCEndpointManagerWithFactory(endpoints, logger, func(endpoint string) (cosmosclient.CometRPC, error) {
		return rpchttp.New(endpoint, "/websocket")
	})
}

func newRPCEndpointManagerWithFactory(endpoints []string, logger log.Logger, factory rpcClientFactory) (*rpcEndpointManager, error) {
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("no RPC endpoints provided")
	}
	if factory == nil {
		return nil, fmt.Errorf("RPC client factory is required")
	}
	return &rpcEndpointManager{
		endpoints: append([]string(nil), endpoints...),
		factory:   factory,
		logger:    logger,
	}, nil
}

func (m *rpcEndpointManager) currentEndpoint() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.endpoints[m.current]
}

func (m *rpcEndpointManager) endpointCount() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return len(m.endpoints)
}

func (m *rpcEndpointManager) usingPrimary() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.current == 0
}

func (m *rpcEndpointManager) primaryEndpoint() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.endpoints[0]
}

func (m *rpcEndpointManager) currentClient() (cosmosclient.CometRPC, string, error) {
	endpoint := m.currentEndpoint()
	client, err := m.factory(endpoint)
	if err != nil {
		return nil, endpoint, err
	}
	return client, endpoint, nil
}

func (m *rpcEndpointManager) primaryClient() (cosmosclient.CometRPC, string, error) {
	endpoint := m.primaryEndpoint()
	client, err := m.factory(endpoint)
	if err != nil {
		return nil, endpoint, err
	}
	return client, endpoint, nil
}

func (m *rpcEndpointManager) switchToPrimary() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.current == 0 {
		return
	}
	m.current = 0
	m.logger.Info("Switched CometBFT RPC endpoint back to primary", "endpoint", m.endpoints[0])
}

// rpcEndpointClient pairs a CometBFT RPC client with the endpoint it dials.
type rpcEndpointClient struct {
	client   cosmosclient.CometRPC
	endpoint string
}

// allClients returns a client for every configured endpoint. Endpoints whose
// client cannot be constructed are skipped (and logged by the caller via the
// returned errs). Used to broadcast a tx to all registered nodes at once so at
// least one includes it before the unordered-tx timeout expires.
func (m *rpcEndpointManager) allClients() ([]rpcEndpointClient, []string) {
	m.mu.RLock()
	endpoints := append([]string(nil), m.endpoints...)
	m.mu.RUnlock()

	clients := make([]rpcEndpointClient, 0, len(endpoints))
	var errs []string
	for _, endpoint := range endpoints {
		client, err := m.factory(endpoint)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", endpoint, err))
			continue
		}
		clients = append(clients, rpcEndpointClient{client: client, endpoint: endpoint})
	}
	return clients, errs
}

func (m *rpcEndpointManager) nextClient() (cosmosclient.CometRPC, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string
	for offset := 1; offset < len(m.endpoints); offset++ {
		idx := (m.current + offset) % len(m.endpoints)
		endpoint := m.endpoints[idx]
		client, err := m.factory(endpoint)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", endpoint, err))
			continue
		}
		m.current = idx
		m.logger.Warn("Switched CometBFT RPC endpoint", "endpoint", endpoint)
		return client, endpoint, nil
	}

	if len(errs) == 0 {
		return nil, "", fmt.Errorf("no fallback RPC endpoints configured")
	}
	return nil, "", fmt.Errorf("failed to create fallback RPC clients: %s", strings.Join(errs, "; "))
}

func shouldFallbackRPCError(ctx context.Context, err error) bool {
	if err == nil || ctx.Err() != nil {
		return false
	}

	errStr := strings.ToLower(err.Error())
	if strings.Contains(errStr, "not found") ||
		strings.Contains(errStr, "error code:") ||
		strings.Contains(errStr, "insufficient funds") ||
		strings.Contains(errStr, "account sequence mismatch") {
		return false
	}

	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "connection closed") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "deadline exceeded") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "bad gateway") ||
		strings.Contains(errStr, "service unavailable") ||
		strings.Contains(errStr, "gateway timeout")
}
