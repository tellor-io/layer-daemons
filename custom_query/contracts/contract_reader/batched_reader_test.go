package reader

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
)

// mockReader is a mock implementation of Reader for testing
type mockReader struct {
	readContractFunc func(ctx context.Context, address, functionSig string, args []string) ([]byte, error)
}

func (m *mockReader) ReadContract(ctx context.Context, address, functionSig string, args []string) ([]byte, error) {
	if m.readContractFunc != nil {
		return m.readContractFunc(ctx, address, functionSig, args)
	}
	return nil, errors.New("readContractFunc not set")
}

func (m *mockReader) Close() {
	// No-op for mock
}

// TestBatchedReader_DisabledBatchingForwardsToWrappedReader tests that when batching is disabled,
// calls are forwarded directly to the wrapped reader
func TestBatchedReader_DisabledBatchingForwardsToWrappedReader(t *testing.T) {
	expectedResult := []byte{0x01, 0x02, 0x03}
	mock := &mockReader{
		readContractFunc: func(ctx context.Context, address, functionSig string, args []string) ([]byte, error) {
			return expectedResult, nil
		},
	}

	collector := NewContractBatchCollector()
	cache := NewBatchCache()
	executor := NewMulticall3Executor(1, common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"), nil)

	batchedReader := NewBatchedReader(mock, collector, executor, cache, false)

	ctx := context.Background()
	result, err := batchedReader.ReadContract(ctx, "0x1234", "test()", []string{})
	if err != nil {
		t.Fatalf("ReadContract failed: %v", err)
	}

	if len(result) != len(expectedResult) {
		t.Errorf("expected result length %d, got %d", len(expectedResult), len(result))
	}

	for i := range expectedResult {
		if result[i] != expectedResult[i] {
			t.Errorf("expected result[%d] = %d, got %d", i, expectedResult[i], result[i])
		}
	}
}

// TestBatchedReader_EnabledBatchingCollectsCalls tests that when batching is enabled,
// calls are added to the collector
func TestBatchedReader_EnabledBatchingCollectsCalls(t *testing.T) {
	mock := &mockReader{
		readContractFunc: func(ctx context.Context, address, functionSig string, args []string) ([]byte, error) {
			return nil, errors.New("should not be called when batching is enabled")
		},
	}

	collector := NewContractBatchCollector()
	cache := NewBatchCache()
	executor := NewMulticall3Executor(1, common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"), nil)

	batchedReader := NewBatchedReader(mock, collector, executor, cache, true)

	chainID := "1"
	batchGroup := "group1"
	callID := "test-call-1"
	address := "0x1234567890123456789012345678901234567890"

	ctx := WithCallInfo(context.Background(), chainID, batchGroup, callID)

	// First call should result in cache miss and return error
	_, err := batchedReader.ReadContract(ctx, address, "test()", []string{})
	if err == nil {
		t.Error("expected error on cache miss, got nil")
	}
	if !errors.Is(err, ErrBatchExecutionRequired) {
		t.Errorf("expected ErrBatchExecutionRequired, got %v", err)
	}

	// Verify call was added to collector
	batch, err := collector.GetBatch(chainID, batchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch) != 1 {
		t.Errorf("expected 1 call in batch, got %d", len(batch))
	}

	if batch[0].CallID != callID {
		t.Errorf("expected CallID %q, got %q", callID, batch[0].CallID)
	}

	expectedTarget := common.HexToAddress(address)
	if batch[0].Target != expectedTarget {
		t.Errorf("expected Target %v, got %v", expectedTarget, batch[0].Target)
	}
}

// TestBatchedReader_CacheHitReturnsImmediately tests that when cache has the result,
// it returns immediately without calling wrapped reader
func TestBatchedReader_CacheHitReturnsImmediately(t *testing.T) {
	mock := &mockReader{
		readContractFunc: func(ctx context.Context, address, functionSig string, args []string) ([]byte, error) {
			return nil, errors.New("should not be called when cache hit")
		},
	}

	collector := NewContractBatchCollector()
	cache := NewBatchCache()
	executor := NewMulticall3Executor(1, common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"), nil)

	batchedReader := NewBatchedReader(mock, collector, executor, cache, true)

	chainID := "1"
	batchGroup := "group1"
	callID := "test-call-1"
	address := "0x1234567890123456789012345678901234567890"
	expectedResult := []byte{0xAA, 0xBB, 0xCC}

	// Pre-populate cache
	err := cache.Set(callID, expectedResult)
	if err != nil {
		t.Fatalf("Set cache failed: %v", err)
	}

	ctx := WithCallInfo(context.Background(), chainID, batchGroup, callID)

	result, err := batchedReader.ReadContract(ctx, address, "test()", []string{})
	if err != nil {
		t.Fatalf("ReadContract failed: %v", err)
	}

	if len(result) != len(expectedResult) {
		t.Errorf("expected result length %d, got %d", len(expectedResult), len(result))
	}

	for i := range expectedResult {
		if result[i] != expectedResult[i] {
			t.Errorf("expected result[%d] = %d, got %d", i, expectedResult[i], result[i])
		}
	}
}

// TestBatchedReader_ExecuteBatch tests that ExecuteBatch executes calls and stores results in cache
func TestBatchedReader_ExecuteBatch(t *testing.T) {
	chainID := uint64(1)
	multicallAddress := common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11")

	// Mock client that returns successful results
	mockClient := &mockEthClient{
		callContractFunc: func(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			// Return results for 2 calls
			result := []byte{
				// Offset to results array
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40,
				// Length of results array = 2
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02,
				// Offset to first result
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40,
				// Offset to second result
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00,
				// First result: success=true, returnData=[0x11, 0x22]
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02,
				0x11, 0x22, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Second result: success=true, returnData=[0x33, 0x44, 0x55]
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03,
				0x33, 0x44, 0x55, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			}
			return result, nil
		},
	}

	mock := &mockReader{}
	collector := NewContractBatchCollector()
	cache := NewBatchCache()
	executor := NewMulticall3Executor(chainID, multicallAddress, mockClient)

	batchedReader := NewBatchedReader(mock, collector, executor, cache, true)

	chainIDStr := "1"
	batchGroup := "group1"
	callID1 := "call-1"
	callID2 := "call-2"
	address1 := "0x1111111111111111111111111111111111111111"
	address2 := "0x2222222222222222222222222222222222222222"

	// Add calls to collector
	ctx1 := WithCallInfo(context.Background(), chainIDStr, batchGroup, callID1)
	_, _ = batchedReader.ReadContract(ctx1, address1, "test()", []string{})

	ctx2 := WithCallInfo(context.Background(), chainIDStr, batchGroup, callID2)
	_, _ = batchedReader.ReadContract(ctx2, address2, "test()", []string{})

	// Execute batch
	ctx := context.Background()
	err := batchedReader.ExecuteBatch(ctx, chainIDStr, batchGroup)
	if err != nil {
		t.Fatalf("ExecuteBatch failed: %v", err)
	}

	// Verify results are in cache
	result1, err := cache.Get(callID1)
	if err != nil {
		t.Fatalf("Get callID1 from cache failed: %v", err)
	}
	expected1 := []byte{0x11, 0x22}
	if len(result1) != len(expected1) {
		t.Errorf("expected callID1 result length %d, got %d", len(expected1), len(result1))
	}

	result2, err := cache.Get(callID2)
	if err != nil {
		t.Fatalf("Get callID2 from cache failed: %v", err)
	}
	expected2 := []byte{0x33, 0x44, 0x55}
	if len(result2) != len(expected2) {
		t.Errorf("expected callID2 result length %d, got %d", len(expected2), len(result2))
	}
}

// TestBatchedReader_ResultsRoutedCorrectlyByCallID tests that results are correctly routed by CallID
func TestBatchedReader_ResultsRoutedCorrectlyByCallID(t *testing.T) {
	chainID := uint64(1)
	multicallAddress := common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11")

	mockClient := &mockEthClient{
		callContractFunc: func(ctx context.Context, msg ethereum.CallMsg, blockNumber *big.Int) ([]byte, error) {
			// Return 3 results with different data
			result := []byte{
				// Offset to results
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40,
				// Length = 3
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03,
				// Offsets
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x60,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x40,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02, 0x20,
				// Result 1: success=true, data=[0xAA]
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0xAA, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Result 2: success=true, data=[0xBB, 0xCC]
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x02,
				0xBB, 0xCC, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// Result 3: success=true, data=[0xDD, 0xEE, 0xFF]
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x03,
				0xDD, 0xEE, 0xFF, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
			}
			return result, nil
		},
	}

	mock := &mockReader{}
	collector := NewContractBatchCollector()
	cache := NewBatchCache()
	executor := NewMulticall3Executor(chainID, multicallAddress, mockClient)

	batchedReader := NewBatchedReader(mock, collector, executor, cache, true)

	chainIDStr := "1"
	batchGroup := "group1"
	callID1 := "query1-source1-call1"
	callID2 := "query2-source2-call2"
	callID3 := "query3-source3-call3"

	// Add calls to collector
	ctx1 := WithCallInfo(context.Background(), chainIDStr, batchGroup, callID1)
	_, _ = batchedReader.ReadContract(ctx1, "0x1111", "test()", []string{})

	ctx2 := WithCallInfo(context.Background(), chainIDStr, batchGroup, callID2)
	_, _ = batchedReader.ReadContract(ctx2, "0x2222", "test()", []string{})

	ctx3 := WithCallInfo(context.Background(), chainIDStr, batchGroup, callID3)
	_, _ = batchedReader.ReadContract(ctx3, "0x3333", "test()", []string{})

	// Execute batch
	ctx := context.Background()
	err := batchedReader.ExecuteBatch(ctx, chainIDStr, batchGroup)
	if err != nil {
		t.Fatalf("ExecuteBatch failed: %v", err)
	}

	// Verify each CallID maps to correct result
	expectedResults := map[string][]byte{
		"query1-source1-call1": {0xAA},
		"query2-source2-call2": {0xBB, 0xCC},
		"query3-source3-call3": {0xDD, 0xEE, 0xFF},
	}

	for callID, expected := range expectedResults {
		result, err := cache.Get(callID)
		if err != nil {
			t.Errorf("result for CallID %s not found in cache: %v", callID, err)
			continue
		}
		if len(result) != len(expected) {
			t.Errorf("CallID %s: expected length %d, got %d", callID, len(expected), len(result))
			continue
		}
		for i := range expected {
			if result[i] != expected[i] {
				t.Errorf("CallID %s: expected result[%d] = %d, got %d", callID, i, expected[i], result[i])
			}
		}
	}
}

// TestBatchedReader_MultipleCallsPerHandler tests that multiple calls from the same handler work correctly
func TestBatchedReader_MultipleCallsPerHandler(t *testing.T) {
	mock := &mockReader{
		readContractFunc: func(ctx context.Context, address, functionSig string, args []string) ([]byte, error) {
			return nil, errors.New("should not be called when batching is enabled")
		},
	}

	collector := NewContractBatchCollector()
	cache := NewBatchCache()
	executor := NewMulticall3Executor(1, common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"), nil)

	batchedReader := NewBatchedReader(mock, collector, executor, cache, true)

	chainID := "1"
	batchGroup := "group1"

	// Make multiple calls with different callIDs
	callIDs := []string{"call1", "call2", "call3"}
	addresses := []string{
		"0x1111111111111111111111111111111111111111",
		"0x2222222222222222222222222222222222222222",
		"0x3333333333333333333333333333333333333333",
	}

	for i, callID := range callIDs {
		ctx := WithCallInfo(context.Background(), chainID, batchGroup, callID)
		_, _ = batchedReader.ReadContract(ctx, addresses[i], "test()", []string{})
	}

	// Verify all calls were added to collector
	batch, err := collector.GetBatch(chainID, batchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch) != len(callIDs) {
		t.Errorf("expected %d calls in batch, got %d", len(callIDs), len(batch))
	}

	// Verify all callIDs are present
	callIDMap := make(map[string]bool)
	for _, call := range batch {
		callIDMap[call.CallID] = true
	}

	for _, callID := range callIDs {
		if !callIDMap[callID] {
			t.Errorf("expected callID %q to be present in batch", callID)
		}
	}
}

// TestBatchedReader_CallIDFormatFromContext tests that callIDs in the format {queryID}-{sourceID}-{callKey}
// are properly extracted from context and used for batching
func TestBatchedReader_CallIDFormatFromContext(t *testing.T) {
	mock := &mockReader{
		readContractFunc: func(ctx context.Context, address, functionSig string, args []string) ([]byte, error) {
			return nil, errors.New("should not be called when batching is enabled")
		},
	}

	collector := NewContractBatchCollector()
	cache := NewBatchCache()
	executor := NewMulticall3Executor(1, common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"), nil)

	batchedReader := NewBatchedReader(mock, collector, executor, cache, true)

	chainID := "1"
	batchGroup := "ethereum"

	// Test callID in the format generated by ParallelFetcher: {queryID}-{sourceID}-{callKey}
	queryID := "query123"
	sourceID := "ethereum"
	callKey := "total_assets"
	expectedCallID := fmt.Sprintf("%s-%s-%s", queryID, sourceID, callKey)

	ctx := WithCallInfo(context.Background(), chainID, batchGroup, expectedCallID)
	address := "0x1234567890123456789012345678901234567890"

	// Call should result in cache miss and return error
	_, err := batchedReader.ReadContract(ctx, address, "test()", []string{})
	if err == nil {
		t.Error("expected error on cache miss, got nil")
	}
	if !errors.Is(err, ErrBatchExecutionRequired) {
		t.Errorf("expected ErrBatchExecutionRequired, got %v", err)
	}

	// Verify call was added to collector with correct CallID
	batch, err := collector.GetBatch(chainID, batchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch) != 1 {
		t.Errorf("expected 1 call in batch, got %d", len(batch))
	}

	if batch[0].CallID != expectedCallID {
		t.Errorf("expected CallID %q, got %q", expectedCallID, batch[0].CallID)
	}

	expectedTarget := common.HexToAddress(address)
	if batch[0].Target != expectedTarget {
		t.Errorf("expected Target %v, got %v", expectedTarget, batch[0].Target)
	}
}

// TestBatchedReader_CallIDFormatMultipleCalls tests that multiple calls with different callKeys
// from the same query and source are properly batched together
func TestBatchedReader_CallIDFormatMultipleCalls(t *testing.T) {
	mock := &mockReader{
		readContractFunc: func(ctx context.Context, address, functionSig string, args []string) ([]byte, error) {
			return nil, errors.New("should not be called when batching is enabled")
		},
	}

	collector := NewContractBatchCollector()
	cache := NewBatchCache()
	executor := NewMulticall3Executor(1, common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"), nil)

	batchedReader := NewBatchedReader(mock, collector, executor, cache, true)

	chainID := "1"
	batchGroup := "ethereum"
	queryID := "query456"
	sourceID := "ethereum"

	// Make multiple calls with different callKeys
	callKeys := []string{"total_assets", "total_supply", "conversion_rate"}
	addresses := []string{
		"0x1111111111111111111111111111111111111111",
		"0x2222222222222222222222222222222222222222",
		"0x3333333333333333333333333333333333333333",
	}

	expectedCallIDs := make([]string, len(callKeys))
	for i, callKey := range callKeys {
		expectedCallID := fmt.Sprintf("%s-%s-%s", queryID, sourceID, callKey)
		expectedCallIDs[i] = expectedCallID

		ctx := WithCallInfo(context.Background(), chainID, batchGroup, expectedCallID)
		_, _ = batchedReader.ReadContract(ctx, addresses[i], "test()", []string{})
	}

	// Verify all calls were added to collector
	batch, err := collector.GetBatch(chainID, batchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch) != len(callKeys) {
		t.Errorf("expected %d calls in batch, got %d", len(callKeys), len(batch))
	}

	// Verify all callIDs are present and in correct format
	callIDMap := make(map[string]bool)
	for _, call := range batch {
		callIDMap[call.CallID] = true
		// Verify format: {queryID}-{sourceID}-{callKey}
		parts := strings.Split(call.CallID, "-")
		if len(parts) != 3 {
			t.Errorf("expected callID format {queryID}-{sourceID}-{callKey}, got %q", call.CallID)
		} else {
			if parts[0] != queryID {
				t.Errorf("expected queryID %q in callID, got %q", queryID, parts[0])
			}
			if parts[1] != sourceID {
				t.Errorf("expected sourceID %q in callID, got %q", sourceID, parts[1])
			}
		}
	}

	for _, expectedCallID := range expectedCallIDs {
		if !callIDMap[expectedCallID] {
			t.Errorf("expected callID %q to be present in batch", expectedCallID)
		}
	}
}

// TestBatchedReader_FallbackWhenCallInfoMissing tests that when call info is not in context,
// the BatchedReader falls back to generating a callID and using default chainID/batchGroup
func TestBatchedReader_FallbackWhenCallInfoMissing(t *testing.T) {
	mock := &mockReader{
		readContractFunc: func(ctx context.Context, address, functionSig string, args []string) ([]byte, error) {
			return nil, errors.New("should not be called when batching is enabled")
		},
	}

	collector := NewContractBatchCollector()
	cache := NewBatchCache()
	executor := NewMulticall3Executor(1, common.HexToAddress("0xcA11bde05977b3631167028862bE2a173976CA11"), nil)

	batchedReader := NewBatchedReader(mock, collector, executor, cache, true)

	address := "0x1234567890123456789012345678901234567890"
	functionSig := "test()"
	args := []string{"arg1", "arg2"}

	// Call without call info in context - should fall back to defaults
	ctx := context.Background()
	_, err := batchedReader.ReadContract(ctx, address, functionSig, args)
	if err == nil {
		t.Error("expected error on cache miss, got nil")
	}
	if !errors.Is(err, ErrBatchExecutionRequired) {
		t.Errorf("expected ErrBatchExecutionRequired, got %v", err)
	}

	// Verify call was added to collector with default chainID and batchGroup
	// The callID should be generated from address, functionSig, and args
	defaultChainID := "1"
	defaultBatchGroup := "default"
	batch, err := collector.GetBatch(defaultChainID, defaultBatchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch) != 1 {
		t.Errorf("expected 1 call in batch, got %d", len(batch))
	}

	// Verify callID was generated (should be a hex string)
	callID := batch[0].CallID
	if callID == "" {
		t.Error("expected generated callID to be non-empty")
	}

	// Verify target address is correct
	expectedTarget := common.HexToAddress(address)
	if batch[0].Target != expectedTarget {
		t.Errorf("expected Target %v, got %v", expectedTarget, batch[0].Target)
	}
}
