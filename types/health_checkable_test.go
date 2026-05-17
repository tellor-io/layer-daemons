package types

import (
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/stretchr/testify/require"
)

type testTimeProvider struct {
	now time.Time
}

func (p *testTimeProvider) Now() time.Time {
	return p.now
}

func TestHealthCheckFailsWhenLastSuccessIsStale(t *testing.T) {
	timeProvider := &testTimeProvider{now: time.Unix(100, 0)}

	healthCheckable := NewTimeBoundedHealthCheckable("test-service", timeProvider, log.NewNopLogger())
	healthCheckable.ReportSuccess()

	timeProvider.now = timeProvider.now.Add(MaxAcceptableUpdateDelay + time.Second)
	err := healthCheckable.HealthCheck()

	require.Error(t, err)
	require.Contains(t, err.Error(), "last successful update occurred")
}
