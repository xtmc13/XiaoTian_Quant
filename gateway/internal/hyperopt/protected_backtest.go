package hyperopt

import (
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/protection"
)

// ProtectedBacktestStrategy 在回测中应用 protection 配置（对标 freqtrade
// hyperopt protection space 回测时 enable_protections 的行为）。
//
// 它包装一个普通 BacktestStrategy：
//  1. 跟踪仓位开合，把平仓交易喂给 ProtectionManager
//     （CooldownPeriod.RecordExit / StoplossGuard.RecordStoplossDetail /
//     LowProfitPairs.RecordTrade，并维护 TradeHistory 供 MaxDrawdown 计算）；
//  2. 每个新开仓信号先过 ProtectionManager.CheckAll，被锁则吞掉信号。
//
// 这样回测评分直接反映 protection 参数的效果，无需改动 backtest.Runner。
type ProtectedBacktestStrategy struct {
	inner     backtest.BacktestStrategy
	mgr       *protection.ProtectionManager
	symbol    string
	timeframe string

	pending     *backtest.Position // runner 就地更新的持仓指针
	history     []protection.TradeRecord
	equityPeak  float64
	equity      float64
	blockedBuys int
}

// NewProtectedBacktestStrategy 包装策略并挂载 protection 管理器。
func NewProtectedBacktestStrategy(inner backtest.BacktestStrategy, mgr *protection.ProtectionManager, timeframe string) *ProtectedBacktestStrategy {
	return &ProtectedBacktestStrategy{
		inner:     inner,
		mgr:       mgr,
		symbol:    inner.Symbol(),
		timeframe: timeframe,
	}
}

func (s *ProtectedBacktestStrategy) Name() string   { return s.inner.Name() }
func (s *ProtectedBacktestStrategy) Symbol() string { return s.symbol }

// BlockedEntries 返回被 protection 拦截的开仓信号数。
func (s *ProtectedBacktestStrategy) BlockedEntries() int { return s.blockedBuys }

// TradeHistory 返回回测中已平仓交易的 protection 记录。
func (s *ProtectedBacktestStrategy) TradeHistory() []protection.TradeRecord {
	out := make([]protection.TradeRecord, len(s.history))
	copy(out, s.history)
	return out
}

func (s *ProtectedBacktestStrategy) OnBar(bar model.Bar, state *backtest.StrategyState) (*model.Signal, error) {
	now := time.UnixMilli(bar.Time)

	// 1. 检测上一笔持仓是否已被 runner 平仓（runner 就地更新 Position 指针）
	if s.pending != nil && s.pending.IsClosed {
		s.recordClose(s.pending)
		s.pending = nil
	}
	if state.Position != nil && !state.Position.IsClosed && state.Position != s.pending {
		s.pending = state.Position
	}

	// 2. 权益跟踪（MaxDrawdown 无交易历史时的回退口径）
	if state.Equity > s.equityPeak {
		s.equityPeak = state.Equity
	}
	s.equity = state.Equity

	// 3. 内部策略出信号
	sig, err := s.inner.OnBar(bar, state)
	if err != nil || sig == nil || s.mgr == nil {
		return sig, err
	}

	// 4. 新开仓信号过 protection 门控（仅对"当前无持仓"的开仓方向）
	if (sig.Direction == "LONG" || sig.Direction == "SHORT") && (state.Position == nil || state.Position.IsClosed) {
		side := "LONG"
		if sig.Direction == "SHORT" {
			side = "SHORT"
		}
		ctx := protection.ProtectionContext{
			Symbol:          s.symbol,
			Timeframe:       s.timeframe,
			CurrentTime:     now,
			Side:            side,
			TradeHistory:    s.history,
			TotalBalance:    state.Equity,
			PeakBalance:     s.equityPeak,
			CurrentDrawdown: s.currentDrawdown(),
		}
		if res := s.mgr.CheckAll(ctx); res.Blocked {
			s.blockedBuys++
			return nil, nil
		}
	}

	return sig, nil
}

func (s *ProtectedBacktestStrategy) OnTick(tick model.Tick, state *backtest.StrategyState) (*model.Signal, error) {
	return s.inner.OnTick(tick, state)
}

// currentDrawdown 由权益峰值计算当前回撤（0-1）。
func (s *ProtectedBacktestStrategy) currentDrawdown() float64 {
	if s.equityPeak <= 0 {
		return 0
	}
	dd := (s.equityPeak - s.equity) / s.equityPeak
	if dd < 0 {
		return 0
	}
	return dd
}

// recordClose 把一笔平仓交易喂给各 protection。
func (s *ProtectedBacktestStrategy) recordClose(pos *backtest.Position) {
	rec := protection.TradeRecord{
		Symbol:     pos.Symbol,
		Side:       orderSideToString(pos.Side),
		EntryPrice: pos.EntryPrice,
		ExitPrice:  pos.ExitPrice,
		Quantity:   pos.Quantity,
		PnL:        pos.RealizedPnL,
		PnLPct:     positionPnLPct(pos),
		IsStoploss: isStoplossExit(pos.ExitReason),
		EntryTime:  time.UnixMilli(pos.EntryTime),
		ExitTime:   time.UnixMilli(pos.ExitTime),
	}
	s.history = append(s.history, rec)

	for _, p := range s.mgr.Protections() {
		switch prot := p.(type) {
		case *protection.CooldownPeriod:
			prot.RecordExit(pos.Symbol, rec.ExitTime)
		case *protection.StoplossGuard:
			if rec.IsStoploss {
				prot.RecordStoplossDetail(pos.Symbol, rec.ExitTime, rec.PnLPct, rec.Side)
			}
		case *protection.LowProfitPairs:
			prot.RecordTrade(rec)
		}
	}
}

func orderSideToString(side model.OrderSide) string {
	if side == model.SideSell {
		return "SHORT"
	}
	return "LONG"
}

// positionPnLPct 计算平仓交易的盈利率（ratio 口径）。
func positionPnLPct(pos *backtest.Position) float64 {
	notional := pos.EntryPrice * pos.Quantity
	if notional <= 0 {
		return 0
	}
	return pos.RealizedPnL / notional
}

// isStoplossExit 判断平仓原因是否为止损/强平
// （策略约定：止损类原因包含 "stop" / "liquidation"，如
// "long stop loss"、"ATR trailing stop hit"）。
func isStoplossExit(reason string) bool {
	r := strings.ToLower(reason)
	return strings.Contains(r, "stop") || strings.Contains(r, "liquidation")
}
