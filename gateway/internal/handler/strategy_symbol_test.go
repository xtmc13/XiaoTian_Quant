package handler

import "testing"

func TestCanonicalEngineSymbol(t *testing.T) {
	cases := []struct {
		name   string
		symbol string
		coin   string
		want   string
	}{
		{"裸币名按 coin 补 USDT", "SOL", "SOL", "SOLUSDT"},
		{"裸币名小写也归一", "sol", "SOL", "SOLUSDT"},
		{"完整币对不重复补", "SOLUSDT", "SOL", "SOLUSDT"},
		{"稳定币后缀保持", "ETHUSDC", "ETH", "ETHUSDC"},
		{"分隔符归一", "btc/usdt", "BTC", "BTCUSDT"},
		{"coin 不一致不补", "SOLBTC", "SOL", "SOLBTC"},
		{"无 coin 非加密切口保持原样", "AAPL", "", "AAPL"},
		{"空串", "", "SOL", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			params := map[string]any{}
			if tc.coin != "" {
				params["coin"] = tc.coin
			}
			if got := canonicalEngineSymbol(tc.symbol, params); got != tc.want {
				t.Fatalf("canonicalEngineSymbol(%q, coin=%q) = %q, want %q", tc.symbol, tc.coin, got, tc.want)
			}
		})
	}
}
