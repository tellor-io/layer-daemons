package main

import (
	"fmt"
	"os"
	"strings"
)

const (
	envRPCNodes  = "RPC_NODES"
	envGRPCNodes = "GRPC_NODES"
)

type endpointListConfig struct {
	Endpoints []string
	Source    string
}

func parseGRPCEndpoints(value string) ([]string, error) {
	return parseEndpointValues(value)
}

func parseRPCEndpoints(value string) ([]string, error) {
	return parseEndpointValues(value)
}

func parseEndpointValues(value string) ([]string, error) {
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

func endpointsFromEnvOrFlag(envName, flagName, flagValue string) (endpointListConfig, error) {
	if envValue, ok := os.LookupEnv(envName); ok {
		endpoints, err := parseEndpointValues(envValue)
		if err != nil {
			return endpointListConfig{}, fmt.Errorf("%s: %w", envName, err)
		}
		return endpointListConfig{Endpoints: endpoints, Source: envName}, nil
	}

	endpoints, err := parseEndpointValues(flagValue)
	if err != nil {
		return endpointListConfig{}, fmt.Errorf("--%s: %w", flagName, err)
	}
	return endpointListConfig{Endpoints: endpoints, Source: "--" + flagName}, nil
}

func grpcEndpointsFromEnvOrFlag(flagValue string) (endpointListConfig, error) {
	return endpointsFromEnvOrFlag(envGRPCNodes, "grpc", flagValue)
}

func rpcEndpointsFromEnvOrFlag(flagValue string) (endpointListConfig, error) {
	return endpointsFromEnvOrFlag(envRPCNodes, "node", flagValue)
}

func moveEndpointToFront(endpoints []string, selected string) []string {
	reordered := append([]string(nil), endpoints...)
	for i, endpoint := range reordered {
		if endpoint != selected {
			continue
		}
		copy(reordered[1:i+1], reordered[0:i])
		reordered[0] = selected
		return reordered
	}
	return reordered
}
