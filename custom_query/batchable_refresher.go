package customquery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"time"

	"cosmossdk.io/log"
	"github.com/tellor-io/layer-daemons/constants"
	servertypes "github.com/tellor-io/layer-daemons/server/types/daemons"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

type batchableTarget struct {
	marketID     uint32
	responsePath []string
	params       map[string]string
}

type batchableEndpointPlan struct {
	endpointID  string
	urlTemplate string
	query       string
	method      string
	timeoutMs   int
	headers     map[string]string
	targets     []batchableTarget
}

func StartBatchableRefresher(
	ctx context.Context,
	logger log.Logger,
	homeDir, localDir, file string,
	queries map[string]QueryConfig,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
	interval time.Duration,
) error {
	if interval <= 0 {
		return fmt.Errorf("batchable refresher interval must be > 0")
	}

	plans, err := BuildBatchableRefreshPlans(homeDir, localDir, file, queries)
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		logger.Info("No batchable cache-backed endpoints configured; refresher not started")
		return nil
	}

	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()

		run := func() {
			runCtx, cancel := context.WithTimeout(ctx, interval)
			defer cancel()
			RefreshBatchableEndpointsOnce(runCtx, logger, plans, priceCache)
		}

		run()
		for {
			select {
			case <-ctx.Done():
				logger.Info("Batchable refresher stopped", "reason", ctx.Err())
				return
			case <-ticker.C:
				run()
			}
		}
	}()

	logger.Info(
		"Started batchable HTTP refresher",
		"interval_ms",
		interval.Milliseconds(),
		"endpoint_count",
		len(plans),
	)
	return nil
}

func BuildBatchableRefreshPlans(
	homeDir, localDir, file string,
	queries map[string]QueryConfig,
) ([]batchableEndpointPlan, error) {
	config, err := readAndParseConfig(homeDir, localDir, file)
	if err != nil {
		return nil, err
	}
	processApiKeys(&config)

	plansByEndpoint := make(map[string]*batchableEndpointPlan)
	for _, query := range queries {
		marketID, err := ResolveMarketIdForQuery(query.ID)
		if err != nil {
			return nil, err
		}

		for _, endpoint := range query.Endpoints {
			template, exists := config.Endpoints[endpoint.EndpointType]
			if !exists {
				continue
			}
			if !template.Batchable || !endpoint.UseCache {
				continue
			}
			if strings.EqualFold(endpoint.EndpointType, "contract") || strings.EqualFold(endpoint.EndpointType, "combined") {
				continue
			}

			plan := plansByEndpoint[endpoint.EndpointType]
			if plan == nil {
				headers := make(map[string]string, len(template.Headers))
				for key, value := range template.Headers {
					if strings.EqualFold(value, "api_key") {
						value = template.ApiKey
					}
					headers[key] = value
				}
				plan = &batchableEndpointPlan{
					endpointID:  endpoint.EndpointType,
					urlTemplate: template.URLTemplate,
					query:       template.Query,
					method:      template.Method,
					timeoutMs:   template.Timeout,
					headers:     headers,
					targets:     []batchableTarget{},
				}
				plansByEndpoint[endpoint.EndpointType] = plan
			}

			plan.targets = append(plan.targets, batchableTarget{
				marketID:     marketID,
				responsePath: endpoint.ResponsePath,
				params:       endpoint.Params,
			})
		}
	}

	plans := make([]batchableEndpointPlan, 0, len(plansByEndpoint))
	for _, plan := range plansByEndpoint {
		plans = append(plans, *plan)
	}
	return plans, nil
}

func RefreshBatchableEndpointsOnce(
	ctx context.Context,
	logger log.Logger,
	plans []batchableEndpointPlan,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
) {
	for _, plan := range plans {
		if err := refreshBatchableEndpoint(ctx, plan, priceCache); err != nil {
			logger.Error("Batchable refresh failed", "endpoint", plan.endpointID, "error", err)
		}
	}
}

func refreshBatchableEndpoint(
	ctx context.Context,
	plan batchableEndpointPlan,
	priceCache *pricefeedservertypes.MarketToExchangePrices,
) error {
	method := strings.ToUpper(plan.method)
	if method != http.MethodGet && method != http.MethodPost {
		return fmt.Errorf("unsupported method %s for endpoint %s", method, plan.endpointID)
	}

	url, queryBody, err := renderBatchRequest(plan)
	if err != nil {
		return err
	}

	timeout := 5 * time.Second
	if plan.timeoutMs > 0 {
		timeout = time.Duration(plan.timeoutMs) * time.Millisecond
	}
	reqCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var bodyReader io.Reader
	if method == http.MethodPost && queryBody != "" {
		bodyReader = strings.NewReader(queryBody)
	}
	req, err := http.NewRequestWithContext(reqCtx, method, url, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to build request: %w", err)
	}
	for key, value := range plan.headers {
		req.Header.Set(key, value)
	}

	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("non-OK response code %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed reading response body: %w", err)
	}

	var payload any
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("failed decoding JSON response: %w", err)
	}

	now := time.Now()
	updatesByMarket := make(map[uint32]*servertypes.MarketPriceUpdate)
	for _, target := range plan.targets {
		valueAny, err := extractPath(payload, target.responsePath)
		if err != nil {
			continue
		}
		valueFloat, err := toFloat64(valueAny)
		if err != nil {
			continue
		}
		marketParam, ok := constants.StaticMarketParamsConfig[target.marketID]
		if !ok {
			continue
		}
		rawPrice := scaleToRawPrice(valueFloat, marketParam.Exponent)
		update := updatesByMarket[target.marketID]
		if update == nil {
			update = &servertypes.MarketPriceUpdate{
				MarketId:       target.marketID,
				ExchangePrices: []*servertypes.ExchangePrice{},
			}
			updatesByMarket[target.marketID] = update
		}
		update.ExchangePrices = append(update.ExchangePrices, &servertypes.ExchangePrice{
			ExchangeId:     plan.endpointID,
			Price:          rawPrice,
			LastUpdateTime: &now,
		})
	}

	updates := make([]*servertypes.MarketPriceUpdate, 0, len(updatesByMarket))
	for _, update := range updatesByMarket {
		updates = append(updates, update)
	}
	if len(updates) > 0 {
		priceCache.UpdatePrices(updates)
	}

	return nil
}

func renderBatchRequest(plan batchableEndpointPlan) (url string, query string, err error) {
	placeholderRegex := regexp.MustCompile(`\{([^{}]+)\}`)
	placeholders := make([]string, 0)
	seen := make(map[string]struct{})
	for _, match := range placeholderRegex.FindAllStringSubmatch(plan.urlTemplate, -1) {
		if len(match) < 2 {
			continue
		}
		if _, ok := seen[match[1]]; !ok {
			placeholders = append(placeholders, match[1])
			seen[match[1]] = struct{}{}
		}
	}
	for _, match := range placeholderRegex.FindAllStringSubmatch(plan.query, -1) {
		if len(match) < 2 {
			continue
		}
		if _, ok := seen[match[1]]; !ok {
			placeholders = append(placeholders, match[1])
			seen[match[1]] = struct{}{}
		}
	}

	resolved := make(map[string]string)
	for _, p := range placeholders {
		if p == "api_key" {
			continue
		}
		values := make([]string, 0, len(plan.targets))
		for _, target := range plan.targets {
			v, ok := target.params[p]
			if !ok {
				return "", "", fmt.Errorf("missing parameter %s for batchable endpoint %s", p, plan.endpointID)
			}
			values = append(values, v)
		}
		uniqueValues := uniqueStrings(values)
		if len(uniqueValues) == 0 {
			return "", "", fmt.Errorf("no values for parameter %s on endpoint %s", p, plan.endpointID)
		}
		if p == "coin_id" {
			resolved[p] = strings.Join(uniqueValues, ",")
			continue
		}
		if len(uniqueValues) != 1 {
			return "", "", fmt.Errorf(
				"unsupported varying parameter %s for endpoint %s in batch mode", p, plan.endpointID,
			)
		}
		resolved[p] = uniqueValues[0]
	}

	url = plan.urlTemplate
	query = plan.query
	for key, value := range resolved {
		placeholder := fmt.Sprintf("{%s}", key)
		url = strings.ReplaceAll(url, placeholder, value)
		query = strings.ReplaceAll(query, placeholder, value)
	}

	return url, query, nil
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	out := make([]string, 0, len(values))
	for _, v := range values {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

func extractPath(current any, path []string) (any, error) {
	for i, key := range path {
		switch v := current.(type) {
		case map[string]any:
			next, ok := v[key]
			if !ok {
				return nil, fmt.Errorf("key not found at path segment %d: %s", i, key)
			}
			current = next
		case []any:
			var idx int
			if _, err := fmt.Sscanf(key, "%d", &idx); err != nil {
				return nil, fmt.Errorf("expected array index, got %s", key)
			}
			if idx < 0 || idx >= len(v) {
				return nil, fmt.Errorf("array index out of bounds: %d", idx)
			}
			current = v[idx]
		default:
			return nil, fmt.Errorf("unexpected type %T at path segment %d", current, i)
		}
	}
	return current, nil
}

func toFloat64(value any) (float64, error) {
	switch v := value.(type) {
	case float64:
		return v, nil
	case float32:
		return float64(v), nil
	case int:
		return float64(v), nil
	case int64:
		return float64(v), nil
	case uint64:
		return float64(v), nil
	case string:
		var out float64
		if _, err := fmt.Sscanf(v, "%f", &out); err != nil {
			return 0, fmt.Errorf("failed parsing string %q as float", v)
		}
		return out, nil
	default:
		return 0, fmt.Errorf("unsupported value type: %T", value)
	}
}

func scaleToRawPrice(value float64, exponent int32) uint64 {
	scale := math.Pow10(int(-exponent))
	if scale == 0 {
		return 0
	}
	scaled := value * scale
	if scaled <= 0 {
		return 0
	}
	return uint64(math.Round(scaled))
}
