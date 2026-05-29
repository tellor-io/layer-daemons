package client

import (
	"testing"

	"github.com/stretchr/testify/require"

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
