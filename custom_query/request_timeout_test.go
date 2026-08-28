package customquery

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestCustomQueryFetchTimeout(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected time.Duration
	}{
		{
			name:     "unset uses default",
			expected: defaultCustomQueryFetchTimeout,
		},
		{
			name:     "valid duration",
			value:    "2s",
			expected: 2 * time.Second,
		},
		{
			name:     "surrounding whitespace",
			value:    " 1500ms ",
			expected: 1500 * time.Millisecond,
		},
		{
			name:     "invalid duration uses default",
			value:    "soon",
			expected: defaultCustomQueryFetchTimeout,
		},
		{
			name:     "zero duration uses default",
			value:    "0s",
			expected: defaultCustomQueryFetchTimeout,
		},
		{
			name:     "negative duration uses default",
			value:    "-1s",
			expected: defaultCustomQueryFetchTimeout,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv(customQueryFetchTimeoutEnv, test.value)
			require.Equal(t, test.expected, customQueryFetchTimeout())
		})
	}
}
