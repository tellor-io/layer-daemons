package dispute

import (
	"os"
	"strconv"
	"strings"
	"time"
)

// LoadConfigFromEnv builds the dispute monitor config from environment variables:
//
//	DISPUTE_MONITOR_ENABLED   - "true"/"1"/"yes"/"on" to enable (default off)
//	API_URLS                  - comma-separated Layer REST API URLs (open-disputes query)
//	DISPUTE_IGNORE_IDS        - comma-separated dispute IDs to ignore
//	DISPUTE_CHECK_INTERVAL    - API poll interval (e.g. 1s; default 1s)
//
// rpcEndpoints are the daemon's resolved RPC nodes, used for new_dispute event subscription.
func LoadConfigFromEnv(rpcEndpoints []string) Config {
	cfg := Config{CheckInterval: 30 * time.Second, RPCEndpoints: rpcEndpoints}

	cfg.Enabled = isTrue(os.Getenv("DISPUTE_MONITOR_ENABLED"))

	if urls := os.Getenv("API_URLS"); urls != "" {
		for _, url := range strings.Split(urls, ",") {
			if trimmed := strings.TrimSpace(url); trimmed != "" {
				cfg.LayerAPIURLs = append(cfg.LayerAPIURLs, trimmed)
			}
		}
	}

	if ignoreIDs := os.Getenv("DISPUTE_IGNORE_IDS"); ignoreIDs != "" {
		for _, idStr := range strings.Split(ignoreIDs, ",") {
			if id, err := strconv.ParseUint(strings.TrimSpace(idStr), 10, 64); err == nil {
				cfg.IgnoreDisputes = append(cfg.IgnoreDisputes, id)
			}
		}
	}

	if interval := os.Getenv("DISPUTE_CHECK_INTERVAL"); interval != "" {
		if d, err := time.ParseDuration(interval); err == nil && d > 0 {
			cfg.CheckInterval = d
		}
	}

	return cfg
}

func isTrue(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}
