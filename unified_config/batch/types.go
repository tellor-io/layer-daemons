package batch

import "time"

// PendingQuery represents a query that is waiting to be batched.
type PendingQuery struct {
	// QueryID is the identifier for the query
	QueryID string

	// SourceID is the identifier for the source
	SourceID string

	// Timestamp is when the query was requested
	Timestamp time.Time
}

// BatchGroup represents a group of pending queries that will be batched together.
type BatchGroup struct {
	// GroupID is the unique identifier for this batch group
	GroupID string

	// SourceID is the identifier for the source this group belongs to
	SourceID string

	// PendingQueries is the list of queries waiting to be batched
	PendingQueries []PendingQuery

	// LastUpdate is when this group was last updated
	LastUpdate time.Time
}

// BatchResult represents the result of a batched query operation.
type BatchResult struct {
	// QueryID is the identifier for the query
	QueryID string

	// SourceID is the identifier for the source
	SourceID string

	// Value is the result value (can be float64 for prices or []byte for contract calls)
	Value interface{}

	// Error is any error that occurred during the batch operation
	Error error
}
