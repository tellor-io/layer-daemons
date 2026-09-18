package customquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	rpcreader "github.com/tellor-io/layer-daemons/custom_query/rpc/rpc_reader"
	"github.com/stretchr/testify/require"
)

func makeGenericRPCReaders(t *testing.T, count int, handler http.HandlerFunc) []RpcHandler {
	t.Helper()

	readers := make([]RpcHandler, count)
	for i := range readers {
		srv := httptest.NewServer(handler)
		t.Cleanup(srv.Close)

		reader, err := rpcreader.NewReader(
			srv.URL,
			http.MethodGet,
			"",
			nil,
			[]string{"price"},
			200,
			nil,
			0,
		)
		require.NoError(t, err)

		readers[i] = RpcHandler{
			Handler:    "generic",
			Reader:     reader,
			EndpointID: "source",
			MarketId:   "TEST-USD",
			SourceId:   "source",
		}
	}
	return readers
}

func TestFetchPriceEarlyFinish(t *testing.T) {
	query := QueryConfig{
		ID:                 "test-query",
		AggregationMethod:  "median",
		MinResponses:       2,
		ResponseType:       "ufixed256x18",
		MaxSpreadPercent:   100,
		FetchTimeoutMs:     500,
		PerSourceTimeoutMs: 200,
		RpcReaders:         makeGenericRPCReaders(t, 5, okPriceHandler),
	}

	start := time.Now()
	result, err := FetchPrice(context.Background(), query, nil)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotEmpty(t, result.EncodedValue)
	require.Less(t, elapsed, 300*time.Millisecond)
}

func TestFetchPriceWaitsForSlowSource(t *testing.T) {
	var calls atomic.Int32
	handler := func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 3 {
			time.Sleep(150 * time.Millisecond)
		}
		okPriceHandler(w, nil)
	}

	query := QueryConfig{
		ID:                 "test-query",
		AggregationMethod:  "median",
		MinResponses:       2,
		ResponseType:       "ufixed256x18",
		MaxSpreadPercent:   100,
		FetchTimeoutMs:     800,
		PerSourceTimeoutMs: 300,
		RpcReaders:         makeGenericRPCReaders(t, 3, handler),
	}

	start := time.Now()
	result, err := FetchPrice(context.Background(), query, nil)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotEmpty(t, result.EncodedValue)
	require.GreaterOrEqual(t, elapsed, 100*time.Millisecond)
	require.Len(t, result.RawResults, 3)
}

func TestFetchPriceHungSourceCutOff(t *testing.T) {
	readers := make([]RpcHandler, 3)
	for i := range readers {
		idx := i
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if idx == 2 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(10 * time.Second):
				}
			}
			okPriceHandler(w, r)
		}))
		t.Cleanup(srv.Close)

		reader, err := rpcreader.NewReader(srv.URL, http.MethodGet, "", nil, []string{"price"}, 200, nil, 0)
		require.NoError(t, err)
		readers[i] = RpcHandler{
			Handler: "generic", Reader: reader, EndpointID: "source", MarketId: "TEST-USD", SourceId: "source",
		}
	}

	query := QueryConfig{
		ID:                 "test-query",
		AggregationMethod:  "median",
		MinResponses:       2,
		ResponseType:       "ufixed256x18",
		MaxSpreadPercent:   100,
		FetchTimeoutMs:     500,
		PerSourceTimeoutMs: 200,
		RpcReaders:         readers,
	}

	start := time.Now()
	result, err := FetchPrice(context.Background(), query, nil)
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotEmpty(t, result.EncodedValue)
	require.Less(t, elapsed, fetchTimeout(query)+300*time.Millisecond)
}

func TestFetchPriceDeadlineWithoutMin(t *testing.T) {
	readers := make([]RpcHandler, 3)
	for i := range readers {
		idx := i
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if idx > 0 {
				select {
				case <-r.Context().Done():
					return
				case <-time.After(10 * time.Second):
				}
			}
			okPriceHandler(w, r)
		}))
		t.Cleanup(srv.Close)

		reader, err := rpcreader.NewReader(srv.URL, http.MethodGet, "", nil, []string{"price"}, 200, nil, 0)
		require.NoError(t, err)
		readers[i] = RpcHandler{
			Handler: "generic", Reader: reader, EndpointID: "source", MarketId: "TEST-USD", SourceId: "source",
		}
	}

	query := QueryConfig{
		ID:                 "test-query",
		AggregationMethod:  "median",
		MinResponses:       2,
		ResponseType:       "ufixed256x18",
		MaxSpreadPercent:   100,
		FetchTimeoutMs:     300,
		PerSourceTimeoutMs: 200,
		RpcReaders:         readers,
	}

	result, err := FetchPrice(context.Background(), query, nil)
	require.Error(t, err)
	require.NotNil(t, result)
	require.GreaterOrEqual(t, len(result.RawResults), 1)
}

func TestFetchPriceRespectsContextDeadline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		case <-time.After(10 * time.Second):
		}
		okPriceHandler(w, r)
	}))
	t.Cleanup(srv.Close)

	reader, err := rpcreader.NewReader(srv.URL, http.MethodGet, "", nil, []string{"price"}, 5000, nil, 0)
	require.NoError(t, err)

	query := QueryConfig{
		ID:                "test-query",
		AggregationMethod: "median",
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		MaxSpreadPercent:  100,
		FetchTimeoutMs:    5000,
		RpcReaders: []RpcHandler{{
			Handler: "generic", Reader: reader, EndpointID: "slow", MarketId: "TEST-USD", SourceId: "slow",
		}},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = FetchPrice(ctx, query, nil)
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Less(t, elapsed, 500*time.Millisecond)
}

func TestFetchPriceAllSourcesFail(t *testing.T) {
	query := QueryConfig{
		ID:                 "test-query",
		AggregationMethod:  "median",
		MinResponses:       1,
		ResponseType:       "ufixed256x18",
		MaxSpreadPercent:   100,
		FetchTimeoutMs:     500,
		PerSourceTimeoutMs: 200,
		RpcReaders: makeGenericRPCReaders(t, 2, func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}),
	}

	result, err := FetchPrice(context.Background(), query, nil)
	require.Error(t, err)
	require.NotNil(t, result)
	require.Len(t, result.RawResults, 2)
}

func TestCollectionDeadlineUsesContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()

	deadline := collectionDeadline(ctx, QueryConfig{FetchTimeoutMs: 5000})
	require.True(t, deadline.Before(time.Now().Add(500 * time.Millisecond)))
}

func okPriceHandler(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte(`{"price": 100}`))
}
