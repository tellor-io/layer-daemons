package customquery_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	customquery "github.com/tellor-io/layer-daemons/custom_query"
)

func TestClassifyQueryFeed(t *testing.T) {
	t.Parallel()

	tests := map[string]struct {
		feedType   string
		target     string
		collateral string
	}{
		"05cddb6b67074aa61fcbe1d2fd5924e028bb699b506267df28c88f7deac4edc6": {
			feedType: customquery.FeedTypeMarket,
			target:   "SDAI",
		},
		"59ae85cec665c779f18255dd4f3d97821e6a122691ee070b9a26888bc2a0e45a": {
			feedType: customquery.FeedTypeMarket,
			target:   "SUSDS",
		},
		"03731257e35c49e44b267640126358e5decebdd8f18b5e8f229542ec86e318cf": {
			feedType:   customquery.FeedTypeFundamental,
			target:     "SUSDE",
			collateral: "USDE",
		},
		"76b504e33305a63a3b80686c0b7bb99e7697466927ba78e224728e80bfaaa0be": {
			feedType: customquery.FeedTypeMarket,
			target:   "TBTC",
		},
		"0bc2d41117ae8779da7623ee76a109c88b84b9bf4d9b404524df04f7d0ca4ca7": {
			feedType:   customquery.FeedTypeFundamental,
			target:     "RETH",
			collateral: "ETH",
		},
		"1962cde2f19178fe2bb2229e78a6d386e6406979edc7b9a1966d89d83b3ebf2e": {
			feedType:   customquery.FeedTypeFundamental,
			target:     "WSTETH",
			collateral: "stETH",
		},
		"d62f132d9d04dde6e223d4366c48b47cd9f90228acdc6fa755dab93266db5176": {
			feedType: customquery.FeedTypeMarket,
			target:   "KING",
		},
		"611fd0e88850bf0cc036d96d04d47605c90b993485c2971e022b5751bbb04f23": {
			feedType: customquery.FeedTypeMarket,
			target:   "stATOM",
		},
		"187f74d310dc494e6efd928107713d4229cd319c2cf300224de02776090809f1": {
			feedType:   customquery.FeedTypeFundamental,
			target:     "SUSN",
			collateral: "USN",
		},
		"ab30caa3e7827a27c153063bce02c0b260b29c0c164040c003f0f9ec66002510": {
			feedType:   customquery.FeedTypeFundamental,
			target:     "SFRXUSD",
			collateral: "FRX",
		},
	}

	for queryID, tc := range tests {
		t.Run(queryID, func(t *testing.T) {
			t.Parallel()
			query, ok := customquery.StaticQueriesConfig[queryID]
			require.True(t, ok, "query %s must exist in StaticQueriesConfig", queryID)

			feedType, target, collateral := customquery.ClassifyQueryFeed(query)
			require.Equal(t, tc.feedType, feedType)
			require.Equal(t, tc.target, target)
			require.Equal(t, tc.collateral, collateral)
		})
	}

	require.Equal(t, len(tests), len(customquery.StaticQueriesConfig),
		"every static query must have a classification test case")
}

func TestGenerateFeedComment(t *testing.T) {
	t.Parallel()

	tests := map[string]string{
		"05cddb6b67074aa61fcbe1d2fd5924e028bb699b506267df28c88f7deac4edc6": "SDAI/USD: (market) median of 3 sources.",
		"03731257e35c49e44b267640126358e5decebdd8f18b5e8f229542ec86e318cf": "SUSDE/USD: (fundamental) ratio from susde contract × USDE/USD pricefeed cache.",
		"187f74d310dc494e6efd928107713d4229cd319c2cf300224de02776090809f1": "SUSN/USD: (fundamental) ratio from susn contract × median USN/USD from 3 sources.",
		"ab30caa3e7827a27c153063bce02c0b260b29c0c164040c003f0f9ec66002510": "SFRXUSD/USD: (fundamental) ratio from sfrxusd contract × median FRX/USD from 3 sources.",
	}

	for queryID, want := range tests {
		t.Run(queryID, func(t *testing.T) {
			t.Parallel()
			query := customquery.StaticQueriesConfig[queryID]
			require.Equal(t, want, customquery.GenerateFeedComment(query))
		})
	}
}

func TestGenerateDefaultConfigTomlString_IncludesFeedComments(t *testing.T) {
	t.Parallel()

	buf := customquery.GenerateDefaultConfigTomlString()
	output := buf.String()

	for queryID, query := range customquery.StaticQueriesConfig {
		comment := customquery.GenerateFeedComment(query)
		sectionHeader := "# " + comment + "\n    [queries." + queryID + "]"
		require.True(t, strings.Contains(output, sectionHeader),
			"expected generated TOML to include comment for query %s", queryID)
	}
}
