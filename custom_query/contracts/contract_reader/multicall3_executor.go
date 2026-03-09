package reader

import (
	"bytes"
	"context"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
)

// Call represents a single contract call to be batched via Multicall3
type Call struct {
	// Target is the address of the contract to call
	Target common.Address
	// CallData is the encoded function call data (method ID + encoded parameters)
	CallData []byte
	// CallID is a unique identifier for this call, used to route results back
	CallID string
}

// ContractCaller is an interface for making contract calls (allows mocking in tests)
type ContractCaller interface {
	CallContract(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error)
}

// Multicall3Executor executes batched contract calls via Multicall3
type Multicall3Executor struct {
	chainID          uint64
	multicallAddress common.Address
	client           ContractCaller
}

// NewMulticall3Executor creates a new Multicall3Executor
func NewMulticall3Executor(chainID uint64, multicallAddress common.Address, client ContractCaller) *Multicall3Executor {
	return &Multicall3Executor{
		chainID:          chainID,
		multicallAddress: multicallAddress,
		client:           client,
	}
}

// Execute executes a batch of contract calls via Multicall3 and returns results mapped by CallID
// Returns an error if the Multicall3 call fails, but individual call failures are indicated
// by empty returnData in the results map
func (e *Multicall3Executor) Execute(ctx context.Context, calls []Call) (map[string][]byte, error) {
	if len(calls) == 0 {
		return make(map[string][]byte), nil
	}

	// Encode calls for Multicall3 aggregate() function
	// Multicall3.aggregate(Call[] memory calls) returns (Result[] memory returnData)
	// where Call is: struct Call { address target; bytes callData; }
	// and Result is: struct Result { bool success; bytes returnData; }

	// Define the ABI for the Call struct and aggregate function
	// Multicall3 aggregate signature: aggregate((address,bytes)[]) returns ((bool,bytes)[])
	multicallABI := `[{"inputs":[{"components":[{"internalType":"address","name":"target","type":"address"},{"internalType":"bytes","name":"callData","type":"bytes"}],"internalType":"struct Multicall3.Call[]","name":"calls","type":"tuple[]"}],"name":"aggregate","outputs":[{"components":[{"internalType":"bool","name":"success","type":"bool"},{"internalType":"bytes","name":"returnData","type":"bytes"}],"internalType":"struct Multicall3.Result[]","name":"returnData","type":"tuple[]"}],"stateMutability":"payable","type":"function"}]`

	parsedABI, err := abi.JSON(bytes.NewReader([]byte(multicallABI)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse Multicall3 ABI: %w", err)
	}

	// Pack the calls array
	// The ABI method expects a single argument: tuple[] where tuple is (address, bytes)
	method := parsedABI.Methods["aggregate"]
	
	// Build the calls array - each element is a struct with target and callData
	// We need to pass it as []struct{Target common.Address; CallData []byte}
	// But go-ethereum's ABI packing is tricky for struct arrays, so we'll use the method's input arguments directly
	callsValue := make([]struct {
		Target   common.Address
		CallData []byte
	}, len(calls))
	
	for i, call := range calls {
		callsValue[i] = struct {
			Target   common.Address
			CallData []byte
		}{
			Target:   call.Target,
			CallData: call.CallData,
		}
	}
	
	// Pack using method inputs
	callData, err := method.Inputs.Pack(callsValue)
	if err != nil {
		return nil, fmt.Errorf("failed to pack aggregate call: %w", err)
	}

	// Prepend method ID (first 4 bytes of keccak256("aggregate((address,bytes)[])"))
	methodID := method.ID
	callData = append(methodID, callData...)

	// Execute the call
	callMsg := ethereum.CallMsg{
		To:   &e.multicallAddress,
		Data: callData,
	}

	result, err := e.client.CallContract(ctx, callMsg, nil)
	if err != nil {
		return nil, fmt.Errorf("Multicall3 contract call failed: %w", err)
	}

	// Decode results manually because go-ethereum's ABI doesn't handle struct arrays well
	// Multicall3 returns Result[] where Result is (bool success, bytes returnData)
	return e.decodeResults(result, len(calls), calls)
}

// decodeResults decodes Multicall3 results from the raw bytes
func (e *Multicall3Executor) decodeResults(result []byte, numCalls int, calls []Call) (map[string][]byte, error) {
	resultMap := make(map[string][]byte, numCalls)

	if len(result) < 32 {
		return nil, fmt.Errorf("result too short: %d bytes", len(result))
	}

	// Read offset to results array (first 32 bytes)
	offset := new(big.Int).SetBytes(result[0:32]).Uint64()
	if offset >= uint64(len(result)) {
		return nil, fmt.Errorf("invalid offset: %d >= %d", offset, len(result))
	}

	// Read length of results array
	if offset+32 > uint64(len(result)) {
		return nil, fmt.Errorf("cannot read length: offset %d + 32 > %d", offset, len(result))
	}
	length := new(big.Int).SetBytes(result[offset : offset+32]).Uint64()

	if length != uint64(numCalls) {
		return nil, fmt.Errorf("result length mismatch: expected %d, got %d", numCalls, length)
	}

	// Decode each result
	// Each result is: offset (32 bytes) pointing to (bool success (32 bytes) + bytes returnData)
	currentOffset := offset + 32 // Start after length

	for i := 0; i < numCalls; i++ {
		// Read offset to this result
		if currentOffset+32 > uint64(len(result)) {
			return nil, fmt.Errorf("cannot read result %d offset", i)
		}
		resultOffset := new(big.Int).SetBytes(result[currentOffset : currentOffset+32]).Uint64()
		currentOffset += 32

		// Read success flag
		if resultOffset+32 > uint64(len(result)) {
			return nil, fmt.Errorf("cannot read result %d success flag", i)
		}
		successBytes := result[resultOffset : resultOffset+32]
		success := new(big.Int).SetBytes(successBytes).Uint64() != 0

		// Read returnData offset
		returnDataOffset := resultOffset + 32
		if returnDataOffset+32 > uint64(len(result)) {
			return nil, fmt.Errorf("cannot read result %d returnData offset", i)
		}
		returnDataLength := new(big.Int).SetBytes(result[returnDataOffset : returnDataOffset+32]).Uint64()

		// Read returnData
		dataStart := returnDataOffset + 32
		if dataStart+returnDataLength > uint64(len(result)) {
			return nil, fmt.Errorf("cannot read result %d returnData", i)
		}

		callID := calls[i].CallID
		if success {
			// Copy the return data
			returnData := make([]byte, returnDataLength)
			copy(returnData, result[dataStart:dataStart+returnDataLength])
			resultMap[callID] = returnData
		} else {
			// Failed call - return empty bytes
			resultMap[callID] = []byte{}
		}
	}

	return resultMap, nil
}

