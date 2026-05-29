package utils

import (
	"fmt"
	"os"
	"strings"
)

const EnvETHRPCNodes = "ETH_RPC_NODES"

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

func ETHRPCNodesFromEnv() ([]string, error) {
	value := strings.TrimSpace(os.Getenv(EnvETHRPCNodes))
	if value == "" {
		return nil, fmt.Errorf("%s not set", EnvETHRPCNodes)
	}
	endpoints, err := ParseEndpointList(value)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", EnvETHRPCNodes, err)
	}
	return endpoints, nil
}
