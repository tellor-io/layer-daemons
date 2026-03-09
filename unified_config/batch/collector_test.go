package batch

import (
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestBatchCollector_NewBatchCollector(t *testing.T) {
	collector := NewBatchCollector()
	if collector == nil {
		t.Fatal("NewBatchCollector returned nil")
	}
	if collector.groups == nil {
		t.Error("groups map should be initialized")
	}
}

func TestBatchCollector_AddQuery(t *testing.T) {
	collector := NewBatchCollector()

	err := collector.AddQuery("query1", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	// Verify query was added
	group, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}

	if group == nil {
		t.Fatal("GetGroup returned nil")
	}

	if group.GroupID != "group1" {
		t.Errorf("expected GroupID %q, got %q", "group1", group.GroupID)
	}

	if group.SourceID != "source1" {
		t.Errorf("expected SourceID %q, got %q", "source1", group.SourceID)
	}

	if len(group.PendingQueries) != 1 {
		t.Fatalf("expected 1 pending query, got %d", len(group.PendingQueries))
	}

	if group.PendingQueries[0].QueryID != "query1" {
		t.Errorf("expected QueryID %q, got %q", "query1", group.PendingQueries[0].QueryID)
	}

	if group.PendingQueries[0].SourceID != "source1" {
		t.Errorf("expected SourceID %q, got %q", "source1", group.PendingQueries[0].SourceID)
	}
}

func TestBatchCollector_AddQueryMultipleQueries(t *testing.T) {
	collector := NewBatchCollector()

	// Add multiple queries to the same group
	err := collector.AddQuery("query1", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	err = collector.AddQuery("query2", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	err = collector.AddQuery("query3", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	group, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}

	if len(group.PendingQueries) != 3 {
		t.Fatalf("expected 3 pending queries, got %d", len(group.PendingQueries))
	}

	// Verify all queries are present
	queryIDs := make(map[string]bool)
	for _, q := range group.PendingQueries {
		queryIDs[q.QueryID] = true
	}

	expected := map[string]bool{"query1": true, "query2": true, "query3": true}
	for id := range expected {
		if !queryIDs[id] {
			t.Errorf("expected query %q to be present", id)
		}
	}
}

func TestBatchCollector_GetGroupClearsQueries(t *testing.T) {
	collector := NewBatchCollector()

	err := collector.AddQuery("query1", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	// Get group - should return queries and clear them
	group, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}

	if len(group.PendingQueries) != 1 {
		t.Errorf("expected 1 pending query, got %d", len(group.PendingQueries))
	}

	// Get group again - should return empty
	group2, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}

	if len(group2.PendingQueries) != 0 {
		t.Errorf("expected 0 pending queries after GetGroup, got %d", len(group2.PendingQueries))
	}
}

func TestBatchCollector_MultipleGroupsIndependent(t *testing.T) {
	collector := NewBatchCollector()

	// Add queries to different groups
	err := collector.AddQuery("query1", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	err = collector.AddQuery("query2", "source2", "group2")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	err = collector.AddQuery("query3", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	// Get group1
	group1, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}

	if len(group1.PendingQueries) != 2 {
		t.Errorf("expected 2 pending queries in group1, got %d", len(group1.PendingQueries))
	}

	// Get group2
	group2, err := collector.GetGroup("group2")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}

	if len(group2.PendingQueries) != 1 {
		t.Errorf("expected 1 pending query in group2, got %d", len(group2.PendingQueries))
	}

	if group2.PendingQueries[0].QueryID != "query2" {
		t.Errorf("expected QueryID %q, got %q", "query2", group2.PendingQueries[0].QueryID)
	}
}

func TestBatchCollector_GetGroupNonExistent(t *testing.T) {
	collector := NewBatchCollector()

	group, err := collector.GetGroup("nonexistent")
	if err != nil {
		t.Fatalf("GetGroup should not return error for non-existent group, got: %v", err)
	}

	if group == nil {
		t.Fatal("GetGroup should return a group even if it doesn't exist")
	}

	if len(group.PendingQueries) != 0 {
		t.Errorf("expected 0 pending queries for non-existent group, got %d", len(group.PendingQueries))
	}
}

func TestBatchCollector_GetAllGroups(t *testing.T) {
	collector := NewBatchCollector()

	// Add queries to multiple groups
	err := collector.AddQuery("query1", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	err = collector.AddQuery("query2", "source2", "group2")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	err = collector.AddQuery("query3", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}

	// Get all groups
	groups := collector.GetAllGroups()

	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}

	// Verify group1
	group1, exists := groups["group1"]
	if !exists {
		t.Fatal("group1 should exist")
	}
	if len(group1.PendingQueries) != 2 {
		t.Errorf("expected 2 pending queries in group1, got %d", len(group1.PendingQueries))
	}

	// Verify group2
	group2, exists := groups["group2"]
	if !exists {
		t.Fatal("group2 should exist")
	}
	if len(group2.PendingQueries) != 1 {
		t.Errorf("expected 1 pending query in group2, got %d", len(group2.PendingQueries))
	}

	// Get all groups again - should be empty (cleared)
	groups2 := collector.GetAllGroups()
	if len(groups2) != 0 {
		t.Errorf("expected 0 groups after GetAllGroups, got %d", len(groups2))
	}
}

func TestBatchCollector_GetAllGroupsEmpty(t *testing.T) {
	collector := NewBatchCollector()

	groups := collector.GetAllGroups()
	if len(groups) != 0 {
		t.Errorf("expected 0 groups, got %d", len(groups))
	}
}

func TestBatchCollector_ThreadSafety(t *testing.T) {
	collector := NewBatchCollector()

	const numGoroutines = 10
	const queriesPerGoroutine = 10

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrently add queries
	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < queriesPerGoroutine; j++ {
				groupID := "group1"
				queryID := fmt.Sprintf("query-%d-%d", id, j)
				sourceID := fmt.Sprintf("source-%d", id)
				err := collector.AddQuery(queryID, sourceID, groupID)
				if err != nil {
					t.Errorf("AddQuery failed: %v", err)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify all queries were added
	group, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}

	expectedCount := numGoroutines * queriesPerGoroutine
	if len(group.PendingQueries) != expectedCount {
		t.Errorf("expected %d pending queries, got %d", expectedCount, len(group.PendingQueries))
	}
}

func TestBatchCollector_QueryTimestamp(t *testing.T) {
	collector := NewBatchCollector()

	before := time.Now()
	err := collector.AddQuery("query1", "source1", "group1")
	if err != nil {
		t.Fatalf("AddQuery failed: %v", err)
	}
	after := time.Now()

	group, err := collector.GetGroup("group1")
	if err != nil {
		t.Fatalf("GetGroup failed: %v", err)
	}

	if len(group.PendingQueries) != 1 {
		t.Fatalf("expected 1 pending query, got %d", len(group.PendingQueries))
	}

	timestamp := group.PendingQueries[0].Timestamp
	if timestamp.Before(before) || timestamp.After(after) {
		t.Errorf("timestamp %v should be between %v and %v", timestamp, before, after)
	}
}
