package client

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/viper"
	bridgetypes "github.com/tellor-io/layer/x/bridge/types"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	autoBalanceGasReserveLoya  = 1_000_000 // 1 TRB
	autoBalanceBridgeMaxRetries = 3
	autoBalanceDenom            = "loya"
)

// autoBalanceSettings holds validated --auto-balance-* flag values.
type autoBalanceSettings struct {
	enabled       bool
	balanceToKeep uint64
	ethAddr       string // lowercase hex without 0x prefix
	hour          int
	minute        int
}

func loadAutoBalanceSettings() (autoBalanceSettings, error) {
	balanceToKeep := viper.GetUint64("auto-balance-to-keep")
	if balanceToKeep == 0 {
		return autoBalanceSettings{}, nil
	}

	ethAddr, err := validateAutoBalanceEthAddr(viper.GetString("auto-balance-eth-addr"))
	if err != nil {
		return autoBalanceSettings{}, err
	}

	hour, minute, err := parseAutoBalanceExecutionTime(viper.GetString("auto-balance-execution-time"))
	if err != nil {
		return autoBalanceSettings{}, err
	}

	return autoBalanceSettings{
		enabled:       true,
		balanceToKeep: balanceToKeep,
		ethAddr:       ethAddr,
		hour:          hour,
		minute:        minute,
	}, nil
}

// validateAutoBalanceEthAddr checks format and returns a normalized address without 0x.
func validateAutoBalanceEthAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", fmt.Errorf("auto-balance-eth-addr is required when auto-balance-to-keep > 0")
	}
	if !strings.HasPrefix(addr, "0x") {
		addr = "0x" + addr
	}
	if !common.IsHexAddress(addr) {
		return "", fmt.Errorf("auto-balance-eth-addr is not a valid ethereum address: %s", addr)
	}
	return strings.ToLower(strings.TrimPrefix(common.HexToAddress(addr).Hex(), "0x")), nil
}

// parseAutoBalanceExecutionTime parses a UTC HH:MM execution time.
func parseAutoBalanceExecutionTime(executionTime string) (hour int, minute int, err error) {
	executionTime = strings.TrimSpace(executionTime)
	parts := strings.Split(executionTime, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("auto-balance-execution-time must be HH:MM, got: %q", executionTime)
	}
	if parts[0] == "" || parts[1] == "" {
		return 0, 0, fmt.Errorf("auto-balance-execution-time must be HH:MM, got: %q", executionTime)
	}
	if len(parts[0]) != 2 || len(parts[1]) != 2 {
		return 0, 0, fmt.Errorf("auto-balance-execution-time must use two-digit hour and minute (HH:MM), got: %q", executionTime)
	}

	hour, err = strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0, fmt.Errorf("auto-balance-execution-time has invalid hour: %q", executionTime)
	}
	minute, err = strconv.Atoi(parts[1])
	if err != nil {
		return 0, 0, fmt.Errorf("auto-balance-execution-time has invalid minute: %q", executionTime)
	}
	if hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("auto-balance-execution-time out of range (hour 0-23, minute 0-59), got: %q", executionTime)
	}
	return hour, minute, nil
}

// nextAutoBalanceRunUTC returns the next UTC run time at hour:minute on or after now.
func nextAutoBalanceRunUTC(now time.Time, hour, minute int) time.Time {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !now.Before(next) {
		next = next.Add(24 * time.Hour)
	}
	return next
}

// computeAutoBalanceBridgeAmount returns the loya amount to bridge and whether it is positive.
func computeAutoBalanceBridgeAmount(walletBal math.Int, balanceToKeep uint64) (math.Int, bool) {
	keepAmt := math.NewIntFromUint64(balanceToKeep)
	gasReserve := math.NewInt(autoBalanceGasReserveLoya)
	amount := walletBal.Sub(keepAmt).Sub(gasReserve)
	return amount, amount.IsPositive()
}

// buildAutoBalanceWithdrawMsg constructs MsgWithdrawTokens for bridging excess wallet balance.
func buildAutoBalanceWithdrawMsg(creator, recipient string, amount math.Int) *bridgetypes.MsgWithdrawTokens {
	return &bridgetypes.MsgWithdrawTokens{
		Creator:   creator,
		Recipient: recipient,
		Amount:    sdk.NewCoin(autoBalanceDenom, amount),
	}
}

func utcDateString(t time.Time) string {
	return t.UTC().Format("2006-01-02")
}

func (c *Client) autoBalanceAlreadyBridgedToday(now time.Time) bool {
	c.autoBalanceMu.Lock()
	defer c.autoBalanceMu.Unlock()
	return c.autoBalanceBridgedDate == utcDateString(now)
}

func (c *Client) markAutoBalanceBridgedToday(now time.Time) {
	c.autoBalanceMu.Lock()
	defer c.autoBalanceMu.Unlock()
	c.autoBalanceBridgedDate = utcDateString(now)
}
