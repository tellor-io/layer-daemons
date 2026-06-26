package client

import (
	"fmt"
	"strings"
)

func endpointRole(index int) string {
	if index == 0 {
		return "primary"
	}
	return "fallback"
}

func endpointIndex(endpoints []string, endpoint string) int {
	for idx, candidate := range endpoints {
		if candidate == endpoint {
			return idx
		}
	}
	return -1
}

func endpointLogFields(endpoints []string, endpoint string) []interface{} {
	index := endpointIndex(endpoints, endpoint)
	if index < 0 {
		return []interface{}{"endpoint_index", "unknown", "endpoint_role", "unknown"}
	}
	return []interface{}{"endpoint_index", index, "endpoint_role", endpointRole(index)}
}

func endpointSafeError(err error, endpoints []string) error {
	if err == nil {
		return nil
	}
	msg := err.Error()
	for _, endpoint := range endpoints {
		msg = strings.ReplaceAll(msg, endpoint, "<redacted endpoint>")
	}
	return fmt.Errorf("%s", msg)
}

func endpointErrorSummary(endpoints []string, endpoint string, err error) string {
	index := endpointIndex(endpoints, endpoint)
	if index < 0 {
		return fmt.Sprintf("endpoint_index=unknown endpoint_role=unknown: %v", endpointSafeError(err, endpoints))
	}
	return fmt.Sprintf("endpoint_index=%d endpoint_role=%s: %v", index, endpointRole(index), endpointSafeError(err, endpoints))
}
