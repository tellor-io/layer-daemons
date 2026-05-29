package client

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	tokenbridgetypes "github.com/tellor-io/layer-daemons/server/types/token_bridge"
	tokenbridgetipstypes "github.com/tellor-io/layer-daemons/server/types/token_bridge_tips"

	"cosmossdk.io/log"
)

func TestWaitForContractInitialized_ReturnsWhenContextAlreadyCanceled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := waitForContractInitialized(ctx, log.NewNopLogger(), time.Hour, func() (bool, error) {
		return false, nil
	})
	require.ErrorIs(t, err, context.Canceled)
}

func TestWaitForContractInitialized_ReturnsNilWhenInitialized(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	var calls atomic.Int32
	err := waitForContractInitialized(ctx, log.NewNopLogger(), time.Millisecond, func() (bool, error) {
		calls.Add(1)
		return true, nil
	})
	require.NoError(t, err)
	require.Equal(t, int32(1), calls.Load())
}

func TestWaitForContractInitialized_ReturnsWhenContextCanceledDuringRetryDelay(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		errCh <- waitForContractInitialized(ctx, log.NewNopLogger(), 500*time.Millisecond, func() (bool, error) {
			return false, errors.New("transient")
		})
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		require.ErrorIs(t, err, context.Canceled)
	case <-time.After(2 * time.Second):
		t.Fatal("waitForContractInitialized did not return after cancel during retry sleep")
	}
}

// TestStartNewClient_StopUnblocksWhenEthereumEnvIncomplete covers the path where
// initializeClientsAndContracts fails before daemonStartup is signaled; Stop() must
// not block forever on daemonStartup.Wait().
func TestStartNewClient_StopUnblocksWhenEthereumEnvIncomplete(t *testing.T) {
	t.Setenv("BRIDGE_CHAIN_RPC_NODES", "")

	ctx := context.Background()
	c := StartNewClient(ctx, log.NewNopLogger(), tokenbridgetypes.NewDepositReports(), tokenbridgetipstypes.NewDepositTips(), "tellor-1")

	stopDone := make(chan struct{})
	go func() {
		c.Stop()
		close(stopDone)
	}()

	select {
	case <-stopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() blocked after init failure — daemonStartup / waitgroup regression")
	}
}

func TestGetBridgeChainRpcUrlsUsesBridgeChainRPCNodes(t *testing.T) {
	t.Setenv("BRIDGE_CHAIN_RPC_NODES", "https://primary.example, https://fallback.example")

	c := newClient(log.NewNopLogger(), tokenbridgetypes.NewDepositReports(), tokenbridgetipstypes.NewDepositTips(), "tellor-1")
	urls, err := c.getBridgeChainRpcUrls()

	require.NoError(t, err)
	require.Equal(t, []string{"https://primary.example", "https://fallback.example"}, urls)
}

func TestGetBridgeChainRpcUrlsRejectsInvalidEndpointList(t *testing.T) {
	t.Setenv("BRIDGE_CHAIN_RPC_NODES", "https://primary.example,,https://fallback.example")

	c := newClient(log.NewNopLogger(), tokenbridgetypes.NewDepositReports(), tokenbridgetipstypes.NewDepositTips(), "tellor-1")
	_, err := c.getBridgeChainRpcUrls()

	require.ErrorContains(t, err, "BRIDGE_CHAIN_RPC_NODES")
	require.ErrorContains(t, err, "empty entry")
}

func TestGetTokenBridgeContractAddress_UsesHardCodedMainnetContract(t *testing.T) {
	c := newClient(log.NewNopLogger(), tokenbridgetypes.NewDepositReports(), tokenbridgetipstypes.NewDepositTips(), "tellor-1")
	address, err := c.getTokenBridgeContractAddress()

	require.NoError(t, err)
	require.Equal(t, "0x6ec401744008f4B018Ed9A36f76e6629799Ee50E", address.Hex())
}

func TestGetTokenBridgeContractAddress_UsesFallbackForCustomChain(t *testing.T) {
	t.Setenv("TOKEN_BRIDGE_TEST_CONTRACT", "0x55355157703A44f7516FBB831333317E98944e32")

	c := newClient(log.NewNopLogger(), tokenbridgetypes.NewDepositReports(), tokenbridgetipstypes.NewDepositTips(), "localnet")
	address, err := c.getTokenBridgeContractAddress()

	require.NoError(t, err)
	require.Equal(t, "0x55355157703A44f7516FBB831333317E98944e32", address.Hex())
}
