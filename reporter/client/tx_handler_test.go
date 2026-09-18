package client

import (
	"encoding/hex"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/stretchr/testify/require"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
	oracletypes "github.com/tellor-io/layer/x/oracle/types"

	"cosmossdk.io/log"
)

func TestGasAdjustment_FixedPerBucket(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")

	require.Equal(t, defaultGasAdjustment, c.gasEstimator.gasAdjustment("test-non-bridge"))
	require.Equal(t, defaultGasAdjustment, c.gasEstimator.gasAdjustment(spotPriceGasBucketKey))
	require.Equal(t, bridgeGasAdjustment, c.gasEstimator.gasAdjustment(bridgeGasBucketKey))
}

func TestBumpEstimateForOutOfGas(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	bucket := "test-non-bridge"

	newEstimate := c.gasEstimator.bumpEstimateForOutOfGas(bucket, 140_000)
	require.Equal(t, uint64(210_000), newEstimate)
	estimate, ok := c.gasEstimator.getCachedEstimate(bucket)
	require.True(t, ok)
	require.Equal(t, uint64(210_000), estimate)

	// Further OOGs keep bumping from GasWanted; adjustment stays fixed.
	newEstimate = c.gasEstimator.bumpEstimateForOutOfGas(bucket, 210_000)
	require.Equal(t, uint64(315_000), newEstimate)
	require.Equal(t, defaultGasAdjustment, c.gasEstimator.gasAdjustment(bucket))
}

func TestBumpEstimateForOutOfGas_Bridge(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")

	require.Equal(t, bridgeGasAdjustment, c.gasEstimator.gasAdjustment(bridgeGasBucketKey))
	newEstimate := c.gasEstimator.bumpEstimateForOutOfGas(bridgeGasBucketKey, 200_000)
	require.Equal(t, uint64(300_000), newEstimate)
	require.Equal(t, bridgeGasAdjustment, c.gasEstimator.gasAdjustment(bridgeGasBucketKey))
}

func TestBumpEstimateAffectsOnlyRelevantBucket(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")

	otherBucket := "*oracletypes.MsgWithdrawTip"
	c.gasEstimator.setEstimate(otherBucket, 100_000)

	_ = c.gasEstimator.bumpEstimateForOutOfGas(spotPriceGasBucketKey, 100_000)
	estimate, ok := c.gasEstimator.getCachedEstimate(spotPriceGasBucketKey)
	require.True(t, ok)
	require.Equal(t, uint64(150_000), estimate)

	otherEstimate, ok := c.gasEstimator.getCachedEstimate(otherBucket)
	require.True(t, ok)
	require.Equal(t, uint64(100_000), otherEstimate)
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

func TestClearAllEstimates(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	bucket := "lazy-reset"

	c.gasEstimator.setEstimate(bucket, 140)
	newEstimate := c.gasEstimator.bumpEstimateForOutOfGas(bucket, 0)
	require.Equal(t, uint64(210), newEstimate)
	estimate, ok := c.gasEstimator.getCachedEstimate(bucket)
	require.True(t, ok)
	require.Equal(t, uint64(210), estimate)

	c.resetAllGasLevelsToBase()
	require.Equal(t, defaultGasAdjustment, c.gasEstimator.gasAdjustment(bucket))
	_, ok = c.gasEstimator.getCachedEstimate(bucket)
	require.False(t, ok)
}

func TestBumpEstimate_PrefersGasWantedOverCache(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	bucket := "scale-wanted"

	c.gasEstimator.setEstimate(bucket, 50_000) // stale; GasWanted should win
	newEstimate := c.gasEstimator.bumpEstimateForOutOfGas(bucket, 140_000)
	require.Equal(t, uint64(210_000), newEstimate)
	estimate, ok := c.gasEstimator.getCachedEstimate(bucket)
	require.True(t, ok)
	require.Equal(t, uint64(210_000), estimate)
}

func TestRaiseEstimateIfHigher(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	bucket := "raise-only"

	c.gasEstimator.setEstimate(bucket, 200_000)

	updated, previous := c.gasEstimator.raiseEstimateIfHigher(bucket, 180_000)
	require.False(t, updated)
	require.Equal(t, uint64(200_000), previous)
	estimate, ok := c.gasEstimator.getCachedEstimate(bucket)
	require.True(t, ok)
	require.Equal(t, uint64(200_000), estimate)

	updated, previous = c.gasEstimator.raiseEstimateIfHigher(bucket, 250_000)
	require.True(t, updated)
	require.Equal(t, uint64(200_000), previous)
	estimate, ok = c.gasEstimator.getCachedEstimate(bucket)
	require.True(t, ok)
	require.Equal(t, uint64(250_000), estimate)
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
			c.gasEstimator.gasAdjustment(bucket)
			c.gasEstimator.bumpEstimateForOutOfGas(bucket, uint64(100+idx))
			c.gasEstimator.raiseEstimateIfHigher(bucket, uint64(200+idx))
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

func TestBuildTxWaitDebugInfo_SubmitValue(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	queryData := mustEncodeQueryData(t, "SpotPrice")
	queryDataHex := hex.EncodeToString(queryData)
	msg := &oracletypes.MsgSubmitValue{
		Creator:   "tellor1abc",
		QueryData: queryData,
		Value:     "0xdeadbeef",
	}
	c.MarketParams = []pricefeedtypes.MarketParam{
		{Pair: "eth-usd", QueryData: queryDataHex},
	}

	timeout := time.Now().UTC()
	info := c.buildTxWaitDebugInfo(42, spotPriceGasBucketKey, 100, 102, timeout, 250000, "ABC123", msg)

	require.Equal(t, "ABC123", info.TxHash)
	require.Equal(t, uint64(42), info.QueryMetaId)
	require.Equal(t, queryDataHex, info.QueryData)
	require.Equal(t, "0xdeadbeef", info.ReportValue)
	require.Equal(t, "SpotPrice", info.QueryType)
	require.Equal(t, "eth-usd", info.MarketPair)
	require.Equal(t, spotPriceGasBucketKey, info.Bucket)
	require.Equal(t, int64(100), info.BroadcastHeight)
	require.Equal(t, uint64(102), info.TimeoutHeight)
	require.Equal(t, uint64(250000), info.GasEstimate)
	require.NotEmpty(t, info.QueryId)
}

func TestClientTxTimeoutDefaults(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	require.Equal(t, defaultTxBroadcastTimeout, c.txBroadcastTimeout)
	require.Equal(t, defaultUnorderedTxTimeout, c.unorderedTxTimeout)
	require.Equal(t, defaultTxTimeoutHeightOffset, c.txTimeoutHeightOffset)
}

func TestGetUniqueUnorderedTimeout_UsesConfiguredTTL(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	c.unorderedTxTimeout = 75 * time.Second

	before := time.Now()
	got := c.GetUniqueUnorderedTimeout()
	require.True(t, got.After(before.Add(74*time.Second)))
	require.True(t, got.Before(before.Add(76*time.Second)))
}

func TestBuildTxWaitDebugInfo_NonSubmitValue(t *testing.T) {
	c := NewClient(log.NewNopLogger(), "0.001loya")
	info := c.buildTxWaitDebugInfo(0, "other", 1, 3, time.Now(), 0, "HASH", nil)
	require.Equal(t, "HASH", info.TxHash)
	require.Empty(t, info.QueryData)
	require.Empty(t, info.ReportValue)
}
