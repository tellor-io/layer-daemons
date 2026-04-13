package customquery

import (
	gometrics "github.com/hashicorp/go-metrics"
	"github.com/tellor-io/layer-daemons/lib/metrics"

	"github.com/cosmos/cosmos-sdk/telemetry"
)

func exchangeLabels(handler ExchangeHandler) []gometrics.Label {
	labels := []gometrics.Label{
		metrics.GetLabelForStringValue(metrics.ExchangeId, string(handler.ExchangeID)),
		metrics.GetLabelForStringValue(metrics.QueryId, handler.QueryID),
	}
	if handler.MarketId != "" {
		labels = append(labels, metrics.GetLabelForStringValue(metrics.MarketId, handler.MarketId))
	}
	return labels
}

func emitExchangeLiveFetchTelemetry(handler ExchangeHandler, err error) {
	callback := metrics.Success
	labels := exchangeLabels(handler)
	if err != nil {
		callback = metrics.Error
		labels = append(labels, metrics.GetLabelForStringValue(metrics.Reason, err.Error()))
	}
	telemetry.IncrCounterWithLabels(
		[]string{metrics.PricefeedDaemon, metrics.CustomQueryExchangeLiveFetch, callback},
		1.0,
		labels,
	)
}

func emitExchangeCacheReadTelemetry(handler ExchangeHandler, hit bool) {
	callback := metrics.Success
	if !hit {
		callback = metrics.Error
	}
	telemetry.IncrCounterWithLabels(
		[]string{metrics.PricefeedDaemon, metrics.CustomQueryExchangeCacheRead, callback},
		1.0,
		exchangeLabels(handler),
	)
}

func emitExchangeRefresherTelemetry(handler ExchangeHandler, err error) {
	callback := metrics.Success
	labels := exchangeLabels(handler)
	if err != nil {
		callback = metrics.Error
		labels = append(labels, metrics.GetLabelForStringValue(metrics.Reason, err.Error()))
	}
	telemetry.IncrCounterWithLabels(
		[]string{metrics.PricefeedDaemon, metrics.CustomQueryExchangeRefresher, callback},
		1.0,
		labels,
	)
}
