package app

import (
	"fmt"
	"testing"

	"github.com/xiaotian-quant/gateway/internal/logging"
	"github.com/xiaotian-quant/gateway/internal/model"
)

// TestGetLastPriceSource：WS 无数据 + REST 探针失败 → 合成价（isSynthetic=true）；
// 探针有值 → 非合成。锚点表保留服务 paper。
func TestGetLastPriceSource(t *testing.T) {
	origProbe := priceRestProbe
	t.Cleanup(func() { priceRestProbe = origProbe })

	// REST 探针失败 → 合成锚点（BTCUSDT=50000）。
	priceRestProbe = func(norm string) (float64, bool) { return 0, false }
	p, synthetic := getLastPriceSource("BTCUSDT")
	if !synthetic {
		t.Fatal("expected synthetic price when REST probe fails")
	}
	if p != 50000 {
		t.Fatalf("anchor price = %v, want 50000", p)
	}

	// REST 探针有值 → 真实来源。
	priceRestProbe = func(norm string) (float64, bool) { return 43210.5, true }
	p, synthetic = getLastPriceSource("BTCUSDT")
	if synthetic {
		t.Fatal("expected real price from probe")
	}
	if p != 43210.5 {
		t.Fatalf("probe price = %v", p)
	}
}

// TestRejectIfSyntheticPrice：合成价 + 实盘 → 拒绝；paper → 放行。
func TestRejectIfSyntheticPrice(t *testing.T) {
	if err := rejectIfSyntheticPrice(true, "binance"); err == nil {
		t.Fatal("live order with synthetic price must be rejected")
	} else if err.Error() != "行情不可用，实盘单已拦截（合成价保护）" {
		t.Fatalf("unexpected error message: %v", err)
	}
	if err := rejectIfSyntheticPrice(true, "paper"); err != nil {
		t.Fatal("paper order with synthetic price must pass")
	}
	if err := rejectIfSyntheticPrice(true, ""); err != nil {
		t.Fatal("empty-exchange (paper) order must pass")
	}
	if err := rejectIfSyntheticPrice(false, "binance"); err != nil {
		t.Fatal("real price live order must pass")
	}
}

// TestFinalizeLiveSubmitResult：实盘失败/空结果 → REJECTED + 错误，绝不转 paper。
func TestFinalizeLiveSubmitResult(t *testing.T) {
	log := logging.New("test")

	// 交易所报错 → REJECTED + 错误透出原始错误。
	ord := &model.OrderData{Symbol: "BTCUSDT", Exchange: "binance", Status: model.StatusPending}
	_, err := finalizeLiveSubmitResult(log, ord, nil, fmt.Errorf("insufficient balance"))
	if err == nil {
		t.Fatal("live submit failure must return error")
	}
	if ord.Status != model.StatusRejected {
		t.Fatalf("status = %v, want REJECTED", ord.Status)
	}

	// 空结果（凭证缺失）→ REJECTED。
	ord2 := &model.OrderData{Symbol: "BTCUSDT", Exchange: "okx", Status: model.StatusPending}
	_, err = finalizeLiveSubmitResult(log, ord2, nil, nil)
	if err == nil {
		t.Fatal("live submit empty result must return error")
	}
	if ord2.Status != model.StatusRejected {
		t.Fatalf("status = %v, want REJECTED", ord2.Status)
	}

	// 成功 → NEW 结果集。
	ord3 := &model.OrderData{Symbol: "BTCUSDT", Exchange: "binance"}
	res, err := finalizeLiveSubmitResult(log, ord3, map[string]any{"orderId": "123"}, nil)
	if err != nil || res["status"] != "NEW" {
		t.Fatalf("success path broken: res=%v err=%v", res, err)
	}
}
