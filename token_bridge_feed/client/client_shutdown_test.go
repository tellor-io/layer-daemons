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
	t.Setenv("ETH_RPC_URL_PRIMARY", "")
	t.Setenv("ETH_RPC_URL_FALLBACK", "")

	ctx := context.Background()
	c := StartNewClient(ctx, log.NewNopLogger(), tokenbridgetypes.NewDepositReports(), tokenbridgetipstypes.NewDepositTips())

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
