package main

import (
	"fmt"
	"os"
	"path/filepath"

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
		homePath := viper.GetString(flags.FlagHome)
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

		// Normal daemon mode - validate required flags
		chainId := viper.GetString(flags.FlagChainID)
		grpcAddr := viper.GetString(flags.FlagGRPC)
		from := viper.GetString(flags.FlagFrom)
		node := viper.GetString(flags.FlagNode)

		if chainId == "" {
			fmt.Printf("Error: --chain-id is required in reporter mode\n")
			os.Exit(1)
		}
		if grpcAddr == "" {
			fmt.Printf("Error: --grpc is required in reporter mode\n")
			os.Exit(1)
		}
		if from == "" {
			fmt.Printf("Error: --from is required in reporter mode\n")
			os.Exit(1)
		}
		if node == "" {
			fmt.Printf("Error: --node is required in reporter mode\n")
			os.Exit(1)
		}

		// Pass prometheusPort to NewApp
		daemons.NewApp(logger, chainId, grpcAddr, homePath, prometheusPort)
	},
}

var (
	prometheusPort int
	testQueryId    string
)

// testCmd represents the test subcommand for testing data feed sources
var testCmd = &cobra.Command{
	Use:   "test",
	Short: "Test data feed sources",
	Long:  "Test mode: verify price feed configurations and calculate medians without starting daemon",
	Run: func(cmd *cobra.Command, args []string) {
		homePath, _ := cmd.Flags().GetString(flags.FlagHome)
		logLevelstr, _ := cmd.Flags().GetString(flags.FlagLogLevel)

		// Validate configs exist before proceeding
		if err := validateConfigsExist(homePath); err != nil {
			fmt.Printf("Error: %v\n", err)
			os.Exit(1)
		}

		loglevel, err := zerolog.ParseLevel(logLevelstr)
		if err != nil {
			fmt.Printf("Error parsing log level: %v\n", err)
			os.Exit(1)
		}
		logger := log.NewLogger(os.Stderr, log.LevelOption(loglevel))

		if err := runTestMode(homePath, logger, testQueryId); err != nil {
			fmt.Printf("Test mode failed: %v\n", err)
			os.Exit(1)
		}
	},
}

// validateConfigsExist checks that required config files exist at the given home path
func validateConfigsExist(homePath string) error {
	configDir := filepath.Join(homePath, "config")
	requiredFiles := []string{
		"market_params.toml",
		"pricefeed_exchange_config.toml",
	}
	for _, file := range requiredFiles {
		if _, err := os.Stat(filepath.Join(configDir, file)); os.IsNotExist(err) {
			return fmt.Errorf("no configs found at %s. Use --home flag to specify config directory", homePath)
		}
	}
	return nil
}

var forceReset bool

// resetConfigsCmd represents the reset-configs subcommand
var resetConfigsCmd = &cobra.Command{
	Use:   "reset-configs",
	Short: "Reset config files to defaults",
	Long:  "Overwrites existing config files with default values generated from the daemon binary. This is destructive and will lose any custom configuration.",
	Run: func(cmd *cobra.Command, args []string) {
		homePath, _ := cmd.Flags().GetString(flags.FlagHome)
		configDir := filepath.Join(homePath, "config")

		// Check if configs exist and warn user
		configFiles := []string{
			"market_params.toml",
			"pricefeed_exchange_config.toml",
			"custom_query_config.toml",
		}

		existingFiles := []string{}
		for _, file := range configFiles {
			if _, err := os.Stat(filepath.Join(configDir, file)); err == nil {
				existingFiles = append(existingFiles, file)
			}
		}

		if len(existingFiles) > 0 && !forceReset {
			fmt.Printf("Warning: The following config files will be overwritten:\n")
			for _, file := range existingFiles {
				fmt.Printf("  - %s\n", filepath.Join(configDir, file))
			}
			fmt.Printf("\nUse --force to proceed with reset.\n")
			os.Exit(1)
		}

		// Ensure config directory exists
		if err := os.MkdirAll(configDir, 0o755); err != nil {
			fmt.Printf("Error creating config directory: %v\n", err)
			os.Exit(1)
		}

		// Reset market_params.toml
		marketParamsPath := filepath.Join(configDir, "market_params.toml")
		marketParamsBuffer := configs.GenerateDefaultMarketParamsTomlString()
		if err := os.WriteFile(marketParamsPath, marketParamsBuffer.Bytes(), 0o644); err != nil {
			fmt.Printf("Error writing %s: %v\n", marketParamsPath, err)
			os.Exit(1)
		}
		fmt.Printf("Reset %s\n", marketParamsPath)

		// Reset pricefeed_exchange_config.toml
		exchangeConfigPath := filepath.Join(configDir, "pricefeed_exchange_config.toml")
		exchangeConfigBuffer := configs.GenerateDefaultExchangeTomlString()
		if err := os.WriteFile(exchangeConfigPath, exchangeConfigBuffer.Bytes(), 0o644); err != nil {
			fmt.Printf("Error writing %s: %v\n", exchangeConfigPath, err)
			os.Exit(1)
		}
		fmt.Printf("Reset %s\n", exchangeConfigPath)

		// Reset custom_query_config.toml
		customQueryPath := filepath.Join(configDir, "custom_query_config.toml")
		customQueryBuffer := customquery.GenerateDefaultConfigTomlString()
		if err := os.WriteFile(customQueryPath, customQueryBuffer.Bytes(), 0o644); err != nil {
			fmt.Printf("Error writing %s: %v\n", customQueryPath, err)
			os.Exit(1)
		}
		fmt.Printf("Reset %s\n", customQueryPath)

		fmt.Printf("\nAll config files have been reset to defaults.\n")
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
	// Add subcommands
	rootCmd.AddCommand(testCmd)
	rootCmd.AddCommand(resetConfigsCmd)

	// Test command flags
	testCmd.Flags().String(flags.FlagHome, appconfig.DefaultNodeHome, "Node home directory")
	testCmd.Flags().String(flags.FlagLogLevel, zerolog.InfoLevel.String(), "The logging level (trace|debug|info|warn|error|fatal|panic|disabled)")
	testCmd.Flags().StringVar(&testQueryId, "query-id", "", "Isolate test to a specific query ID (hex string)")

	// Reset-configs command flags
	resetConfigsCmd.Flags().String(flags.FlagHome, appconfig.DefaultNodeHome, "Node home directory")
	resetConfigsCmd.Flags().BoolVar(&forceReset, "force", false, "Force reset without confirmation")

	// Root command flags
	rootCmd.Flags().String(flags.FlagHome, appconfig.DefaultNodeHome, "Node home directory")
	rootCmd.Flags().String(flags.FlagFrom, "", "Name of the key to use")
	rootCmd.Flags().String(flags.FlagGRPC, "0.0.0.0:9090", "Address to listen on")
	rootCmd.Flags().String(flags.FlagChainID, "layer", "Chain ID")
	rootCmd.Flags().String(flags.FlagKeyringBackend, flags.DefaultKeyringBackend, "Select keyring's backend (os|file|kwallet|pass|test|memory)")
	rootCmd.Flags().String(flags.FlagLogLevel, zerolog.InfoLevel.String(), "The logging level (trace|debug|info|warn|error|fatal|panic|disabled or '*:<level>,<key>:<level>')")
	rootCmd.Flags().String(flags.FlagBroadcastMode, flags.BroadcastSync, "Transaction broadcasting mode (sync|async)")
	rootCmd.Flags().String(flags.FlagNode, "", "<host>:<port> to CometBFT RPC interface for layer")
	rootCmd.Flags().IntVar(&prometheusPort, "prometheus-port", 26661, "Port to serve Prometheus metrics on (default 26661). Applicable only if telemetry is enabled in app.toml.")

	// Price Guard Flags
	rootCmd.Flags().Bool("price-guard-enabled", false, "Enable price guard to prevent reporting prices that differ from last reported price by a given threshold")
	rootCmd.Flags().Float64("price-guard-threshold", 0, "Price change threshold (0.5 = 50%, 0.01 = 1% (up to 15 decimals)) - submissions exceeding this will be blocked")
	rootCmd.Flags().Duration("price-guard-max-age", 0, "Maximum age of stored price before treating as expired (e.g. 1m, 1h)")
	rootCmd.Flags().Bool("price-guard-update-on-blocked", false, "Update last known price even if submission is blocked (default false)")

	// Automatic Unbonding flags
	rootCmd.Flags().Uint32("auto-unbonding-frequency", 0, "Enable automatic unbonding every N days (0 = disabled, 1 - 21 days = valid")
	rootCmd.Flags().Uint32("auto-unbonding-amount", 0, "Amount of tokens in loya to unbond each unbonding transaction (0 = disabled)")
	rootCmd.Flags().String("auto-unbonding-max-stake-percentage", "0.0", "Maximum percentage of stake to unbond each unbonding transaction (0 = disabled, 1.0 = 100%). If unbonding amount exceeds this percentage, we will skip the unbonding transaction until it exceeds this percentage again.")

	// Note: --from, --grpc, --chain-id, and --node are only required in normal mode
	// We validate them in the Run function instead of marking as required

	// Try to load .env from current directory, or parent directory if not found
	// .env file is optional, so we ignore errors - allows daemon to run without .env
	if err := godotenv.Load(); err != nil {
		// Try parent directory (for when running from daemons/ subdirectory)
		_ = godotenv.Load("../.env")
	}

	if err := viper.BindPFlags(rootCmd.Flags()); err != nil {
		panic(err)
	}
}
