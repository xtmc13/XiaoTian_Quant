package handler

import (
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/marketplace"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 条目 stats 回测对照（回测 vs 实盘双曲线透明化）──
// grid_bot：同一网格引擎在考核窗口同期 K 线上重放（marketplace.RunGridBacktest），
// 产出 backtest_series + deviation；K 线只走本地存储（DataDownloader），不足/失败
// 降级为 backtest_series=null + backtest_error，绝不编造曲线，端点不因此 500。
// ai_bot：AI 信号驱动无确定性回测对照，backtest_series=null + backtest_note 说明。

// aiBotBacktestNote AI 信号驱动条目无回测对照的如实说明。
const aiBotBacktestNote = "AI 信号驱动策略无回测对照：信号由模型实时生成，历史行情无法确定性重放"

// attachBacktestCompare 把回测对照字段挂到 stats 响应（按 object_kind 分派）。
func attachBacktestCompare(resp gin.H, l *store.MarketListingRecord, series []*store.MarketListingStatsRecord) {
	if l.ObjectKind != marketplace.ObjectKindGridBot {
		resp["backtest_series"] = nil
		resp["backtest_note"] = aiBotBacktestNote
		return
	}
	attachGridBacktestCompare(resp, l, series)
}

func attachGridBacktestCompare(resp gin.H, l *store.MarketListingRecord, series []*store.MarketListingStatsRecord) {
	fail := func(msg string) {
		resp["backtest_series"] = nil
		resp["backtest_error"] = msg
	}
	bot, err := gridBotRepo.GetByID(l.ObjectID)
	if err != nil || bot == nil {
		fail("源网格机器人不存在或已删除，无回测对照")
		return
	}
	fromMs, toMs, truncated := marketplace.GridBacktestWindow(l, bot, time.Now())
	if toMs <= fromMs {
		fail("考核窗口不足一个完整交易日，暂无回测对照")
		return
	}
	var bars []model.Bar
	if DataDownloader != nil {
		bars = DataDownloader.LoadBarsForBacktest(bot.Symbol, marketplace.GridBacktestInterval, fromMs, toMs)
	}
	cmp, err := marketplace.RunGridBacktest(l, bot, bars, fromMs, toMs, truncated)
	if err != nil {
		fail(err.Error())
		return
	}
	resp["backtest_series"] = cmp.Series
	resp["backtest_assumptions"] = gin.H{
		"interval":    cmp.Interval,
		"fee_rate":    cmp.FeeRate,
		"slippage":    0,
		"window_days": cmp.WindowDays,
		"truncated":   cmp.Truncated,
		"bars":        cmp.Bars,
	}
	note := fmt.Sprintf("回测对照：同一网格引擎在考核窗口同期 %s K线上重放（等差网格限价成交、手续费 %.1f%%、无滑点；K线内路径按开→低→高→收/开→高→低→收近似；窗口起点全新建仓，不含实盘撮合延迟与流动性约束）",
		cmp.Interval, cmp.FeeRate*100)
	if cmp.Truncated {
		note = fmt.Sprintf("考核窗口超过 %d 天，回测仅覆盖最近 %d 天；", marketplace.GridBacktestMaxWindowDays, cmp.WindowDays) + note
	}
	resp["backtest_note"] = note
	// 偏差指标：实盘最新累计收益 vs 回测累计收益（百分点）；两侧都有数据才下发。
	if len(series) > 0 && len(cmp.Series) > 0 {
		live := series[len(series)-1].TotalReturnPct
		diff := store.RoundFloat(live-cmp.TotalReturnPct, 4)
		direction := "flat"
		if diff > 0 {
			direction = "higher"
		} else if diff < 0 {
			direction = "lower"
		}
		resp["deviation"] = gin.H{
			"live_return_pct":     live,
			"backtest_return_pct": cmp.TotalReturnPct,
			"diff_pct":            diff,
			"direction":           direction,
		}
	}
}
