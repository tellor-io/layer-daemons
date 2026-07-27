package types

import (
	"testing"

	"cosmossdk.io/math"
	"github.com/stretchr/testify/require"
	reportertypes "github.com/tellor-io/layer/x/reporter/types"
)

func TestGogoProtoCodecRoundTripsAvailableTips(t *testing.T) {
	codec := gogoProtoCodec{}
	original := &reportertypes.QueryAvailableTipsResponse{
		AvailableTips: math.LegacyMustNewDecFromStr("123.45"),
	}

	bz, err := codec.Marshal(original)
	require.NoError(t, err)

	var decoded reportertypes.QueryAvailableTipsResponse
	require.NoError(t, codec.Unmarshal(bz, &decoded))
	require.True(t, original.AvailableTips.Equal(decoded.AvailableTips))
}
