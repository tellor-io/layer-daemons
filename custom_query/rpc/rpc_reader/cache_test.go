package rpc_reader

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// withSharedCache swaps the process-wide cache for the duration of a test.
func withSharedCache(t *testing.T, ttl time.Duration) {
	t.Helper()
	prev := sharedCache
	sharedCache = newResponseCache(ttl)
	t.Cleanup(func() { sharedCache = prev })
}

// countingServer returns an httptest server that records how many times it was hit.
func countingServer(t *testing.T, body string) (*httptest.Server, *int32) {
	t.Helper()
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// newTestReader builds a reader with a generous timeout (NewReader treats the
// timeout arg as seconds for the client and ms for the per-attempt context).
func newTestReader(t *testing.T, url string) *Reader {
	t.Helper()
	r, err := NewReader(url, http.MethodGet, "", nil, nil, 5000, nil)
	if err != nil {
		t.Fatalf("NewReader: %v", err)
	}
	return r
}

func TestFetchJSON_ServesFromCacheWithinTTL(t *testing.T) {
	withSharedCache(t, time.Minute)
	srv, hits := countingServer(t, `{"price":1}`)
	r := newTestReader(t, srv.URL)

	for i := 0; i < 5; i++ {
		body, err := r.FetchJSON(context.Background())
		if err != nil {
			t.Fatalf("FetchJSON: %v", err)
		}
		if string(body) != `{"price":1}` {
			t.Fatalf("unexpected body: %s", body)
		}
	}
	if got := atomic.LoadInt32(hits); got != 1 {
		t.Fatalf("expected 1 upstream hit, got %d", got)
	}
}

func TestFetchJSON_RefetchesAfterTTL(t *testing.T) {
	withSharedCache(t, 20*time.Millisecond)
	srv, hits := countingServer(t, `{"price":1}`)
	r := newTestReader(t, srv.URL)

	if _, err := r.FetchJSON(context.Background()); err != nil {
		t.Fatalf("FetchJSON: %v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := r.FetchJSON(context.Background()); err != nil {
		t.Fatalf("FetchJSON: %v", err)
	}
	if got := atomic.LoadInt32(hits); got != 2 {
		t.Fatalf("expected 2 upstream hits after TTL expiry, got %d", got)
	}
}

func TestFetchJSON_DisabledPassesThrough(t *testing.T) {
	withSharedCache(t, 0)
	srv, hits := countingServer(t, `{"price":1}`)
	r := newTestReader(t, srv.URL)

	for i := 0; i < 3; i++ {
		if _, err := r.FetchJSON(context.Background()); err != nil {
			t.Fatalf("FetchJSON: %v", err)
		}
	}
	if got := atomic.LoadInt32(hits); got != 3 {
		t.Fatalf("expected 3 upstream hits when cache disabled, got %d", got)
	}
}

func TestFetchJSON_CoalescesConcurrentRequests(t *testing.T) {
	withSharedCache(t, time.Minute)
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		time.Sleep(80 * time.Millisecond) // hold the connection so callers overlap
		_, _ = w.Write([]byte(`{"price":1}`))
	}))
	t.Cleanup(srv.Close)
	r := newTestReader(t, srv.URL)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := r.FetchJSON(context.Background()); err != nil {
				t.Errorf("FetchJSON: %v", err)
			}
		}()
	}
	wg.Wait()
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("expected concurrent requests to coalesce into 1 upstream hit, got %d", got)
	}
}

func TestCacheKey_DistinctByURLQueryAndHeaders(t *testing.T) {
	mk := func(url, query string, headers map[string]string) string {
		r, err := NewReader(url, http.MethodGet, query, headers, nil, 5000, nil)
		if err != nil {
			t.Fatalf("NewReader: %v", err)
		}
		return r.cacheKey()
	}
	base := mk("http://x/a", "", nil)
	cases := map[string]string{
		"different url":    mk("http://x/b", "", nil),
		"different query":  mk("http://x/a", "{q}", nil),
		"different header": mk("http://x/a", "", map[string]string{"k": "v"}),
	}
	for name, key := range cases {
		if key == base {
			t.Errorf("cacheKey should differ for %s but matched base", name)
		}
	}
	// Same inputs must produce the same key.
	if mk("http://x/a", "", nil) != base {
		t.Error("cacheKey not stable for identical inputs")
	}
}
