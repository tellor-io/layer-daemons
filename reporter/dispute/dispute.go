// Package dispute implements the reporter's dispute failsafe: before the reporter starts
// (and continuously after), it refuses to report while there is an open, non-ignored
// dispute on the network. It monitors via both the chain API and new_dispute events, and
// on any non-ignored open dispute it PANICS to exit the process — a robust failsafe that
// stops reporting immediately. Opt-in via DISPUTE_MONITOR_ENABLED.
//
// Ported from the monitor's dispute package; the only design change is dropping the DB
// failsafe in favor of new_dispute event subscription alongside the API check.
package dispute

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	rpchttp "github.com/cometbft/cometbft/rpc/client/http"
	disputetypes "github.com/tellor-io/layer/x/dispute/types"
	"golang.org/x/sync/errgroup"

	"cosmossdk.io/log"

	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
)

const (
	Component          = "dispute_monitor"
	ReasonOpenDisputes = "OPEN DISPUTES DETECTED - not safe to continue reporting. Add dispute IDs to DISPUTE_IGNORE_IDS if safe to ignore"
	// disputeEventQuery matches any tx emitting a new_dispute event.
	disputeEventQuery    = "tm.event='Tx' AND new_dispute.dispute_id EXISTS"
	defaultCheckInterval = 30 * time.Second
)

type Config struct {
	Enabled        bool          // opt-in (DISPUTE_MONITOR_ENABLED)
	LayerAPIURLs   []string      // Layer REST API URLs for querying open disputes
	RPCEndpoints   []string      // CometBFT RPC endpoints for new_dispute event subscription
	IgnoreDisputes []uint64      // Dispute IDs that are safe to ignore
	CheckInterval  time.Duration // How often the API poll re-checks (default 1s)
}

type Monitor struct {
	cfg        Config
	logger     log.Logger
	httpClient *http.Client
	cdc        codec.JSONCodec
}

func New(logger log.Logger, cfg Config) *Monitor {
	if cfg.CheckInterval <= 0 {
		cfg.CheckInterval = defaultCheckInterval
	}
	return &Monitor{
		cfg:        cfg,
		logger:     logger.With("component", Component),
		httpClient: &http.Client{Timeout: 10 * time.Second},
		cdc:        codec.NewProtoCodec(codectypes.NewInterfaceRegistry()),
	}
}

// CheckBeforeStart runs one synchronous dispute check before the reporter starts any other
// component. If there is an open, non-ignored dispute it panics — so the reporter never
// starts reporting while a dispute is open. The caller only constructs the monitor when it
// is enabled.
func (m *Monitor) CheckBeforeStart(ctx context.Context) {
	if len(m.cfg.LayerAPIURLs) == 0 {
		m.logger.Error("dispute monitor enabled but no API_URLS configured - the failsafe cannot query disputes")
	}
	m.logger.Info("dispute monitor: checking for open disputes before starting any component",
		"api_urls", m.cfg.LayerAPIURLs, "ignore_disputes", m.cfg.IgnoreDisputes, "check_interval", m.cfg.CheckInterval)
	m.checkDisputes(ctx)
}

// Run continuously monitors for open disputes using both new_dispute events and an API
// poll, panicking on any non-ignored open dispute. No-op when disabled.
func (m *Monitor) Run(ctx context.Context) {
	go m.subscribeEvents(ctx)

	// Immediate check, then poll. (Matches the original monitor's behavior.)
	m.checkDisputes(ctx)

	ticker := time.NewTicker(m.cfg.CheckInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			m.logger.Info("dispute monitor stopped")
			return
		case <-ticker.C:
			m.checkDisputes(ctx)
		}
	}
}

// checkDisputes queries all API nodes and panics if any non-ignored dispute is open.
func (m *Monitor) checkDisputes(ctx context.Context) {
	for _, disputeID := range m.queryAllAPINodes(ctx, m.cfg.LayerAPIURLs) {
		if !isIgnored(m.cfg.IgnoreDisputes, disputeID) {
			m.logger.Error("OPEN DISPUTE DETECTED - PANIC", "dispute_id", disputeID, "ignored_ids", m.cfg.IgnoreDisputes)
			panic(fmt.Sprintf("%s: dispute_id=%d", ReasonOpenDisputes, disputeID))
		}
		m.logger.Warn("open dispute found but ignored", "dispute_id", disputeID)
	}
}

// subscribeEvents keeps a best-effort new_dispute subscription; on any event it re-checks
// (which panics if the dispute is not ignored). Falls back to the API poll if unavailable.
func (m *Monitor) subscribeEvents(ctx context.Context) {
	if len(m.cfg.RPCEndpoints) == 0 {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		client, err := rpchttp.New(m.cfg.RPCEndpoints[0], "/websocket")
		if err == nil && !client.IsRunning() {
			err = client.Start()
		}
		if err != nil {
			m.logger.Warn("dispute monitor: event subscription unavailable, relying on API poll", "error", err)
			if !sleepCtx(ctx, m.cfg.CheckInterval) {
				return
			}
			continue
		}
		sub := fmt.Sprintf("reporter-dispute-monitor-%d", time.Now().UnixNano())
		evCh, err := client.Subscribe(ctx, sub, disputeEventQuery)
		if err != nil {
			m.logger.Warn("dispute monitor: subscribe failed, relying on API poll", "error", err)
			_ = client.Stop()
			if !sleepCtx(ctx, m.cfg.CheckInterval) {
				return
			}
			continue
		}
		for open := true; open; {
			select {
			case <-ctx.Done():
				_ = client.Unsubscribe(ctx, sub, disputeEventQuery)
				_ = client.Stop()
				return
			case _, ok := <-evCh:
				if !ok {
					open = false // channel closed; re-subscribe
					break
				}
				m.logger.Warn("dispute monitor: new_dispute event received - checking")
				m.checkDisputes(ctx) // panics if not ignored
			}
		}
		_ = client.Unsubscribe(ctx, sub, disputeEventQuery)
		_ = client.Stop()
	}
}

// queryAllAPINodes queries every API URL in parallel and returns the union of open IDs.
func (m *Monitor) queryAllAPINodes(ctx context.Context, apiURLs []string) []uint64 {
	if len(apiURLs) == 0 {
		return nil
	}
	type result struct {
		ids []uint64
		err error
	}
	resultsCh := make(chan result, len(apiURLs))

	g, gCtx := errgroup.WithContext(ctx)
	for _, apiURL := range apiURLs {
		url := apiURL
		g.Go(func() error {
			ids, err := m.queryDisputesFromAPI(gCtx, url)
			select {
			case resultsCh <- result{ids: ids, err: err}:
			case <-gCtx.Done():
			}
			return nil
		})
	}
	_ = g.Wait()
	close(resultsCh)

	allIDs := make(map[uint64]struct{})
	errorCount := 0
	for res := range resultsCh {
		if res.err != nil {
			m.logger.Debug("dispute API query failed", "error", res.err)
			errorCount++
			continue
		}
		for _, id := range res.ids {
			allIDs[id] = struct{}{}
		}
	}
	if errorCount == len(apiURLs) && errorCount > 0 {
		m.logger.Error("all dispute API nodes failed to respond")
	}

	ids := make([]uint64, 0, len(allIDs))
	for id := range allIDs {
		ids = append(ids, id)
	}
	return ids
}

func (m *Monitor) queryDisputesFromAPI(ctx context.Context, baseURL string) ([]uint64, error) {
	url := fmt.Sprintf("%s/tellor-io/layer/dispute/open-disputes", strings.TrimRight(baseURL, "/"))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Decode into the actual layer struct via the proto-JSON codec. This is strict
	// (gogoproto jsonpb rejects unknown fields), so any change to the chain's response
	// shape surfaces as an error here instead of being silently dropped.
	var parsed disputetypes.QueryOpenDisputesResponse
	if err := m.cdc.UnmarshalJSON(body, &parsed); err != nil {
		return nil, fmt.Errorf("decode open-disputes response: %w", err)
	}
	if parsed.OpenDisputes == nil {
		return nil, fmt.Errorf("unexpected open-disputes response: missing openDisputes field")
	}
	return parsed.OpenDisputes.Ids, nil
}

func isIgnored(ignoreList []uint64, disputeID uint64) bool {
	for _, ignoreID := range ignoreList {
		if ignoreID == disputeID {
			return true
		}
	}
	return false
}

// ParseDisputeID converts a decimal string to uint64.
func ParseDisputeID(val string) (uint64, error) {
	return strconv.ParseUint(val, 10, 64)
}

// sleepCtx sleeps for d or until ctx is done; returns false if ctx was canceled.
func sleepCtx(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
