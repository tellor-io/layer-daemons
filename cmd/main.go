package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	daemons "github.com/tellor-io/layer-daemons"
	"github.com/tellor-io/layer-daemons/appconfig"
	"github.com/tellor-io/layer-daemons/configs"
	customquery "github.com/tellor-io/layer-daemons/custom_query"
	daemonflags "github.com/tellor-io/layer-daemons/flags"
	// need this for the address bech32 prefix config
	_ "github.com/tellor-io/layer/app/config"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/client/flags"
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "reporterd",
	Short: "Run reporter daemon",
	Long:  "reporterd is a daemon that runs the reporter that interacts with the layer chain.",
	Run: func(cmd *cobra.Command, args []string) {
		// Prefer LAYER_HOME over viper "home" because AutomaticEnv maps home -> shell $HOME.
		homePath := os.Getenv("LAYER_HOME")
		if homePath == "" {
			homePath = viper.GetString(flags.FlagHome)
		}
		// Keep viper in sync for downstream consumers reading "home".
		viper.Set(flags.FlagHome, homePath)
		testMode := viper.GetBool("test")
		testQueryID := viper.GetString("test-query-id")
		prometheusPort := viper.GetInt("prometheus-port")
		logLevelstr := viper.GetString(flags.FlagLogLevel)
		configs.WriteDefaultPricefeedExchangeToml(homePath)
		configs.WriteDefaultMarketParamsToml(homePath)
		customquery.WriteDefaultConfigToml(homePath, "config", "custom_query_config.toml")
		loglevel, err := zerolog.ParseLevel(logLevelstr)
		if err != nil {
			fmt.Printf("Error parsing log level: %v\n", err)
			os.Exit(1)
		}
		logger := log.NewLogger(os.Stderr, log.LevelOption(loglevel))

		// Check if test mode is enabled
		if testMode {
			if err := runTestMode(homePath, logger, testQueryID); err != nil {
				fmt.Printf("Test mode failed: %v\n", err)
				os.Exit(1)
			}
			os.Exit(0)
		}
		if testQueryID != "" {
			fmt.Fprintf(os.Stderr, "Error: --test-query-id requires --test\n")
			os.Exit(1)
		}

		// Normal daemon mode - validate required flags
		grpcCfg, err := grpcEndpointsFromEnvOrFlag(viper.GetString(flags.FlagGRPC))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}
		from := viper.GetString(flags.FlagFrom)
		rpcCfg, err := rpcEndpointsFromEnvOrFlag(viper.GetString(flags.FlagNode))
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		if homePath == "" {
			fmt.Printf("Error: --home (or LAYER_HOME env var) is required\n")
			os.Exit(1)
		}
		if len(grpcCfg.Endpoints) == 0 {
			fmt.Printf("Error: %s or --%s is required in reporter mode\n", envGRPCNodes, flags.FlagGRPC)
			os.Exit(1)
		}
		if from == "" {
			fmt.Printf("Error: --from is required in reporter mode\n")
			os.Exit(1)
		}
		if len(rpcCfg.Endpoints) == 0 {
			fmt.Printf("Error: %s or --%s is required in reporter mode\n", envRPCNodes, flags.FlagNode)
			os.Exit(1)
		}

		// Set up signal handling for graceful shutdown
		ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
		defer stop()

		chainId, grpcAddr, selectedRPCNode, err := detectChainIDFromEndpoints(ctx, grpcCfg.Endpoints, rpcCfg.Endpoints)
		if err != nil {
			fmt.Printf("Error: could not detect chain ID: %v\n", err)
			os.Exit(1)
		}
		logger.Info(
			"Detected chain ID",
			"chain_id", chainId,
			"grpc_endpoint", grpcAddr,
			"grpc_source", grpcCfg.Source,
			"rpc_endpoint", selectedRPCNode,
			"rpc_source", rpcCfg.Source,
		)

		// Pass prometheusPort and signal context to NewApp
		appInstance := daemons.NewApp(
			ctx,
			logger,
			chainId,
			moveEndpointToFront(grpcCfg.Endpoints, grpcAddr),
			moveEndpointToFront(rpcCfg.Endpoints, selectedRPCNode),
			homePath,
			prometheusPort,
		)

		// Wait for signal
		<-ctx.Done()
		logger.Info("Received shutdown signal, shutting down gracefully...")

		// Gracefully shutdown
		appInstance.Shutdown()
	},
}

func main() {
	daemonflags.AddDaemonFlagsToCmd(rootCmd)
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().String(flags.FlagHome, appconfig.DefaultNodeHome, "Node home directory")
	rootCmd.Flags().String(flags.FlagFrom, "", "Name of the key to use")
	rootCmd.Flags().String(flags.FlagGRPC, "0.0.0.0:9090", "Address to listen on")
	rootCmd.Flags().String(flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|kwallet|pass|test|memory)")
	rootCmd.Flags().String(flags.FlagLogLevel, zerolog.InfoLevel.String(), "The logging level (trace|debug|info|warn|error|fatal|panic|disabled or '*:<level>,<key>:<level>')")
	rootCmd.Flags().String(flags.FlagBroadcastMode, flags.BroadcastSync, "Transaction broadcasting mode (sync|async)")
	rootCmd.Flags().String(flags.FlagNode, "", "<host>:<port> to CometBFT RPC interface for layer")
	rootCmd.Flags().Int("prometheus-port", 26661, "Port to serve Prometheus metrics on (default 26661). Applicable only if telemetry is enabled in app.toml.")

	// Price Guard Flags
	rootCmd.Flags().Bool("price-guard-enabled", false, "Enable price guard to prevent reporting prices that differ from last reported price by a given threshold")
	rootCmd.Flags().Float64("price-guard-threshold", 0, "Price change threshold (0.5 = 50%, 0.01 = 1% (up to 15 decimals)) - submissions exceeding this will be blocked")
	rootCmd.Flags().Duration("price-guard-max-age", 0, "Maximum age of stored price before treating as expired (e.g. 1m, 1h)")
	rootCmd.Flags().Bool("price-guard-update-on-blocked", false, "Update last known price even if submission is blocked (default false)")

	// Test mode flag
	rootCmd.Flags().Bool("test", false, "Test mode: verify price feed configurations and calculate medians without starting daemon")
	rootCmd.Flags().String("test-query-id", "", "With --test, only run this custom query id (64-char hex); skips exchange/market tests. Exits non-zero if the query fails.")
	// Automatic Unbonding flags
	rootCmd.Flags().Uint32("auto-unbonding-frequency", 0, "Enable automatic unbonding every N days (0 = disabled, 1 - 21 days = valid)")
	rootCmd.Flags().Uint32("auto-unbonding-amount", 0, "Amount of tokens in loya to unbond each unbonding transaction (0 = disabled)")
	rootCmd.Flags().String("auto-unbonding-max-stake-percentage", "0.0", "Maximum percentage of stake to unbond each unbonding transaction (0 = disabled, 1.0 = 100%). If unbonding amount exceeds this percentage, we will skip the unbonding transaction until it exceeds this percentage again.")
	rootCmd.Flags().Duration("refresh-gas-estimates-interval", 12*time.Hour, "Interval for resetting cached gas estimates and gas-adjustment levels (<=0 disables)")
	// Remote signer: when set, tx signing is delegated to the remote signer service instead of the local keyring
	rootCmd.Flags().String("remote-signer-addr", "", "gRPC address of the remote signer service (e.g. localhost:9191). When set, tx signing uses the remote signer instead of the local keyring.")
	rootCmd.Flags().String("remote-signer-ca-cert", "", "Path to the CA certificate for verifying the remote signer's TLS certificate.")
	rootCmd.Flags().String("remote-signer-client-cert", "", "Path to the client TLS certificate presented to the remote signer.")
	rootCmd.Flags().String("remote-signer-client-key", "", "Path to the client TLS private key.")

	// Auto-bridge: keep wallet at a fixed balance by bridging the excess to Ethereum
	rootCmd.Flags().Uint64(daemonflags.FlagAutoBalanceToKeep, 0, "Keep this amount of loya in the wallet; bridge any excess to Ethereum at --auto-balance-execution-time (0 = disabled)")
	rootCmd.Flags().String(daemonflags.FlagAutoBalanceExecutionTime, "00:00", "UTC time to execute the auto-balance bridge (HH:MM, e.g. '03:00')")
	rootCmd.Flags().String(daemonflags.FlagAutoBalanceBridgeToEthAddr, "", "Ethereum address to bridge excess tokens to (required when auto-balance-to-keep > 0)")

	// Note: --home, --from, --grpc, and --node are validated in Run so that
	// env vars (LAYER_HOME, FROM, GRPC_NODES, RPC_NODES) are also accepted.

	// Try to load .env from current directory, or parent directory if not found.
	// .env file is optional — allows the daemon to run without one if env vars are set another way.
	if err := godotenv.Load(); err != nil {
		_ = godotenv.Load("../.env")
	}

	if err := viper.BindPFlags(rootCmd.Flags()); err != nil {
		panic(err)
	}

	// Allow all flags to be set via environment variables.
	viper.SetEnvKeyReplacer(strings.NewReplacer("-", "_", ".", "_"))
	viper.AutomaticEnv()
}
