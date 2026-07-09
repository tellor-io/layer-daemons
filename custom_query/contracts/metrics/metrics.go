package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// Contract call metrics
	ContractCallSuccess = promauto.NewCounter(prometheus.CounterOpts{
		Name: "contract_reader_calls_success_total",
		Help: "Total number of successful contract calls",
	})

	ContractCallErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "contract_reader_calls_errors_total",
		Help: "Total number of failed contract calls",
	})

	ContractCallDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "contract_reader_call_duration_seconds",
		Help:    "Duration of contract calls in seconds",
		Buckets: prometheus.DefBuckets,
	})

	// RPC endpoint metrics
	RPCCallSuccess = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rpc_reader_calls_success_total",
		Help: "Total number of successful RPC calls",
	})

	RPCCallErrors = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rpc_reader_calls_errors_total",
		Help: "Total number of failed RPC calls",
	})

	RPCCallDuration = promauto.NewHistogram(prometheus.HistogramOpts{
		Name:    "rpc_reader_call_duration_seconds",
		Help:    "Duration of RPC calls in seconds",
		Buckets: prometheus.DefBuckets,
	})

	// Response-cache metrics (see rpc_reader/cache.go). Useful to confirm the
	// cache is actually shielding upstream APIs from rate limits.
	RPCCacheHits = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rpc_reader_cache_hits_total",
		Help: "Total number of RPC responses served from the in-memory cache",
	})

	RPCCacheMisses = promauto.NewCounter(prometheus.CounterOpts{
		Name: "rpc_reader_cache_misses_total",
		Help: "Total number of RPC cache misses that triggered an upstream fetch",
	})

	// Health check metrics (used by both contract and RPC readers)
	RPCHealthCheckFailures = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "reader_health_check_failures_total",
		Help: "Total number of health check failures",
	}, []string{"endpoint", "type"})
)
