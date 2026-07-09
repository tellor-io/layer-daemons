package customquery

var StaticEndpointTemplateConfig = map[string]*EndpointTemplate{
	"coingecko": {
		URLTemplate:    "https://api.coingecko.com/api/v3/simple/price?ids={coin_id}&vs_currencies=usd",
		Method:         "GET",
		Timeout:        5000,
		MaxDataAgeSecs: 600, // 10 minutes
	},
	"coingeckoPro": {
		URLTemplate:    "https://pro-api.coingecko.com/api/v3/simple/price?ids={coin_id}&vs_currencies=usd&x_cg_pro_api_key={api_key}",
		Method:         "GET",
		Timeout:        5000,
		ApiKey:         "${CGPRO_API_KEY}",
		MaxDataAgeSecs: 600, // 10 minutes
	},
	"coinpaprika": {
		URLTemplate:    "https://api.coinpaprika.com/v1/tickers/{coin_id}?quotes=USD",
		Method:         "GET",
		Timeout:        5000,
		MaxDataAgeSecs: 600, // 10 minutes
	},
	"curve": {
		URLTemplate:    "https://prices.curve.finance/v1/usd_price/ethereum/{contract_address}",
		Method:         "GET",
		Timeout:        5000,
		MaxDataAgeSecs: 600, // 10 minutes
	},
	// curveSusdeFactoryStableNg: fixed getPools URL for factory-stable-ng (legacy name). Use with curve_factory_price
	// and params target_token, exclude_pools, merge_get_pools_url (see curveEthereumGetPools for parameterized registry).
	"curveSusdeFactoryStableNg": {
		URLTemplate:    "https://api.curve.fi/api/getPools/ethereum/factory-stable-ng",
		Method:         "GET",
		Timeout:        5000,
		MaxDataAgeSecs: 600, // 10 minutes
	},
	// curveEthereumGetPools: Curve getPools for ethereum/{registry}. Params: registry (URL), plus handler params on Reader.
	"curveEthereumGetPools": {
		URLTemplate:    "https://api.curve.fi/api/getPools/ethereum/{registry}",
		Method:         "GET",
		Timeout:        5000,
		MaxDataAgeSecs: 600, // 10 minutes
	},
	"crypto": {
		URLTemplate:    "https://api.crypto.com/v2/public/get-ticker?instrument_name={instrument_name}",
		Method:         "GET",
		Timeout:        5000,
		MaxDataAgeSecs: 600, // 10 minutes
	},
	"coinmarketcap": {
		URLTemplate: "https://pro-api.coinmarketcap.com/v1/cryptocurrency/quotes/latest?id={id}",
		Method:      "GET",
		Timeout:     5000,
		ApiKey:      "${CMC_PRO_API_KEY}",
		Headers: map[string]string{
			"Accept":            "application/json",
			"X-CMC_PRO_API_KEY": "api_key",
		},
		MaxDataAgeSecs: 600, // 10 minutes
	},
	"coinbase": {
		URLTemplate:    "https://api.coinbase.com/v2/prices/{currency_pair}/spot",
		Method:         "GET",
		Timeout:        5000,
		MaxDataAgeSecs: 600, // 10 minutes
	},
	"osmosis": {
		URLTemplate:    "https://lcd.osmosis.zone/osmosis/gamm/v1beta1/pools/{pool_id}",
		Method:         "GET",
		Timeout:        5000,
		MaxDataAgeSecs: 86400, // 24 hours — uses native last_liquidity_update timestamp
	},
	"uniswapV4ethereum": {
		// docs: https://docs.uniswap.org/api/subgraph/overview — requires SUBGRAPH_API_KEY in the environment.
		URLTemplate:    "https://gateway.thegraph.com/api/{api_key}/subgraphs/id/DiYPVdygkfjDWhbxGSqAQxwBKmfKnkWQojqeM2rkLb3G",
		Query:          `{"query": "{ token(id: \"{token_address}\") { derivedETH } }"}`,
		Method:         "POST",
		Timeout:        5000,
		Headers:        map[string]string{"Content-Type": "application/json"},
		ApiKey:         "${SUBGRAPH_API_KEY}",
		MaxDataAgeSecs: 600, // 10 minutes — indexed data can lag behind chain head
	},
	// theGraphUniswapStylePool: The Graph gateway + Uniswap v3/v4 Pool entity (token0/token1 + token0Price/token1Price).
	// Params: subgraph_id, pool_id, target_token, quote_token for subgraph_uniswap_pool_pair_usd (uses token1Price when target is token0, token0Price when target is token1). SUBGRAPH_API_KEY required.
	"theGraphUniswapStylePool": {
		URLTemplate:    "https://gateway.thegraph.com/api/{api_key}/subgraphs/id/{subgraph_id}",
		Query:          `{"query": "{ pool(id: \"{pool_id}\") { token0 { id } token1 { id } token0Price token1Price } _meta { block { timestamp } } }"}`,
		Method:         "POST",
		Timeout:        5000,
		Headers:        map[string]string{"Content-Type": "application/json"},
		ApiKey:         "${SUBGRAPH_API_KEY}",
		MaxDataAgeSecs: 600, // 10 minutes — uses native _meta.block.timestamp
	},
	"uniswapV3ethereum": {
		// Ethereum Uniswap v3 subgraph on The Graph network gateway. Requires SUBGRAPH_API_KEY in the environment at reporter startup.
		URLTemplate:    "https://gateway.thegraph.com/api/{api_key}/subgraphs/id/5zvR82QoaXYFyDEKLZ9t6v9adgnptxYpKpSbxtgVENFV",
		Query:          `{"query": "{ token(id: \"{token_address}\") { derivedETH } }"}`,
		Method:         "POST",
		Timeout:        5000,
		Headers:        map[string]string{"Content-Type": "application/json"},
		ApiKey:         "${SUBGRAPH_API_KEY}",
		MaxDataAgeSecs: 600, // 10 minutes — indexed data can lag behind chain head
	},
	"sushiswapKatana": {
		// docs: https://docs.sushi.com/api/examples/pricing
		URLTemplate:    "https://api.sushi.com/price/v1/747474",
		Method:         "GET",
		Timeout:        5000,
		MaxDataAgeSecs: 600, // 10 minutes
	},
}

var StaticRPCEndpointTemplateConfig = map[string]*RPCEndpointTemplate{
	"ethereum": {
		URLs: []string{
			"https://mainnet.infura.io/v3/${INFURA_API_KEY}",
			"https://eth-mainnet.alchemyapi.io/v2/${ALCHEMY_API_KEY}",
			"https://rpc.ankr.com/eth",
		},
	},
}

var StaticQueriesConfig = map[string]*QueryConfig{
	"05cddb6b67074aa61fcbe1d2fd5924e028bb699b506267df28c88f7deac4edc6": {
		ID:                "05cddb6b67074aa61fcbe1d2fd5924e028bb699b506267df28c88f7deac4edc6",
		AggregationMethod: "median",
		MaxSpreadPercent:  50.0,
		MinResponses:      2,
		ResponseType:      "ufixed256x18",
		Endpoints: []EndpointConfig{
			{
				EndpointType: "coingecko",
				ResponsePath: []string{"savings-dai", "usd"},
				Params: map[string]string{
					"coin_id": "savings-dai",
				},
				MarketId: "SDAI-USD",
			},
			{
				EndpointType: "coinpaprika",
				ResponsePath: []string{"quotes", "USD", "price"},
				Params: map[string]string{
					"coin_id": "sdai-savings-dai",
				},
				MarketId: "SDAI-USD",
			},
			{
				EndpointType: "curve",
				ResponsePath: []string{"data", "usd_price"},
				Params: map[string]string{
					"contract_address": "0x83F20F44975D03b1b09e64809B757c47f942BEeA",
				},
				MarketId: "SDAI-USD",
			},
		},
	},
	"03731257e35c49e44b267640126358e5decebdd8f18b5e8f229542ec86e318cf": {
		ID:                "03731257e35c49e44b267640126358e5decebdd8f18b5e8f229542ec86e318cf",
		AggregationMethod: "median",
		MaxSpreadPercent:  10.0,
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		Endpoints: []EndpointConfig{
			{
				EndpointType: "contract",
				Handler:      "susdeusd_handler",
				Chain:        "ethereum",
				MarketId:     "SUSDE-USD",
			},
		},
	},
	"76b504e33305a63a3b80686c0b7bb99e7697466927ba78e224728e80bfaaa0be": {
		ID:                "76b504e33305a63a3b80686c0b7bb99e7697466927ba78e224728e80bfaaa0be",
		AggregationMethod: "median",
		MaxSpreadPercent:  100.0,
		MinResponses:      2,
		ResponseType:      "ufixed256x18",
		Endpoints: []EndpointConfig{
			{
				EndpointType: "coingeckoPro",
				ResponsePath: []string{"tbtc", "usd"},
				Params: map[string]string{
					"coin_id": "tbtc",
				},
				MarketId: "TBTC-USD",
			},
			{
				EndpointType: "coinmarketcap",
				ResponsePath: []string{"data", "26133", "quote", "USD", "price"},
				Params: map[string]string{
					// "symbol": "TBTC",
					"id": "26133",
				},
				MarketId: "TBTC-USD",
			},
			{
				EndpointType: "coinbase",
				ResponsePath: []string{"data", "amount"},
				Params: map[string]string{
					"currency_pair": "TBTC-USD",
				},
				MarketId: "TBTC-USD",
			},
		},
	},
	"0bc2d41117ae8779da7623ee76a109c88b84b9bf4d9b404524df04f7d0ca4ca7": {
		ID:                "0bc2d41117ae8779da7623ee76a109c88b84b9bf4d9b404524df04f7d0ca4ca7",
		AggregationMethod: "median",
		MaxSpreadPercent:  100.0,
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		Endpoints: []EndpointConfig{
			{
				EndpointType: "contract",
				Handler:      "reth_handler",
				Chain:        "ethereum",
				MarketId:     "RETH-USD",
			},
		},
	},
	"1962cde2f19178fe2bb2229e78a6d386e6406979edc7b9a1966d89d83b3ebf2e": {
		ID:                "1962cde2f19178fe2bb2229e78a6d386e6406979edc7b9a1966d89d83b3ebf2e",
		AggregationMethod: "median",
		MaxSpreadPercent:  100.0,
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		Endpoints: []EndpointConfig{
			{
				EndpointType: "contract",
				Handler:      "wsteth_handler",
				Chain:        "ethereum",
				MarketId:     "WSTETH-USD",
			},
		},
	},
	"ab30caa3e7827a27c153063bce02c0b260b29c0c164040c003f0f9ec66002510": {
		ID:                "ab30caa3e7827a27c153063bce02c0b260b29c0c164040c003f0f9ec66002510",
		AggregationMethod: "median",
		MaxSpreadPercent:  10.0,
		MinResponses:      1,
		ResponseType:      "ufixed256x18",
		Endpoints: []EndpointConfig{
			{
				EndpointType: "combined",
				Handler:      "sfrxusd_price",
				CombinedSources: map[string]string{
					"ethereum":    "contract:ethereum",
					"coingecko":   "rpc:coingecko",
					"curve":       "rpc:curve",
					"coinpaprika": "rpc:coinpaprika",
				},
				CombinedConfig: map[string]any{
					"min_responses":      2,
					"max_spread_percent": 50.0,
					"coingecko_params": map[string]any{
						"coin_id": "frax",
					},
					"coingecko_response_path": []string{"frax", "usd"},
					"curve_params": map[string]any{
						"contract_address": "0x853d955aCEf822Db058eb8505911ED77F175b99e",
					},
					"curve_response_path": []string{"data", "usd_price"},
					"coinpaprika_params": map[string]any{
						"coin_id": "frax-frax",
					},
					"coinpaprika_response_path": []string{"quotes", "USD", "price"},
				},
				MarketId: "SFRXUSD-USD",
			},
		},
	},
}
