package reader

import (
	"sync"

	"github.com/ethereum/go-ethereum/common"
)

// ContractBatchCollector collects contract calls for batching.
// It is thread-safe and groups calls by chainID and batchGroup.
type ContractBatchCollector struct {
	mu      sync.Mutex
	batches map[string]map[string][]Call // keyed by chainID, then batchGroup
}

// NewContractBatchCollector creates a new ContractBatchCollector.
func NewContractBatchCollector() *ContractBatchCollector {
	return &ContractBatchCollector{
		batches: make(map[string]map[string][]Call),
	}
}

// AddCall adds a contract call to the appropriate batch.
// It is thread-safe and will create the chain and group if they don't exist.
func (c *ContractBatchCollector) AddCall(chainID, batchGroup, callID string, target common.Address, callData []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get or create the chain map
	chainBatches, exists := c.batches[chainID]
	if !exists {
		chainBatches = make(map[string][]Call)
		c.batches[chainID] = chainBatches
	}

	// Get or create the batch group
	batch, exists := chainBatches[batchGroup]
	if !exists {
		batch = make([]Call, 0)
	}

	// Make a copy of callData to avoid external modifications
	callDataCopy := make([]byte, len(callData))
	copy(callDataCopy, callData)

	// Create the call
	call := Call{
		Target:   target,
		CallData: callDataCopy,
		CallID:   callID,
	}

	// Add the call to the batch
	batch = append(batch, call)
	chainBatches[batchGroup] = batch

	return nil
}

// GetBatch returns the batch of calls for the given chainID and batchGroup, and clears it.
// If the batch doesn't exist, it returns an empty slice.
// It is thread-safe.
func (c *ContractBatchCollector) GetBatch(chainID, batchGroup string) ([]Call, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get the chain map
	chainBatches, exists := c.batches[chainID]
	if !exists {
		// Return empty slice
		return []Call{}, nil
	}

	// Get the batch
	batch, exists := chainBatches[batchGroup]
	if !exists {
		// Return empty slice
		return []Call{}, nil
	}

	// Create a copy of the batch
	result := make([]Call, len(batch))
	for i, call := range batch {
		// Copy CallData to avoid external modifications
		callDataCopy := make([]byte, len(call.CallData))
		copy(callDataCopy, call.CallData)
		result[i] = Call{
			Target:   call.Target,
			CallData: callDataCopy,
			CallID:   call.CallID,
		}
	}

	// Clear the batch
	chainBatches[batchGroup] = make([]Call, 0)

	return result, nil
}

// GetAllBatches returns all batches with pending calls and clears them.
// It is thread-safe.
func (c *ContractBatchCollector) GetAllBatches() map[string]map[string][]Call {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := make(map[string]map[string][]Call)

	// Copy all batches with pending calls
	for chainID, chainBatches := range c.batches {
		resultChain := make(map[string][]Call)
		hasPending := false

		for batchGroup, batch := range chainBatches {
			if len(batch) > 0 {
				hasPending = true
				// Create a copy of the batch
				batchCopy := make([]Call, len(batch))
				for i, call := range batch {
					// Copy CallData to avoid external modifications
					callDataCopy := make([]byte, len(call.CallData))
					copy(callDataCopy, call.CallData)
					batchCopy[i] = Call{
						Target:   call.Target,
						CallData: callDataCopy,
						CallID:   call.CallID,
					}
				}
				resultChain[batchGroup] = batchCopy

				// Clear the batch
				chainBatches[batchGroup] = make([]Call, 0)
			}
		}

		if hasPending {
			result[chainID] = resultChain
		}
	}

	return result
}
