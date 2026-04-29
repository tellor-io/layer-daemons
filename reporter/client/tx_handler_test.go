package client

import (
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"
	oracletypes "github.com/tellor-io/layer/x/oracle/types"

	"cosmossdk.io/log"
)

func TestGasEstimatorEscalation_NonBridge(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	bucket := "test-non-bridge"

	require.Equal(t, 1.25, c.gasEstimator.currentGasAdjustment(bucket))
	changed, from, to := c.gasEstimator.escalateGasLevel(bucket)
	require.True(t, changed)
	require.Equal(t, 1.25, from)
	require.Equal(t, 1.6, to)
	require.Equal(t, 1.6, c.gasEstimator.currentGasAdjustment(bucket))

	changed, from, to = c.gasEstimator.escalateGasLevel(bucket)
	require.True(t, changed)
	require.Equal(t, 1.6, from)
	require.Equal(t, 2.0, to)
	require.Equal(t, 2.0, c.gasEstimator.currentGasAdjustment(bucket))

	changed, from, to = c.gasEstimator.escalateGasLevel(bucket)
	require.False(t, changed)
	require.Equal(t, 2.0, from)
	require.Equal(t, 2.0, to)
}

func TestGasEstimatorEscalation_Bridge(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")

	require.Equal(t, 1.75, c.gasEstimator.currentGasAdjustment(bridgeGasBucketKey))
	changed, from, to := c.gasEstimator.escalateGasLevel(bridgeGasBucketKey)
	require.True(t, changed)
	require.Equal(t, 1.75, from)
	require.Equal(t, 2.0, to)

	changed, from, to = c.gasEstimator.escalateGasLevel(bridgeGasBucketKey)
	require.False(t, changed)
	require.Equal(t, 2.0, from)
	require.Equal(t, 2.0, to)
}

func TestEscalationAffectsOnlyRelevantBucket(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")

	otherBucket := "*oracletypes.MsgWithdrawTip"
	require.Equal(t, 1.25, c.gasEstimator.currentGasAdjustment(otherBucket))

	changed, _, _ := c.gasEstimator.escalateGasLevel(spotPriceGasBucketKey)
	require.True(t, changed)
	require.Equal(t, 1.6, c.gasEstimator.currentGasAdjustment(spotPriceGasBucketKey))
	require.Equal(t, 1.25, c.gasEstimator.currentGasAdjustment(otherBucket))
}

func TestRetryPolicyMatrix(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")

	spotMsg := &oracletypes.MsgSubmitValue{
		Creator:   "reporter",
		QueryData: mustEncodeQueryData(t, "SpotPrice"),
		Value:     "0x1234",
	}
	nonSpotMsg := &oracletypes.MsgSubmitValue{
		Creator:   "reporter",
		QueryData: mustEncodeQueryData(t, "TRBBridgeV2"),
		Value:     "0x1234",
	}

	require.Equal(t, 2, c.maxAttemptsForTx(spotMsg))
	require.Equal(t, 3, c.maxAttemptsForTx(nonSpotMsg))
}

func TestResetAllGasLevelsToBase_IsLazy(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	bucket := "lazy-reset"

	c.gasEstimator.setEstimate(bucket, 111)
	changed, _, _ := c.gasEstimator.escalateGasLevel(bucket)
	require.True(t, changed)
	require.Equal(t, 1.6, c.gasEstimator.currentGasAdjustment(bucket))
	estimate, ok := c.gasEstimator.getCachedEstimate(bucket)
	require.False(t, ok)
	require.Equal(t, uint64(111), estimate)

	c.resetAllGasLevelsToBase()
	require.Equal(t, 1.25, c.gasEstimator.currentGasAdjustment(bucket))
	_, ok = c.gasEstimator.getCachedEstimate(bucket)
	require.False(t, ok)
}

func TestGasEstimatorConcurrentAccess(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	bucket := "concurrent"

	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			c.gasEstimator.setEstimate(bucket, uint64(100+idx))
			c.gasEstimator.currentGasAdjustment(bucket)
			c.gasEstimator.escalateGasLevel(bucket)
			c.gasEstimator.getCachedEstimate(bucket)
		}(i)
	}
	wg.Wait()
}

func mustEncodeQueryData(t *testing.T, queryType string) []byte {
	t.Helper()
	stringType, err := abi.NewType("string", "", nil)
	require.NoError(t, err)
	bytesType, err := abi.NewType("bytes", "", nil)
	require.NoError(t, err)
	args := abi.Arguments{
		{Type: stringType},
		{Type: bytesType},
	}
	bz, err := args.Pack(queryType, []byte("args"))
	require.NoError(t, err)
	return bz
}
