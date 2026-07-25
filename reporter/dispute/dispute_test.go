package dispute

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rs/zerolog"
	disputetypes "github.com/tellor-io/layer/x/dispute/types"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

var (
	queryInterval = 50 * time.Millisecond
	testCodec     = codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
)

func testLogger() log.Logger {
	return log.NewLogger(os.Stderr, log.LevelOption(zerolog.DebugLevel), log.ColorOption(false))
}

// mockOpenDisputesResponse marshals the actual layer response struct so the test exercises
// the real proto-JSON shape (and breaks if the upstream struct changes).
func mockOpenDisputesResponse(ids []uint64) []byte {
	resp := &disputetypes.QueryOpenDisputesResponse{
		OpenDisputes: &disputetypes.OpenDisputes{Ids: ids},
	}
	b, err := testCodec.MarshalJSON(resp)
	if err != nil {
		panic(err)
	}
	return b
}

func serveDisputes(ids []uint64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(mockOpenDisputesResponse(ids))
	}))
}

// runExpectPanic runs the monitor and returns the recovered panic value (or nil).
func runExpectPanic(t *testing.T, cfg Config) (panicked bool, msg string) {
	t.Helper()
	cfg.Enabled = true
	m := New(testLogger(), cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	panicCh := make(chan any, 1)
	doneCh := make(chan struct{})
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicCh <- r
			}
			close(doneCh)
		}()
		m.Run(ctx)
	}()

	select {
	case p := <-panicCh:
		s, _ := p.(string)
		return true, s
	case <-doneCh:
		return false, ""
	case <-time.After(10 * queryInterval):
		return false, "timeout"
	}
}

func TestPanicsOnOpenDisputes_MultiServer(t *testing.T) {
	open := serveDisputes([]uint64{1, 2, 3})
	defer open.Close()
	none := serveDisputes([]uint64{})
	defer none.Close()

	// One server open, one none → must panic.
	panicked, msg := runExpectPanic(t, Config{LayerAPIURLs: []string{open.URL, none.URL}, CheckInterval: queryInterval})
	if !panicked || !strings.Contains(msg, ReasonOpenDisputes) {
		t.Fatalf("expected panic with %q, got panicked=%v msg=%q", ReasonOpenDisputes, panicked, msg)
	}

	// Both servers open → must panic.
	open2 := serveDisputes([]uint64{4, 5})
	defer open2.Close()
	panicked, msg = runExpectPanic(t, Config{LayerAPIURLs: []string{open.URL, open2.URL}, CheckInterval: queryInterval})
	if !panicked || !strings.Contains(msg, "OPEN DISPUTES DETECTED") {
		t.Fatalf("expected panic (both open), got panicked=%v msg=%q", panicked, msg)
	}
}

func TestDoesNotPanicOnErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	if panicked, msg := runExpectPanic(t, Config{LayerAPIURLs: []string{srv.URL}, CheckInterval: queryInterval}); panicked {
		t.Fatalf("monitor panicked on API errors (should not): %q", msg)
	}
}

func TestDoesNotPanicWhenNoOpenDisputes(t *testing.T) {
	srv := serveDisputes([]uint64{})
	defer srv.Close()
	if panicked, msg := runExpectPanic(t, Config{LayerAPIURLs: []string{srv.URL}, CheckInterval: queryInterval}); panicked {
		t.Fatalf("monitor panicked with no open disputes (should not): %q", msg)
	}
}

func TestDoesNotPanicWhenDisputeIsIgnored(t *testing.T) {
	srv := serveDisputes([]uint64{42})
	defer srv.Close()
	if panicked, msg := runExpectPanic(t, Config{
		LayerAPIURLs:   []string{srv.URL},
		IgnoreDisputes: []uint64{42},
		CheckInterval:  queryInterval,
	}); panicked {
		t.Fatalf("monitor panicked on an ignored dispute (should not): %q", msg)
	}
}

func TestErrorsOnMalformedResponse(t *testing.T) {
	// A response missing the openDisputes structure must be treated as an error (no panic).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	defer srv.Close()
	if panicked, msg := runExpectPanic(t, Config{LayerAPIURLs: []string{srv.URL}, CheckInterval: queryInterval}); panicked {
		t.Fatalf("monitor panicked on a malformed response (should treat as error): %q", msg)
	}
}

// serveDisputesWithPower routes /open-disputes to the ID list and /dispute/dispute/{id} to a
// minimal per-dispute response carrying the given report power (for stake-filter tests).
func serveDisputesWithPower(ids []uint64, powers map[uint64]uint64) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/open-disputes"):
			_, _ = w.Write(mockOpenDisputesResponse(ids))
		case strings.Contains(r.URL.Path, "/dispute/dispute/"):
			parts := strings.Split(r.URL.Path, "/")
			id, _ := strconv.ParseUint(parts[len(parts)-1], 10, 64)
			_, _ = w.Write([]byte(fmt.Sprintf(
				`{"dispute":{"disputeId":"%d","metadata":{"open":true,"initialEvidence":{"power":"%d"}}}}`, id, powers[id])))
		default:
			http.NotFound(w, r)
		}
	}))
}

// TestTriggerThreshold: a single open dispute must not halt reporting when the threshold is 2,
// but two concurrent disputes must. This defeats a single adversary's self-dispute (issue #47).
func TestTriggerThreshold(t *testing.T) {
	one := serveDisputes([]uint64{1})
	defer one.Close()
	if panicked, msg := runExpectPanic(t, Config{LayerAPIURLs: []string{one.URL}, TriggerThreshold: 2, CheckInterval: queryInterval}); panicked {
		t.Fatalf("panicked with 1 dispute under threshold 2: %q", msg)
	}
	two := serveDisputes([]uint64{1, 2})
	defer two.Close()
	if panicked, _ := runExpectPanic(t, Config{LayerAPIURLs: []string{two.URL}, TriggerThreshold: 2, CheckInterval: queryInterval}); !panicked {
		t.Fatal("expected panic with 2 disputes at threshold 2")
	}
}

// TestStakeWeightedFiltering: a dispute against a below-threshold (griefing) report is
// auto-ignored, while one against a high-power report still halts reporting (issue #47).
func TestStakeWeightedFiltering(t *testing.T) {
	low := serveDisputesWithPower([]uint64{1}, map[uint64]uint64{1: 50})
	defer low.Close()
	if panicked, msg := runExpectPanic(t, Config{LayerAPIURLs: []string{low.URL}, MinReporterPower: 100, CheckInterval: queryInterval}); panicked {
		t.Fatalf("panicked on a low-power (griefing) dispute: %q", msg)
	}
	high := serveDisputesWithPower([]uint64{1}, map[uint64]uint64{1: 500})
	defer high.Close()
	if panicked, _ := runExpectPanic(t, Config{LayerAPIURLs: []string{high.URL}, MinReporterPower: 100, CheckInterval: queryInterval}); !panicked {
		t.Fatal("expected panic on a high-power dispute")
	}
}

// TestGracePeriodHaltsIfPersists: when the dispute is still open after the grace period, the
// failsafe still halts.
func TestGracePeriodHaltsIfPersists(t *testing.T) {
	srv := serveDisputes([]uint64{7})
	defer srv.Close()
	if panicked, _ := runExpectPanic(t, Config{LayerAPIURLs: []string{srv.URL}, GracePeriod: 80 * time.Millisecond, CheckInterval: queryInterval}); !panicked {
		t.Fatal("expected panic after grace period when the dispute persists")
	}
}

// TestGracePeriodResumes: a dispute that clears during the grace period lets reporting resume
// automatically, with no panic and no operator intervention (issue #47).
func TestGracePeriodResumes(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if !strings.HasSuffix(r.URL.Path, "/open-disputes") {
			http.NotFound(w, r)
			return
		}
		if calls.Add(1) == 1 { // open on the first poll, cleared thereafter
			_, _ = w.Write(mockOpenDisputesResponse([]uint64{7}))
			return
		}
		_, _ = w.Write(mockOpenDisputesResponse(nil))
	}))
	defer srv.Close()
	if panicked, msg := runExpectPanic(t, Config{LayerAPIURLs: []string{srv.URL}, GracePeriod: 80 * time.Millisecond, CheckInterval: queryInterval}); panicked {
		t.Fatalf("expected resume (no panic) when disputes clear during grace: %q", msg)
	}
}

func TestIsIgnored(t *testing.T) {
	if !isIgnored([]uint64{1, 2, 3}, 2) {
		t.Fatal("2 should be ignored")
	}
	if isIgnored([]uint64{1, 2, 3}, 9) {
		t.Fatal("9 should not be ignored")
	}
}

func TestParseDisputeID(t *testing.T) {
	id, err := ParseDisputeID("123")
	if err != nil || id != 123 {
		t.Fatalf("ParseDisputeID(123) = %d, %v", id, err)
	}
	if _, err := ParseDisputeID("nope"); err == nil {
		t.Fatal("expected error for non-numeric id")
	}
}

func TestLoadConfigFromEnv(t *testing.T) {
	t.Setenv("DISPUTE_MONITOR_ENABLED", "true")
	t.Setenv("API_URLS", "http://a:1317, http://b:1317")
	t.Setenv("DISPUTE_IGNORE_IDS", "5, 9")
	t.Setenv("DISPUTE_CHECK_INTERVAL", "3s")
	t.Setenv("DISPUTE_MIN_REPORTER_POWER", "1000")
	t.Setenv("DISPUTE_TRIGGER_THRESHOLD", "2")
	t.Setenv("DISPUTE_GRACE_PERIOD", "10m")
	cfg := LoadConfigFromEnv([]string{"tcp://rpc:26657"})
	if !cfg.Enabled {
		t.Fatal("expected enabled")
	}
	if len(cfg.LayerAPIURLs) != 2 || cfg.LayerAPIURLs[1] != "http://b:1317" {
		t.Fatalf("api urls: %v", cfg.LayerAPIURLs)
	}
	if len(cfg.IgnoreDisputes) != 2 || cfg.IgnoreDisputes[0] != 5 {
		t.Fatalf("ignore: %v", cfg.IgnoreDisputes)
	}
	if cfg.CheckInterval != 3*time.Second {
		t.Fatalf("interval: %v", cfg.CheckInterval)
	}
	if len(cfg.RPCEndpoints) != 1 {
		t.Fatalf("rpc: %v", cfg.RPCEndpoints)
	}
	if cfg.MinReporterPower != 1000 {
		t.Fatalf("min power: %d", cfg.MinReporterPower)
	}
	if cfg.TriggerThreshold != 2 {
		t.Fatalf("threshold: %d", cfg.TriggerThreshold)
	}
	if cfg.GracePeriod != 10*time.Minute {
		t.Fatalf("grace: %v", cfg.GracePeriod)
	}
}
