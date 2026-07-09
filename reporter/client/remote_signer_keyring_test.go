package client

import (
	"testing"

	"github.com/stretchr/testify/require"
	_ "github.com/tellor-io/layer/app/config"

	cosmossecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/bech32"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
)

func TestRemoteSignerAccountAddressRequiresPublicKeyMatch(t *testing.T) {
	pubKey := &cosmossecp.PubKey{Key: testCompressedPubKey(1)}
	addr, err := bech32.ConvertAndEncode("tellor", sdk.AccAddress(pubKey.Address()))
	require.NoError(t, err)

	accAddr, err := remoteSignerAccountAddress(pubKey, addr)
	require.NoError(t, err)
	require.True(t, accAddr.Equals(sdk.AccAddress(pubKey.Address())))

	otherAddr, err := bech32.ConvertAndEncode("tellor", sdk.AccAddress(make([]byte, 20)))
	require.NoError(t, err)

	_, err = remoteSignerAccountAddress(pubKey, otherAddr)
	require.Error(t, err)
	require.ErrorContains(t, err, "does not match fetched public key")
}

func TestRemoteSignerKeyringRejectsUnknownKeyName(t *testing.T) {
	kr, err := newRemoteSignerKeyring("reporter", testCompressedPubKey(2), nil, false)
	require.NoError(t, err)

	_, err = kr.Key("other")
	require.Error(t, err)
	require.ErrorContains(t, err, "not found")

	_, _, err = kr.Sign("other", []byte("sign-doc"), signing.SignMode_SIGN_MODE_DIRECT)
	require.Error(t, err)
	require.ErrorContains(t, err, "not found")
}

func testCompressedPubKey(seed byte) []byte {
	key := make([]byte, 33)
	key[0] = 0x02
	for i := 1; i < len(key); i++ {
		key[i] = seed + byte(i)
	}
	return key
}
