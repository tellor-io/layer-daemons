package reader

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"strings"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"golang.org/x/crypto/sha3"
)

// ErrBatchExecutionRequired indicates that the call result is not in cache and needs batch execution
var ErrBatchExecutionRequired = errors.New("batch execution required")

// Context keys for call information
type contextKey string

const (
	chainIDKey    contextKey = "chainID"
	batchGroupKey contextKey = "batchGroup"
	callIDKey     contextKey = "callID"
)

// WithCallInfo adds call information to the context
func WithCallInfo(ctx context.Context, chainID, batchGroup, callID string) context.Context {
	ctx = context.WithValue(ctx, chainIDKey, chainID)
	ctx = context.WithValue(ctx, batchGroupKey, batchGroup)
	ctx = context.WithValue(ctx, callIDKey, callID)
	return ctx
}

// getCallInfo extracts call information from context
func getCallInfo(ctx context.Context) (chainID, batchGroup, callID string, ok bool) {
	chainIDVal := ctx.Value(chainIDKey)
	batchGroupVal := ctx.Value(batchGroupKey)
	callIDVal := ctx.Value(callIDKey)

	if chainIDVal == nil || batchGroupVal == nil || callIDVal == nil {
		return "", "", "", false
	}

	chainID, ok1 := chainIDVal.(string)
	batchGroup, ok2 := batchGroupVal.(string)
	callID, ok3 := callIDVal.(string)

	if !ok1 || !ok2 || !ok3 {
		return "", "", "", false
	}

	return chainID, batchGroup, callID, true
}

// generateCallID generates a unique call ID from address, functionSig, and args
func generateCallID(address, functionSig string, args []string) string {
	// Create a hash from address + functionSig + args
	hash := sha256.New()
	hash.Write([]byte(address))
	hash.Write([]byte(functionSig))
	for _, arg := range args {
		hash.Write([]byte(arg))
	}
	sum := hash.Sum(nil)
	return hex.EncodeToString(sum[:16]) // Use first 16 bytes for shorter ID
}

// ContractReader interface defines the contract reading methods
type ContractReader interface {
	ReadContract(ctx context.Context, address, functionSig string, args []string) ([]byte, error)
	Close()
}

// BatchedReader wraps a ContractReader to intercept and batch calls
type BatchedReader struct {
	reader    ContractReader
	collector *ContractBatchCollector
	executor  *Multicall3Executor
	cache     *BatchCache
	enabled   bool
}

// NewBatchedReader creates a new BatchedReader
func NewBatchedReader(reader ContractReader, collector *ContractBatchCollector, executor *Multicall3Executor, cache *BatchCache, enabled bool) *BatchedReader {
	return &BatchedReader{
		reader:    reader,
		collector: collector,
		executor:  executor,
		cache:     cache,
		enabled:   enabled,
	}
}

// ReadContract intercepts ReadContract calls and either batches them or forwards to wrapped reader
func (b *BatchedReader) ReadContract(ctx context.Context, address, functionSig string, args []string) ([]byte, error) {
	// If batching is disabled, forward to wrapped reader
	if !b.enabled {
		return b.reader.ReadContract(ctx, address, functionSig, args)
	}

	// Extract call information from context
	chainID, batchGroup, callID, hasCallInfo := getCallInfo(ctx)

	// If call info is not in context, generate a callID and use defaults
	// This allows the code to work even if step 2.17 hasn't been implemented yet
	if !hasCallInfo {
		// Generate callID from address, functionSig, and args
		callID = generateCallID(address, functionSig, args)
		// Use default values - these should be set properly in step 2.17
		chainID = "1" // Default chain ID
		batchGroup = "default" // Default batch group
	}

	// Check cache first
	cachedResult, err := b.cache.Get(callID)
	if err == nil {
		// Cache hit - return immediately
		return cachedResult, nil
	}

	// Cache miss - add call to collector
	target := common.HexToAddress(address)

	// Encode the function call to get callData
	callData, err := encodeFunctionCall(functionSig, args)
	if err != nil {
		return nil, fmt.Errorf("failed to encode function call: %w", err)
	}

	// Add call to collector
	err = b.collector.AddCall(chainID, batchGroup, callID, target, callData)
	if err != nil {
		return nil, fmt.Errorf("failed to add call to collector: %w", err)
	}

	// Return error to trigger batch execution
	return nil, ErrBatchExecutionRequired
}

// encodeFunctionCall encodes a function signature and arguments into callData
// This duplicates the logic from Reader.encodeFunctionCall to avoid needing a Reader instance
func encodeFunctionCall(functionSig string, args []string) ([]byte, error) {
	// e.g., "getExchangeRate() returns (uint256)" -> "getExchangeRate()"
	parenIndex := strings.Index(functionSig, "(")
	if parenIndex == -1 {
		return nil, fmt.Errorf("invalid function signature: %s", functionSig)
	}

	// Get the part before "returns" if it exists
	funcPart := functionSig
	if idx := strings.Index(functionSig, " returns"); idx != -1 {
		funcPart = functionSig[:idx]
	}

	// Calculate method ID (first 4 bytes of the hash)
	hash := sha3.NewLegacyKeccak256()
	hash.Write([]byte(funcPart))
	methodID := hash.Sum(nil)[:4]

	if len(args) == 0 {
		return methodID, nil
	}

	// get parameter types
	closeParenIndex := strings.Index(funcPart, ")")
	if closeParenIndex == -1 || closeParenIndex <= parenIndex+1 {
		return methodID, nil
	}

	paramString := funcPart[parenIndex+1 : closeParenIndex]
	paramTypes := strings.Split(paramString, ",")

	for i := range paramTypes {
		paramTypes[i] = strings.TrimSpace(paramTypes[i])
	}

	abiArgs := make(abi.Arguments, len(paramTypes))
	for i, paramType := range paramTypes {
		typ, err := abi.NewType(paramType, "", nil)
		if err != nil {
			return nil, fmt.Errorf("invalid parameter type %s: %w", paramType, err)
		}
		abiArgs[i] = abi.Argument{Type: typ}
	}

	values := make([]interface{}, len(args))
	for i, arg := range args {
		val, err := parseArgument(arg, paramTypes[i])
		if err != nil {
			return nil, fmt.Errorf("failed to parse argument %d: %w", i, err)
		}
		values[i] = val
	}

	encodedArgs, err := abiArgs.Pack(values...)
	if err != nil {
		return nil, fmt.Errorf("failed to encode arguments: %w", err)
	}

	// Combine methodID and encodedArgs
	callData := append(methodID, encodedArgs...)
	return callData, nil
}

// parseArgument parses an argument string into the appropriate type
func parseArgument(arg, paramType string) (interface{}, error) {
	switch {
	case strings.HasPrefix(paramType, "uint"):
		value := new(big.Int)
		value, ok := value.SetString(arg, 10)
		if !ok {
			return nil, fmt.Errorf("invalid uint value: %s", arg)
		}
		return value, nil
	case strings.HasPrefix(paramType, "int"):
		value := new(big.Int)
		value, ok := value.SetString(arg, 10)
		if !ok {
			return nil, fmt.Errorf("invalid int value: %s", arg)
		}
		return value, nil
	case paramType == "address":
		if !common.IsHexAddress(arg) {
			return nil, fmt.Errorf("invalid address: %s", arg)
		}
		return common.HexToAddress(arg), nil
	case paramType == "bool":
		return arg == "true" || arg == "1", nil
	case paramType == "bytes32":
		bytes, err := hex.DecodeString(strings.TrimPrefix(arg, "0x"))
		if err != nil {
			return nil, fmt.Errorf("invalid bytes32 value: %s", arg)
		}
		var bytes32 [32]byte
		copy(bytes32[:], bytes)
		return bytes32, nil
	case paramType == "string":
		return arg, nil
	case strings.HasPrefix(paramType, "bytes"):
		return hex.DecodeString(strings.TrimPrefix(arg, "0x"))
	default:
		return nil, fmt.Errorf("unsupported parameter type: %s", paramType)
	}
}

// ExecuteBatch executes a batch of calls for the given chainID and batchGroup
func (b *BatchedReader) ExecuteBatch(ctx context.Context, chainID, batchGroup string) error {
	// Get calls from collector
	calls, err := b.collector.GetBatch(chainID, batchGroup)
	if err != nil {
		return fmt.Errorf("failed to get batch: %w", err)
	}

	if len(calls) == 0 {
		// No calls to execute
		return nil
	}

	// Execute via Multicall3Executor
	// We need to convert chainID string to uint64
	// The executor already has the chainID set, but we need to make sure it matches
	// For now, we'll use the executor's chainID (it's set at creation time)
	// In a real implementation, we might need multiple executors per chainID

	// Execute the batch
	results, err := b.executor.Execute(ctx, calls)
	if err != nil {
		return fmt.Errorf("failed to execute batch: %w", err)
	}

	// Store results in cache
	for callID, result := range results {
		err = b.cache.Set(callID, result)
		if err != nil {
			// Log error but continue with other results
			// We don't want to fail the entire batch if one cache set fails
			continue
		}
	}

	return nil
}

// Close closes the wrapped reader
func (b *BatchedReader) Close() {
	if b.reader != nil {
		b.reader.Close()
	}
}
