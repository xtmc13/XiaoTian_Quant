package order

import (
	"testing"
	"time"
)

// TestConditionalEnginePriceFreshness 锁定条件引擎价格缓存的新鲜度判定：
// 刚喂价命中；超窗（WS 冻结残留陈旧价）返回 0；无符号返回 0。
func TestConditionalEnginePriceFreshness(t *testing.T) {
	ce := GetConditionalEngine()

	ce.UpdatePrice("BTCUSDT", 100.0)
	if p := ce.GetPrice("BTCUSDT"); p != 100.0 {
		t.Fatalf("fresh price should hit, got %v", p)
	}
	t.Cleanup(func() { ce.UpdatePrice("BTCUSDT", 100.0) })

	// 模拟缓存冻结：把收到时间拨到新鲜度窗外。
	ce.priceMu.Lock()
	cp := ce.priceCache["BTCUSDT"]
	cp.at = time.Now().Add(-(priceStaleWindow + time.Minute))
	ce.priceCache["BTCUSDT"] = cp
	ce.priceMu.Unlock()
	if p := ce.GetPrice("BTCUSDT"); p != 0 {
		t.Fatalf("stale price should return 0, got %v", p)
	}

	if p := ce.GetPrice("NOSUCHSYMBOL"); p != 0 {
		t.Fatalf("missing symbol should return 0, got %v", p)
	}
}
