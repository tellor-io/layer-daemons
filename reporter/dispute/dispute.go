// Package dispute implements the reporter's dispute failsafe: before the reporter starts
// (and continuously after), it refuses to report while there are open, non-ignored disputes
// on the network. It monitors via both the chain API and new_dispute events, and PANICS to
// exit the process once the failsafe fires — a robust way to stop reporting immediately.
// Opt-in via DISPUTE_MONITOR_ENABLED.
//
// To resist a low-cost griefing attack (stake 1 TRB, self-dispute, halt the whole reporter
// network), the trigger is tunable (issue #47), all defaults preserving the original strict
// behavior:
//   - DISPUTE_MIN_REPORTER_POWER auto-ignores disputes against tiny-stake reports.
//   - DISPUTE_TRIGGER_THRESHOLD requires N concurrent qualifying disputes before pausing.
//   - DISPUTE_GRACE_PERIOD waits and re-evaluates before halting, resuming automatically if
//     the disputes clear — so a transient or self-resolving dispute needs no operator action.
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

	// Grief-prevention knobs (issue #47). Defaults preserve the original strict behavior:
	// pause on the first open, non-ignored dispute, immediately.
	//
	//   - MinReporterPower: when > 0, disputes against a report whose power is below this are
	//     auto-ignored, so a cheap self-dispute from a tiny stake can't halt the network.
	//   - TriggerThreshold: number of qualifying open disputes required before pausing
	//     (default 1). Set to 2+ so a single adversary's self-dispute does not halt reporting.
	//   - GracePeriod: when > 0, on reaching the threshold the monitor waits this long and
	//     re-evaluates; if the disputes clear (resolved or now ignorable) it resumes reporting
	//     without operator intervention instead of halting.
	MinReporterPower uint64        // DISPUTE_MIN_REPORTER_POWER (0 = disabled)
	TriggerThreshold int           // DISPUTE_TRIGGER_THRESHOLD (default 1)
	GracePeriod      time.Duration // DISPUTE_GRACE_PERIOD (0 = pause immediately)
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
	if cfg.TriggerThreshold <= 0 {
		cfg.TriggerThreshold = 1
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

// checkDisputes evaluates the open disputes and triggers the failsafe once the number of
// qualifying disputes reaches TriggerThreshold. A qualifying dispute is open, not in the
// ignore list, and — when MinReporterPower is set — against a report whose power is at or
// above the threshold. Below-threshold counts keep reporting.
func (m *Monitor) checkDisputes(ctx context.Context) {
	qualifying := m.qualifyingDisputes(ctx)
	if len(qualifying) < m.cfg.TriggerThreshold {
		if len(qualifying) > 0 {
			m.logger.Warn("open disputes below trigger threshold - still reporting",
				"qualifying", qualifying, "threshold", m.cfg.TriggerThreshold)
		}
		return
	}
	m.trigger(ctx, qualifying)
}

// qualifyingDisputes returns the open dispute IDs that count toward the failsafe: not
// ignored, and — when MinReporterPower > 0 — disputing a report whose power is at or above
// the threshold (grief protection: cheap disputes from tiny stakes are auto-ignored). If a
// dispute's power cannot be read, it is counted, so an API hiccup fails safe toward pausing.
func (m *Monitor) qualifyingDisputes(ctx context.Context) []uint64 {
	var qualifying []uint64
	for _, id := range m.queryAllAPINodes(ctx, m.cfg.LayerAPIURLs) {
		if isIgnored(m.cfg.IgnoreDisputes, id) {
			m.logger.Warn("open dispute found but ignored", "dispute_id", id)
			continue
		}
		if m.cfg.MinReporterPower > 0 {
			power, err := m.queryDisputePower(ctx, id)
			switch {
			case err != nil:
				m.logger.Error("could not read disputed report power - counting dispute (fail safe)", "dispute_id", id, "error", err)
			case power < m.cfg.MinReporterPower:
				m.logger.Warn("dispute against low-power reporter auto-ignored", "dispute_id", id, "power", power, "min_power", m.cfg.MinReporterPower)
				continue
			}
		}
		qualifying = append(qualifying, id)
	}
	return qualifying
}

// trigger halts reporting when the failsafe fires. With a grace period it first waits and
// re-evaluates: if the disputes clear (resolved, or now below threshold) it resumes reporting
// without operator intervention; otherwise it panics to stop the reporter immediately.
func (m *Monitor) trigger(ctx context.Context, qualifying []uint64) {
	if m.cfg.GracePeriod > 0 {
		m.logger.Warn("qualifying disputes reached trigger threshold - waiting grace period before halting",
			"qualifying", qualifying, "threshold", m.cfg.TriggerThreshold, "grace_period", m.cfg.GracePeriod)
		if !sleepCtx(ctx, m.cfg.GracePeriod) {
			return
		}
		qualifying = m.qualifyingDisputes(ctx)
		if len(qualifying) < m.cfg.TriggerThreshold {
			m.logger.Info("disputes cleared during grace period - resuming reporting", "remaining", qualifying)
			return
		}
	}
	m.logger.Error("OPEN DISPUTES DETECTED - PANIC", "dispute_ids", qualifying, "threshold", m.cfg.TriggerThreshold, "ignored_ids", m.cfg.IgnoreDisputes)
	panic(fmt.Sprintf("%s: dispute_ids=%v", ReasonOpenDisputes, qualifying))
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

// queryDisputePower returns the power of the report a dispute is challenging, from the first
// API node that answers. Used for stake-weighted grief filtering.
func (m *Monitor) queryDisputePower(ctx context.Context, id uint64) (uint64, error) {
	lastErr := fmt.Errorf("no API URLs configured")
	for _, baseURL := range m.cfg.LayerAPIURLs {
		power, err := m.queryDisputePowerFromAPI(ctx, baseURL, id)
		if err == nil {
			return power, nil
		}
		lastErr = err
	}
	return 0, lastErr
}

func (m *Monitor) queryDisputePowerFromAPI(ctx context.Context, baseURL string, id uint64) (uint64, error) {
	url := fmt.Sprintf("%s/tellor-io/layer/dispute/dispute/%d", strings.TrimRight(baseURL, "/"), id)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, err
	}
	resp, err := m.httpClient.Do(req)
	if err != nil {
		return 0, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var parsed disputetypes.QueryDisputeResponse
	if err := m.cdc.UnmarshalJSON(body, &parsed); err != nil {
		return 0, fmt.Errorf("decode dispute response: %w", err)
	}
	if parsed.Dispute == nil || parsed.Dispute.Metadata == nil {
		return 0, fmt.Errorf("unexpected dispute response: missing dispute/metadata")
	}
	return parsed.Dispute.Metadata.InitialEvidence.Power, nil
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
