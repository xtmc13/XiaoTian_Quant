package marketplace

import (
	"fmt"
	"time"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/grid"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 回测对照（回测 vs 实盘双曲线透明化）──
// grid_bot 条目：用与实盘 runner 同一网格引擎（grid.BotEngine）在实盘考核
// 窗口同期的历史 K 线上重放，产出回测累计收益曲线，与平台快照的实盘曲线
// 叠加展示，直接暴露回测/实盘口径偏差。
//
// 口径假设（随响应 backtest_assumptions/backtest_note 披露）：
//   - 等差网格、限价单按档位价成交，无滑点；
//   - 手续费率取市场统一默认（backtest.DefaultRunnerConfig().Commission，
//     0.1%），不用机器人自身费率，保证条目间可比；
//   - K 线内价格路径按 开→低→高→收（阳线）/ 开→高→低→收（阴线）近似撮合；
//   - 回测从窗口内价格首次进入网格区间处按配置全新启动（含初始建仓），
//     实盘曲线是窗口起点既有持仓的延续，两侧起点口径存在差异；
//   - 窗口跨度超过 GridBacktestMaxWindowDays 时只回测最近 N 天（truncated）。
//
// ai_bot 条目：AI 信号由模型实时生成，历史行情无法确定性重放，不提供对照
// （stats 端点返回 backtest_series=null + backtest_note 说明）。

const (
	// GridBacktestInterval 回测对照 K 线粒度（90 天窗口 ≈ 2160 根，重放耗时可忽略）。
	GridBacktestInterval = "1h"
	// GridBacktestMaxWindowDays 回测区间上限（天），超出截断保 stats 端点稳定。
	GridBacktestMaxWindowDays = 90
	// gridBacktestMinBars 生成对照曲线所需的最少 K 线数。
	gridBacktestMinBars = 2
	// gridBacktestCacheStrategy 回测对照在全局 ResultCache 中的策略名命名空间。
	gridBacktestCacheStrategy = "market_grid_compare"
)

// GridBacktestPoint 回测权益曲线点（时间毫秒 + 累计收益%，基准=条目初始权益，
// 与实盘 series 的 total_return_pct 同口径，可直接叠加对比）。
type GridBacktestPoint struct {
	Time      int64   `json:"time"`
	ReturnPct float64 `json:"return_pct"`
}

// GridBacktestCompare 回测对照结果。
type GridBacktestCompare struct {
	Series         []GridBacktestPoint
	TotalReturnPct float64
	WindowDays     int
	Truncated      bool
	Interval       string
	FeeRate        float64
	Bars           int
}

// GridBacktestWindow 计算回测窗口 [fromMs, toMs]：实盘考核窗口同期，
// toMs 截断到当日 UTC 零点（缓存键日内稳定），跨度超上限时截断 fromMs。
func GridBacktestWindow(l *store.MarketListingRecord, bot *store.GridBotRecord, now time.Time) (fromMs, toMs int64, truncated bool) {
	since := l.ProbationStartedAt
	if since <= 0 {
		since = bot.CreatedAt / 1000
	}
	fromMs = since * 1000
	y, m, d := now.UTC().Date()
	toMs = time.Date(y, m, d, 0, 0, 0, 0, time.UTC).UnixMilli()
	if maxSpan := int64(GridBacktestMaxWindowDays) * 86400_000; toMs-fromMs > maxSpan {
		fromMs = toMs - maxSpan
		truncated = true
	}
	return fromMs, toMs, truncated
}

// RunGridBacktest 在给定 K 线上重放网格引擎，产出对照曲线；K 线不足、窗口内
// 价格未进入网格区间、配置非法等均返回 error（调用方降级为 backtest_error，
// 绝不编造曲线）。结果按 (listing + 窗口 + 配置) 缓存（复用 backtest 全局
// ResultCache，TTL 24h；error 不缓存）。
func RunGridBacktest(l *store.MarketListingRecord, bot *store.GridBotRecord, bars []model.Bar, fromMs, toMs int64, truncated bool) (*GridBacktestCompare, error) {
	if len(bars) < gridBacktestMinBars {
		return nil, fmt.Errorf("本地 %s %s K线数据不足（%d 根），无法生成回测对照", bot.Symbol, GridBacktestInterval, len(bars))
	}
	feeRate := backtest.DefaultRunnerConfig().Commission
	base := bot.InitialEquity
	if base <= 0 {
		base = bot.Investment
	}
	if base <= 0 {
		base = 10000
	}
	params := map[string]any{
		"listing_id": l.ID, "from_ms": fromMs, "to_ms": toMs,
		"symbol": bot.Symbol, "lower": bot.LowerPrice, "upper": bot.UpperPrice,
		"grid_count": bot.GridCount, "investment": bot.Investment,
		"mode": string(grid.ParseMode(bot.Mode)), "fee_rate": feeRate, "base": base,
	}
	cache := backtest.GetGlobalCache()
	if cached, _, ok := cache.Get(gridBacktestCacheStrategy, bot.Symbol, GridBacktestInterval, params); ok && cached != nil && len(cached.EquityCurve) > 0 {
		return buildGridCompare(cached, base, fromMs, toMs, truncated, feeRate, len(bars)), nil
	}

	equity, err := replayGridEngine(bot, bars, feeRate)
	if err != nil {
		return nil, err
	}
	res := backtest.MetricsFromEquity(equity, base, 0.02, nil, 0)
	cache.Set(gridBacktestCacheStrategy, bot.Symbol, GridBacktestInterval, params, res, nil)
	return buildGridCompare(res, base, fromMs, toMs, truncated, feeRate, len(bars)), nil
}

// buildGridCompare 把引擎重放的权益曲线（计价币绝对值）归一化为累计收益%序列。
func buildGridCompare(res *backtest.RunResult, base float64, fromMs, toMs int64, truncated bool, feeRate float64, bars int) *GridBacktestCompare {
	series := make([]GridBacktestPoint, 0, len(res.EquityCurve))
	for _, pt := range res.EquityCurve {
		series = append(series, GridBacktestPoint{
			Time:      pt.Timestamp,
			ReturnPct: store.RoundFloat((pt.Equity/base-1)*100, 4),
		})
	}
	return &GridBacktestCompare{
		Series:         series,
		TotalReturnPct: store.RoundFloat(res.TotalReturnPct, 4),
		WindowDays:     int((toMs - fromMs) / 86400_000),
		Truncated:      truncated,
		Interval:       GridBacktestInterval,
		FeeRate:        feeRate,
		Bars:           bars,
	}
}

// replayGridEngine 以窗口内首根收盘价在网格区间内的 K 线启动引擎（与实盘
// StartBot 要求启动价在区间内一致），逐根按 K 线内路径喂价撮合，
// 每根收盘取一次权益快照（与实盘 Snapshot 口径一致）。
func replayGridEngine(bot *store.GridBotRecord, bars []model.Bar, feeRate float64) ([]backtest.EquityPoint, error) {
	start := -1
	for i := range bars {
		if bars[i].Close >= bot.LowerPrice && bars[i].Close <= bot.UpperPrice {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, fmt.Errorf("窗口内 %s 价格未进入网格区间 [%v, %v]，无回测对照", bot.Symbol, bot.LowerPrice, bot.UpperPrice)
	}
	eng, _, err := grid.NewBotEngine(grid.Config{
		Symbol:       bot.Symbol,
		Lower:        bot.LowerPrice,
		Upper:        bot.UpperPrice,
		GridCount:    bot.GridCount,
		Investment:   bot.Investment,
		FeeRate:      feeRate,
		CurrentPrice: bars[start].Close,
	}, grid.ParseMode(bot.Mode))
	if err != nil {
		return nil, fmt.Errorf("grid backtest: %w", err)
	}
	equity := make([]backtest.EquityPoint, 0, len(bars)-start)
	for i := start; i < len(bars); i++ {
		b := bars[i]
		for _, p := range gridBarPath(b) {
			eng.OnPriceTick(p, b.Time)
		}
		eq, _, _ := eng.Snapshot(b.Close, b.Time)
		equity = append(equity, backtest.EquityPoint{Timestamp: b.Time, Equity: eq})
	}
	if len(equity) == 0 {
		return nil, fmt.Errorf("grid backtest: empty equity")
	}
	return equity, nil
}

// gridBarPath K 线内价格路径近似：阳线按 开→低→高→收，阴线按 开→高→低→收；
// 去除相邻重复价位（同价重喂对挂单簿无意义）。
func gridBarPath(b model.Bar) []float64 {
	path := make([]float64, 0, 4)
	push := func(p float64) {
		if p <= 0 {
			return
		}
		if n := len(path); n > 0 && path[n-1] == p {
			return
		}
		path = append(path, p)
	}
	push(b.Open)
	if b.Close >= b.Open {
		push(b.Low)
		push(b.High)
	} else {
		push(b.High)
		push(b.Low)
	}
	push(b.Close)
	return path
}
