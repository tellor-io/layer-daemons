package batch

import (
	"sync"
	"time"
)

// BatchCollector collects pending queries for batching.
// It is thread-safe and groups queries by batch group ID.
type BatchCollector struct {
	mu     sync.Mutex
	groups map[string]*BatchGroup
}

// NewBatchCollector creates a new BatchCollector.
func NewBatchCollector() *BatchCollector {
	return &BatchCollector{
		groups: make(map[string]*BatchGroup),
	}
}

// AddQuery adds a query to the appropriate batch group.
// It is thread-safe and will create the group if it doesn't exist.
func (c *BatchCollector) AddQuery(queryID, sourceID, groupID string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Get or create the batch group
	group, exists := c.groups[groupID]
	if !exists {
		group = &BatchGroup{
			GroupID:        groupID,
			SourceID:       sourceID,
			PendingQueries: make([]PendingQuery, 0),
			LastUpdate:     time.Now(),
		}
		c.groups[groupID] = group
	}

	// Add the query to the group
	query := PendingQuery{
		QueryID:   queryID,
		SourceID:  sourceID,
		Timestamp: time.Now(),
	}
	group.PendingQueries = append(group.PendingQueries, query)
	group.LastUpdate = time.Now()

	return nil
}

// GetGroup returns the batch group for the given groupID and clears its pending queries.
// If the group doesn't exist, it returns an empty group.
// It is thread-safe.
func (c *BatchCollector) GetGroup(groupID string) (*BatchGroup, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	group, exists := c.groups[groupID]
	if !exists {
		// Return an empty group
		return &BatchGroup{
			GroupID:        groupID,
			PendingQueries: make([]PendingQuery, 0),
		}, nil
	}

	// Create a copy of the group with pending queries
	result := &BatchGroup{
		GroupID:        group.GroupID,
		SourceID:       group.SourceID,
		PendingQueries: make([]PendingQuery, len(group.PendingQueries)),
		LastUpdate:     group.LastUpdate,
	}
	copy(result.PendingQueries, group.PendingQueries)

	// Clear the pending queries in the original group
	group.PendingQueries = make([]PendingQuery, 0)
	group.LastUpdate = time.Now()

	return result, nil
}

// GetAllGroups returns all batch groups with pending queries and clears them.
// It is thread-safe.
func (c *BatchCollector) GetAllGroups() map[string]*BatchGroup {
	c.mu.Lock()
	defer c.mu.Unlock()

	result := make(map[string]*BatchGroup)

	// Copy all groups with pending queries
	for groupID, group := range c.groups {
		if len(group.PendingQueries) > 0 {
			resultGroup := &BatchGroup{
				GroupID:        group.GroupID,
				SourceID:       group.SourceID,
				PendingQueries: make([]PendingQuery, len(group.PendingQueries)),
				LastUpdate:     group.LastUpdate,
			}
			copy(resultGroup.PendingQueries, group.PendingQueries)
			result[groupID] = resultGroup

			// Clear the pending queries in the original group
			group.PendingQueries = make([]PendingQuery, 0)
			group.LastUpdate = time.Now()
		}
	}

	return result
}
