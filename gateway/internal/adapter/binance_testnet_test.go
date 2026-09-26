package adapter

import "testing"

/* ── Binance testnet URL 切换（现货已有，合约是本次修复）─────── */

// fapiBaseURL() 修复前在 testnet=true 时仍返回生产 fapi.binance.com，
// testnet 合约凭证会被生产网关拒签。修复后必须对称切换：
//   - 现货 testnet → testnet.binance.vision（原有行为，回归锁定）
//   - 合约 testnet → testnet.binancefuture.com
func TestBinanceTestnetURLs(t *testing.T) {
	// 防外部环境变量污染断言（t.Setenv 自动恢复）。
	t.Setenv("BINANCE_REST_URL", "")
	t.Setenv("BINANCE_FAPI_URL", "")
	t.Setenv("BINANCE_WS_URL", "")

	prod := NewBinanceAdapter("key", "secret", false)
	btAssertEq(t, prod.baseURL(), BinanceRestURL, "prod spot REST")
	btAssertEq(t, prod.fapiBaseURL(), BinanceFuturesRestURL, "prod futures REST")
	btAssertEq(t, prod.wsURL(), BinanceWsURL, "prod spot WS")

	tn := NewBinanceAdapter("key", "secret", true)
	btAssertEq(t, tn.baseURL(), BinanceTestURL, "testnet spot REST")
	btAssertEq(t, tn.fapiBaseURL(), BinanceFuturesTestRestURL, "testnet futures REST")
	btAssertEq(t, tn.wsURL(), BinanceTestWsURL, "testnet spot WS")
}

// 环境变量逃生门优先级高于 testnet 切换（自托管/mock 网关场景）。
func TestBinanceURLEnvOverrideBeatsTestnet(t *testing.T) {
	t.Setenv("BINANCE_REST_URL", "http://127.0.0.1:9001/api/v3")
	t.Setenv("BINANCE_FAPI_URL", "http://127.0.0.1:9002")
	t.Setenv("BINANCE_WS_URL", "ws://127.0.0.1:9003/ws")

	tn := NewBinanceAdapter("key", "secret", true)
	btAssertEq(t, tn.baseURL(), "http://127.0.0.1:9001/api/v3", "env spot REST wins")
	btAssertEq(t, tn.fapiBaseURL(), "http://127.0.0.1:9002", "env futures REST wins")
	btAssertEq(t, tn.wsURL(), "ws://127.0.0.1:9003/ws", "env spot WS wins")
}
