package rpc_reader

import (
	"context"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/tellor-io/layer-daemons/custom_query/contracts/metrics"
	"golang.org/x/sync/singleflight"
)

// The reporter polls the cyclelist roughly every 200ms, so a single custom
// query can re-hit the same upstream JSON API (CoinGecko, CoinMarketCap, the
// Uniswap/The Graph subgraph, osmosis, etc.) several times per second. With
// free-tier API keys that quickly trips rate limits (HTTP 429) and produces the
// "report generation failed" bursts.
//
// This cache sits in front of rpc_reader.FetchJSON and serves an identical
// request from memory for up to CUSTOM_QUERY_CACHE_TTL, collapsing those
// redundant calls into one upstream fetch per interval. Concurrent identical
// requests are coalesced via singleflight so a cache miss can never cause a
// stampede.
//
// Tuning (env vars, read once at startup):
//   - CUSTOM_QUERY_CACHE_TTL : cache freshness window, a Go duration (e.g. "3s",
//     "500ms"). Set to "0" to disable the cache entirely (pure pass-through to
//     the previous behavior). Defaults to defaultCacheTTL.
//
// Correctness note: the TTL bounds how stale a reported price can be, so it must
// stay well under the chain's value-freshness expectations. The default is
// deliberately small.
const (
	cacheTTLEnv     = "CUSTOM_QUERY_CACHE_TTL"
	defaultCacheTTL = 3 * time.Second
)

type cacheEntry struct {
	body      []byte
	fetchedAt time.Time
}

type responseCache struct {
	ttl   time.Duration
	mu    sync.RWMutex
	items map[string]cacheEntry
	group singleflight.Group
}

// sharedCache is process-wide: readers are rebuilt per query config, so the
// cache must outlive any single Reader instance to be effective across cycles.
var sharedCache = newResponseCache(cacheTTLFromEnv())

func cacheTTLFromEnv() time.Duration {
	v := strings.TrimSpace(os.Getenv(cacheTTLEnv))
	if v == "" {
		return defaultCacheTTL
	}
	d, err := time.ParseDuration(v)
	if err != nil || d < 0 {
		return defaultCacheTTL
	}
	return d // 0 => disabled
}

func newResponseCache(ttl time.Duration) *responseCache {
	return &responseCache{ttl: ttl, items: make(map[string]cacheEntry)}
}

func (c *responseCache) enabled() bool { return c.ttl > 0 }

// get returns a cached body if present and still within the TTL.
func (c *responseCache) get(key string) ([]byte, bool) {
	c.mu.RLock()
	e, ok := c.items[key]
	c.mu.RUnlock()
	if !ok || time.Since(e.fetchedAt) > c.ttl {
		return nil, false
	}
	return e.body, true
}

func (c *responseCache) set(key string, body []byte) {
	c.mu.Lock()
	c.items[key] = cacheEntry{body: body, fetchedAt: time.Now()}
	c.mu.Unlock()
}

// cacheKey uniquely identifies an HTTP request. It must capture everything that
// can change the response: method, URL (which carries API-key query params),
// POST body (GraphQL queries), and headers (which may carry API keys).
func (r *Reader) cacheKey() string {
	var b strings.Builder
	b.WriteString(r.client.method)
	b.WriteByte('\n')
	b.WriteString(r.client.baseURL)
	b.WriteByte('\n')
	b.WriteString(r.Query)
	b.WriteByte('\n')

	keys := make([]string, 0, len(r.Headers))
	for k := range r.Headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(r.Headers[k])
		b.WriteByte('\n')
	}
	return b.String()
}

// FetchJSON returns the JSON response for the reader's request, served from the
// in-memory cache when a fresh copy exists. When caching is disabled it falls
// straight through to the underlying retrying fetch.
//
// The returned []byte may be shared with other callers; treat it as read-only
// (all current callers only json.Unmarshal it, which does not mutate).
func (r *Reader) FetchJSON(ctx context.Context) ([]byte, error) {
	if !sharedCache.enabled() {
		return r.fetchWithRetry(ctx)
	}

	key := r.cacheKey()
	if body, ok := sharedCache.get(key); ok {
		metrics.RPCCacheHits.Inc()
		return body, nil
	}
	metrics.RPCCacheMisses.Inc()

	v, err, _ := sharedCache.group.Do(key, func() (interface{}, error) {
		// Re-check: a concurrent leader may have just populated the cache.
		if body, ok := sharedCache.get(key); ok {
			return body, nil
		}
		body, err := r.fetchWithRetry(ctx)
		if err != nil {
			return nil, err
		}
		sharedCache.set(key, body)
		return body, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}
