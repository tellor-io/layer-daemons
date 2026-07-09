package client

import (
	"context"

	"google.golang.org/grpc"

	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewRemoteSignerKeyringTx is an exported wrapper around newKeyringFromRemoteSigner
// that signs every tx through the scope-checked SignTx RPC (the blind SignRaw is
// disabled on the hardened signer). It lets standalone operator commands
// (cmd/unjail, cmd/create-reporter, cmd/create-validator, cmd/edit-validator) sign
// cosmos txs through the remote signer without a local key; each command's message
// type must be on the signer's SignTx allowlist. The chain ID returned by the
// signer is discarded here; tools that need it should call
// newKeyringFromRemoteSigner directly.
func NewRemoteSignerKeyringTx(ctx context.Context, keyName, addr, caCert, clientCert, clientKey string) (keyring.Keyring, sdk.AccAddress, *grpc.ClientConn, error) {
	kr, accAddr, _, conn, err := newKeyringFromRemoteSigner(ctx, keyName, addr, caCert, clientCert, clientKey, true)
	return kr, accAddr, conn, err
}
