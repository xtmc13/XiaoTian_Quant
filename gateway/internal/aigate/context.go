package aigate

import (
	"encoding/json"
	"math"
	"time"

	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/risk"
	"github.com/xiaotian-quant/gateway/internal/service"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 决策上下文构建 ──
// 组装结构化上下文（订单、来源、近期 K 线摘要、账户风险状态、近期表现），
// 序列化为确定性 prompt（Go struct 字段序固定 = 序列化结果确定）。
// 所有数据源 best-effort：失败只降级（字段缺失/标记 unavailable），绝不报错——
// 上下文不全不是拦截理由（与 QuantDinger "missing evidence is not a rejection" 一致）。

// Bar 是一根 K 线（时间毫秒）。
type Bar struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// PositionInfo 是持仓快照（决策上下文用）。
type PositionInfo struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
	EntryPrice    float64 `json:"entry_price"`
	CurrentPrice  float64 `json:"current_price"`
	UnrealizedPnL float64 `json:"unrealized_pnl"`
	Leverage      float64 `json:"leverage,omitempty"`
}

// AccountInfo 是账户风险状态快照。
type AccountInfo struct {
	Equity            float64 `json:"equity"`
	Available         float64 `json:"available"`
	DrawdownPct       float64 `json:"drawdown_pct"`
	PositionCount     int     `json:"position_count"`
	UnrealizedPnL     float64 `json:"unrealized_pnl"`
	DailyOrderCount   int     `json:"daily_order_count"`
	ConsecutiveLosses int     `json:"consecutive_losses"`
	CircuitBreaker    string  `json:"circuit_breaker"` // CLOSED|OPEN|HALF_OPEN
}

// SourceStats 是订单来源（bot/策略）当日活跃度。
type SourceStats struct {
	TodayOrders   int `json:"today_orders"`
	TodayFilled   int `json:"today_filled"`
	TodayRejected int `json:"today_rejected"`
}

// ContextSources 是上下文数据源集合（生产默认真实服务，单测注入假数据）。
type ContextSources struct {
	RecentBars  func(symbol string, limit int) []Bar
	Positions   func() []PositionInfo
	Account     func() AccountInfo
	SourceStats func(source string) SourceStats
	Now         func() time.Time
}

// DecisionContext 是发给 LLM 的确定性上下文（字段序固定，JSON 输出确定）。
type DecisionContext struct {
	Version    int            `json:"version"`
	CapturedAt string         `json:"captured_at"`
	Order      orderContext   `json:"order"`
	Market     marketSummary  `json:"market"`
	Account    AccountInfo    `json:"account_risk"`
	Positions  []PositionInfo `json:"positions"`
	SourcePerf SourceStats    `json:"source_performance"`
}

type orderContext struct {
	Source       string  `json:"source"` // manual | signal:<s> | dca:<id> | pystrat:<id> ...
	Symbol       string  `json:"symbol"`
	Side         string  `json:"side"`
	OrderType    string  `json:"order_type"`
	MarketType   string  `json:"market_type"`
	PositionSide string  `json:"position_side,omitempty"`
	Quantity     float64 `json:"quantity"`
	RefPrice     float64 `json:"ref_price"`
	Notional     float64 `json:"notional"`
	Leverage     float64 `json:"leverage,omitempty"`
}

type marketSummary struct {
	Available       bool    `json:"available"`
	Bars            int     `json:"bars"`
	IntervalSeconds int64   `json:"interval_seconds,omitempty"`
	DataAgeSeconds  float64 `json:"data_age_seconds,omitempty"`
	IsStale         bool    `json:"is_stale,omitempty"`
	LastClose       float64 `json:"last_close,omitempty"`
	RefDeviationPct float64 `json:"ref_deviation_pct,omitempty"` // 委托价相对最新收盘偏离%
	Return1BarPct   float64 `json:"return_1bar_pct,omitempty"`
	Return5BarPct   float64 `json:"return_5bar_pct,omitempty"`
	Return20BarPct  float64 `json:"return_20bar_pct,omitempty"`
	MA5             float64 `json:"ma5,omitempty"`
	MA10            float64 `json:"ma10,omitempty"`
	MA20            float64 `json:"ma20,omitempty"`
	RSI14           float64 `json:"rsi14,omitempty"`
	ATR14           float64 `json:"atr14,omitempty"`
	ATR14Pct        float64 `json:"atr14_pct,omitempty"`
	VolumeRatio20   float64 `json:"volume_ratio_20,omitempty"`
	PricePosition20 float64 `json:"price_position_20,omitempty"` // 最新价在 20 根高低区间的位置 0-1
}

// JSON 序列化上下文（确定性：struct 字段序 + encoding/json 字典序 map 均固定）。
func (d *DecisionContext) JSON() string {
	data, err := json.Marshal(d)
	if err != nil {
		return `{"version":1,"error":"context_marshal_failed"}`
	}
	return string(data)
}

// Build 组装决策上下文。任一数据源 panic/失败均降级，不向上传播。
func (s ContextSources) Build(req *order.Request, source string, bars int) *DecisionContext {
	now := time.Now()
	if s.Now != nil {
		now = s.Now()
	}
	if bars <= 0 {
		bars = 30
	}
	refPrice := req.Price

	ctx := &DecisionContext{
		Version:    1,
		CapturedAt: now.UTC().Format(time.RFC3339),
		Order: orderContext{
			Source:       source,
			Symbol:       req.Symbol,
			Side:         string(req.Side),
			OrderType:    string(req.OrderType),
			MarketType:   string(req.MarketType),
			PositionSide: string(req.PositionSide),
			Quantity:     req.Quantity,
			RefPrice:     refPrice,
			Notional:     refPrice * req.Quantity,
			Leverage:     req.Leverage,
		},
		Positions: []PositionInfo{},
	}

	if s.RecentBars != nil {
		func() {
			defer func() { _ = recover() }()
			ctx.Market = SummarizeBars(s.RecentBars(req.Symbol, bars), refPrice, now)
		}()
	}
	if s.Positions != nil {
		func() {
			defer func() { _ = recover() }()
			positions := s.Positions()
			if len(positions) > 20 { // 控制 token：最多 20 条持仓
				positions = positions[:20]
			}
			ctx.Positions = positions
		}()
	}
	if s.Account != nil {
		func() {
			defer func() { _ = recover() }()
			ctx.Account = s.Account()
		}()
	}
	if s.SourceStats != nil {
		func() {
			defer func() { _ = recover() }()
			ctx.SourcePerf = s.SourceStats(source)
		}()
	}
	return ctx
}

// ── K 线摘要（确定性技术指标：MA/RSI14(Wilder)/ATR14/收益/量比） ──

// SummarizeBars 把 K 线序列压成固定大小摘要（token 成本与 K 线根数解耦）。
func SummarizeBars(bars []Bar, refPrice float64, now time.Time) marketSummary {
	clean := make([]Bar, 0, len(bars))
	for _, b := range bars {
		if b.Close <= 0 || b.High <= 0 || b.Low <= 0 {
			continue
		}
		clean = append(clean, b)
	}
	if len(clean) < 5 {
		return marketSummary{Available: false, Bars: len(clean)}
	}
	closes := make([]float64, len(clean))
	for i, b := range clean {
		closes[i] = b.Close
	}
	last := clean[len(clean)-1]

	sum := marketSummary{
		Available: true,
		Bars:      len(clean),
		LastClose: round6(last.Close),
	}
	if refPrice > 0 && last.Close > 0 {
		sum.RefDeviationPct = round3((refPrice/last.Close - 1) * 100)
	}
	sum.Return1BarPct = round3(pctChange(closes, 1))
	sum.Return5BarPct = round3(pctChange(closes, 5))
	sum.Return20BarPct = round3(pctChange(closes, 20))
	sum.MA5 = round6(sma(closes, 5))
	sum.MA10 = round6(sma(closes, 10))
	sum.MA20 = round6(sma(closes, 20))
	sum.RSI14 = round2(rsi(closes, 14))
	atr, atrPct := atr14(clean)
	sum.ATR14 = round6(atr)
	sum.ATR14Pct = round3(atrPct)
	sum.VolumeRatio20 = round3(volumeRatio(clean, 20))
	sum.PricePosition20 = round3(pricePosition(clean, 20))

	// 数据新鲜度：相邻 K 线时间差推断周期，年龄 > 3 个周期视为陈旧。
	if n := len(clean); n >= 2 && clean[n-1].Time > 0 && clean[n-2].Time > 0 {
		intervalMs := clean[n-1].Time - clean[n-2].Time
		if intervalMs > 0 {
			sum.IntervalSeconds = intervalMs / 1000
			ageSec := float64(now.UnixMilli()-last.Time) / 1000
			if ageSec >= 0 {
				sum.DataAgeSeconds = math.Round(ageSec*10) / 10
				sum.IsStale = ageSec > float64(intervalMs/1000)*3
			}
		}
	}
	return sum
}

func pctChange(closes []float64, n int) float64 {
	if len(closes) <= n || closes[len(closes)-n-1] <= 0 {
		return 0
	}
	return (closes[len(closes)-1]/closes[len(closes)-n-1] - 1) * 100
}

func sma(values []float64, n int) float64 {
	if len(values) < n || n <= 0 {
		return 0
	}
	sum := 0.0
	for _, v := range values[len(values)-n:] {
		sum += v
	}
	return sum / float64(n)
}

// rsi 用 Wilder 平滑（与 TradingView/Binance 口径一致）。
func rsi(closes []float64, period int) float64 {
	if len(closes) <= period {
		return 0
	}
	var gainSum, lossSum float64
	for i := 1; i <= period; i++ {
		diff := closes[i] - closes[i-1]
		if diff >= 0 {
			gainSum += diff
		} else {
			lossSum -= diff
		}
	}
	avgGain := gainSum / float64(period)
	avgLoss := lossSum / float64(period)
	for i := period + 1; i < len(closes); i++ {
		diff := closes[i] - closes[i-1]
		if diff >= 0 {
			avgGain = (avgGain*float64(period-1) + diff) / float64(period)
			avgLoss = avgLoss * float64(period-1) / float64(period)
		} else {
			avgGain = avgGain * float64(period-1) / float64(period)
			avgLoss = (avgLoss*float64(period-1) - diff) / float64(period)
		}
	}
	if avgLoss == 0 {
		if avgGain == 0 {
			return 50
		}
		return 100
	}
	rs := avgGain / avgLoss
	return 100 - 100/(1+rs)
}

func atr14(bars []Bar) (atr, atrPct float64) {
	period := 14
	if len(bars) < period+1 {
		return 0, 0
	}
	trs := make([]float64, 0, len(bars)-1)
	for i := 1; i < len(bars); i++ {
		h, l, pc := bars[i].High, bars[i].Low, bars[i-1].Close
		tr := math.Max(h-l, math.Max(math.Abs(h-pc), math.Abs(l-pc)))
		trs = append(trs, tr)
	}
	// Wilder 平滑
	atr = 0
	for i := 0; i < period; i++ {
		atr += trs[i]
	}
	atr /= float64(period)
	for i := period; i < len(trs); i++ {
		atr = (atr*float64(period-1) + trs[i]) / float64(period)
	}
	last := bars[len(bars)-1].Close
	if last > 0 {
		atrPct = atr / last * 100
	}
	return atr, atrPct
}

func volumeRatio(bars []Bar, n int) float64 {
	if len(bars) < n+1 {
		return 0
	}
	sum := 0.0
	for _, b := range bars[len(bars)-n-1 : len(bars)-1] {
		sum += b.Volume
	}
	avg := sum / float64(n)
	if avg <= 0 {
		return 0
	}
	return bars[len(bars)-1].Volume / avg
}

func pricePosition(bars []Bar, n int) float64 {
	if len(bars) < n {
		return 0
	}
	high, low := math.Inf(-1), math.Inf(1)
	for _, b := range bars[len(bars)-n:] {
		high = math.Max(high, b.High)
		low = math.Min(low, b.Low)
	}
	if high <= low {
		return 0
	}
	return (bars[len(bars)-1].Close - low) / (high - low)
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round3(v float64) float64 { return math.Round(v*1000) / 1000 }
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// ── 生产数据源 ──

// DefaultContextSources 接真实服务：K 线走行情服务（5s 超时 + 30s 缓存），
// 持仓/账户走 portfolio 全局管理器，风控运行时状态走 risk 全局管理器，
// 来源当日活跃度走 xt_orders 的 client_oid 前缀统计。
// 注意：本函数只组装闭包，不在此处触碰任何全局单例（调用时才取），
// 保证进程启动顺序无关、测试替换安全。
func DefaultContextSources() ContextSources {
	return ContextSources{
		RecentBars: func(symbol string, limit int) []Bar {
			raw, err := service.GetMarketService().FetchKlines(symbol, "1h", limit)
			if err != nil {
				return nil
			}
			bars := make([]Bar, 0, len(raw))
			for _, k := range raw {
				bars = append(bars, Bar{
					Time:   int64FromAny(k["time"]),
					Open:   floatFromAny(k["open"]),
					High:   floatFromAny(k["high"]),
					Low:    floatFromAny(k["low"]),
					Close:  floatFromAny(k["close"]),
					Volume: floatFromAny(k["volume"]),
				})
			}
			return bars
		},
		Positions: func() []PositionInfo {
			mgr := portfolio.GetManager()
			if mgr == nil {
				return nil
			}
			var out []PositionInfo
			for _, p := range mgr.GetPositions() {
				if p == nil || p.Quantity <= 0 {
					continue
				}
				out = append(out, PositionInfo{
					Symbol:        p.Symbol,
					Side:          p.Side,
					Quantity:      p.Quantity,
					EntryPrice:    p.AvgEntryPrice,
					CurrentPrice:  p.CurrentPrice,
					UnrealizedPnL: p.UnrealizedPnL,
					Leverage:      p.Leverage,
				})
			}
			return out
		},
		Account: func() AccountInfo {
			var info AccountInfo
			if mgr := portfolio.GetManager(); mgr != nil {
				info.Equity = round6(mgr.TotalEquity())
				info.Available = round6(mgr.AvailableBalance())
				info.DrawdownPct = round3(mgr.Drawdown())
				info.PositionCount = len(mgr.GetPositions())
				info.UnrealizedPnL = round6(mgr.FuturesPnL())
			}
			dailyOrders, losses, breaker := risk.GetManager().StateSnapshot()
			info.DailyOrderCount = dailyOrders
			info.ConsecutiveLosses = losses
			info.CircuitBreaker = breaker
			return info
		},
		SourceStats: func(source string) SourceStats {
			kind := SourceKind(source)
			if kind == "" || kind == "manual" || kind == "signal" {
				return SourceStats{}
			}
			dayStart := time.Now().Truncate(24 * time.Hour).UnixMilli()
			total, filled, rejected, err := store.CountOrdersByClientOIDPrefix(source, dayStart)
			if err != nil {
				return SourceStats{}
			}
			return SourceStats{TodayOrders: total, TodayFilled: filled, TodayRejected: rejected}
		},
		Now: time.Now,
	}
}

func floatFromAny(v any) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case int64:
		return float64(val)
	case int:
		return float64(val)
	}
	return 0
}

func int64FromAny(v any) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	case int:
		return int64(val)
	}
	return 0
}
