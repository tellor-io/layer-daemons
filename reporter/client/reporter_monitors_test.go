package client

import (
	"context"
	"testing"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
	"github.com/tellor-io/layer-daemons/flags"
	layertypes "github.com/tellor-io/layer/types"

	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/types/bech32"
)

func TestValidatorOperatorAddressDerivesFromReporterWhenUnset(t *testing.T) {
	t.Setenv(reportersValidatorAddressEnv, "")
	reporterAddr := testBech32Address(t, "tellor", 1)

	valAddr, source, err := validatorOperatorAddress(reporterAddr)

	require.NoError(t, err)
	require.Equal(t, toValidatorOperator(reporterAddr), valAddr)
	require.Equal(t, "derived", source)
}

func TestValidatorOperatorAddressUsesConfiguredOverride(t *testing.T) {
	configuredValAddr := testBech32Address(t, "tellorvaloper", 2)
	t.Setenv(reportersValidatorAddressEnv, " "+configuredValAddr+" ")

	valAddr, source, err := validatorOperatorAddress(testBech32Address(t, "tellor", 1))

	require.NoError(t, err)
	require.Equal(t, configuredValAddr, valAddr)
	require.Equal(t, reportersValidatorAddressEnv, source)
}

func TestValidatorOperatorAddressRejectsInvalidOverridePrefix(t *testing.T) {
	t.Setenv(reportersValidatorAddressEnv, testBech32Address(t, "tellor", 1))

	_, _, err := validatorOperatorAddress(testBech32Address(t, "tellor", 2))

	require.Error(t, err)
	require.ErrorContains(t, err, "tellorvaloper")
}

func TestValidatorOperatorAddressRejectsInvalidOverrideAddress(t *testing.T) {
	t.Setenv(reportersValidatorAddressEnv, "not-a-bech32-address")

	_, _, err := validatorOperatorAddress(testBech32Address(t, "tellor", 1))

	require.Error(t, err)
	require.ErrorContains(t, err, reportersValidatorAddressEnv)
}

func TestShouldWithdrawTipsToWalletFlagBypassesQueries(t *testing.T) {
	viper.Set(flags.FlagWithdrawToWallet, true)
	t.Cleanup(func() { viper.Set(flags.FlagWithdrawToWallet, false) })

	withdrawToWallet, err := (&Client{}).shouldWithdrawTipsToWallet(context.Background())

	require.NoError(t, err)
	require.True(t, withdrawToWallet)
}

func TestProjectedReporterShareExceeds(t *testing.T) {
	totalBonded := layertypes.PowerReduction.MulRaw(1_000)
	threshold := math.LegacyMustNewDecFromStr("0.295")

	require.False(t, projectedReporterShareExceeds(
		294,
		math.LegacyNewDecFromInt(layertypes.PowerReduction),
		totalBonded,
		threshold,
	), "exactly 29.5 percent should continue staking")
	require.True(t, projectedReporterShareExceeds(
		294,
		math.LegacyNewDecFromInt(layertypes.PowerReduction.AddRaw(1)),
		totalBonded,
		threshold,
	), "more than 29.5 percent should withdraw to wallet")
}

func testBech32Address(t *testing.T, prefix string, seed byte) string {
	t.Helper()

	addrBytes := make([]byte, 20)
	for i := range addrBytes {
		addrBytes[i] = seed + byte(i)
	}
	addr, err := bech32.ConvertAndEncode(prefix, addrBytes)
	require.NoError(t, err)
	return addr
}
