package risk

import (
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

func baseSnap() PortfolioSnapshot {
	return PortfolioSnapshot{
		UserID:        7,
		Equity:        10000,
		Available:     8000,
		MarginUsed:    2000,
		ExposurePct:   30,
		DailyPnL:      0,
		PositionCount: 2,
	}
}

func TestGuardAllowsHealthyPortfolio(t *testing.T) {
	g := NewPortfolioGuard(DefaultGuardConfig())
	if err := g.Check(baseSnap(), OrderIntent{Side: model.SideBuy, OrderValue: 1000}); err != nil {
		t.Fatalf("healthy portfolio should pass: %v", err)
	}
}

func TestGuardRejectsNetExposureOverflow(t *testing.T) {
	g := NewPortfolioGuard(DefaultGuardConfig())
	snap := baseSnap()
	snap.ExposurePct = 120 // 120% equity
	if err := g.Check(snap, OrderIntent{Side: model.SideBuy}); err == nil {
		t.Fatal("exposure overflow should reject")
	} else {
		t.Logf("rejected: %v", err)
	}
	// 敞口超限不置永久刹车。
	if tripped, _ := g.IsTripped(7); tripped {
		t.Fatal("exposure overflow should not trip breaker")
	}
}

func TestGuardRejectsConcentration(t *testing.T) {
	g := NewPortfolioGuard(DefaultGuardConfig())
	snap := baseSnap()
	snap.ExposurePct = 90 // 90% > 80%
	if err := g.Check(snap, OrderIntent{Side: model.SideBuy}); err == nil {
		t.Fatal("concentration overflow should reject")
	}
}

func TestGuardTripsOnDailyLossAndAllowsClose(t *testing.T) {
	g := NewPortfolioGuard(DefaultGuardConfig())
	snap := baseSnap()
	snap.DailyPnL = -1500 // 15% loss > 10% limit

	if err := g.Check(snap, OrderIntent{Side: model.SideBuy}); err == nil {
		t.Fatal("daily loss breach should reject new open")
	}
	tripped, reason := g.IsTripped(7)
	if !tripped {
		t.Fatal("daily loss breach should trip breaker")
	}
	if reason == "" {
		t.Fatal("trip reason should be recorded")
	}

	// 刹车态下新开仓继续被拦。
	if err := g.Check(snap, OrderIntent{Side: model.SideBuy}); err == nil {
		t.Fatal("tripped breaker should reject new open")
	}
	// 减仓/平仓单一律放行。
	if err := g.Check(snap, OrderIntent{Side: model.SideSell, IsClose: true}); err != nil {
		t.Fatalf("close order must pass through breaker: %v", err)
	}
}

func TestGuardTripsOnLowMarginRatio(t *testing.T) {
	g := NewPortfolioGuard(DefaultGuardConfig())
	snap := baseSnap()
	snap.MarginUsed = 8000 // equity/margin = 125% < 150%
	if err := g.Check(snap, OrderIntent{Side: model.SideBuy}); err == nil {
		t.Fatal("low margin ratio should reject")
	}
	if tripped, _ := g.IsTripped(7); !tripped {
		t.Fatal("low margin ratio should trip breaker")
	}
}

func TestGuardBreakerResetsAfterCooldown(t *testing.T) {
	cfg := DefaultGuardConfig()
	cfg.Cooldown = 20 * time.Millisecond
	g := NewPortfolioGuard(cfg)
	snap := baseSnap()
	snap.DailyPnL = -1500

	_ = g.Check(snap, OrderIntent{Side: model.SideBuy})
	if tripped, _ := g.IsTripped(7); !tripped {
		t.Fatal("should be tripped before cooldown")
	}
	time.Sleep(30 * time.Millisecond)
	if err := g.Check(snap, OrderIntent{Side: model.SideBuy}); err == nil {
		t.Fatal("still lossing: should reject after reset")
	}
	// 人工解除刹车后，恢复盈亏的账户放行。
	g.Reset(7)
	snap.DailyPnL = 0
	if err := g.Check(snap, OrderIntent{Side: model.SideBuy}); err != nil {
		t.Fatalf("after reset and recovered, should pass: %v", err)
	}
}

func TestGuardDailyLossBreached(t *testing.T) {
	g := NewPortfolioGuard(DefaultGuardConfig())
	snap := baseSnap()
	if g.DailyLossBreached(snap) {
		t.Fatal("healthy should not be breached")
	}
	snap.DailyPnL = -1200 // 12% > 10%
	if !g.DailyLossBreached(snap) {
		t.Fatal("loss over limit should be breached")
	}
}
