package client

import (
	"context"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	tokenbridgetypes "github.com/tellor-io/layer-daemons/server/types/token_bridge"
	tokenbridgetipstypes "github.com/tellor-io/layer-daemons/server/types/token_bridge_tips"
	tokenbridge "github.com/tellor-io/layer-daemons/token_bridge_feed/abi/v2"

	"cosmossdk.io/log"
)

type Client struct {
	lastReportedDepositId    *big.Int
	logger                   log.Logger
	tokenDepositsCache       *tokenbridgetypes.DepositReports
	tokenBridgeTipsCache     *tokenbridgetipstypes.DepositTips
	daemonStartup            sync.WaitGroup
	runningSubtasksWaitGroup sync.WaitGroup
	tickers                  []*time.Ticker
	stops                    []chan bool

	primaryEthClient       *ethclient.Client
	fallbackEthClient      *ethclient.Client
	primaryBridgeContract  *tokenbridge.TokenBridgeV2
	fallbackBridgeContract *tokenbridge.TokenBridgeV2
}

type DepositReceipt struct {
	DepositId   *big.Int
	Sender      common.Address
	Recipient   string
	Amount      *big.Int
	Tip         *big.Int
	BlockHeight *big.Int
}

type DepositReport struct {
	QueryData []byte
	Value     string
}

// Struct to unmarshal the JSON data
type APIResponse struct {
	Status string `json:"status"`
	Data   []struct {
		ExecBlockNumber int `json:"exec_block_number"`
	} `json:"data"`
}

func StartNewClient(ctx context.Context, logger log.Logger, tokenDepositsCache *tokenbridgetypes.DepositReports, tokenBridgeTipsCache *tokenbridgetipstypes.DepositTips) *Client {
	logger.Info("Starting tokenbridge daemon")

	client := newClient(logger, tokenDepositsCache, tokenBridgeTipsCache)
	client.runningSubtasksWaitGroup.Add(1)
	go func() {
		defer client.runningSubtasksWaitGroup.Done()
		client.start(ctx)
	}()
	return client
}

// waitForContractInitialized polls until query returns (true, nil), or ctx ends.
// On query errors it waits retryDelay between attempts (interruptible by ctx).
func waitForContractInitialized(ctx context.Context, logger log.Logger, retryDelay time.Duration, query func() (bool, error)) error {
	for {
		if err := ctx.Err(); err != nil {
			logger.Debug("TokenBridgeClient: context canceled during contract init wait")
			return err
		}

		initialized, err := query()
		if err != nil {
			logger.Error("Failed to check initialization status, retrying...", "error", err)
			select {
			case <-ctx.Done():
				logger.Debug("TokenBridgeClient: context canceled during contract init wait")
				return ctx.Err()
			case <-time.After(retryDelay):
			}
			continue
		}
		if initialized {
			logger.Info("Contract is initialized, starting deposit monitoring")
			return nil
		}
		logger.Info("Contract not yet initialized, waiting...")
		select {
		case <-ctx.Done():
			logger.Debug("TokenBridgeClient: context canceled during contract init wait")
			return ctx.Err()
		case <-time.After(retryDelay):
		}
	}
}

func newClient(logger log.Logger, tokenDepositsCache *tokenbridgetypes.DepositReports, tokenBridgeTipsCache *tokenbridgetipstypes.DepositTips) *Client {
	logger = logger.With(log.ModuleKey, "tokenbridge-daemon")
	client := &Client{
		tickers:              []*time.Ticker{},
		stops:                []chan bool{},
		logger:               logger,
		tokenDepositsCache:   tokenDepositsCache,
		tokenBridgeTipsCache: tokenBridgeTipsCache,
	}

	// Set the client's daemonStartup state to indicate that the daemon has not finished starting up.
	client.daemonStartup.Add(1)
	return client
}

func (c *Client) start(ctx context.Context) {
	// If we exit before signaling startup complete, Stop() must not block on daemonStartup forever.
	startupSignaled := false
	defer func() {
		if !startupSignaled {
			c.daemonStartup.Done()
		}
	}()

	// Initialize clients and contracts first (needed to check initialization status)
	if err := c.initializeClientsAndContracts(); err != nil {
		c.logger.Error("Failed to initialize clients and contracts", "error", err)
		return
	}

	const initRetryDelay = 2 * time.Minute

	// Wait for contract to be initialized before starting the main loop
	c.logger.Info("Waiting for contract to be initialized...")
	if err := waitForContractInitialized(ctx, c.logger, initRetryDelay, c.QueryHasContractBeenInitialized); err != nil {
		return
	}

	if err := c.InitializeDeposits(); err != nil {
		c.logger.Error("Failed to initialize deposits", "error", err)
		return
	}
	// Mark startup as complete after initialization
	startupSignaled = true
	c.daemonStartup.Done()

	ticker := time.NewTicker(10 * time.Second)
	c.tickers = append(c.tickers, ticker)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			c.logger.Debug("TokenBridgeClient: context canceled, exiting")
			return
		case <-ticker.C:
			// Process regular deposits
			if err := c.QueryTokenBridgeContract(); err != nil {
				c.logger.Error("Failed to query and process deposits", "error", err)
			}

			// Process tips
			if err := c.ProcessPendingTips(); err != nil {
				c.logger.Error("Failed to process pending tips", "error", err)
			}
		}
	}
}

// Add new method to process tips
func (c *Client) ProcessPendingTips() error {
	oldestTipQueryData, err := c.tokenBridgeTipsCache.GetOldestTip()
	if err != nil {
		return nil
	}

	// Decode the query data to extract depositId
	queryType, depositId, err := c.DecodeQueryData(oldestTipQueryData.QueryData)
	if err != nil {
		c.logger.Error("Failed to decode tip query data", "error", err)
		c.tokenBridgeTipsCache.RemoveOldestTip()
		return nil
	}

	// Verify this is a TRBBridge query
	if queryType != "TRBBridgeV2" {
		c.logger.Error("Invalid query type for tip", "queryType", queryType)
		c.tokenBridgeTipsCache.RemoveOldestTip()
		return nil
	}

	// Query deposit details
	depositTicket, err := c.QueryDepositDetails(depositId)
	if err != nil {
		c.logger.Error("Failed to query deposit details for tip", "error", err)
		return nil
	}

	// Check whether the deposit exists
	if depositTicket.Amount.Cmp(big.NewInt(0)) == 0 {
		c.logger.Info("Deposit does not exist", "depositId", depositId)
		c.tokenBridgeTipsCache.RemoveOldestTip()
		return nil
	}

	// Check finality
	isFinal, err := c.CheckForFinality(depositTicket.BlockHeight)
	if err != nil || !isFinal {
		c.logger.Info("Tip deposit not yet final", "depositId", depositId)
		return nil
	}

	reportValue, err := c.EncodeReportValue(depositTicket)
	if err != nil {
		c.logger.Error("Failed to encode report value for tip", "error", err)
		return nil
	}

	// Add to deposits cache
	c.tokenDepositsCache.AddReport(tokenbridgetypes.DepositReport{
		QueryData: oldestTipQueryData.QueryData,
		Value:     reportValue,
	})

	// Remove from tips cache
	c.tokenBridgeTipsCache.RemoveOldestTip()
	c.logger.Info("Processed tip and added to deposits", "depositId", depositId)

	return nil
}

// Add helper method to decode query data
func (c *Client) DecodeQueryData(queryData []byte) (string, *big.Int, error) {
	// Prepare types for decoding
	StringType, err := abi.NewType("string", "", nil)
	if err != nil {
		return "", nil, err
	}
	BytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return "", nil, err
	}

	// Decode outer layer
	args := abi.Arguments{{Type: StringType}, {Type: BytesType}}
	decoded, err := args.Unpack(queryData)
	if err != nil {
		return "", nil, err
	}

	queryType := decoded[0].(string)
	innerData := decoded[1].([]byte)

	// Decode inner layer
	BoolType, err := abi.NewType("bool", "", nil)
	if err != nil {
		return "", nil, err
	}
	Uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return "", nil, err
	}

	innerArgs := abi.Arguments{{Type: BoolType}, {Type: Uint256Type}}
	innerDecoded, err := innerArgs.Unpack(innerData)
	if err != nil {
		return "", nil, err
	}

	isDeposit := innerDecoded[0].(bool)
	if !isDeposit {
		return "", nil, fmt.Errorf("tip is not a deposit")
	}
	depositId := innerDecoded[1].(*big.Int)
	return queryType, depositId, nil
}

func (c *Client) QueryAPI(urlStr string) ([]byte, error) {
	c.logger.Info("querying token_bridge_client api")
	parsedUrl, err := url.ParseRequestURI(urlStr)
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(parsedUrl.String())
	if err != nil {
		return nil, fmt.Errorf("failed to make API request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("API request failed with status: %s", resp.Status)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read API response: %w", err)
	}

	return body, nil
}

func (c *Client) getEthRpcUrls() (string, string, error) {
	primaryUrl := os.Getenv("ETH_RPC_URL_PRIMARY")
	if primaryUrl == "" {
		return "", "", fmt.Errorf("ETH_RPC_URL_PRIMARY not set")
	}

	fallbackUrl := os.Getenv("ETH_RPC_URL_FALLBACK")
	if fallbackUrl == "" {
		return "", "", fmt.Errorf("ETH_RPC_URL_FALLBACK not set")
	}

	return strings.TrimSpace(primaryUrl), strings.TrimSpace(fallbackUrl), nil
}

// initializeClientsAndContracts sets up the Ethereum clients and contract instances
// This must be called before checking if the contract is initialized
func (c *Client) initializeClientsAndContracts() error {
	primaryUrl, fallbackUrl, err := c.getEthRpcUrls()
	if err != nil {
		return fmt.Errorf("failed to get ETH RPC urls: %w", err)
	}

	// Connect to primary endpoint
	c.primaryEthClient, err = ethclient.Dial(primaryUrl)
	if err != nil {
		return fmt.Errorf("failed to connect to primary RPC endpoint: %w", err)
	}

	// Connect to fallback endpoint
	c.fallbackEthClient, err = ethclient.Dial(fallbackUrl)
	if err != nil {
		return fmt.Errorf("failed to connect to fallback RPC endpoint: %w", err)
	}

	contractAddress, err := c.getTokenBridgeContractAddress()
	if err != nil {
		return fmt.Errorf("failed to get token bridge contract address: %w", err)
	}

	// Initialize contracts
	c.primaryBridgeContract, err = tokenbridge.NewTokenBridgeV2(contractAddress, c.primaryEthClient)
	if err != nil {
		return fmt.Errorf("failed to instantiate primary TokenBridge contract: %w", err)
	}

	c.fallbackBridgeContract, err = tokenbridge.NewTokenBridgeV2(contractAddress, c.fallbackEthClient)
	if err != nil {
		return fmt.Errorf("failed to instantiate fallback TokenBridge contract: %w", err)
	}

	return nil
}

func (c *Client) InitializeDeposits() error {
	// Ensure clients and contracts are initialized (in case they weren't already)
	if c.primaryBridgeContract == nil || c.fallbackBridgeContract == nil {
		if err := c.initializeClientsAndContracts(); err != nil {
			return fmt.Errorf("failed to initialize clients and contracts: %w", err)
		}
	}

	latestDepositId, err := c.QueryCurrentDepositId()
	if err != nil {
		return fmt.Errorf("failed to query the latest deposit ID: %w", err)
	}

	c.lastReportedDepositId = latestDepositId
	return nil
}

func (c *Client) QueryTokenBridgeContract() error {
	var latestDepositId *big.Int
	var err error

	for retries := 0; retries < 3; retries++ {
		latestDepositId, err = c.QueryCurrentDepositId()
		if err != nil {
			if retries < 2 {
				c.logger.Error("Failed to query latest deposit ID, reconnecting...",
					"attempt", retries+1, "error", err)
				if err := c.reconnectEthClient(); err != nil {
					c.logger.Error("Failed to reconnect", "error", err)
					time.Sleep(time.Second * 5)
					continue
				}
			} else {
				return fmt.Errorf("failed to query the latest deposit ID: %w", err)
			}
		} else {
			break
		}
	}

	if c.lastReportedDepositId == nil {
		c.lastReportedDepositId = big.NewInt(0)
	}

	if latestDepositId.Uint64() > c.lastReportedDepositId.Uint64() {
		nextDepositId := big.NewInt(int64(c.lastReportedDepositId.Uint64() + 1))

		depositTicket, err := c.QueryDepositDetails(nextDepositId)
		if err != nil {
			return fmt.Errorf("failed to query deposit details: %w", err)
		}

		// Check if the block height is final
		isFinal, err := c.CheckForFinality(depositTicket.BlockHeight)
		if err != nil {
			return fmt.Errorf("failed to check if block height is final: %w", err)
		}

		if !isFinal {
			c.logger.Info("Block height is not final", "blockHeight", depositTicket.BlockHeight)
			return nil
		}

		// assemble and add to pending reports
		queryData, err := c.EncodeQueryData(depositTicket)
		if err != nil {
			c.logger.Error("Failed to encode query data", "error", err)
		}
		reportValue, err := c.EncodeReportValue(depositTicket)
		if err != nil {
			c.logger.Error("Failed to encode report value", "error", err)
		}

		// Update the token deposits cache
		c.tokenDepositsCache.AddReport(tokenbridgetypes.DepositReport{QueryData: queryData, Value: reportValue})

		// Update the last reported deposit ID
		c.lastReportedDepositId = nextDepositId
		c.logger.Info("Added deposit to pending reports", "depositId", c.lastReportedDepositId)
	}

	return nil
}

func (c *Client) CheckForFinality(blockHeight *big.Int) (bool, error) {
	// Try primary first
	currentBlock, err := c.primaryEthClient.BlockNumber(context.Background())
	if err != nil {
		c.logger.Error("Failed to query primary client, trying fallback", "error", err)
		// Try fallback
		currentBlock, err = c.fallbackEthClient.BlockNumber(context.Background())
		if err != nil {
			return false, fmt.Errorf("failed to query block number from both endpoints: %w", err)
		}
	}

	currentBlockBigInt := new(big.Int).SetUint64(currentBlock)
	return currentBlockBigInt.Sub(currentBlockBigInt, blockHeight).Cmp(big.NewInt(100)) >= 0, nil
}

func (c *Client) EncodeQueryData(depositReceipt DepositReceipt) ([]byte, error) {
	// encode query data
	queryTypeString := "TRBBridgeV2"
	toLayerBool := true
	// prepare encoding
	StringType, err := abi.NewType("string", "", nil)
	if err != nil {
		return nil, err
	}
	Uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, err
	}
	BoolType, err := abi.NewType("bool", "", nil)
	if err != nil {
		return nil, err
	}
	BytesType, err := abi.NewType("bytes", "", nil)
	if err != nil {
		return nil, err
	}

	// encode query data arguments first
	queryDataArgs := abi.Arguments{
		{Type: BoolType},
		{Type: Uint256Type},
	}
	queryDataArgsEncoded, err := queryDataArgs.Pack(toLayerBool, depositReceipt.DepositId)
	if err != nil {
		return nil, err
	}

	// encode query data
	finalArgs := abi.Arguments{
		{Type: StringType},
		{Type: BytesType},
	}
	queryDataEncoded, err := finalArgs.Pack(queryTypeString, queryDataArgsEncoded)
	if err != nil {
		return nil, err
	}
	return queryDataEncoded, nil
}

// replicate solidity encoding, abi.encode(address ethSender, string layerRecipient, uint256 amount)
func (c *Client) EncodeReportValue(depositReceipt DepositReceipt) ([]byte, error) {
	// prepare encoding
	AddressType, err := abi.NewType("address", "", nil)
	if err != nil {
		return nil, err
	}
	Uint256Type, err := abi.NewType("uint256", "", nil)
	if err != nil {
		return nil, err
	}
	StringType, err := abi.NewType("string", "", nil)
	if err != nil {
		return nil, err
	}

	reportValueArgs := abi.Arguments{
		{Type: AddressType},
		{Type: StringType},
		{Type: Uint256Type},
		{Type: Uint256Type},
	}

	// encode report value arguments
	reportValueArgsEncoded, err := reportValueArgs.Pack(depositReceipt.Sender, depositReceipt.Recipient, depositReceipt.Amount, depositReceipt.Tip)
	if err != nil {
		return nil, err
	}

	return reportValueArgsEncoded, nil
}

func (c *Client) getTokenBridgeContractAddress() (common.Address, error) {
	tokenBridgeContractAddress := os.Getenv("TOKEN_BRIDGE_CONTRACT")
	if tokenBridgeContractAddress == "" {
		return common.Address{}, fmt.Errorf("TOKEN_BRIDGE_CONTRACT not set")
	} else {
		fmt.Println("TOKEN_BRIDGE_CONTRACT", tokenBridgeContractAddress)
	}
	return common.HexToAddress(tokenBridgeContractAddress), nil
}

// Add new helper function for reconnection
func (c *Client) reconnectEthClient() error {
	primaryUrl, fallbackUrl, err := c.getEthRpcUrls()
	if err != nil {
		return fmt.Errorf("failed to get ETH RPC urls: %w", err)
	}

	// Close existing clients
	if c.primaryEthClient != nil {
		c.primaryEthClient.Close()
	}
	if c.fallbackEthClient != nil {
		c.fallbackEthClient.Close()
	}

	// Reconnect primary
	c.primaryEthClient, err = ethclient.Dial(primaryUrl)
	if err != nil {
		return fmt.Errorf("failed to reconnect to primary endpoint: %w", err)
	}

	// Reconnect fallback
	c.fallbackEthClient, err = ethclient.Dial(fallbackUrl)
	if err != nil {
		return fmt.Errorf("failed to reconnect to fallback endpoint: %w", err)
	}

	contractAddress, err := c.getTokenBridgeContractAddress()
	if err != nil {
		return fmt.Errorf("failed to get token bridge contract address: %w", err)
	}

	// Reinitialize contracts
	c.primaryBridgeContract, err = tokenbridge.NewTokenBridgeV2(contractAddress, c.primaryEthClient)
	if err != nil {
		return fmt.Errorf("failed to reinstantiate primary TokenBridge contract: %w", err)
	}

	c.fallbackBridgeContract, err = tokenbridge.NewTokenBridgeV2(contractAddress, c.fallbackEthClient)
	if err != nil {
		return fmt.Errorf("failed to reinstantiate fallback TokenBridge contract: %w", err)
	}

	return nil
}

func (c *Client) QueryCurrentDepositId() (*big.Int, error) {
	// try primary first
	depositId, err := c.primaryBridgeContract.DepositId(nil)
	if err != nil {
		c.logger.Error("Failed to query primary contract, trying fallback", "error", err)
		// try fallback
		depositId, err = c.fallbackBridgeContract.DepositId(nil)
		if err != nil {
			return nil, fmt.Errorf("failed to query deposit ID from both endpoints: %w", err)
		}
	}
	return depositId, nil
}

func (c *Client) QueryHasContractBeenInitialized() (bool, error) {
	// try primary first
	initialized, err := c.primaryBridgeContract.Initialized(nil)
	if err != nil {
		c.logger.Error("Failed to query primary contract, trying fallback", "error", err)
		// try fallback
		initialized, err = c.fallbackBridgeContract.Initialized(nil)
	}

	if err != nil {
		c.logger.Error("Failed to query fallback contract", "error", err)
		return false, fmt.Errorf("failed to query has contract been initialized from both endpoints: %w", err)
	}
	return initialized, nil
}

func (c *Client) QueryDepositDetails(depositId *big.Int) (DepositReceipt, error) {
	// Try primary first
	deposit, err := c.primaryBridgeContract.Deposits(nil, depositId)
	if err != nil {
		c.logger.Error("Failed to query primary contract, trying fallback", "error", err)
		// Try fallback
		deposit, err = c.fallbackBridgeContract.Deposits(nil, depositId)
		if err != nil {
			return DepositReceipt{}, fmt.Errorf("failed to query deposit details from both endpoints: %w", err)
		}
	}

	if deposit.Amount.Cmp(big.NewInt(0)) == 0 || deposit.BlockHeight.Cmp(big.NewInt(0)) == 0 {
		return DepositReceipt{}, fmt.Errorf("deposit details are not available yet. RPC returned zero values")
	}

	return DepositReceipt{
		DepositId:   depositId,
		Sender:      deposit.Sender,
		Recipient:   deposit.Recipient,
		Amount:      deposit.Amount,
		Tip:         deposit.Tip,
		BlockHeight: deposit.BlockHeight,
	}, nil
}

// Stop stops the token bridge client and all running subtasks
func (c *Client) Stop() {
	c.logger.Debug("TokenBridgeClient: initiating shutdown")
	// Wait for startup to complete (if it hasn't already)
	c.daemonStartup.Wait()

	// Stop all tickers
	for _, ticker := range c.tickers {
		ticker.Stop()
	}

	// Close all stop channels
	for _, stop := range c.stops {
		close(stop)
	}

	// Close Ethereum clients
	if c.primaryEthClient != nil {
		c.primaryEthClient.Close()
	}
	if c.fallbackEthClient != nil {
		c.fallbackEthClient.Close()
	}

	// Wait for all subtasks to complete
	c.runningSubtasksWaitGroup.Wait()
	c.logger.Info("TokenBridgeClient: stopped")
}
