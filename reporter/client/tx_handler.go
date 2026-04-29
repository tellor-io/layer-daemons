package client

import (
	"context"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"time"

	cmttypes "github.com/cometbft/cometbft/rpc/core/types"
	globalfeetypes "github.com/strangelove-ventures/globalfee/x/globalfee/types"
	"github.com/tellor-io/layer-daemons/lib/metrics"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
)

const (
	bridgeGasBucketKey              = "bridge"
	spotPriceGasBucketKey           = "spot-price"
	defaultNonBridgeBucketConfigKey = "default-non-bridge"
	outOfGasCode                    = uint32(11)
	defaultMaxTxAttempts            = 3
)

type gasBucketConfig struct {
	levels  []float64
	baseIdx int
}

type gasBucketState struct {
	cachedEstimate uint64
	hasEstimate    bool
	levelIdx       int
}

type gasEstimateState struct {
	mu          sync.RWMutex
	configByKey map[string]gasBucketConfig
	bucketState map[string]gasBucketState
}

func newGasEstimateState(configByKey map[string]gasBucketConfig) *gasEstimateState {
	return &gasEstimateState{
		configByKey: configByKey,
		bucketState: make(map[string]gasBucketState),
	}
}

func (s *gasEstimateState) configForBucket(bucket string) gasBucketConfig {
	if cfg, ok := s.configByKey[bucket]; ok {
		return cfg
	}
	return s.configByKey[defaultNonBridgeBucketConfigKey]
}

func (s *gasEstimateState) getOrInitBucketState(bucket string) gasBucketState {
	cfg := s.configForBucket(bucket)
	state, ok := s.bucketState[bucket]
	if !ok {
		state = gasBucketState{levelIdx: cfg.baseIdx}
		s.bucketState[bucket] = state
	}
	return state
}

func (s *gasEstimateState) currentGasAdjustment(bucket string) float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrInitBucketState(bucket)
	cfg := s.configForBucket(bucket)
	return cfg.levels[state.levelIdx]
}

func (s *gasEstimateState) getCachedEstimate(bucket string) (uint64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrInitBucketState(bucket)
	return state.cachedEstimate, state.hasEstimate
}

func (s *gasEstimateState) setEstimate(bucket string, gas uint64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrInitBucketState(bucket)
	state.cachedEstimate = gas
	state.hasEstimate = true
	s.bucketState[bucket] = state
}

func (s *gasEstimateState) escalateGasLevel(bucket string) (bool, float64, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrInitBucketState(bucket)
	cfg := s.configForBucket(bucket)
	from := cfg.levels[state.levelIdx]
	if state.levelIdx >= len(cfg.levels)-1 {
		return false, from, from
	}
	state.levelIdx++
	state.hasEstimate = false
	to := cfg.levels[state.levelIdx]
	s.bucketState[bucket] = state
	return true, from, to
}

func (s *gasEstimateState) setBucketToMaxLevel(bucket string) (float64, float64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.getOrInitBucketState(bucket)
	cfg := s.configForBucket(bucket)
	from := cfg.levels[state.levelIdx]
	maxIdx := len(cfg.levels) - 1
	if state.levelIdx != maxIdx {
		state.levelIdx = maxIdx
		state.hasEstimate = false
		s.bucketState[bucket] = state
	}
	to := cfg.levels[state.levelIdx]
	return from, to
}

func (s *gasEstimateState) resetAllGasLevelsToBase() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for bucket, state := range s.bucketState {
		cfg := s.configForBucket(bucket)
		state.levelIdx = cfg.baseIdx
		state.hasEstimate = false
		state.cachedEstimate = 0
		s.bucketState[bucket] = state
	}
}

func newFactory(clientCtx client.Context) tx.Factory {
	return tx.Factory{}.
		WithChainID(clientCtx.ChainID).
		WithKeybase(clientCtx.Keyring).
		WithGasAdjustment(1.25).
		WithGas(defaultGas).
		WithSignMode(signing.SignMode_SIGN_MODE_DIRECT).
		WithAccountRetriever(clientCtx.AccountRetriever).
		WithTxConfig(clientCtx.TxConfig)
}

func handleBroadcastResult(resp *sdk.TxResponse, err error) error {
	if err != nil {
		if strings.Contains(err.Error(), "not found") {
			return fmt.Errorf("make sure that your account has enough balance")
		}
		return err
	}

	if resp.Code > 0 {
		return fmt.Errorf("error code: '%d' msg: '%s'", resp.Code, resp.RawLog)
	}
	return nil
}

func (c *Client) WaitForTx(ctx context.Context, hash string) (*cmttypes.ResultTx, error) {
	waiting := true
	bz, err := hex.DecodeString(hash)
	if err != nil {
		return nil, fmt.Errorf("unable to decode tx hash '%s'; err: %w", hash, err)
	}

	waitedBlockCount := 0
	for waiting {
		resp, err := c.cosmosCtx.Client.Tx(ctx, bz, false)
		if err != nil {
			if strings.Contains(err.Error(), "not found") {
				if waitedBlockCount == 2 {
					return nil, fmt.Errorf("waited for next block and transaction is still not found")
				}
				err := c.WaitForNextBlock(ctx)
				if err != nil {
					return nil, fmt.Errorf("waiting for next block: err: %w", err)
				}
				waitedBlockCount++
				continue
			}
			return nil, fmt.Errorf("fetching tx '%s'; err: %w", hash, err)
		}
		// Tx found
		return resp, nil
	}
	return nil, fmt.Errorf("fetching tx '%s'; err: %w", hash, err)
}

func (c *Client) WaitForNextBlock(ctx context.Context) error {
	return c.WaitForNBlocks(ctx, 1)
}

func (c *Client) WaitForNBlocks(ctx context.Context, n int64) error {
	start, err := c.LatestBlockHeight(ctx)
	if err != nil {
		return err
	}
	return c.WaitForBlockHeight(ctx, start+n)
}

func (c *Client) LatestBlockHeight(ctx context.Context) (int64, error) {
	resp, err := c.Status(ctx)
	if err != nil {
		return 0, err
	}
	return resp.SyncInfo.LatestBlockHeight, nil
}

func (c *Client) Status(ctx context.Context) (*cmttypes.ResultStatus, error) {
	return c.cosmosCtx.Client.Status(ctx)
}

func (c *Client) WaitForBlockHeight(ctx context.Context, h int64) error {
	ticker := time.NewTicker(time.Millisecond * 250)
	defer ticker.Stop()

	for {
		latestHeight, err := c.LatestBlockHeight(ctx)
		if err != nil {
			return err
		}
		if latestHeight >= h {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("timeout exceeded waiting for block, err: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (c *Client) bucketForTx(msg sdk.Msg, isBridge bool) string {
	if isBridge {
		return bridgeGasBucketKey
	}
	if c.isSpotPriceSubmitValue(msg) {
		return spotPriceGasBucketKey
	}
	return reflect.TypeOf(msg).String()
}

func (c *Client) isSpotPriceSubmitValue(msg sdk.Msg) bool {
	submitValueMsg, ok := msg.(interface{ GetQueryData() []byte })
	if !ok {
		return false
	}
	return strings.EqualFold(c.GetQueryType(submitValueMsg.GetQueryData()), "SpotPrice")
}

func (c *Client) EstimateGas(ctx context.Context, txf tx.Factory, bucket string, msg ...sdk.Msg) (uint64, error) {
	if gasEstimate, ok := c.gasEstimator.getCachedEstimate(bucket); ok {
		return gasEstimate, nil
	}
	adjustment := c.gasEstimator.currentGasAdjustment(bucket)
	txf = txf.WithGasAdjustment(adjustment)
	_, gasEstimate, err := tx.CalculateGas(c.cosmosCtx, txf, msg...)
	if err != nil {
		return 0, fmt.Errorf("error calculating gas: %w", err)
	}
	c.gasEstimator.setEstimate(bucket, gasEstimate)
	return gasEstimate, nil
}

func (c *Client) resetAllGasLevelsToBase() {
	c.gasEstimator.resetAllGasLevelsToBase()
}

func (c *Client) sendTx(ctx context.Context, queryMetaId uint64, isBridge bool, msg ...sdk.Msg) (*cmttypes.ResultTx, error) {
	telemetry.IncrCounter(1, "daemon_sending_txs", "called")
	if len(msg) == 0 {
		return nil, fmt.Errorf("no messages provided")
	}
	bucket := c.bucketForTx(msg[0], isBridge)

	// Track success status for defer cleanup
	txSuccess := false

	// Always reset commitedIds on any error, unless explicitly successful
	defer func() {
		if !txSuccess && queryMetaId != 0 {
			mutex.Lock()
			delete(commitedIds, queryMetaId)
			mutex.Unlock()
		}
	}()

	spotPriceTx := c.isSpotPriceSubmitValue(msg[0])
	maxAttempts := c.maxAttemptsForTx(msg[0])

	var lastResp *cmttypes.ResultTx
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		if attempt > 1 {
			c.logger.Info("Retrying tx after out-of-gas", "attempt", attempt, "maxAttempts", maxAttempts, "bucket", bucket)
		}
		txnResponse, txHash, err := c.sendTxOnce(ctx, bucket, msg...)
		if err != nil {
			return nil, err
		}
		lastResp = txnResponse
		c.logger.Info(fmt.Sprintf("transaction hash: %s", txHash))
		c.logger.Info(fmt.Sprintf("response after submit message: %d", txnResponse.TxResult.Code))

		if txnResponse.TxResult.Code == 0 {
			txSuccess = true // Prevent defer cleanup - keep queryMeta marked as committed
			telemetry.IncrCounter(1, "daemon_sending_txs", "success")
			telemetry.IncrCounterWithLabels([]string{"daemon_tx_gas_used_count"}, float32(txnResponse.TxResult.GasUsed), []metrics.Label{{Name: "chain_id", Value: c.cosmosCtx.ChainID}})
			return txnResponse, nil
		}

		if txnResponse.TxResult.Code != outOfGasCode {
			return txnResponse, nil
		}

		changed, from, to := c.gasEstimator.escalateGasLevel(bucket)
		c.logger.Info("Detected out-of-gas tx response",
			"code", txnResponse.TxResult.Code,
			"bucket", bucket,
			"attempt", attempt,
			"escalated", changed,
			"from", from,
			"to", to,
		)
		if !changed {
			return txnResponse, nil
		}
	}

	if spotPriceTx {
		from, to := c.gasEstimator.setBucketToMaxLevel(bucket)
		c.logger.Info("Skipping third spot price send attempt after two failures",
			"bucket", bucket,
			"from", from,
			"to", to,
		)
	}
	return lastResp, nil
}

func (c *Client) maxAttemptsForTx(msg sdk.Msg) int {
	if c.isSpotPriceSubmitValue(msg) {
		return 2
	}
	return defaultMaxTxAttempts
}

func (c *Client) sendTxOnce(ctx context.Context, bucket string, msg ...sdk.Msg) (*cmttypes.ResultTx, string, error) {
	block, err := c.CmtService.GetLatestBlock(ctx, &cmtservice.GetLatestBlockRequest{})
	if err != nil {
		return nil, "", fmt.Errorf("error getting block: %w", err)
	}
	txf := newFactory(c.cosmosCtx)

	// Configure for unordered transactions (Cosmos SDK 0.53.4+)
	// Set sequence to 0, enable unordered mode, and set unique timeout timestamp
	// https://docs.cosmos.network/v0.53/build/architecture/adr-070-unordered-account
	txf = txf.WithSequence(0).
		WithGasPrices(c.minGasFee).
		WithTimeoutHeight(uint64(block.SdkBlock.Header.Height + 2)).
		WithUnordered(true).
		WithTimeoutTimestamp(c.GetUniqueUnorderedTimeout())
	txf, err = txf.Prepare(c.cosmosCtx)
	if err != nil {
		return nil, "", fmt.Errorf("error preparing transaction factory: %w", err)
	}
	gasEstimate, err := c.EstimateGas(ctx, txf, bucket, msg...)
	if err == nil {
		txf = txf.WithGas(gasEstimate)
	}
	txn, err := txf.BuildUnsignedTx(msg...)
	if err != nil {
		return nil, "", fmt.Errorf("error building unsigned transaction: %w", err)
	}
	if err = tx.Sign(c.cosmosCtx.CmdContext, txf, c.cosmosCtx.FromName, txn, true); err != nil {
		return nil, "", fmt.Errorf("error when signing transaction: %w", err)
	}

	txBytes, err := c.cosmosCtx.TxConfig.TxEncoder()(txn.GetTx())
	if err != nil {
		return nil, "", fmt.Errorf("error encoding transaction: %w", err)
	}
	res, err := c.cosmosCtx.BroadcastTx(txBytes)
	if err := handleBroadcastResult(res, err); err != nil {
		return nil, "", fmt.Errorf("error broadcasting transaction result: %w", err)
	}

	txnResponse, err := c.WaitForTx(ctx, res.TxHash)
	if err != nil {
		return nil, "", fmt.Errorf("error waiting for transaction: %w", err)
	}
	for _, event := range txnResponse.TxResult.Events {
		if event.Type == "new_report" {
			for _, attr := range event.Attributes {
				c.logger.Info("NewReport", attr.Key, attr.Value)
			}
		}
	}
	return txnResponse, res.TxHash, nil
}

func (c *Client) SetGasPrice(ctx context.Context) error {
	gfResponse, err := c.GlobalfeeClient.MinimumGasPrices(ctx, &globalfeetypes.QueryMinimumGasPricesRequest{})
	if err != nil {
		return fmt.Errorf("getting minimum gas price (globalfee): %w", err)
	}
	localPrice, err := sdk.ParseDecCoins(c.minGasFee)
	if err != nil {
		return fmt.Errorf("parsing local gas price: %w", err)
	}

	p := gasprice(gfResponse.MinimumGasPrices, localPrice)
	if p.IsZero() {
		return fmt.Errorf("unable to set gas price, global and local gas prices are zero")
	}
	c.minGasFee = p.String()
	return nil
}

func gasprice(local, global sdk.DecCoins) sdk.DecCoin {
	_local := sdk.NewDecCoin("loya", math.ZeroInt())
	for _, coin := range local {
		if coin.Denom == "loya" && coin.Amount.GT(math.LegacyZeroDec()) {
			_local = coin
		}
	}
	_global := sdk.NewDecCoin("loya", math.ZeroInt())
	for _, coin := range global {
		if coin.Denom == "loya" && coin.Amount.GT(math.LegacyZeroDec()) {
			_global = coin
		}
	}

	return sdk.DecCoin{
		Denom:  "loya",
		Amount: math.LegacyMaxDec(_local.Amount, _global.Amount),
	}
}

// func getcommitId(events []abcitypes.Event) (uint64, error) {
// 	for _, event := range events {
// 		if event.Type == "new_commit" {
// 			for _, attr := range event.Attributes {
// 				if attr.Key == "commit_id" {
// 					value, err := strconv.Atoi(attr.Value)
// 					if err != nil {
// 						return 0, err
// 					}
// 					return uint64(value), nil
// 				}
// 			}
// 		}
// 	}
// 	return 0, fmt.Errorf("commit_id not found")
// }
