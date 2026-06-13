package client

import (
	"context"
	"crypto/sha256"
	"fmt"

	signerv1 "github.com/tellor-io/bridge-remote-signer/api/gen/signer/v1"
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
}

// newRemoteSignerKeyring creates a remoteSignerKeyring.
// pubKeyBytes must be a 33-byte compressed secp256k1 public key.
func newRemoteSignerKeyring(keyName string, pubKeyBytes []byte, signerConn signerv1.BridgeSignerClient) (*remoteSignerKeyring, error) {
	if len(pubKeyBytes) != 33 {
		return nil, fmt.Errorf("newRemoteSignerKeyring: expected 33-byte compressed public key, got %d", len(pubKeyBytes))
	}
	pubKey := &cosmossecp.PubKey{Key: pubKeyBytes}
	return &remoteSignerKeyring{
		keyName:    keyName,
		pubKey:     pubKey,
		signerConn: signerConn,
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
// Computes sha256(msg) and calls SignRaw on the remote signer, returning a 64-byte (r||s) signature.
func (r *remoteSignerKeyring) Sign(_ string, msg []byte, _ signing.SignMode) ([]byte, cryptotypes.PubKey, error) {
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
// the account address. The returned grpc.ClientConn must be closed when done.
func newKeyringFromRemoteSigner(ctx context.Context, keyName, addr string) (keyring.Keyring, sdk.AccAddress, *grpc.ClientConn, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("dial remote signer at %s: %w", addr, err)
	}

	signerClient := signerv1.NewBridgeSignerClient(conn)

	pubKeyResp, err := signerClient.GetPublicKey(ctx, &signerv1.GetPublicKeyRequest{})
	if err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("GetPublicKey from remote signer: %w", err)
	}

	addrResp, err := signerClient.GetAddress(ctx, &signerv1.GetAddressRequest{Prefix: "tellor"})
	if err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("GetAddress from remote signer: %w", err)
	}

	accAddr, err := sdk.AccAddressFromBech32(addrResp.Address)
	if err != nil {
		conn.Close()
		return nil, nil, nil, fmt.Errorf("parse address %q from remote signer: %w", addrResp.Address, err)
	}

	kr, err := newRemoteSignerKeyring(keyName, pubKeyResp.PublicKey, signerClient)
	if err != nil {
		conn.Close()
		return nil, nil, nil, err
	}

	return kr, accAddr, conn, nil
}
