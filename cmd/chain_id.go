package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
)

type detectedEndpointChainID struct {
	endpoint string
	chainID  string
}

type chainIDDetector func(context.Context, string) (string, error)

var chainIDEndpointDetectTimeout = 15 * time.Second

func detectChainIDFromEndpoints(ctx context.Context, grpcAddrs, nodeRPCAddrs []string) (string, string, string, error) {
	grpcChainIDs, err := detectEndpointChainIDs(ctx, "gRPC", grpcAddrs, chainIDFromGRPC)
	if err != nil {
		return "", "", "", err
	}

	rpcChainIDs, err := detectEndpointChainIDs(ctx, "node RPC", nodeRPCAddrs, chainIDFromRPC)
	if err != nil {
		return "", "", "", err
	}

	if err := validateReachableChainIDs("gRPC", grpcChainIDs); err != nil {
		return "", "", "", err
	}
	if err := validateReachableChainIDs("node RPC", rpcChainIDs); err != nil {
		return "", "", "", err
	}
	if grpcChainIDs[0].chainID != rpcChainIDs[0].chainID {
		return "", "", "", fmt.Errorf("chain ID mismatch: gRPC returned %q, node RPC returned %q", grpcChainIDs[0].chainID, rpcChainIDs[0].chainID)
	}

	return grpcChainIDs[0].chainID, grpcChainIDs[0].endpoint, rpcChainIDs[0].endpoint, nil
}

func detectEndpointChainIDs(ctx context.Context, endpointType string, endpoints []string, detector chainIDDetector) ([]detectedEndpointChainID, error) {
	var detected []detectedEndpointChainID
	var errs []string
	for _, endpoint := range endpoints {
		endpointCtx, cancel := context.WithTimeout(ctx, chainIDEndpointDetectTimeout)
		chainID, err := detector(endpointCtx, endpoint)
		cancel()
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", endpoint, err))
			continue
		}
		detected = append(detected, detectedEndpointChainID{endpoint: endpoint, chainID: chainID})
	}
	if len(detected) == 0 {
		return nil, fmt.Errorf("failed to detect chain ID via any %s endpoint: %s", endpointType, strings.Join(errs, "; "))
	}
	return detected, nil
}

func validateReachableChainIDs(endpointType string, detected []detectedEndpointChainID) error {
	expected := detected[0].chainID
	for _, item := range detected[1:] {
		if item.chainID != expected {
			return fmt.Errorf("%s endpoints disagree on chain ID: %s returned %q, %s returned %q", endpointType, detected[0].endpoint, expected, item.endpoint, item.chainID)
		}
	}
	return nil
}

func chainIDFromGRPC(ctx context.Context, grpcAddr string) (string, error) {
	conn, err := grpc.DialContext(ctx, grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return "", fmt.Errorf("dial: %w", err)
	}
	defer conn.Close()

	resp, err := cmtservice.NewServiceClient(conn).GetNodeInfo(ctx, &cmtservice.GetNodeInfoRequest{})
	if err != nil {
		return "", fmt.Errorf("GetNodeInfo: %w", err)
	}

	return resp.DefaultNodeInfo.Network, nil
}

func chainIDFromRPC(ctx context.Context, nodeRPCAddr string) (string, error) {
	rpcClient, err := rpchttp.New(nodeRPCAddr, "/websocket")
	if err != nil {
		return "", fmt.Errorf("create client: %w", err)
	}

	status, err := rpcClient.Status(ctx)
	if err != nil {
		return "", fmt.Errorf("status: %w", err)
	}

	return status.NodeInfo.Network, nil
}
