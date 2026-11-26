package risk

import (
	"fmt"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── 组合级风控守卫（D3）─────────────────────────────────────
//
// 现有 Manager 是单笔/单上下文检查；本守卫按账户聚合全部机器人的敞口、
// 保证金占用与当日盈亏，做组合级刹车：
//   - 总敞口 / 权益超阈：拒绝新开仓；
//   - 同向集中度（单边敞口占比）超阈：拒绝新开仓；
//   - 保证金率低于下限：刹车，只允许减仓；
//   - 当日亏损达权益上限：刹车，只允许减仓。
//
// 平仓/减仓单（IsClose）一律放行——刹车只拦新开仓，不阻止离场。
// 敞口口径与 PortfolioManager 对齐：占权益百分比（ExposurePct）。

// PortfolioSnapshot 组合级聚合快照，由调用方从账户/持仓汇总填入。
type PortfolioSnapshot struct {
	UserID        int64   // 账户属主；0 表示全局/单用户时代
	Equity        float64 // 总权益（USDT）
	Available     float64 // 可用余额
	MarginUsed    float64 // 已用保证金（USDT）
	ExposurePct   float64 // 总敞口/权益 %（含多空持仓价值）
	DailyPnL      float64 // 当日盈亏（已实现 + 浮动，USDT）
	PositionCount int
}

// OrderIntent 待下单意图。
type OrderIntent struct {
	Symbol     string
	Side       model.OrderSide
	IsClose    bool // 平仓/减仓单：刹车放行
	OrderValue float64
}

// GuardConfig 组合风控阈值。
type GuardConfig struct {
	MaxNetExposurePct  float64       // 总敞口/权益 上限 %
	MaxConcentrationPct float64      // 单边敞口/权益 上限 %（同向集中度）
	MinMarginRatioPct  float64       // 保证金率下限 %
	MaxDailyLossPct    float64       // 账户日亏上限（占权益 %），超限刹车
	Cooldown           time.Duration // 刹车自动解除冷却
}

// DefaultGuardConfig 提供保守默认值。
func DefaultGuardConfig() GuardConfig {
	return GuardConfig{
		MaxNetExposurePct:  100.0,
		MaxConcentrationPct: 80.0,
		MinMarginRatioPct:  150.0,
		MaxDailyLossPct:     10.0,
		Cooldown:           5 * time.Minute,
	}
}

type tripState struct {
	Reason    string
	TrippedAt int64
}

// PortfolioGuard 按账户维护组合级刹车状态。
type PortfolioGuard struct {
	cfg      GuardConfig
	mu       sync.RWMutex
	tripped  map[int64]*tripState
	OnTrip   func(userID int64, reason string)
}

// NewPortfolioGuard 创建组合风控守卫。
func NewPortfolioGuard(cfg GuardConfig) *PortfolioGuard {
	return &PortfolioGuard{
		cfg:     cfg,
		tripped: make(map[int64]*tripState),
	}
}

// Check 组合级校验。nil 允许新开仓；error 拒绝（含刹车/超限原因）。
// 减仓单直接放行。
func (g *PortfolioGuard) Check(snap PortfolioSnapshot, intent OrderIntent) error {
	if intent.IsClose {
		return nil
	}

	// 既有刹车态：冷却未过继续拦截；过冷却自动解除。
	if ts, ok := g.tripState(snap.UserID); ok {
		if time.Since(time.UnixMilli(ts.TrippedAt)) < g.cfg.Cooldown {
			return fmt.Errorf("portfolio circuit breaker tripped: %s", ts.Reason)
		}
		g.clearTrip(snap.UserID)
	}

	if snap.Equity <= 0 {
		return nil // 无权益基线：不做比例强拦，交给单笔检查
	}

	// 日亏上限（硬刹车）。
	maxLoss := snap.Equity * g.cfg.MaxDailyLossPct / 100
	if snap.DailyPnL < -maxLoss {
		g.trip(snap.UserID, fmt.Sprintf("daily loss %.2f exceeds limit %.2f", -snap.DailyPnL, maxLoss))
		return fmt.Errorf("daily loss limit breached: %.2f < -%.2f", snap.DailyPnL, maxLoss)
	}

	// 保证金率（硬刹车）：保证金占用相对权益的安全垫不足。
	if snap.MarginUsed > 0 {
		marginRatio := snap.Equity / snap.MarginUsed * 100
		if marginRatio < g.cfg.MinMarginRatioPct {
			g.trip(snap.UserID, fmt.Sprintf("margin ratio %.1f%% below minimum %.1f%%", marginRatio, g.cfg.MinMarginRatioPct))
			return fmt.Errorf("margin ratio %.1f%% below minimum %.1f%%", marginRatio, g.cfg.MinMarginRatioPct)
		}
	}

	// 总敞口占比（拦新开，不置永久刹车）。
	if g.cfg.MaxNetExposurePct > 0 && snap.ExposurePct > g.cfg.MaxNetExposurePct {
		return fmt.Errorf("net exposure %.1f%% exceeds limit %.1f%%", snap.ExposurePct, g.cfg.MaxNetExposurePct)
	}

	// 同向集中度：总敞口占比超单边上限（当前为总敞口口径；持仓方向维度拆分后细化到多/空单边）。
	if g.cfg.MaxConcentrationPct > 0 && snap.ExposurePct > g.cfg.MaxConcentrationPct {
		return fmt.Errorf("concentration %.1f%% exceeds limit %.1f%%", snap.ExposurePct, g.cfg.MaxConcentrationPct)
	}

	return nil
}

// IsTripped 返回账户是否处于刹车态及原因。
func (g *PortfolioGuard) IsTripped(userID int64) (bool, string) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	if ts, ok := g.tripped[userID]; ok {
		return true, ts.Reason
	}
	return false, ""
}

// Reset 人工解除账户刹车。
func (g *PortfolioGuard) Reset(userID int64) {
	g.clearTrip(userID)
}

// DailyLossBreached 供策略/账户层查询是否已触日亏上限（自动停策略口径）。
func (g *PortfolioGuard) DailyLossBreached(snap PortfolioSnapshot) bool {
	if snap.Equity <= 0 || g.cfg.MaxDailyLossPct <= 0 {
		return false
	}
	return snap.DailyPnL < -snap.Equity*g.cfg.MaxDailyLossPct/100
}

func (g *PortfolioGuard) tripState(userID int64) (*tripState, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	ts, ok := g.tripped[userID]
	return ts, ok
}

func (g *PortfolioGuard) trip(userID int64, reason string) {
	g.mu.Lock()
	g.tripped[userID] = &tripState{Reason: reason, TrippedAt: time.Now().UnixMilli()}
	g.mu.Unlock()
	if g.OnTrip != nil {
		g.OnTrip(userID, reason)
	}
}

func (g *PortfolioGuard) clearTrip(userID int64) {
	g.mu.Lock()
	delete(g.tripped, userID)
	g.mu.Unlock()
}
