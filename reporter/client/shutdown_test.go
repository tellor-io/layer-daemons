package client

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tellor-io/layer-daemons/flags"

	"cosmossdk.io/log"
)

// TestTrySend_PreventsPanicOnClosedChannel tests that trySend with a canceled
// context exits safely.
func TestTrySend_PreventsPanicOnClosedChannel(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001uloya")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// trySend must not panic
	ok := c.trySend(ctx, TxChannelInfo{isBridge: false})
	require.False(t, ok, "trySend should return false when context is canceled")
}

// TestStopThenBroadcastExitsCleanly tests the shutdown sequence:
// BroadcastTxMsgToChain is running, then Stop() closes txChan, and
// BroadcastTxMsgToChain exits without panic or hang.
func TestStopThenBroadcastExitsCleanly(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001uloya")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	broadcastDone := make(chan struct{})
	go func() {
		defer close(broadcastDone)
		c.BroadcastTxMsgToChain(ctx)
	}()

	// Give BroadcastTxMsgToChain time to enter its select loop
	time.Sleep(10 * time.Millisecond)

	// This is what Stop() does
	close(c.txChan)

	select {
	case <-broadcastDone:
		// BroadcastTxMsgToChain exited cleanly
	case <-time.After(2 * time.Second):
		t.Fatal("BroadcastTxMsgToChain hung after txChan was closed")
	}
}

// TestStartReporterDaemonTaskLoop_ExitsOnCancelledContext verifies that the
// startup busy-loop in StartReporterDaemonTaskLoop respects context cancellation
// rather than looping forever.
func TestStartReporterDaemonTaskLoop_ExitsOnCancelledContext(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var wg sync.WaitGroup
	done := make(chan struct{})
	go func() {
		defer close(done)

		StartReporterDaemonTaskLoop(c, ctx, flags.DaemonFlags{}, &wg)
	}()

	select {
	case <-done:

	case <-time.After(3 * time.Second):
		t.Fatal("StartReporterDaemonTaskLoop did not exit with canceled context — Bug #3 regression")
	}
}

// TestConcurrentTrySendDuringShutdown simulates the real shutdown race:
// multiple monitor goroutines try to send to txChan while the context is
// being canceled. None should panic.
func TestConcurrentTrySendDuringShutdown(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	ctx, cancel := context.WithCancel(context.Background())

	// Start a consumer so some sends might succeed before cancellation
	consumerDone := make(chan struct{})
	go func() {
		defer close(consumerDone)
		for range c.txChan {
		}
	}()

	var senderWg sync.WaitGroup
	for i := 0; i < 10; {
		i++
		senderWg.Add(1)
		go func() {
			defer senderWg.Done()
			// Each goroutine tries to send,some will succeed, some will
			// hit the canceled context. None should panic.
			c.trySend(ctx, TxChannelInfo{isBridge: false})
		}()
	}

	// Cancel context while senders are sending
	cancel()

	sendersDone := make(chan struct{})
	go func() {
		senderWg.Wait()
		close(c.txChan) // clean up
		close(sendersDone)
	}()

	select {
	case <-sendersDone:
		<-consumerDone
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent trySend goroutines did not complete during shutdown")
	}
}

// TestFullShutdownLifecycle tests the complete shutdown flow end-to-end:
// context cancel -> monitors exit -> Stop() closes txChan -> BroadcastTxMsgToChain exits -> wg completes.
func TestFullShutdownLifecycle(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001uloya")
	ctx, cancel := context.WithCancel(context.Background())

	// Start BroadcastTxMsgToChain and a monitor, tracking them with the client's WaitGroup
	c.wg.Add(1)
	go func() {
		defer c.wg.Done()
		c.BroadcastTxMsgToChain(ctx)
	}()

	c.wg.Add(1)
	go c.MonitorCyclelistQuery(ctx, &c.wg)

	// Let everything start
	time.Sleep(10 * time.Millisecond)

	// 1. Cancel context (simulates SIGTERM)
	cancel()

	// 2. Call Stop() (what Shutdown() does)
	stopDone := make(chan struct{})
	go func() {
		defer close(stopDone)
		c.Stop()
	}()

	select {
	case <-stopDone:
		// Full shutdown completed cleanly
	case <-time.After(5 * time.Second):
		t.Fatal("full shutdown lifecycle did not complete")
	}
}
