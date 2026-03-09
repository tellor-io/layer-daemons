package reader

import (
	"fmt"
	"sync"
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func TestContractBatchCollector_NewContractBatchCollector(t *testing.T) {
	collector := NewContractBatchCollector()
	if collector == nil {
		t.Fatal("NewContractBatchCollector returned nil")
	}
	if collector.batches == nil {
		t.Error("batches map should be initialized")
	}
}

func TestContractBatchCollector_AddCall(t *testing.T) {
	collector := NewContractBatchCollector()

	chainID := "1"
	batchGroup := "group1"
	callID := "call1"
	target := common.HexToAddress("0x1234567890123456789012345678901234567890")
	callData := []byte{0x01, 0x02, 0x03}

	err := collector.AddCall(chainID, batchGroup, callID, target, callData)
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	// Verify call was added
	batch, err := collector.GetBatch(chainID, batchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch) != 1 {
		t.Fatalf("expected 1 call in batch, got %d", len(batch))
	}

	if batch[0].CallID != callID {
		t.Errorf("expected CallID %q, got %q", callID, batch[0].CallID)
	}

	if batch[0].Target != target {
		t.Errorf("expected Target %v, got %v", target, batch[0].Target)
	}

	if len(batch[0].CallData) != len(callData) {
		t.Errorf("expected CallData length %d, got %d", len(callData), len(batch[0].CallData))
	}

	for i := range callData {
		if batch[0].CallData[i] != callData[i] {
			t.Errorf("expected CallData[%d] = %d, got %d", i, callData[i], batch[0].CallData[i])
		}
	}
}

func TestContractBatchCollector_AddCallMultipleCalls(t *testing.T) {
	collector := NewContractBatchCollector()

	chainID := "1"
	batchGroup := "group1"

	calls := []struct {
		callID   string
		target   common.Address
		callData []byte
	}{
		{"call1", common.HexToAddress("0x1111111111111111111111111111111111111111"), []byte{0x01}},
		{"call2", common.HexToAddress("0x2222222222222222222222222222222222222222"), []byte{0x02}},
		{"call3", common.HexToAddress("0x3333333333333333333333333333333333333333"), []byte{0x03}},
	}

	for _, call := range calls {
		err := collector.AddCall(chainID, batchGroup, call.callID, call.target, call.callData)
		if err != nil {
			t.Fatalf("AddCall failed for %s: %v", call.callID, err)
		}
	}

	batch, err := collector.GetBatch(chainID, batchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch) != 3 {
		t.Fatalf("expected 3 calls in batch, got %d", len(batch))
	}

	// Verify all calls are present
	callIDs := make(map[string]bool)
	for _, call := range batch {
		callIDs[call.CallID] = true
	}

	expected := map[string]bool{"call1": true, "call2": true, "call3": true}
	for id := range expected {
		if !callIDs[id] {
			t.Errorf("expected call %q to be present", id)
		}
	}
}

func TestContractBatchCollector_GetBatchClearsCalls(t *testing.T) {
	collector := NewContractBatchCollector()

	chainID := "1"
	batchGroup := "group1"
	callID := "call1"
	target := common.HexToAddress("0x1111111111111111111111111111111111111111")
	callData := []byte{0x01}

	err := collector.AddCall(chainID, batchGroup, callID, target, callData)
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	// Get batch - should return calls and clear them
	batch, err := collector.GetBatch(chainID, batchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch) != 1 {
		t.Errorf("expected 1 call in batch, got %d", len(batch))
	}

	// Get batch again - should return empty
	batch2, err := collector.GetBatch(chainID, batchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch2) != 0 {
		t.Errorf("expected 0 calls after GetBatch, got %d", len(batch2))
	}
}

func TestContractBatchCollector_MultipleChainsGroupsIndependent(t *testing.T) {
	collector := NewContractBatchCollector()

	// Add calls to different chains and groups
	err := collector.AddCall("1", "group1", "call1", common.HexToAddress("0x1111"), []byte{0x01})
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	err = collector.AddCall("2", "group1", "call2", common.HexToAddress("0x2222"), []byte{0x02})
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	err = collector.AddCall("1", "group2", "call3", common.HexToAddress("0x3333"), []byte{0x03})
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	err = collector.AddCall("1", "group1", "call4", common.HexToAddress("0x4444"), []byte{0x04})
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	// Get chain1/group1
	batch1, err := collector.GetBatch("1", "group1")
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch1) != 2 {
		t.Errorf("expected 2 calls in chain1/group1, got %d", len(batch1))
	}

	// Get chain2/group1
	batch2, err := collector.GetBatch("2", "group1")
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch2) != 1 {
		t.Errorf("expected 1 call in chain2/group1, got %d", len(batch2))
	}

	if batch2[0].CallID != "call2" {
		t.Errorf("expected CallID %q, got %q", "call2", batch2[0].CallID)
	}

	// Get chain1/group2
	batch3, err := collector.GetBatch("1", "group2")
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch3) != 1 {
		t.Errorf("expected 1 call in chain1/group2, got %d", len(batch3))
	}

	if batch3[0].CallID != "call3" {
		t.Errorf("expected CallID %q, got %q", "call3", batch3[0].CallID)
	}
}

func TestContractBatchCollector_GetBatchNonExistent(t *testing.T) {
	collector := NewContractBatchCollector()

	batch, err := collector.GetBatch("1", "nonexistent")
	if err != nil {
		t.Fatalf("GetBatch should not return error for non-existent batch, got: %v", err)
	}

	if batch == nil {
		t.Fatal("GetBatch should return a slice even if batch doesn't exist")
	}

	if len(batch) != 0 {
		t.Errorf("expected 0 calls for non-existent batch, got %d", len(batch))
	}
}

func TestContractBatchCollector_GetAllBatches(t *testing.T) {
	collector := NewContractBatchCollector()

	// Add calls to multiple chains and groups
	err := collector.AddCall("1", "group1", "call1", common.HexToAddress("0x1111"), []byte{0x01})
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	err = collector.AddCall("2", "group1", "call2", common.HexToAddress("0x2222"), []byte{0x02})
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	err = collector.AddCall("1", "group2", "call3", common.HexToAddress("0x3333"), []byte{0x03})
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	err = collector.AddCall("1", "group1", "call4", common.HexToAddress("0x4444"), []byte{0x04})
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	// Get all batches
	allBatches := collector.GetAllBatches()

	// Should have 2 chains
	if len(allBatches) != 2 {
		t.Fatalf("expected 2 chains, got %d", len(allBatches))
	}

	// Verify chain1
	chain1, exists := allBatches["1"]
	if !exists {
		t.Fatal("chain1 should exist")
	}
	if len(chain1) != 2 {
		t.Fatalf("expected 2 groups in chain1, got %d", len(chain1))
	}

	// Verify chain1/group1
	group1, exists := chain1["group1"]
	if !exists {
		t.Fatal("chain1/group1 should exist")
	}
	if len(group1) != 2 {
		t.Errorf("expected 2 calls in chain1/group1, got %d", len(group1))
	}

	// Verify chain1/group2
	group2, exists := chain1["group2"]
	if !exists {
		t.Fatal("chain1/group2 should exist")
	}
	if len(group2) != 1 {
		t.Errorf("expected 1 call in chain1/group2, got %d", len(group2))
	}

	// Verify chain2
	chain2, exists := allBatches["2"]
	if !exists {
		t.Fatal("chain2 should exist")
	}
	if len(chain2) != 1 {
		t.Fatalf("expected 1 group in chain2, got %d", len(chain2))
	}

	// Verify chain2/group1
	chain2Group1, exists := chain2["group1"]
	if !exists {
		t.Fatal("chain2/group1 should exist")
	}
	if len(chain2Group1) != 1 {
		t.Errorf("expected 1 call in chain2/group1, got %d", len(chain2Group1))
	}

	// Get all batches again - should be empty (cleared)
	allBatches2 := collector.GetAllBatches()
	if len(allBatches2) != 0 {
		t.Errorf("expected 0 chains after GetAllBatches, got %d", len(allBatches2))
	}
}

func TestContractBatchCollector_GetAllBatchesEmpty(t *testing.T) {
	collector := NewContractBatchCollector()

	allBatches := collector.GetAllBatches()
	if len(allBatches) != 0 {
		t.Errorf("expected 0 chains, got %d", len(allBatches))
	}
}

func TestContractBatchCollector_ThreadSafety(t *testing.T) {
	collector := NewContractBatchCollector()

	const numGoroutines = 10
	const callsPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrently add calls
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < callsPerGoroutine; j++ {
				chainID := "1"
				batchGroup := "group1"
				callID := fmt.Sprintf("call-%d-%d", id, j)
				target := common.HexToAddress(fmt.Sprintf("0x%040d", id*100+j))
				callData := []byte{byte(id), byte(j)}
				err := collector.AddCall(chainID, batchGroup, callID, target, callData)
				if err != nil {
					t.Errorf("AddCall failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all calls were added
	batch, err := collector.GetBatch("1", "group1")
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	expectedCount := numGoroutines * callsPerGoroutine
	if len(batch) != expectedCount {
		t.Errorf("expected %d calls, got %d", expectedCount, len(batch))
	}
}

func TestContractBatchCollector_CallDataIsCopied(t *testing.T) {
	collector := NewContractBatchCollector()

	chainID := "1"
	batchGroup := "group1"
	callID := "call1"
	target := common.HexToAddress("0x1111111111111111111111111111111111111111")
	callData := []byte{0x01, 0x02, 0x03}

	err := collector.AddCall(chainID, batchGroup, callID, target, callData)
	if err != nil {
		t.Fatalf("AddCall failed: %v", err)
	}

	// Modify original callData
	callData[0] = 0xFF

	// Get batch and verify original data is preserved
	batch, err := collector.GetBatch(chainID, batchGroup)
	if err != nil {
		t.Fatalf("GetBatch failed: %v", err)
	}

	if len(batch) != 1 {
		t.Fatalf("expected 1 call, got %d", len(batch))
	}

	if batch[0].CallData[0] == 0xFF {
		t.Error("CallData should be copied, modification to original should not affect stored data")
	}

	if batch[0].CallData[0] != 0x01 {
		t.Errorf("expected CallData[0] = 0x01, got 0x%02x", batch[0].CallData[0])
	}
}
