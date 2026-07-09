package client

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	signerv1 "github.com/tellor-io/bridge-remote-signer/api/gen/signer/v1"
	"google.golang.org/grpc"
)

// stubSignerClient implements signerv1.BridgeSignerClient for testing the
// best-effort chain-ID discovery. Only GetChainID is exercised; the other
// methods are present to satisfy the interface and panic if called unexpectedly.
type stubSignerClient struct {
	chainID string
	err     error
}

func (s stubSignerClient) GetChainID(_ context.Context, _ *signerv1.GetChainIDRequest, _ ...grpc.CallOption) (*signerv1.GetChainIDResponse, error) {
	if s.err != nil {
		return nil, s.err
	}
	return &signerv1.GetChainIDResponse{ChainId: s.chainID}, nil
}

func (s stubSignerClient) Sign(context.Context, *signerv1.SignRequest, ...grpc.CallOption) (*signerv1.SignResponse, error) {
	panic("Sign not expected in this test")
}

func (s stubSignerClient) GetPublicKey(context.Context, *signerv1.GetPublicKeyRequest, ...grpc.CallOption) (*signerv1.GetPublicKeyResponse, error) {
	panic("GetPublicKey not expected in this test")
}

func (s stubSignerClient) SignRaw(context.Context, *signerv1.SignRawRequest, ...grpc.CallOption) (*signerv1.SignRawResponse, error) {
	panic("SignRaw not expected in this test")
}

func (s stubSignerClient) GetAddress(context.Context, *signerv1.GetAddressRequest, ...grpc.CallOption) (*signerv1.GetAddressResponse, error) {
	panic("GetAddress not expected in this test")
}

func (s stubSignerClient) SignTx(context.Context, *signerv1.SignTxRequest, ...grpc.CallOption) (*signerv1.SignTxResponse, error) {
	panic("SignTx not expected in this test")
}

func (s stubSignerClient) SignBridgeCheckpoint(context.Context, *signerv1.SignBridgeCheckpointRequest, ...grpc.CallOption) (*signerv1.SignBridgeCheckpointResponse, error) {
	panic("SignBridgeCheckpoint not expected in this test")
}

func TestFetchRemoteSignerChainID(t *testing.T) {
	ctx := context.Background()

	// Signer returns a chain ID — it is used.
	require.Equal(t, "layertest-5", fetchRemoteSignerChainID(ctx, stubSignerClient{chainID: "layertest-5"}))

	// Signer does not implement the RPC / errors — discovery is best-effort, so "".
	require.Equal(t, "", fetchRemoteSignerChainID(ctx, stubSignerClient{err: fmt.Errorf("unimplemented")}))

	// Signer implements the RPC but has no chain ID configured — "".
	require.Equal(t, "", fetchRemoteSignerChainID(ctx, stubSignerClient{chainID: ""}))
}
