package flags

import (
	"github.com/spf13/cast"
	"github.com/spf13/cobra"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
)

// List of CLI flags for Server and Client.
const (
	// Flag names
	FlagUnixSocketAddress           = "unix-socket-address"
	FlagPanicOnDaemonFailureEnabled = "panic-on-daemon-failure-enabled"
	FlagMaxDaemonUnhealthySeconds   = "max-daemon-unhealthy-seconds"

	FlagPriceDaemonLoopDelayMs         = "price-daemon-loop-delay-ms"
	FlagBatchableRefreshIntervalMs     = "batchable-refresh-interval-ms"
	FlagExchangeCacheRefreshIntervalMs = "exchange-cache-refresh-interval-ms"
	FlagOracleConfigFile               = "oracle-config-file"
	FlagOracleConfigDir                = "oracle-config-dir"

	FlagKeyringBackend = "keyring-backend"
)

// Shared flags contains configuration flags shared by all daemons.
type SharedFlags struct {
	// SocketAddress is the location of the unix socket to communicate with the daemon gRPC service.
	SocketAddress string
	// PanicOnDaemonFailureEnabled toggles whether the daemon should panic on failure.
	PanicOnDaemonFailureEnabled bool
	// MaxDaemonUnhealthySeconds is the maximum allowable duration for which a daemon can be unhealthy.
	MaxDaemonUnhealthySeconds uint32
}

// PriceFlags contains configuration flags for the Price Daemon.
type PriceFlags struct {
	// Enabled toggles the price daemon on or off.
	// TODO: remove this field
	Enabled bool
	// LoopDelayMs configures the update frequency of the price daemon.
	LoopDelayMs uint32
	// BatchableRefreshIntervalMs configures custom-query batchable source cache refresh cadence.
	BatchableRefreshIntervalMs uint32
	// ExchangeCacheRefreshIntervalMs configures custom-query exchange source cache refresh cadence.
	ExchangeCacheRefreshIntervalMs uint32
	// OracleConfigFile is the TOML file name (under OracleConfigDir) with endpoints, queries, and optional [[markets]].
	OracleConfigFile string
	// OracleConfigDir is the directory under the node home for the oracle TOML (default "config").
	OracleConfigDir string
}

// DaemonFlags contains the collected configuration flags for all daemons.
type DaemonFlags struct {
	Shared SharedFlags
	Price  PriceFlags
}

var defaultDaemonFlags *DaemonFlags

// GetDefaultDaemonFlags returns the default values for the Daemon Flags using a singleton pattern.
func GetDefaultDaemonFlags() DaemonFlags {
	if defaultDaemonFlags == nil {
		defaultDaemonFlags = &DaemonFlags{
			Shared: SharedFlags{
				SocketAddress:               "/tmp/daemons.sock",
				PanicOnDaemonFailureEnabled: true,
				MaxDaemonUnhealthySeconds:   5 * 60, // 5 minutes.
			},
			Price: PriceFlags{
				Enabled:                        true,
				LoopDelayMs:                    3_000,
				BatchableRefreshIntervalMs:     3_000,
				ExchangeCacheRefreshIntervalMs: 3_000,
				OracleConfigFile:               "custom_query_config.toml",
				OracleConfigDir:                "config",
			},
		}
	}
	return *defaultDaemonFlags
}

// AddDaemonFlagsToCmd adds the required flags to instantiate a server and client for
// price updates. These flags should be applied to the `start` command LAYER Cosmos application.
func AddDaemonFlagsToCmd(
	cmd *cobra.Command,
) {
	//
	df := GetDefaultDaemonFlags()

	// Shared Flags.
	cmd.Flags().String(
		FlagUnixSocketAddress,
		df.Shared.SocketAddress,
		"Socket address for the daemons to send updates to, if not set "+
			"will establish default location to ingest daemon updates from",
	)
	cmd.Flags().Bool(
		FlagPanicOnDaemonFailureEnabled,
		df.Shared.PanicOnDaemonFailureEnabled,
		"Enables panicking when a daemon fails.",
	)
	cmd.Flags().Uint32(
		FlagMaxDaemonUnhealthySeconds,
		df.Shared.MaxDaemonUnhealthySeconds,
		"Maximum allowable duration for which a daemon can be unhealthy.",
	)

	// Price Daemon.
	cmd.Flags().Uint32(
		FlagPriceDaemonLoopDelayMs,
		df.Price.LoopDelayMs,
		"Delay in milliseconds between sending price updates to the application.",
	)
	cmd.Flags().Uint32(
		FlagBatchableRefreshIntervalMs,
		df.Price.BatchableRefreshIntervalMs,
		"Delay in milliseconds between custom_query batchable endpoint cache refreshes.",
	)
	cmd.Flags().Uint32(
		FlagExchangeCacheRefreshIntervalMs,
		df.Price.ExchangeCacheRefreshIntervalMs,
		"Delay in milliseconds between custom_query exchange endpoint cache refreshes.",
	)
	cmd.Flags().String(
		FlagOracleConfigFile,
		df.Price.OracleConfigFile,
		"Oracle TOML file name (single file for queries, optional [[markets]], exchange legs).",
	)
	cmd.Flags().String(
		FlagOracleConfigDir,
		df.Price.OracleConfigDir,
		"Directory under the node home containing the oracle TOML (default config).",
	)
}

// GetDaemonFlagValuesFromOptions gets all daemon flag values from the `AppOptions` struct.
func GetDaemonFlagValuesFromOptions(
	appOpts servertypes.AppOptions,
) DaemonFlags {
	// Default value
	result := GetDefaultDaemonFlags()

	// Shared Flags
	if option := appOpts.Get(FlagUnixSocketAddress); option != nil {
		if v, err := cast.ToStringE(option); err == nil {
			result.Shared.SocketAddress = v
		}
	}
	if option := appOpts.Get(FlagPanicOnDaemonFailureEnabled); option != nil {
		if v, err := cast.ToBoolE(option); err == nil {
			result.Shared.PanicOnDaemonFailureEnabled = v
		}
	}
	if option := appOpts.Get(FlagMaxDaemonUnhealthySeconds); option != nil {
		if v, err := cast.ToUint32E(option); err == nil {
			result.Shared.MaxDaemonUnhealthySeconds = v
		}
	}

	// Price Daemon.
	if option := appOpts.Get(FlagPriceDaemonLoopDelayMs); option != nil {
		if v, err := cast.ToUint32E(option); err == nil {
			result.Price.LoopDelayMs = v
		}
	}
	if option := appOpts.Get(FlagBatchableRefreshIntervalMs); option != nil {
		if v, err := cast.ToUint32E(option); err == nil {
			result.Price.BatchableRefreshIntervalMs = v
		}
	}
	if option := appOpts.Get(FlagExchangeCacheRefreshIntervalMs); option != nil {
		if v, err := cast.ToUint32E(option); err == nil {
			result.Price.ExchangeCacheRefreshIntervalMs = v
		}
	}
	if option := appOpts.Get(FlagOracleConfigFile); option != nil {
		if v, err := cast.ToStringE(option); err == nil && v != "" {
			result.Price.OracleConfigFile = v
		}
	}
	if option := appOpts.Get(FlagOracleConfigDir); option != nil {
		if v, err := cast.ToStringE(option); err == nil && v != "" {
			result.Price.OracleConfigDir = v
		}
	}
	return result
}
