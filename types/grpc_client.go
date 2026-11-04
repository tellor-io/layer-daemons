package types

import (
	"context"
	"fmt"
	"net"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"github.com/tellor-io/layer-daemons/constants"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GrpcClientImpl is the struct that implements the `GrpcClient` interface.
type GrpcClientImpl struct{}

// Ensure the `GrpcClient` interface is implemented at compile time.
var _ GrpcClient = (*GrpcClientImpl)(nil)

// GrpcClient is an interface that encapsulates the `NewGrpcConnection` function and `CloseConnection`.
type GrpcClient interface {
	NewGrpcConnection(ctx context.Context, socketAddress string) (*grpc.ClientConn, error)
	NewTcpConnection(ctx context.Context, endpoint string) (*grpc.ClientConn, error)
	CloseConnection(grpcConn *grpc.ClientConn) error
}

// gogoProtoCodec unmarshals with cosmos/gogoproto so custom types like math.LegacyDec
// work. The default google.golang.org/protobuf codec panics on those fields.
type gogoProtoCodec struct{}

func (gogoProtoCodec) Marshal(v any) ([]byte, error) {
	msg, ok := v.(gogoproto.Message)
	if !ok {
		return nil, fmt.Errorf("failed to assert gogoproto.Message")
	}
	return gogoproto.Marshal(msg)
}

func (gogoProtoCodec) Unmarshal(data []byte, v any) error {
	msg, ok := v.(gogoproto.Message)
	if !ok {
		return fmt.Errorf("failed to assert gogoproto.Message")
	}
	return gogoproto.Unmarshal(data, msg)
}

func (gogoProtoCodec) Name() string {
	return "proto"
}

func cosmosGRPCDialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithDefaultCallOptions(grpc.ForceCodec(gogoProtoCodec{})),
	}
}

// NewGrpcConnection calls `grpc.Dial` with custom parameters to create a secure connection
// with context that blocks until the underlying connection is up.
func (g *GrpcClientImpl) NewGrpcConnection(
	ctx context.Context,
	socketAddress string,
) (*grpc.ClientConn, error) {
	opts := append(cosmosGRPCDialOptions(),
		// https://github.com/grpc/grpc-go/blob/master/dialoptions.go#L264
		grpc.WithContextDialer(func(ctx context.Context, addr string) (net.Conn, error) {
			// Create a custom `net.Dialer` in order to specify `unix` as the desired network.
			var dialer net.Dialer
			return dialer.DialContext(ctx, constants.UnixProtocol, addr)
		}),
	)
	return grpc.DialContext(ctx, socketAddress, opts...)
}

// NewTcpConnection calls `grpc.Dial` to create an insecure tcp connection.
func (g *GrpcClientImpl) NewTcpConnection(
	ctx context.Context,
	endpoint string,
) (*grpc.ClientConn, error) {
	return grpc.DialContext(ctx, endpoint, cosmosGRPCDialOptions()...)
}

// CloseConnection calls `grpc.ClientConn.Close()` to close a grpc connection.
func (g *GrpcClientImpl) CloseConnection(grpcConn *grpc.ClientConn) error {
	return grpcConn.Close()
}
