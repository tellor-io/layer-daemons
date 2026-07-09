package client

import (
	"context"
	"crypto/sha256"
	"fmt"

	signerv1 "github.com/tellor-io/bridge-remote-signer/api/gen/signer/v1"
	bridgetls "github.com/tellor-io/bridge-remote-signer/api/tls"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cosmossecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
)

// remoteSignerKeyring implements keyring.Keyring backed by a remote gRPC signer.
// Only Key() and Sign() are functional; all other methods return errors.
// This lets the reporter sign cosmos txs without a local private key file.
type remoteSignerKeyring struct {
	keyName    string
	pubKey     cryptotypes.PubKey
	signerConn signerv1.BridgeSignerClient

	// useSignTx selects the signer RPC. When true (the always-on reporter
	// daemon), Sign sends the raw SignDoc to SignTx, where the signer decodes it
	// and rejects any message type not on its allowlist — so a compromised
	// reporter cannot sign an arbitrary fund-moving tx. When false (the one-shot
	// operator commands run manually with the node cert), Sign uses the blind
	// SignRaw path, since those commands sign high-stakes one-time messages
	// (create-validator/-reporter, unjail) that must NOT be on the reporter
	// allowlist.
	useSignTx bool
}

// newRemoteSignerKeyring creates a remoteSignerKeyring.
// pubKeyBytes must be a 33-byte compressed secp256k1 public key.
func newRemoteSignerKeyring(keyName string, pubKeyBytes []byte, signerConn signerv1.BridgeSignerClient, useSignTx bool) (*remoteSignerKeyring, error) {
	if len(pubKeyBytes) != 33 {
		return nil, fmt.Errorf("newRemoteSignerKeyring: expected 33-byte compressed public key, got %d", len(pubKeyBytes))
	}
	pubKey := &cosmossecp.PubKey{Key: pubKeyBytes}
	return &remoteSignerKeyring{
		keyName:    keyName,
		pubKey:     pubKey,
		signerConn: signerConn,
		useSignTx:  useSignTx,
	}, nil
}

// Backend implements keyring.Keyring.
func (r *remoteSignerKeyring) Backend() string { return "remote-signer" }

// List implements keyring.Keyring.
func (r *remoteSignerKeyring) List() ([]*keyring.Record, error) {
	rec, err := keyring.NewOfflineRecord(r.keyName, r.pubKey)
	if err != nil {
		return nil, err
	}
	return []*keyring.Record{rec}, nil
}

// SupportedAlgorithms implements keyring.Keyring.
func (r *remoteSignerKeyring) SupportedAlgorithms() (keyring.SigningAlgoList, keyring.SigningAlgoList) {
	return keyring.SigningAlgoList{hd.Secp256k1}, keyring.SigningAlgoList{}
}

// Key implements keyring.Keyring. Returns the offline record for the managed key.
func (r *remoteSignerKeyring) Key(uid string) (*keyring.Record, error) {
	if uid != r.keyName {
		return nil, fmt.Errorf("remoteSignerKeyring.Key: key %q not found", uid)
	}
	rec, err := keyring.NewOfflineRecord(uid, r.pubKey)
	if err != nil {
		return nil, fmt.Errorf("remoteSignerKeyring.Key: %w", err)
	}
	return rec, nil
}

// KeyByAddress implements keyring.Keyring.
func (r *remoteSignerKeyring) KeyByAddress(address sdk.Address) (*keyring.Record, error) {
	myAddr := sdk.AccAddress(r.pubKey.Address())
	if !myAddr.Equals(address) {
		return nil, fmt.Errorf("remoteSignerKeyring.KeyByAddress: address not found")
	}
	return r.Key(r.keyName)
}

// Delete implements keyring.Keyring.
func (r *remoteSignerKeyring) Delete(_ string) error {
	return fmt.Errorf("remoteSignerKeyring: Delete not supported")
}

// DeleteByAddress implements keyring.Keyring.
func (r *remoteSignerKeyring) DeleteByAddress(_ sdk.Address) error {
	return fmt.Errorf("remoteSignerKeyring: DeleteByAddress not supported")
}

// Rename implements keyring.Keyring.
func (r *remoteSignerKeyring) Rename(_, _ string) error {
	return fmt.Errorf("remoteSignerKeyring: Rename not supported")
}

// NewMnemonic implements keyring.Keyring.
func (r *remoteSignerKeyring) NewMnemonic(_ string, _ keyring.Language, _, _ string, _ keyring.SignatureAlgo) (*keyring.Record, string, error) {
	return nil, "", fmt.Errorf("remoteSignerKeyring: NewMnemonic not supported")
}

// NewAccount implements keyring.Keyring.
func (r *remoteSignerKeyring) NewAccount(_, _, _, _ string, _ keyring.SignatureAlgo) (*keyring.Record, error) {
	return nil, fmt.Errorf("remoteSignerKeyring: NewAccount not supported")
}

// SaveLedgerKey implements keyring.Keyring.
func (r *remoteSignerKeyring) SaveLedgerKey(_ string, _ keyring.SignatureAlgo, _ string, _, _, _ uint32) (*keyring.Record, error) {
	return nil, fmt.Errorf("remoteSignerKeyring: SaveLedgerKey not supported")
}

// SaveOfflineKey implements keyring.Keyring.
func (r *remoteSignerKeyring) SaveOfflineKey(_ string, _ cryptotypes.PubKey) (*keyring.Record, error) {
	return nil, fmt.Errorf("remoteSignerKeyring: SaveOfflineKey not supported")
}

// SaveMultisig implements keyring.Keyring.
func (r *remoteSignerKeyring) SaveMultisig(_ string, _ cryptotypes.PubKey) (*keyring.Record, error) {
	return nil, fmt.Errorf("remoteSignerKeyring: SaveMultisig not supported")
}

// Sign implements keyring.Signer (part of keyring.Keyring).
//
// For SIGN_MODE_DIRECT the supplied msg is the raw protobuf-encoded SignDoc
// bytes. When useSignTx is set, those bytes are sent to the signer's SignTx RPC,
// which decodes the SignDoc and rejects any message type not on its allowlist
// before signing sha256(SignDoc) — so the reporter can only sign the message
// types it is scoped to. Otherwise it falls back to the blind SignRaw path
// (sha256(msg) -> SignRaw) used by the trusted one-shot operator commands.
// Both paths return a 64-byte (r||s) signature.
func (r *remoteSignerKeyring) Sign(uid string, msg []byte, _ signing.SignMode) ([]byte, cryptotypes.PubKey, error) {
	if uid != r.keyName {
		return nil, nil, fmt.Errorf("remoteSignerKeyring.Sign: key %q not found", uid)
	}
	if r.useSignTx {
		resp, err := r.signerConn.SignTx(context.Background(), &signerv1.SignTxRequest{
			SignDoc:   msg,
			RequestId: "reporter-tx",
		})
		if err != nil {
			return nil, nil, fmt.Errorf("remoteSignerKeyring.Sign: SignTx RPC failed: %w", err)
		}
		return resp.Signature, r.pubKey, nil
	}

	hash := sha256.Sum256(msg)
	resp, err := r.signerConn.SignRaw(context.Background(), &signerv1.SignRawRequest{
		Msg:       hash[:],
		RequestId: "reporter-tx",
	})
	if err != nil {
		return nil, nil, fmt.Errorf("remoteSignerKeyring.Sign: SignRaw RPC failed: %w", err)
	}
	return resp.Signature, r.pubKey, nil
}

// SignByAddress implements keyring.Signer (part of keyring.Keyring).
func (r *remoteSignerKeyring) SignByAddress(address sdk.Address, msg []byte, signMode signing.SignMode) ([]byte, cryptotypes.PubKey, error) {
	myAddr := sdk.AccAddress(r.pubKey.Address())
	if !myAddr.Equals(address) {
		return nil, nil, fmt.Errorf("remoteSignerKeyring.SignByAddress: address mismatch")
	}
	return r.Sign(r.keyName, msg, signMode)
}

// ImportPrivKey implements keyring.Importer.
func (r *remoteSignerKeyring) ImportPrivKey(_, _, _ string) error {
	return fmt.Errorf("remoteSignerKeyring: ImportPrivKey not supported")
}

// ImportPrivKeyHex implements keyring.Importer.
func (r *remoteSignerKeyring) ImportPrivKeyHex(_, _, _ string) error {
	return fmt.Errorf("remoteSignerKeyring: ImportPrivKeyHex not supported")
}

// ImportPubKey implements keyring.Importer.
func (r *remoteSignerKeyring) ImportPubKey(_, _ string) error {
	return fmt.Errorf("remoteSignerKeyring: ImportPubKey not supported")
}

// MigrateAll implements keyring.Migrator.
func (r *remoteSignerKeyring) MigrateAll() ([]*keyring.Record, error) {
	return nil, fmt.Errorf("remoteSignerKeyring: MigrateAll not supported")
}

// ExportPubKeyArmor implements keyring.Exporter.
func (r *remoteSignerKeyring) ExportPubKeyArmor(_ string) (string, error) {
	return "", fmt.Errorf("remoteSignerKeyring: ExportPubKeyArmor not supported")
}

// ExportPubKeyArmorByAddress implements keyring.Exporter.
func (r *remoteSignerKeyring) ExportPubKeyArmorByAddress(_ sdk.Address) (string, error) {
	return "", fmt.Errorf("remoteSignerKeyring: ExportPubKeyArmorByAddress not supported")
}

// ExportPrivKeyArmor implements keyring.Exporter.
func (r *remoteSignerKeyring) ExportPrivKeyArmor(_, _ string) (string, error) {
	return "", fmt.Errorf("remoteSignerKeyring: ExportPrivKeyArmor not supported")
}

// ExportPrivKeyArmorByAddress implements keyring.Exporter.
func (r *remoteSignerKeyring) ExportPrivKeyArmorByAddress(_ sdk.Address, _ string) (string, error) {
	return "", fmt.Errorf("remoteSignerKeyring: ExportPrivKeyArmorByAddress not supported")
}

// Ensure compile-time interface compliance.
var _ keyring.Keyring = (*remoteSignerKeyring)(nil)

// newKeyringFromRemoteSigner dials the remote signer at addr, fetches the public key
// and bech32 address, and returns a keyring backed by the remote signer along with
// the account address and the chain ID the signer is configured for (empty if the
// signer does not provide one). The returned grpc.ClientConn must be closed when done.
// When caCert, clientCert, and clientKey are all non-empty, mTLS is used;
// otherwise the connection falls back to insecure (for local/test use only).
// useSignTx selects the scope-checked SignTx path (reporter daemon) vs the blind
// SignRaw path (one-shot operator commands); see remoteSignerKeyring.useSignTx.
func newKeyringFromRemoteSigner(ctx context.Context, keyName, addr, caCert, clientCert, clientKey string, useSignTx bool) (keyring.Keyring, sdk.AccAddress, string, *grpc.ClientConn, error) {
	var dialOpt grpc.DialOption
	if caCert != "" && clientCert != "" && clientKey != "" {
		creds, err := bridgetls.NewClientCredentials(caCert, clientCert, clientKey, "bridge-signer")
		if err != nil {
			return nil, nil, "", nil, fmt.Errorf("load mTLS credentials: %w", err)
		}
		dialOpt = grpc.WithTransportCredentials(creds)
	} else {
		dialOpt = grpc.WithTransportCredentials(insecure.NewCredentials())
	}
	conn, err := grpc.NewClient(addr, dialOpt)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("dial remote signer at %s: %w", addr, err)
	}
	// Close the connection on any error path; cleared once we hand it to the caller.
	ok := false
	defer func() {
		if !ok {
			conn.Close()
		}
	}()

	signerClient := signerv1.NewBridgeSignerClient(conn)

	pubKeyResp, err := signerClient.GetPublicKey(ctx, &signerv1.GetPublicKeyRequest{})
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("GetPublicKey from remote signer: %w", err)
	}

	addrResp, err := signerClient.GetAddress(ctx, &signerv1.GetAddressRequest{Prefix: "tellor"})
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("GetAddress from remote signer: %w", err)
	}

	kr, err := newRemoteSignerKeyring(keyName, pubKeyResp.PublicKey, signerClient, useSignTx)
	if err != nil {
		return nil, nil, "", nil, err
	}

	accAddr, err := remoteSignerAccountAddress(kr.pubKey, addrResp.Address)
	if err != nil {
		return nil, nil, "", nil, err
	}

	// Best-effort: discover the chain ID from the signer so it does not have to be
	// supplied separately. A signer built before the GetChainID RPC (or one with no
	// chain ID configured) returns an error here; that is not fatal — the caller
	// falls back to its own chain-ID source.
	chainID := fetchRemoteSignerChainID(ctx, signerClient)

	ok = true
	return kr, accAddr, chainID, conn, nil
}

// fetchRemoteSignerChainID asks the remote signer for its configured chain ID.
// It returns "" (and never an error) when the signer does not implement the
// GetChainID RPC or has no chain ID configured, so chain-ID discovery stays
// optional and backward compatible with older signers.
func fetchRemoteSignerChainID(ctx context.Context, signerClient signerv1.BridgeSignerClient) string {
	resp, err := signerClient.GetChainID(ctx, &signerv1.GetChainIDRequest{})
	if err != nil {
		return ""
	}
	return resp.ChainId
}

// remoteSignerAccountAddress parses the bech32 address the signer reported and verifies it
// matches the fetched public key, so a misconfigured signer cannot make the reporter act on
// the wrong account.
func remoteSignerAccountAddress(pubKey cryptotypes.PubKey, bech32Addr string) (sdk.AccAddress, error) {
	accAddr, err := sdk.AccAddressFromBech32(bech32Addr)
	if err != nil {
		return nil, fmt.Errorf("parse address %q from remote signer: %w", bech32Addr, err)
	}
	expectedAddr := sdk.AccAddress(pubKey.Address())
	if !expectedAddr.Equals(accAddr) {
		return nil, fmt.Errorf("remote signer address %q does not match fetched public key", bech32Addr)
	}
	return accAddr, nil
}
