package utils

import (
	"fmt"
	"os"
	"strings"
)

const (
	EnvBridgeChainRPCNodes = "BRIDGE_CHAIN_RPC_NODES"
	EnvETHMainnetRPCNodes  = "ETH_MAINNET_RPC_NODES"
)

func ParseEndpointList(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	endpoints := make([]string, 0, len(parts))
	for _, part := range parts {
		endpoint := strings.TrimSpace(part)
		if endpoint == "" {
			return nil, fmt.Errorf("endpoint list contains an empty entry")
		}
		endpoints = append(endpoints, endpoint)
	}
	return endpoints, nil
}

func BridgeChainRPCNodesFromEnv() ([]string, error) {
	value := strings.TrimSpace(os.Getenv(EnvBridgeChainRPCNodes))
	if value == "" {
		return nil, fmt.Errorf("%s not set", EnvBridgeChainRPCNodes)
	}
	endpoints, err := ParseEndpointList(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvBridgeChainRPCNodes, err)
	}
	return endpoints, nil
}

func ETHMainnetRPCNodesFromEnv() ([]string, error) {
	value := strings.TrimSpace(os.Getenv(EnvETHMainnetRPCNodes))
	if value == "" {
		return nil, fmt.Errorf("%s not set", EnvETHMainnetRPCNodes)
	}
	endpoints, err := ParseEndpointList(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvETHMainnetRPCNodes, err)
	}
	return endpoints, nil
}
