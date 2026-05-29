package client

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/spf13/viper"
	"github.com/tellor-io/layer-daemons/flags"
	tokenbridgetipstypes "github.com/tellor-io/layer-daemons/server/types/token_bridge_tips"
	bridgetypes "github.com/tellor-io/layer/x/bridge/types"
	oracletypes "github.com/tellor-io/layer/x/oracle/types"
	reportertypes "github.com/tellor-io/layer/x/reporter/types"

	"cosmossdk.io/math"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/cosmos/cosmos-sdk/types/query"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

const (
	defaultQueryTimeout          = 10 * time.Second
	defaultTxTimeout             = 10 * time.Second
	defaultRetryDelay            = 200 * time.Millisecond
	maxRetryDelay                = 30 * time.Second
	reportersValidatorAddressEnv = "REPORTERS_VALIDATOR_ADDRESS"
)

// toValidatorOperator converts a tellor1xxx bech32 address to its tellorvaloper1xxx
// counterpart by re-encoding the same underlying bytes with the validator prefix.
func toValidatorOperator(walletAddr string) string {
	if walletAddr == "" {
		return ""
	}
	_, addrBytes, err := bech32.DecodeAndConvert(walletAddr)
	if err != nil {
		return ""
	}
	valoperAddr, err := bech32.ConvertAndEncode("tellorvaloper", addrBytes)
	if err != nil {
		return ""
	}
	return valoperAddr
}

func validatorOperatorAddress(reporterAddr string) (string, string, error) {
	configuredAddr := strings.TrimSpace(os.Getenv(reportersValidatorAddressEnv))
	if configuredAddr != "" {
		prefix, _, err := bech32.DecodeAndConvert(configuredAddr)
		if err != nil {
			return "", "", fmt.Errorf("%s is not a valid bech32 address: %w", reportersValidatorAddressEnv, err)
		}
		if prefix != "tellorvaloper" {
			return "", "", fmt.Errorf("%s must use tellorvaloper prefix, got %q", reportersValidatorAddressEnv, prefix)
		}
		return configuredAddr, reportersValidatorAddressEnv, nil
	}

	valAddr := toValidatorOperator(reporterAddr)
	if valAddr == "" {
		return "", "", fmt.Errorf("could not derive validator operator address from reporter address")
	}
	return valAddr, "derived", nil
}

func (c *Client) MonitorCyclelistQuery(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	prevQueryData := []byte{}
	retryDelay := defaultRetryDelay
	ticker := time.NewTicker(defaultRetryDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			queryCtx, cancel := context.WithTimeout(ctx, defaultQueryTimeout)
			querydata, querymeta, err := c.CurrentQuery(queryCtx)
			cancel()

			if err != nil || querymeta == nil {
				c.logger.Error("query failed", "error", err)
				// Exponential backoff on error: 200ms → 400ms → … capped at 30s
				retryDelay *= 2
				if retryDelay > maxRetryDelay {
					retryDelay = maxRetryDelay
				}
				ticker.Reset(retryDelay)
				continue
			}

			// Reset delay on success
			if retryDelay != defaultRetryDelay {
				retryDelay = defaultRetryDelay
				ticker.Reset(retryDelay)
			}

			mutex.Lock()
			hasCommited := commitedIds[querymeta.Id]
			mutex.Unlock()
			if bytes.Equal(querydata, prevQueryData) || hasCommited {
				continue
			}

			// Handle report generation with timeout
			txCtx, cancel := context.WithTimeout(ctx, defaultTxTimeout)
			done := make(chan struct{})

			c.logger.Info(fmt.Sprintf("starting to generate spot price report at %d", time.Now().Unix()))
			go func() {
				defer close(done)
				err := c.GenerateAndBroadcastSpotPriceReport(txCtx, querydata, querymeta)
				if err != nil {
					c.logger.Error("report generation failed", "error", err)
				}
			}()

			select {
			case <-done:
				cancel()
			case <-txCtx.Done():
				c.logger.Error(fmt.Sprintf("report generation timed out at %d", time.Now().Unix()))
				cancel()
			}

			prevQueryData = querydata
		}
	}
}

func (c *Client) MonitorTokenBridgeReports(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			txCtx, cancel := context.WithTimeout(ctx, defaultTxTimeout)
			done := make(chan struct{})

			go func() {
				defer close(done)
				err := c.GenerateDepositMessages(txCtx)
				if err != nil {
					c.logger.Error("deposit generation failed", "error", err)
				}
			}()

			select {
			case <-done:
				cancel()
			case <-txCtx.Done():
				c.logger.Error("deposit generation timed out")
				cancel()
			}

			c.LogProcessStats()
		}
	}
}

func (c *Client) MonitorForTippedQueries(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	retryDelay := defaultRetryDelay
	ticker := time.NewTicker(defaultRetryDelay)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			queryCtx, cancel := context.WithTimeout(ctx, defaultQueryTimeout)
			var res *oracletypes.QueryTippedQueriesForDaemonResponse
			err := c.withGRPCFallback(queryCtx, "tipped queries lookup", func() error {
				var err error
				res, err = c.OracleQueryClient.TippedQueriesForDaemon(queryCtx, &oracletypes.QueryTippedQueriesForDaemonRequest{
					Pagination: &query.PageRequest{
						Offset: 0,
					},
				})
				return err
			})
			cancel()

			if err != nil || res == nil || len(res.Queries) == 0 {
				if err != nil {
					// Exponential backoff on error
					retryDelay *= 2
					if retryDelay > maxRetryDelay {
						retryDelay = maxRetryDelay
					}
					ticker.Reset(retryDelay)
				}
				continue
			}

			// Reset delay on success
			if retryDelay != defaultRetryDelay {
				retryDelay = defaultRetryDelay
				ticker.Reset(retryDelay)
			}

			status, err := c.Status(ctx)
			if err != nil {
				continue
			}

			height := uint64(status.SyncInfo.LatestBlockHeight)

			for _, query := range res.Queries {
				queryType := c.GetQueryType(query.GetQueryData())
				mutex.Lock()
				haveCommited := commitedIds[query.Id]
				mutex.Unlock()
				if height > query.Expiration || haveCommited ||
					!strings.EqualFold(queryType, "SpotPrice") && !strings.EqualFold(queryType, "TRBBridge") {
					continue
				}

				if strings.EqualFold(queryType, "TRBBridge") {
					mutex.Lock()
					haveCommitedTip := depositTipMap[query.Id]
					mutex.Unlock()
					if haveCommitedTip {
						continue
					}
					queryData := query.GetQueryData()
					tipQueryData := tokenbridgetipstypes.QueryData{QueryData: queryData}
					c.TokenBridgeTipsCache.AddTip(tipQueryData)
					mutex.Lock()
					depositTipMap[query.Id] = true
					mutex.Unlock()
					continue
				}

				txCtx, cancel := context.WithTimeout(ctx, defaultTxTimeout)
				done := make(chan struct{})

				go func(q *oracletypes.QueryMeta) {
					defer close(done)
					err := c.GenerateAndBroadcastSpotPriceReport(txCtx, q.GetQueryData(), q)
					if err != nil {
						c.logger.Error("tipped query report failed", "error", err)
					}
				}(query)

				select {
				case <-done:
					cancel()
				case <-txCtx.Done():
					c.logger.Error("tipped query report timed out")
					cancel()
				}
			}
		}
	}
}

func (c *Client) WithdrawAndStakeEarnedRewardsPeriodically(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	freqVar := os.Getenv("WITHDRAW_FREQUENCY")
	if freqVar == "" {
		freqVar = "43200" // default to being 12 hours or 43200 seconds
	}
	frequency, err := strconv.Atoi(freqVar)
	if err != nil {
		c.logger.Error("Could not start auto rewards withdrawal process due to incorrect parameter. Please enter the number of seconds to wait in between claiming rewards")
		return
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		valAddr, valAddrSource, err := validatorOperatorAddress(c.accAddr.String())
		if err != nil {
			c.logger.Error("could not resolve validator operator address", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Duration(frequency) * time.Second):
			}
			continue
		}
		c.logger.Info("Using validator operator address for reward withdrawal", "validator_address", valAddr, "source", valAddrSource)

		withdrawMsg := &reportertypes.MsgWithdrawTip{
			SelectorAddress:  c.accAddr.String(),
			ValidatorAddress: valAddr,
		}
		c.trySend(ctx, TxChannelInfo{Msg: withdrawMsg, isBridge: false, NumRetries: 0, QueryMetaId: 0})

		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(frequency) * time.Second):
		}
	}
}

func (c *Client) AutoUnbondStakePeriodically(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	frequency := viper.GetUint32("auto-unbonding-frequency")
	amount := viper.GetUint32("auto-unbonding-amount")
	maxStakePercentageStr := viper.GetString("auto-unbonding-max-stake-percentage")

	if frequency == 0 {
		c.logger.Info("Auto unbonding is disabled")
		return
	}

	secondsInDay := 86400
	ticker := time.NewTicker(time.Duration(secondsInDay*int(frequency)) * time.Second)
	defer ticker.Stop()
	maxStakePercentage, err := math.LegacyNewDecFromStr(maxStakePercentageStr)
	if err != nil {
		c.logger.Error("Could not start auto unbonding process due to incorrect parameter. Please enter a valid decimal for the maximum stake percentage")
		panic(err)
	}
	unbondAmount := math.NewInt(int64(amount))
	valAddr, valAddrSource, err := validatorOperatorAddress(c.accAddr.String())
	if err != nil {
		c.logger.Error("could not resolve validator operator address, auto-unbonding disabled", "error", err)
		return
	}
	c.logger.Info("Using validator operator address for auto-unbonding", "validator_address", valAddr, "source", valAddrSource)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.logger.Info("Trying to unbond stake")
			var reporterData *reportertypes.QuerySelectionsToResponse
			err := c.withGRPCFallback(ctx, "reporter selections lookup", func() error {
				var err error
				reporterData, err = c.ReporterClient.SelectionsTo(ctx, &reportertypes.QuerySelectionsToRequest{
					ReporterAddress: c.accAddr.String(),
				})
				return err
			})
			if err != nil {
				c.logger.Error("error getting reporter data", "error", err)
				continue
			}
			if len(reporterData.Selections) == 0 {
				continue
			}
			reporterStake := math.LegacyZeroDec()
			for _, selection := range reporterData.Selections {
				if selection.Selector == c.accAddr.String() {
					reporterStake = selection.DelegationsTotal.ToLegacyDec()
					break
				}
			}

			if shouldSkipAutoUnbond(reporterStake, maxStakePercentage, unbondAmount) {
				maxStakeAbleToWithdraw := reporterStake.Mul(maxStakePercentage)
				c.logger.Info("Not enough stake to withdraw", "reporterStake", reporterStake, "maxStakeAbleToWithdraw", maxStakeAbleToWithdraw)
				continue
			}

			unbondMsg := &stakingtypes.MsgUndelegate{
				DelegatorAddress: c.accAddr.String(),
				ValidatorAddress: valAddr,
				Amount:           sdk.NewCoin("loya", unbondAmount),
			}
			c.trySend(ctx, TxChannelInfo{Msg: unbondMsg, isBridge: false, NumRetries: 0, QueryMetaId: 0})

		}
	}
}

func shouldSkipAutoUnbond(reporterStake, maxStakePercentage math.LegacyDec, unbondAmount math.Int) bool {
	if !maxStakePercentage.GT(math.LegacyZeroDec()) {
		return false
	}
	return reporterStake.Mul(maxStakePercentage).LT(math.LegacyNewDecFromInt(unbondAmount))
}

// AutoBridgeWalletExcessPeriodically watches the wallet balance once per day at the configured
// UTC time. Whenever the balance exceeds --auto-balance-to-keep (loya), the excess (minus a
// 1 TRB gas reserve) is bridged to the Ethereum address supplied by --auto-balance-bridge-to-eth-addr.
func (c *Client) AutoBridgeWalletExcessPeriodically(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	balanceToKeep := viper.GetUint64(flags.FlagAutoBalanceToKeep)
	if balanceToKeep == 0 {
		c.logger.Info("Auto balance-to-keep is disabled")
		return
	}

	ethAddr, err := normalizeAutoBalanceEthAddr(viper.GetString(flags.FlagAutoBalanceBridgeToEthAddr))
	if err != nil {
		c.logger.Error("invalid auto-balance bridge address", "error", err)
		return
	}
	hour, minute, err := parseAutoBalanceExecutionTime(viper.GetString(flags.FlagAutoBalanceExecutionTime))
	if err != nil {
		c.logger.Error("invalid auto-balance execution time", "error", err)
		return
	}

	for {
		now := time.Now().UTC()
		next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
		if !now.Before(next) {
			next = next.Add(24 * time.Hour)
		}

		select {
		case <-ctx.Done():
			c.logger.Info("Auto balance-to-keep stopped")
			return
		case <-time.After(time.Until(next)):
		}

		c.logger.Info("Auto balance-to-keep: checking wallet balance")

		var balResp *banktypes.QueryBalanceResponse
		err = c.withGRPCFallback(ctx, "wallet balance lookup", func() error {
			var err error
			balResp, err = c.BankClient.Balance(ctx, &banktypes.QueryBalanceRequest{
				Address: c.accAddr.String(),
				Denom:   "loya",
			})
			return err
		})
		if err != nil {
			c.logger.Error("auto balance-to-keep: failed to query wallet balance", "error", err)
			continue
		}

		walletBal := balResp.Balance.Amount
		keepAmt := math.NewIntFromUint64(balanceToKeep)
		// Reserve 1 TRB (1_000_000 loya) for future gas so the wallet can keep operating.
		gasReserve := math.NewInt(1_000_000)
		amountToBridge := walletBal.Sub(keepAmt).Sub(gasReserve)

		if !amountToBridge.IsPositive() {
			c.logger.Info(
				"auto balance-to-keep: wallet below threshold, nothing to bridge",
				"wallet_loya", walletBal.String(),
				"keep_loya", keepAmt.String(),
			)
			continue
		}

		c.logger.Info(
			"auto balance-to-keep: bridging excess",
			"wallet_loya", walletBal.String(),
			"keep_loya", keepAmt.String(),
			"bridge_amount_loya", amountToBridge.String(),
			"destination", "0x"+ethAddr,
		)

		msg := &bridgetypes.MsgWithdrawTokens{
			Creator:   c.accAddr.String(),
			Recipient: ethAddr,
			Amount:    sdk.NewCoin("loya", amountToBridge),
		}
		c.trySend(ctx, TxChannelInfo{Msg: msg, isBridge: true, NumRetries: 0, QueryMetaId: 0})
	}
}

func (c *Client) LogProcessStats() {
	count := runtime.NumGoroutine()
	c.logger.Info(fmt.Sprintf("Number of Goroutines: %d\n", count))

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	c.logger.Info(fmt.Sprintf("Memory Stats: { 'alloc': %d, 'total alloc': %d, 'mallocs': %d, 'frees': %d, 'heap released': %d}", m.Alloc, m.TotalAlloc, m.Mallocs, m.Frees, m.HeapReleased))

	pid := int32(os.Getpid())
	p, err := process.NewProcess(pid)
	if err != nil {
		c.logger.Error(fmt.Sprintf("Error getting process info: %v\n", err))
		return
	}

	// Get CPU usage percentage
	cpuPercent, err := p.CPUPercent()
	if err != nil {
		c.logger.Error(fmt.Sprintf("Error getting CPU percent: %v\n", err))
		return
	}

	numThreads, err := p.NumThreads()
	if err != nil {
		c.logger.Error(fmt.Sprintf("Error getting num of threads: %v\n", numThreads))
		return
	}

	c.logger.Info(fmt.Sprintf("CPU Usage: %.2f%%, Num of threads: %d\n", cpuPercent, numThreads))
}

func (c *Client) GetQueryType(querydata []byte) string {
	// in solidity, querydata encoded as abi.encode(string queryType, bytes queryArgs)
	StringType, err := abi.NewType("string", "", nil)
	if err != nil {
		return ""
	}
	BytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return ""
	}
	initialArgs := abi.Arguments{
		{Type: StringType},
		{Type: BytesType},
	}
	queryDataDecodedPartial, err := initialArgs.Unpack(querydata)
	if err != nil {
		return ""
	}
	return queryDataDecodedPartial[0].(string)
}
