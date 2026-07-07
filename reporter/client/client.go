package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	"github.com/ethereum/go-ethereum/common"
	"github.com/spf13/viper"
	globalfeetypes "github.com/strangelove-ventures/globalfee/x/globalfee/types"
	customquery "github.com/tellor-io/layer-daemons/custom_query"
	daemonflags "github.com/tellor-io/layer-daemons/flags"
	pricefeedtypes "github.com/tellor-io/layer-daemons/pricefeed/client/types"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
	tokenbridgetypes "github.com/tellor-io/layer-daemons/server/types/token_bridge"
	tokenbridgetipstypes "github.com/tellor-io/layer-daemons/server/types/token_bridge_tips"
	daemontypes "github.com/tellor-io/layer-daemons/types"
	oracletypes "github.com/tellor-io/layer/x/oracle/types"
	reportertypes "github.com/tellor-io/layer/x/reporter/types"
	"google.golang.org/grpc"

	"cosmossdk.io/log"
	"cosmossdk.io/math"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

const (
	defaultGas                   = uint64(240000)
	primaryEndpointCheckInterval = 5 * time.Minute
	primaryEndpointProbeTimeout  = 10 * time.Second
)

var ErrKeyringPasswordFile = errors.New("keyring password file validation failed")

var validKeyringBackends = map[string]struct{}{
	"os":      {},
	"file":    {},
	"kwallet": {},
	"pass":    {},
	"test":    {},
	"memory":  {},
}

var (
	commitedIds   = make(map[uint64]bool)
	depositTipMap = make(map[uint64]bool) // map of deposit tips already sent to bridge daemon

	// Atomic counter for unordered tx timeout uniqueness (nanosecond increment)
	txTimeoutNonce uint64
)

var mutex = &sync.RWMutex{}

type repeatingPasswordReader struct {
	mu   sync.Mutex
	line []byte
	pos  int
}

func newRepeatingPasswordReader(pass string) io.Reader {
	return &repeatingPasswordReader{line: []byte(pass + "\n")}
}

func (r *repeatingPasswordReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for i := range p {
		p[i] = r.line[r.pos]
		r.pos = (r.pos + 1) % len(r.line)
	}
	return len(p), nil
}

// IsKeyringPasswordFileError reports whether startup failed while validating
// KEYRING_PASSWORD_FILE or the account it is expected to unlock.
func IsKeyringPasswordFileError(err error) bool {
	return errors.Is(err, ErrKeyringPasswordFile)
}

// keyringReader returns an io.Reader for the keyring passphrase.
// If KEYRING_PASSWORD_FILE is set, it reads the password from that file.
// Otherwise it falls back to stdin for interactive use.
func keyringReader() (io.Reader, bool, error) {
	passFile := os.Getenv("KEYRING_PASSWORD_FILE")
	if passFile == "" {
		return os.Stdin, false, nil
	}

	data, err := os.ReadFile(passFile)
	if err != nil {
		return nil, true, fmt.Errorf("%w: could not read KEYRING_PASSWORD_FILE %q: %w", ErrKeyringPasswordFile, passFile, err)
	}
	pass := strings.TrimSpace(string(data))
	if pass == "" {
		return nil, true, fmt.Errorf("%w: KEYRING_PASSWORD_FILE %q is empty", ErrKeyringPasswordFile, passFile)
	}

	// The file backend may ask for the passphrase repeatedly while opening and
	// signing. Keep answers available for the lifetime of the daemon.
	return newRepeatingPasswordReader(pass), true, nil
}

func validateKeyringBackendConfig(backend string, usingPasswordFile bool) error {
	backend = strings.TrimSpace(backend)
	if backend == "" {
		return fmt.Errorf("keyring-backend is required; set KEYRING_BACKEND or --keyring-backend")
	}
	if _, ok := validKeyringBackends[backend]; !ok {
		return fmt.Errorf("unsupported keyring-backend %q; valid values are os, file, kwallet, pass, test, memory", backend)
	}
	if usingPasswordFile && backend != "file" {
		return fmt.Errorf("%w: KEYRING_PASSWORD_FILE requires KEYRING_BACKEND=file, got %q", ErrKeyringPasswordFile, backend)
	}
	return nil
}

func validateKeyringAccountUnlocked(kr keyring.Keyring, keyName string) error {
	sig, pubKey, err := kr.Sign(keyName, []byte("layer-daemons keyring unlock check"), signingtypes.SignMode_SIGN_MODE_DIRECT)
	if err != nil {
		return fmt.Errorf("%w: account %q could not be unlocked with KEYRING_PASSWORD_FILE: %w", ErrKeyringPasswordFile, keyName, err)
	}
	if len(sig) == 0 || pubKey == nil {
		return fmt.Errorf("%w: account %q did not return a valid unlock signature", ErrKeyringPasswordFile, keyName)
	}
	return nil
}

type TxChannelInfo struct {
	Msg         sdk.Msg
	isBridge    bool
	NumRetries  uint8
	QueryMetaId uint64 // track which queryMeta this transaction is for (0 if not applicable)
}

type Client struct {
	// reporter account name
	AccountName string
	// Query clients
	OracleQueryClient oracletypes.QueryClient
	BankClient        banktypes.QueryClient

	ReporterClient  reportertypes.QueryClient
	CmtService      cmtservice.ServiceClient
	GlobalfeeClient globalfeetypes.QueryClient
	AuthClient      authtypes.QueryClient

	cosmosCtxMu          sync.RWMutex
	cosmosCtx            client.Context
	MarketParams         []pricefeedtypes.MarketParam
	MarketToExchange     *pricefeedservertypes.MarketToExchangePrices
	TokenDepositsCache   *tokenbridgetypes.DepositReports
	TokenBridgeTipsCache *tokenbridgetipstypes.DepositTips
	Custom_query         map[string]customquery.QueryConfig

	accAddr   sdk.AccAddress
	minGasFee string
	// logger is the logger for the daemon.
	logger     log.Logger
	txChan     chan TxChannelInfo
	txMu       sync.RWMutex
	txClosed   bool
	PriceGuard *PriceGuard
	// Gas estimate refresh interval; <=0 disables periodic refresh.
	refreshGasEstimatesInterval time.Duration
	gasEstimator                *gasEstimateState

	// Resources that need cleanup
	grpcMu           sync.RWMutex
	grpcConn         *grpc.ClientConn
	grpcClient       daemontypes.GrpcClient
	rpcClient        *rpchttp.HTTP // direct reference for WebSocket subscriptions
	grpcManager      *grpcEndpointManager
	rpcMu            sync.RWMutex
	rpcManager       *rpcEndpointManager
	remoteSignerConn *grpc.ClientConn // non-nil when --remote-signer-addr is set
	wg               sync.WaitGroup
	broadcastWg      sync.WaitGroup // Tracks goroutines in BroadcastTxMsgToChain
	stopOnce         sync.Once
}

// GetUniqueUnorderedTimeout generates a unique timeout timestamp for unordered transactions.
// Returns current time + 30 seconds + atomic nanosecond increment for uniqueness.
// https://docs.cosmos.network/v0.53/build/architecture/adr-070-unordered-account
func (c *Client) GetUniqueUnorderedTimeout() time.Time {
	// Atomically increment nonce and add to base timeout (30 seconds from now)
	nonce := atomic.AddUint64(&txTimeoutNonce, 1)
	return time.Now().Add(30 * time.Second).Add(time.Duration(nonce) * time.Nanosecond)
}

func NewClient(logger log.Logger, valGasMin string) *Client {
	logger = logger.With("module", "reporter-client")
	txChan := make(chan TxChannelInfo)
	return &Client{
		cosmosCtx: client.Context{},
		logger:    logger,
		minGasFee: valGasMin,
		txChan:    txChan,
		gasEstimator: newGasEstimateState(map[string]gasBucketConfig{
			bridgeGasBucketKey: {
				levels:  []float64{1.75, 2.0},
				baseIdx: 0,
			},
			defaultNonBridgeBucketConfigKey: {
				levels:  []float64{1.25, 1.6, 2.0},
				baseIdx: 0,
			},
		}),
	}
}

func (c *Client) Start(
	ctx context.Context,
	flags daemonflags.DaemonFlags,
	grpcEndpoints []string,
	grpcClient daemontypes.GrpcClient,
	marketParams []pricefeedtypes.MarketParam,
	marketToExchange *pricefeedservertypes.MarketToExchangePrices,
	tokenDepositsCache *tokenbridgetypes.DepositReports,
	tokenBridgeTipsCache *tokenbridgetipstypes.DepositTips,
	custom_queries map[string]customquery.QueryConfig,
	chainId string,
	rpcEndpoints []string,
) error {
	// Log the daemon flags.
	c.logger.Info(
		"Starting reporter daemon with flags",
	)

	c.MarketParams = marketParams
	RegisterPriceGuardMarketParams(marketParams)
	c.MarketToExchange = marketToExchange

	c.TokenDepositsCache = tokenDepositsCache
	c.TokenBridgeTipsCache = tokenBridgeTipsCache
	c.Custom_query = custom_queries
	grpcManager, err := newGRPCEndpointManager(grpcEndpoints, c.logger, grpcClient)
	if err != nil {
		return fmt.Errorf("failed to initialize gRPC endpoint manager: %w", err)
	}
	c.grpcManager = grpcManager
	c.grpcClient = grpcClient

	if err := c.connectInitialGRPCEndpoint(ctx); err != nil {
		return err
	}

	keyName := viper.GetString("from")
	homeDir := viper.GetString("home")
	brdcstMode := viper.GetString("broadcast-mode")
	kb := viper.GetString("keyring-backend")

	// Read price guard config
	priceGuardEnabled := viper.GetBool("price-guard-enabled")
	updateOnBlocked := viper.GetBool("price-guard-update-on-blocked")

	var priceGuardThreshold float64
	var priceGuardMaxAge time.Duration

	if priceGuardEnabled {
		// If price guard is enabled, require explicit configuration
		if !viper.IsSet("price-guard-threshold") {
			return fmt.Errorf("price-guard-enabled is true but price-guard-threshold is not set")
		}
		priceGuardThreshold = viper.GetFloat64("price-guard-threshold")
		if priceGuardThreshold <= 0 {
			return fmt.Errorf("price-guard-threshold must be greater than 0, got: %f", priceGuardThreshold)
		}

		if !viper.IsSet("price-guard-max-age") {
			return fmt.Errorf("price-guard-enabled is true but price-guard-max-age is not set")
		}
		priceGuardMaxAge = viper.GetDuration("price-guard-max-age")
		if priceGuardMaxAge <= 0 {
			return fmt.Errorf("price-guard-max-age must be greater than 0, got: %s", priceGuardMaxAge)
		}

		if !viper.IsSet("price-guard-update-on-blocked") {
			return fmt.Errorf("price-guard-enabled is true but price-guard-update-on-blocked is not set")
		}
	} else if viper.IsSet("price-guard-threshold") || viper.IsSet("price-guard-max-age") || viper.IsSet("price-guard-update-on-blocked") {
		return fmt.Errorf("price-guard flags are set but price-guard-enabled is false")
	}

	c.PriceGuard = NewPriceGuard(priceGuardThreshold, priceGuardMaxAge, priceGuardEnabled, updateOnBlocked, c.logger)

	// Read auto unbonding configuration
	autoUnbondingFrequency := viper.GetUint32("auto-unbonding-frequency")
	autoUnbondingAmount := viper.GetUint32("auto-unbonding-amount")
	autoUnbondingMaxStakePercentage := viper.GetString("auto-unbonding-max-stake-percentage")
	c.refreshGasEstimatesInterval = viper.GetDuration("refresh-gas-estimates-interval")

	if autoUnbondingFrequency > 0 {
		if autoUnbondingFrequency > 21 {
			return fmt.Errorf("auto-unbonding-frequency must be between 1 and 21 days when set, got: %d", autoUnbondingFrequency)
		}
		if autoUnbondingAmount == 0 {
			return fmt.Errorf("auto-unbonding-amount must be greater than 0 when auto-unbonding-frequency is set")
		}
		maxStakePercentage, err := math.LegacyNewDecFromStr(autoUnbondingMaxStakePercentage)
		if err != nil {
			return fmt.Errorf("auto-unbonding-max-stake-percentage must be a valid decimal, got: %s", autoUnbondingMaxStakePercentage)
		}
		if maxStakePercentage.LT(math.LegacyZeroDec()) || maxStakePercentage.GT(math.LegacyNewDecFromInt(math.NewInt(1))) {
			return fmt.Errorf("auto-unbonding-max-stake-percentage must be between 0.0 and 1.0, got: %s", autoUnbondingMaxStakePercentage)
		}
	}

	// Log price guard configuration
	if priceGuardEnabled {
		c.logger.Info(
			"Price guard enabled",
			"threshold", fmt.Sprintf("%.5f%%", priceGuardThreshold*100),
			"max_age", priceGuardMaxAge.String(),
			"update_on_blocked", updateOnBlocked,
		)
	} else {
		c.logger.Info("Price guard disabled")
	}

	if autoUnbondingFrequency > 0 {
		c.logger.Info(
			"Auto unbonding enabled",
			"frequency", autoUnbondingFrequency,
			"amount", autoUnbondingAmount,
			"max_stake_percentage", autoUnbondingMaxStakePercentage,
		)
	} else {
		c.logger.Info("Auto unbonding disabled")
	}

	// Read and validate auto-balance-to-keep configuration
	autoBalanceToKeep := viper.GetUint64(daemonflags.FlagAutoBalanceToKeep)
	if autoBalanceToKeep > 0 {
		autoBalanceEthAddr, err := normalizeAutoBalanceEthAddr(viper.GetString(daemonflags.FlagAutoBalanceBridgeToEthAddr))
		if err != nil {
			return err
		}
		if autoBalanceEthAddr == "" {
			return fmt.Errorf("%s is required when %s > 0", daemonflags.FlagAutoBalanceBridgeToEthAddr, daemonflags.FlagAutoBalanceToKeep)
		}
		if _, _, err := parseAutoBalanceExecutionTime(viper.GetString(daemonflags.FlagAutoBalanceExecutionTime)); err != nil {
			return err
		}
		c.logger.Info(
			"Auto balance-to-keep enabled",
			"balance_to_keep_loya", autoBalanceToKeep,
			"execution_time", viper.GetString(daemonflags.FlagAutoBalanceExecutionTime),
			"eth_addr", "0x"+autoBalanceEthAddr,
		)
	} else {
		c.logger.Info("Auto balance-to-keep disabled")
	}

	if c.refreshGasEstimatesInterval > 0 {
		c.logger.Info("Periodic gas estimate refresh enabled", "interval", c.refreshGasEstimatesInterval.String())
	} else {
		c.logger.Info("Periodic gas estimate refresh disabled")
	}

	c.cosmosCtx = c.cosmosCtx.WithChainID(chainId)
	c.cosmosCtx = c.cosmosCtx.WithHomeDir(homeDir)
	c.cosmosCtx = c.cosmosCtx.WithKeyringDir(homeDir)
	c.cosmosCtx = c.cosmosCtx.WithBroadcastMode(brdcstMode)
	c.cosmosCtx = c.cosmosCtx.WithAccountRetriever(authtypes.AccountRetriever{})

	rpcManager, err := newRPCEndpointManager(rpcEndpoints, c.logger)
	if err != nil {
		return fmt.Errorf("failed to initialize RPC endpoint manager: %w", err)
	}
	rpcClientVal, rpcEndpoint, err := rpcManager.currentClient()
	if err != nil {
		return fmt.Errorf("failed to create RPC client: %w", err)
	}
	c.rpcManager = rpcManager
	c.setRPCClient(rpcClientVal)
	c.logger.Info("CometBFT RPC client established", "endpoint", rpcEndpoint)
	encodingConfig := CreateEncodingConfig()
	c.cosmosCtx = c.cosmosCtx.WithCodec(encodingConfig.Codec).WithInterfaceRegistry(encodingConfig.InterfaceRegistry).WithTxConfig(encodingConfig.TxConfig)
	remoteSignerAddr := viper.GetString("remote-signer-addr")
	if remoteSignerAddr != "" {
		// Use remote signer for tx signing — no local private key needed.
		c.logger.Info("Using remote signer for tx signing", "addr", remoteSignerAddr)
		caCert := viper.GetString("remote-signer-ca-cert")
		clientCert := viper.GetString("remote-signer-client-cert")
		clientKey := viper.GetString("remote-signer-client-key")
		kr, signerAccAddr, signerConn, err := newKeyringFromRemoteSigner(ctx, keyName, remoteSignerAddr, caCert, clientCert, clientKey)
		if err != nil {
			return fmt.Errorf("failed to initialize remote signer keyring: %w", err)
		}
		// Store the connection so it gets closed during shutdown.
		c.remoteSignerConn = signerConn
		c.cosmosCtx = c.cosmosCtx.WithKeyring(kr)
		c.cosmosCtx = c.cosmosCtx.WithFrom(keyName).WithFromName(keyName).WithFromAddress(signerAccAddr)
	} else {
		keyringInput, usingPasswordFile, err := keyringReader()
		if err != nil {
			return err
		}
		if err := validateKeyringBackendConfig(kb, usingPasswordFile); err != nil {
			return err
		}
		c.logger.Info("Using keyring backend", "backend", kb)
		kr, err := keyring.New("", kb, homeDir, keyringInput, encodingConfig.Codec)
		if err != nil {
			if usingPasswordFile {
				return fmt.Errorf("%w: could not initialize keyring backend %q: %w", ErrKeyringPasswordFile, kb, err)
			}
			return fmt.Errorf("could not initialize keyring backend %q: %w", kb, err)
		}
		record, err := kr.Key(keyName)
		if err != nil {
			if usingPasswordFile {
				return fmt.Errorf("%w: account %q could not be read from keyring backend %q: %w", ErrKeyringPasswordFile, keyName, kb, err)
			}
			return fmt.Errorf("account %q could not be read from keyring backend %q: %w", keyName, kb, err)
		}
		addr, err := record.GetAddress()
		if err != nil {
			return err
		}
		if usingPasswordFile {
			if err := validateKeyringAccountUnlocked(kr, keyName); err != nil {
				return err
			}
			c.logger.Info("KEYRING_PASSWORD_FILE unlocked keyring account successfully", "account", keyName, "address", addr.String())
		}
		c.cosmosCtx = c.cosmosCtx.WithKeyring(kr)
		c.cosmosCtx = c.cosmosCtx.WithFrom(keyName).WithFromName(keyName).WithFromAddress(addr)
	}
	c.accAddr = c.cosmosCtx.GetFromAddress()

	StartReporterDaemonTaskLoop(
		c,
		ctx,
		flags,
		&c.wg,
	)

	return nil
}

func (c *Client) connectInitialGRPCEndpoint(ctx context.Context) error {
	c.logger.Info("Establishing gRPC connection", "endpoint", c.grpcManager.currentEndpoint())
	conn, endpoint, err := c.grpcManager.currentConnection(ctx)
	if err != nil {
		c.logger.Warn("Failed to establish gRPC connection, trying fallback endpoint", "endpoint", endpoint, "error", err)
		for attempt := 0; attempt < c.grpcManager.endpointCount()-1; attempt++ {
			conn, endpoint, err = c.grpcManager.nextConnection(ctx)
			if err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("failed to establish gRPC connection to Cosmos query services: %w", err)
		}
	}

	c.setGRPCConnection(conn)
	c.logger.Info("gRPC connection established successfully", "endpoint", endpoint)
	return nil
}

func (c *Client) setGRPCConnection(conn *grpc.ClientConn) {
	c.grpcConn = conn
	c.cosmosCtxMu.Lock()
	c.cosmosCtx = c.cosmosCtx.WithGRPCClient(conn)
	c.cosmosCtxMu.Unlock()

	// Rebuild all generated clients so subsequent queries use the active connection.
	c.OracleQueryClient = oracletypes.NewQueryClient(conn)
	c.BankClient = banktypes.NewQueryClient(conn)
	c.ReporterClient = reportertypes.NewQueryClient(conn)
	c.GlobalfeeClient = globalfeetypes.NewQueryClient(conn)
	c.CmtService = cmtservice.NewServiceClient(conn)
	c.AuthClient = authtypes.NewQueryClient(conn)
}

func (c *Client) withGRPCQueryClient(call func() error) error {
	c.grpcMu.RLock()
	defer c.grpcMu.RUnlock()
	return call()
}

func (c *Client) reconnectGRPCEndpoint(ctx context.Context, operation string, lastErr error) error {
	c.grpcMu.Lock()
	defer c.grpcMu.Unlock()

	oldConn := c.grpcConn
	c.logger.Warn(
		"Cosmos gRPC operation failed, trying fallback endpoint",
		"operation", operation,
		"endpoint", c.grpcManager.currentEndpoint(),
		"error", lastErr,
	)

	conn, endpoint, err := c.grpcManager.nextConnection(ctx)
	if err != nil {
		return err
	}
	c.setGRPCConnection(conn)

	if oldConn != nil && c.grpcClient != nil {
		if err := c.grpcClient.CloseConnection(oldConn); err != nil {
			c.logger.Warn("Failed to close previous gRPC connection", "error", err)
		}
	}
	c.logger.Info("Cosmos gRPC connection re-established", "endpoint", endpoint)
	return nil
}

func (c *Client) withGRPCFallback(ctx context.Context, operation string, call func() error) error {
	err := c.withGRPCQueryClient(call)
	if !shouldFallbackGRPCError(ctx, err) || c.grpcManager == nil {
		return err
	}

	lastErr := err
	for attempt := 0; attempt < c.grpcManager.endpointCount()-1; attempt++ {
		if err := c.reconnectGRPCEndpoint(ctx, operation, lastErr); err != nil {
			return fmt.Errorf("%s failed on gRPC endpoints: %w; last error: %w", operation, err, lastErr)
		}

		err = c.withGRPCQueryClient(call)
		if err == nil {
			return nil
		}
		if !shouldFallbackGRPCError(ctx, err) {
			return err
		}
		lastErr = err
	}

	return fmt.Errorf("%s failed on all gRPC endpoints: %w", operation, lastErr)
}

func (c *Client) tryRestorePrimaryGRPCEndpoint(ctx context.Context) {
	if c.grpcManager == nil || c.grpcManager.usingPrimary() {
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, primaryEndpointProbeTimeout)
	defer cancel()

	conn, endpoint, err := c.grpcManager.primaryConnection(probeCtx)
	if err != nil {
		c.logger.Warn("Primary Cosmos gRPC endpoint is not ready", "endpoint", endpoint, "error", err)
		return
	}

	resp, err := cmtservice.NewServiceClient(conn).GetNodeInfo(probeCtx, &cmtservice.GetNodeInfoRequest{})
	if err != nil {
		c.closeGRPCConnection(conn)
		c.logger.Warn("Primary Cosmos gRPC endpoint health check failed", "endpoint", endpoint, "error", err)
		return
	}
	chainID := c.chainID()
	if resp.DefaultNodeInfo.Network != chainID {
		c.closeGRPCConnection(conn)
		c.logger.Warn(
			"Primary Cosmos gRPC endpoint returned unexpected chain ID",
			"endpoint", endpoint,
			"expected_chain_id", chainID,
			"actual_chain_id", resp.DefaultNodeInfo.Network,
		)
		return
	}

	c.grpcMu.Lock()
	if c.grpcManager.usingPrimary() {
		c.grpcMu.Unlock()
		c.closeGRPCConnection(conn)
		return
	}
	oldConn := c.grpcConn
	c.setGRPCConnection(conn)
	c.grpcManager.switchToPrimary()
	c.grpcMu.Unlock()

	c.closeGRPCConnection(oldConn)
}

func (c *Client) closeGRPCConnection(conn *grpc.ClientConn) {
	if conn == nil || c.grpcClient == nil {
		return
	}
	if err := c.grpcClient.CloseConnection(conn); err != nil {
		c.logger.Warn("Failed to close gRPC connection", "error", err)
	}
}

func (c *Client) rpcContextWithClient(rpcClient client.CometRPC) client.Context {
	clientCtx := c.currentCosmosContext()
	return clientCtx.WithClient(rpcClient)
}

func (c *Client) chainID() string {
	return c.currentCosmosContext().ChainID
}

func StartReporterDaemonTaskLoop(
	client *Client,
	ctx context.Context,
	flags daemonflags.DaemonFlags,
	wg *sync.WaitGroup,
) {
	reporterCreated := false
	// Check if the reporter is created
	for !reporterCreated {
		select {
		case <-ctx.Done():
			client.logger.Debug("StartReporterDaemonTaskLoop: context canceled during reporter startup")
			return
		default:
		}
		reporterCreated = client.checkReporter(ctx)
		if reporterCreated {
			client.logger.Info("Reporter exists, setting gas price")
			err := client.SetGasPrice(ctx)
			if err != nil {
				client.logger.Error("Setting gas price failed, required before reporter can report", "error", err)
				reporterCreated = false
				time.Sleep(time.Second)
			} else {
				client.logger.Info("Gas price set successfully", "gas_price", client.minGasFee)
			}
		} else {
			time.Sleep(time.Second)
			client.logger.Warn("Reporter not found, retrying...", "selector_address", client.accAddr.String())
		}
	}

	wg.Add(1)
	go client.RestorePrimaryEndpointsPeriodically(ctx, wg)

	select {
	case <-ctx.Done():
		client.logger.Debug("StartReporterDaemonTaskLoop: context canceled before starting monitors")
		return
	case <-time.After(5 * time.Second):
	}
	err := client.WaitForNextBlock(ctx)
	if err != nil {
		client.logger.Error("Waiting for next block", "error", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		client.BroadcastTxMsgToChain(ctx)
	}()

	wg.Add(1)
	go client.MonitorCyclelistQuery(ctx, wg)

	wg.Add(1)
	go client.MonitorTokenBridgeReports(ctx, wg)

	wg.Add(1)
	go client.MonitorForTippedQueries(ctx, wg)

	wg.Add(1)
	go client.WithdrawAndStakeEarnedRewardsPeriodically(ctx, wg)

	wg.Add(1)
	go client.AutoUnbondStakePeriodically(ctx, wg)

	wg.Add(1)
	go client.AutoBridgeWalletExcessPeriodically(ctx, wg)

	if client.refreshGasEstimatesInterval > 0 {
		wg.Add(1)
		go client.RefreshGasEstimatesPeriodically(ctx, wg)
	}

	wg.Wait()
}

func (c *Client) setRPCClient(rpcClient client.CometRPC) {
	c.rpcMu.Lock()
	defer c.rpcMu.Unlock()
	c.cosmosCtxMu.Lock()
	defer c.cosmosCtxMu.Unlock()
	if httpClient, ok := rpcClient.(*rpchttp.HTTP); ok {
		c.rpcClient = httpClient
	}
	c.cosmosCtx = c.cosmosCtx.WithClient(rpcClient)
}

func (c *Client) currentCosmosContext() client.Context {
	c.cosmosCtxMu.RLock()
	defer c.cosmosCtxMu.RUnlock()
	return c.cosmosCtx
}

func (c *Client) RestorePrimaryEndpointsPeriodically(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()

	if c.rpcManager == nil {
		return
	}

	ticker := time.NewTicker(primaryEndpointCheckInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			c.tryRestorePrimaryRPCEndpoint(ctx)
			c.tryRestorePrimaryGRPCEndpoint(ctx)
		}
	}
}

func (c *Client) tryRestorePrimaryRPCEndpoint(ctx context.Context) {
	if c.rpcManager == nil || c.rpcManager.usingPrimary() {
		return
	}

	probeCtx, cancel := context.WithTimeout(ctx, primaryEndpointProbeTimeout)
	defer cancel()

	rpcClientVal, endpoint, err := c.rpcManager.primaryClient()
	if err != nil {
		c.logger.Warn("Primary CometBFT RPC endpoint is not ready", "endpoint", endpoint, "error", err)
		return
	}
	httpClient, ok := rpcClientVal.(*rpchttp.HTTP)
	if !ok {
		return
	}
	if !httpClient.IsRunning() {
		if err := httpClient.Start(); err != nil {
			c.logger.Warn("Primary CometBFT RPC endpoint failed to start", "endpoint", endpoint, "error", err)
			return
		}
	}
	status, err := httpClient.Status(probeCtx)
	if err != nil {
		c.logger.Warn("Primary CometBFT RPC endpoint health check failed", "endpoint", endpoint, "error", err)
		return
	}
	chainID := c.currentCosmosContext().ChainID
	if status.NodeInfo.Network != chainID {
		c.logger.Warn(
			"Primary CometBFT RPC endpoint returned unexpected chain ID",
			"endpoint", endpoint,
			"expected_chain_id", chainID,
			"actual_chain_id", status.NodeInfo.Network,
		)
		return
	}

	c.setRPCClient(rpcClientVal)
	c.rpcManager.switchToPrimary()
	c.logger.Info("CometBFT RPC endpoint restored to primary", "endpoint", endpoint)
}

func (c *Client) RefreshGasEstimatesPeriodically(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	ticker := time.NewTicker(c.refreshGasEstimatesInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("RefreshGasEstimatesPeriodically: context canceled, exiting")
			return
		case <-ticker.C:
			c.logger.Info("Refreshing gas estimate buckets to base levels")
			c.resetAllGasLevelsToBase()
		}
	}
}

func (c *Client) checkReporter(ctx context.Context) bool {
	c.logger.Info("Checking if reporter is created", "address", c.accAddr.String())

	// Retry logic for connection issues - gRPC connections are lazy and may fail initially
	maxRetries := 3
	retryDelay := time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			c.logger.Info("Retrying reporter check", "attempt", attempt+1, "max_retries", maxRetries)
			time.Sleep(retryDelay)
			retryDelay *= 2 // Exponential backoff
		}

		// First try to check if the address is a reporter directly
		var reporterResp *reportertypes.QueryReporterResponse
		err := c.withGRPCFallback(ctx, "reporter lookup", func() error {
			var err error
			reporterResp, err = c.ReporterClient.Reporter(ctx, &reportertypes.QueryReporterRequest{ReporterAddress: c.accAddr.String()})
			return err
		})
		if err == nil {
			c.logger.Info("Reporter found (direct)", "address", c.accAddr.String(), "reporter", reporterResp)
			return true
		}

		// Check if it's a connection error that we should retry
		if isConnectionError(err) && attempt < maxRetries-1 {
			c.logger.Debug("Connection error, will retry", "error", err, "attempt", attempt+1)
			continue
		}

		c.logger.Debug("Direct reporter check failed, trying selector", "error", err, "address", c.accAddr.String())
		// If not a reporter, check if it's a selector that has selected a reporter
		var selectorResp *reportertypes.QuerySelectorReporterResponse
		err = c.withGRPCFallback(ctx, "selector reporter lookup", func() error {
			var err error
			selectorResp, err = c.ReporterClient.SelectorReporter(ctx, &reportertypes.QuerySelectorReporterRequest{SelectorAddress: c.accAddr.String()})
			return err
		})
		if err == nil {
			c.logger.Info("Reporter found (via selector)", "address", c.accAddr.String(), "reporter", selectorResp.Reporter)
			return true
		}

		// Check if it's a connection error that we should retry
		if isConnectionError(err) && attempt < maxRetries-1 {
			c.logger.Debug("Connection error on selector check, will retry", "error", err, "attempt", attempt+1)
			continue
		}

		// If we get here and it's not a connection error, or we've exhausted retries, return false
		if !isConnectionError(err) || attempt == maxRetries-1 {
			c.logger.Info("Reporter check failed - address is neither a reporter nor a selector", "error", err, "address", c.accAddr.String())
			return false
		}
	}

	return false
}

// isConnectionError checks if an error is a transient connection error that should be retried
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := err.Error()
	return strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "connection reset") ||
		strings.Contains(errStr, "connection closed") ||
		strings.Contains(errStr, "transport: Error while dialing") ||
		strings.Contains(errStr, "Unavailable")
}

// trySend attempts to send to txChan but returns false if the context is canceled.
// This prevents panics from sending on a closed channel during shutdown.
func (c *Client) trySend(ctx context.Context, info TxChannelInfo) bool {
	c.txMu.RLock()
	defer c.txMu.RUnlock()
	if c.txClosed {
		c.logger.Debug("trySend: tx channel closed, dropping tx")
		return false
	}

	select {
	case c.txChan <- info:
		return true
	case <-ctx.Done():
		c.logger.Debug("trySend: context canceled, dropping tx")
		return false
	}
}

func (c *Client) closeTxChan() {
	c.txMu.Lock()
	defer c.txMu.Unlock()
	if c.txClosed {
		return
	}
	close(c.txChan)
	c.txClosed = true
}

func normalizeAutoBalanceEthAddr(addr string) (string, error) {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return "", nil
	}
	if !common.IsHexAddress(addr) {
		return "", fmt.Errorf("%s must be a valid Ethereum address, got: %s", daemonflags.FlagAutoBalanceBridgeToEthAddr, addr)
	}
	return strings.TrimPrefix(common.HexToAddress(addr).Hex(), "0x"), nil
}

func parseAutoBalanceExecutionTime(executionTime string) (int, int, error) {
	parts := strings.SplitN(executionTime, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid %s, expected HH:MM, got: %s", daemonflags.FlagAutoBalanceExecutionTime, executionTime)
	}
	hour, errH := strconv.Atoi(parts[0])
	minute, errM := strconv.Atoi(parts[1])
	if errH != nil || errM != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 {
		return 0, 0, fmt.Errorf("invalid %s value, expected HH:MM in UTC, got: %s", daemonflags.FlagAutoBalanceExecutionTime, executionTime)
	}
	return hour, minute, nil
}

// Stop stops the reporter client gracefully
func (c *Client) Stop() {
	c.stopOnce.Do(func() {
		c.logger.Debug("ReporterClient: initiating shutdown")

		// Close the transaction channel to signal BroadcastTxMsgToChain to stop.
		c.closeTxChan()

		// Wait for all goroutines to finish
		c.wg.Wait()

		// Wait for broadcast goroutines to finish
		c.broadcastWg.Wait()

		// Close gRPC connection
		c.grpcMu.Lock()
		defer c.grpcMu.Unlock()
		if c.grpcConn != nil && c.grpcClient != nil {
			if err := c.grpcClient.CloseConnection(c.grpcConn); err != nil {
				c.logger.Error("Failed to close gRPC connection", "error", err)
			}
		}

		// Close remote signer connection if used
		if c.remoteSignerConn != nil {
			if err := c.remoteSignerConn.Close(); err != nil {
				c.logger.Error("Failed to close remote signer connection", "error", err)
			}
		}

		c.logger.Info("ReporterClient: stopped")
	})
}
