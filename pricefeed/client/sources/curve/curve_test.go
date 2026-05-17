package curve_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tellor-io/layer-daemons/lib"
	"github.com/tellor-io/layer-daemons/pricefeed/client/sources/curve"
	"github.com/tellor-io/layer-daemons/pricefeed/client/sources/testutil"
	"github.com/tellor-io/layer-daemons/testutil/daemons/pricefeed"
)

const susdsTicker = "0xa3931d71877c0e7a3148cb7eb4463524fec27fbd"

func TestCurvePriceFunction(t *testing.T) {
	tests := map[string]struct {
		responseJsonString     string
		expectedPriceMap       map[string]uint64
		expectedUnavailableMap map[string]error
		expectedError          string
	}{
		"success": {
			responseJsonString: `{"data":{"address":"0xa3931d71877c0e7a3148cb7eb4463524fec27fbd","usd_price":1.25}}`,
			expectedPriceMap: map[string]uint64{
				susdsTicker: 1_250_000,
			},
		},
		"address mismatch": {
			responseJsonString: `{"data":{"address":"0x0000000000000000000000000000000000000000","usd_price":1.25}}`,
			expectedPriceMap:   map[string]uint64{},
			expectedUnavailableMap: map[string]error{
				susdsTicker: errors.New("response address 0x0000000000000000000000000000000000000000 did not match requested ticker"),
			},
		},
		"missing data": {
			responseJsonString: `{"data":null}`,
			expectedPriceMap:   map[string]uint64{},
			expectedUnavailableMap: map[string]error{
				susdsTicker: errors.New("missing data"),
			},
		},
		"invalid json": {
			responseJsonString: `{"data":`,
			expectedError:      "failed to decode Curve price response: unexpected EOF",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			response := testutil.CreateResponseFromJson(tc.responseJsonString)
			prices, unavailable, err := curve.CurvePriceFunction(
				response,
				map[string]int32{susdsTicker: -6},
				lib.Median[uint64],
			)

			if tc.expectedError != "" {
				require.EqualError(t, err, tc.expectedError)
				require.Nil(t, prices)
				require.Nil(t, unavailable)
				return
			}

			require.NoError(t, err)
			require.Equal(t, tc.expectedPriceMap, prices)
			pricefeed.ErrorMapsEqual(t, tc.expectedUnavailableMap, unavailable)
		})
	}
}
