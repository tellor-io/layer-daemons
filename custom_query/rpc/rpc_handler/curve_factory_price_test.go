package rpchandler

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/tellor-io/layer-daemons/custom_query/contracts/contract_handlers"
	reader "github.com/tellor-io/layer-daemons/custom_query/rpc/rpc_reader"
	pricefeedservertypes "github.com/tellor-io/layer-daemons/server/types/pricefeed"
)

func susdeCurveFactoryParams() map[string]string {
	return map[string]string{
		"target_token":        strings.ToLower(contract_handlers.SUSDE_CONTRACT),
		"exclude_pools":       "0x4b5e827f4c0a1042272a11857a355da1f4ceebae",
		"merge_get_pools_url": "https://api.curve.finance/api/getPools/ethereum/main",
	}
}

func TestCurveFactoryPriceHandler_medianAndExclusions(t *testing.T) {
	oldGet := curveFactoryHTTPGet
	curveFactoryHTTPGet = func(ctx context.Context, url string) ([]byte, error) {
		return nil, fmt.Errorf("skip network in unit test")
	}
	t.Cleanup(func() { curveFactoryHTTPGet = oldGet })

	const fixture = `{
  "success": true,
  "data": {
    "poolData": [
      {
        "address": "0x1111111111111111111111111111111111111111",
        "coins": [
          {"address": "0x9D39A5DE30e57443BfF2A8307A4256c8797A3497", "usdPrice": 1.20, "symbol": "sUSDe"},
          {"address": "0xdAC17F958D2ee523a2206206994597C13D831ec7", "usdPrice": 1.0, "symbol": "USDT"}
        ]
      },
      {
        "address": "0x2222222222222222222222222222222222222222",
        "coins": [
          {"address": "0x9D39A5DE30e57443BfF2A8307A4256c8797A3497", "usdPrice": 1.40, "symbol": "sUSDe"},
          {"address": "0xA0b86991c6218b36c1d19D4a2e9Eb0cE3606eB48", "usdPrice": 1.0, "symbol": "USDC"}
        ]
      },
      {
        "address": "0x4b5E827F4C0a1042272a11857a355dA1F4Ceebae",
        "coins": [
          {"address": "0x9D39A5DE30e57443BfF2A8307A4256c8797A3497", "usdPrice": 0.57, "symbol": "sUSDe"},
          {"address": "0x0000000000000000000000000000000000000001", "usdPrice": 0.57, "symbol": "sUSD"}
        ]
      }
    ]
  }
}`

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixture))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodGet, "", nil, nil, 5000, susdeCurveFactoryParams(), 1)
	require.NoError(t, err)

	var h CurveFactoryPriceHandler
	v, err := h.FetchValue(context.Background(), rdr, false, 0, &pricefeedservertypes.MarketToExchangePrices{}, 0)
	require.NoError(t, err)
	require.InDelta(t, 1.30, v, 1e-9)
}

func TestCurveFactoryPriceHandler_v1Fallback(t *testing.T) {
	oldGet := curveFactoryHTTPGet
	curveFactoryHTTPGet = func(ctx context.Context, url string) ([]byte, error) {
		if strings.Contains(url, "getPools") || strings.Contains(url, "api.curve.fi") {
			return nil, fmt.Errorf("skip")
		}
		if strings.Contains(url, "usd_price") {
			addr := strings.ToLower(contract_handlers.SUSDE_CONTRACT)
			return []byte(fmt.Sprintf(`{"data":{"usd_price":1.25,"address":"%s"}}`, addr)), nil
		}
		return nil, fmt.Errorf("unexpected url %s", url)
	}
	t.Cleanup(func() { curveFactoryHTTPGet = oldGet })

	const emptyPools = `{"success":true,"data":{"poolData":[]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(emptyPools))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodGet, "", nil, nil, 5000, susdeCurveFactoryParams(), 1)
	require.NoError(t, err)

	var h CurveFactoryPriceHandler
	v, err := h.FetchValue(context.Background(), rdr, false, 0, &pricefeedservertypes.MarketToExchangePrices{}, 0)
	require.NoError(t, err)
	require.InDelta(t, 1.25, v, 1e-9)
}

func TestCurveFactoryPriceHandler_errors(t *testing.T) {
	oldGet := curveFactoryHTTPGet
	curveFactoryHTTPGet = func(ctx context.Context, url string) ([]byte, error) {
		return nil, fmt.Errorf("skip")
	}
	t.Cleanup(func() { curveFactoryHTTPGet = oldGet })

	t.Run("success_false", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"success":false,"data":{"poolData":[]}}`))
		}))
		t.Cleanup(srv.Close)
		rdr, err := reader.NewReader(srv.URL, http.MethodGet, "", nil, nil, 5000, susdeCurveFactoryParams(), 1)
		require.NoError(t, err)
		var h CurveFactoryPriceHandler
		_, err = h.FetchValue(context.Background(), rdr, false, 0, &pricefeedservertypes.MarketToExchangePrices{}, 0)
		require.Error(t, err)
	})

	t.Run("no_samples_and_v1_fails", func(t *testing.T) {
		curveFactoryHTTPGet = func(ctx context.Context, url string) ([]byte, error) {
			return nil, fmt.Errorf("network down")
		}
		t.Cleanup(func() { curveFactoryHTTPGet = oldGet })
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`{"success":true,"data":{"poolData":[]}}`))
		}))
		t.Cleanup(srv.Close)
		rdr, err := reader.NewReader(srv.URL, http.MethodGet, "", nil, nil, 5000, susdeCurveFactoryParams(), 1)
		require.NoError(t, err)
		var h CurveFactoryPriceHandler
		_, err = h.FetchValue(context.Background(), rdr, false, 0, &pricefeedservertypes.MarketToExchangePrices{}, 0)
		require.Error(t, err)
	})
}

func TestCurveFactoryPriceHandler_missingTargetToken(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"success":true,"data":{"poolData":[]}}`))
	}))
	t.Cleanup(srv.Close)
	rdr, err := reader.NewReader(srv.URL, http.MethodGet, "", nil, nil, 5000, map[string]string{}, 1)
	require.NoError(t, err)
	var h CurveFactoryPriceHandler
	_, err = h.FetchValue(context.Background(), rdr, false, 0, &pricefeedservertypes.MarketToExchangePrices{}, 0)
	require.Error(t, err)
}

func TestCurveFactoryPriceHandler_dataAgeTooOld(t *testing.T) {
	oldGet := curveFactoryHTTPGet
	curveFactoryHTTPGet = func(ctx context.Context, url string) ([]byte, error) {
		return nil, fmt.Errorf("skip network in unit test")
	}
	t.Cleanup(func() { curveFactoryHTTPGet = oldGet })

	const fixture = `{"success":true,"data":{"poolData":[{"address":"0x1111","coins":[{"address":"0x9d39a5de30e57443bff2a8307a4256c8797a3497","usdPrice":1.20,"symbol":"sUSDe"},{"address":"0xdac17f958d2ee523a2206206994597c13d831ec7","usdPrice":1.0,"symbol":"USDT"}]}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixture))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodGet, "", nil, nil, 5000, susdeCurveFactoryParams(), 1)
	require.NoError(t, err)

	var h CurveFactoryPriceHandler
	// maxDataAge = 1ns forces fetch-time check to fail immediately
	_, err = h.FetchValue(context.Background(), rdr, false, 0, &pricefeedservertypes.MarketToExchangePrices{}, 1*time.Nanosecond)
	require.Error(t, err)
	require.Contains(t, err.Error(), "data age")
}

func TestCurveFactoryPriceHandler_dataAgeDisabled(t *testing.T) {
	oldGet := curveFactoryHTTPGet
	curveFactoryHTTPGet = func(ctx context.Context, url string) ([]byte, error) {
		return nil, fmt.Errorf("skip network in unit test")
	}
	t.Cleanup(func() { curveFactoryHTTPGet = oldGet })

	const fixture = `{"success":true,"data":{"poolData":[{"address":"0x1111","coins":[{"address":"0x9d39a5de30e57443bff2a8307a4256c8797a3497","usdPrice":1.20,"symbol":"sUSDe"},{"address":"0xdac17f958d2ee523a2206206994597c13d831ec7","usdPrice":1.0,"symbol":"USDT"}]}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(fixture))
	}))
	t.Cleanup(srv.Close)

	rdr, err := reader.NewReader(srv.URL, http.MethodGet, "", nil, nil, 5000, susdeCurveFactoryParams(), 1)
	require.NoError(t, err)

	var h CurveFactoryPriceHandler
	// maxDataAge = 0 disables the check
	v, err := h.FetchValue(context.Background(), rdr, false, 0, &pricefeedservertypes.MarketToExchangePrices{}, 0)
	require.NoError(t, err)
	require.InDelta(t, 1.20, v, 1e-9)
}
