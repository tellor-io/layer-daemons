package curve

import (
	"github.com/tellor-io/layer-daemons/exchange_common"
	"github.com/tellor-io/layer-daemons/pricefeed/client/types"
)

var CurveDetails = types.ExchangeQueryDetails{
	Exchange:      exchange_common.EXCHANGE_ID_CURVE,
	Url:           "https://prices.curve.finance/v1/usd_price/ethereum/$",
	PriceFunction: CurvePriceFunction,
	IsMultiMarket: false,
}
