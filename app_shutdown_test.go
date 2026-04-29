package daemons

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"cosmossdk.io/log"
)

// TestAppShutdown_WaitsForGoroutinesThenReturns verifies the core contract of
// Shutdown: it blocks until all tracked goroutines (wg) finish, then returns.
// Without the wg.Wait() in Shutdown, this test would return immediately
// before the goroutine finishes (or hang forever if wg.Wait never returns).
func TestAppShutdown_WaitsForGoroutinesThenReturns(t *testing.T) {
	app := &App{
		logger: log.NewNopLogger(),
	}

	goroutineRan := false
	var mu sync.Mutex

	app.wg.Add(1)
	go func() {
		defer app.wg.Done()
		time.Sleep(50 * time.Millisecond)
		mu.Lock()
		goroutineRan = true
		mu.Unlock()
	}()

	app.Shutdown()

	mu.Lock()
	require.True(t, goroutineRan, "Shutdown must wait for goroutines to finish before returning")
	mu.Unlock()
}

// TestAppShutdown_CleansUpTempDir verifies Shutdown removes the temp directory
// it created during initialization.
func TestAppShutdown_CleansUpTempDir(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "test-shutdown-cleanup")
	require.NoError(t, err)

	defer os.RemoveAll(tmpDir)

	app := &App{
		logger:  log.NewNopLogger(),
		tempDir: tmpDir,
	}

	app.Shutdown()

	_, err = os.Stat(tmpDir)
	require.True(t, os.IsNotExist(err), "Shutdown must remove tempDir")
}

// TestAppShutdown_SkipsTempDirWhenEmpty verifies Shutdown doesn't try to remove
// anything when tempDir is empty.
func TestAppShutdown_SkipsTempDirWhenEmpty(t *testing.T) {
	app := &App{
		logger:  log.NewNopLogger(),
		tempDir: "",
	}

	require.NotPanics(t, func() {
		app.Shutdown()
	})
}

// TestAppShutdown_NilClientsSafe verifies Shutdown doesn't panic when client
// fields are nil, which happens if the app fails during initialization.
func TestAppShutdown_NilClientsSafe(t *testing.T) {
	app := &App{
		logger: log.NewNopLogger(),
	}

	require.NotPanics(t, func() {
		app.Shutdown()
	})
}
