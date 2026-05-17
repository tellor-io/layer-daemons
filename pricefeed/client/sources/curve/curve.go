package curve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	price_function "github.com/tellor-io/layer-daemons/pricefeed/client/sources"
	"github.com/tellor-io/layer-daemons/pricefeed/types"
)

type usdPriceResponse struct {
	Data *struct {
		Address  string  `json:"address"`
		USDPrice float64 `json:"usd_price"`
	} `json:"data"`
}

type ticker struct {
	pair  string
	price string
}

var _ price_function.Ticker = (*ticker)(nil)

func (t ticker) GetPair() string {
	return t.pair
}

func (t ticker) GetAskPrice() string {
	return t.price
}

func (t ticker) GetBidPrice() string {
	return t.price
}

func (t ticker) GetLastPrice() string {
	return t.price
}

func CurvePriceFunction(
	response *http.Response,
	tickerToExponent map[string]int32,
	resolver types.Resolver,
) (tickerToPrice map[string]uint64, unavailableTickers map[string]error, err error) {
	var parsed usdPriceResponse
	if err := json.NewDecoder(response.Body).Decode(&parsed); err != nil {
		return nil, nil, fmt.Errorf("failed to decode Curve price response: %w", err)
	}

	unavailableTickers = make(map[string]error)
	if parsed.Data == nil {
		for ticker := range tickerToExponent {
			unavailableTickers[ticker] = fmt.Errorf("missing data")
		}
		return map[string]uint64{}, unavailableTickers, nil
	}

	address := strings.ToLower(strings.TrimSpace(parsed.Data.Address))
	if parsed.Data.USDPrice <= 0 {
		for ticker := range tickerToExponent {
			unavailableTickers[ticker] = fmt.Errorf("invalid USD price for %s", ticker)
		}
		return map[string]uint64{}, unavailableTickers, nil
	}

	tickers := make([]ticker, 0, 1)
	for requestedTicker := range tickerToExponent {
		if strings.ToLower(strings.TrimSpace(requestedTicker)) != address {
			unavailableTickers[requestedTicker] = fmt.Errorf("response address %s did not match requested ticker", parsed.Data.Address)
			continue
		}
		tickers = append(tickers, ticker{
			pair:  requestedTicker,
			price: price_function.ConvertFloat64ToString(parsed.Data.USDPrice),
		})
	}

	tickerToPrice, unavailableFromHelper, err := price_function.GetMedianPricesFromTickers(
		tickers,
		tickerToExponent,
		resolver,
	)
	if err != nil {
		return nil, nil, err
	}

	for ticker, err := range unavailableFromHelper {
		if _, alreadySet := unavailableTickers[ticker]; !alreadySet {
			unavailableTickers[ticker] = err
		}
	}

	return tickerToPrice, unavailableTickers, nil
}
