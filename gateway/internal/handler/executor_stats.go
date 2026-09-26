package handler

import (
	"net/http"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 信号统计端点（GET /executor/stats）──
//
// 数据来源：SignalRepo（信号计数/频率）+ xt_signal_executions
// （成功率/达成率/盈亏/曲线/分组）。核心聚合是纯函数
// computeExecutorStats，造数据即可单测，不依赖 HTTP。

type executorDailyPoint struct {
	Date    string  `json:"date"`
	PnL     float64 `json:"pnl"`
	Signals int     `json:"signals"`
}

type executorSourceStat struct {
	SourceID   string  `json:"source_id"`
	SourceName string  `json:"source_name"`
	Signals    int     `json:"signals"`
	Closed     int     `json:"closed"`
	Wins       int     `json:"wins"`
	CumPnL     float64 `json:"cum_pnl"`
}

type executorSymbolStat struct {
	Symbol      string  `json:"symbol"`
	Signals     int     `json:"signals"`
	Closed      int     `json:"closed"`
	Wins        int     `json:"wins"`
	CumPnL      float64 `json:"cum_pnl"`
	PnL         float64 `json:"pnl"` // 兼容别名（前端契约）
	WinRate     float64 `json:"win_rate"`
	SuccessRate float64 `json:"success_rate"`
}

type executorStatsData struct {
	TotalSignals     int                  `json:"total_signals"`
	TodaySignals     int                  `json:"today_signals"`
	Executed         int                  `json:"executed"`
	Closed           int                  `json:"closed"`
	Wins             int                  `json:"wins"`
	SuccessRate      float64              `json:"success_rate"` // 平仓中盈利比例
	Tp1Rate          float64              `json:"tp1_rate"`     // 各档达成率（占平仓数）
	Tp2Rate          float64              `json:"tp2_rate"`
	Tp3Rate          float64              `json:"tp3_rate"`
	SlRate           float64              `json:"sl_rate"`
	AvgSignalsPerDay float64              `json:"avg_signals_per_day"`
	CumPnL           float64              `json:"cum_pnl"`
	Daily            []executorDailyPoint `json:"daily"`     // 近 30 日盈亏曲线
	TotalPnL         float64              `json:"total_pnl"` // 兼容别名（前端契约）
	PnlCurve         []executorDailyPoint `json:"pnl_curve"` // 兼容别名（前端契约）
	BySource         []executorSourceStat `json:"by_source"` // 按来源分组
	BySymbol         []executorSymbolStat `json:"by_symbol"` // 按 symbol 分组（?symbol= 过滤）
}

func dayKey(ms int64) string {
	return time.UnixMilli(ms).Format("2006-01-02")
}

// computeExecutorStats 聚合信号与执行记录。
// signals/exec 需为时间升序；sourceNames 为信号源 id→名称。
// 近 30 日曲线窗口固定为调用当日往前 30 天。
func computeExecutorStats(signals []*store.SignalRecord, execs []*store.SignalExecution, sourceNames map[string]string) executorStatsData {
	st := executorStatsData{Daily: []executorDailyPoint{}, BySource: []executorSourceStat{}, BySymbol: []executorSymbolStat{}}
	st.TotalSignals = len(signals)
	dayStart := todayStartMs()
	firstDay := int64(0)
	dailySignals := map[string]int{}
	srcSignals := map[string]int{}
	symSignals := map[string]int{}
	for _, s := range signals {
		if s.CreatedAt >= dayStart {
			st.TodaySignals++
		}
		if firstDay == 0 || s.CreatedAt < firstDay {
			firstDay = s.CreatedAt
		}
		dailySignals[dayKey(s.CreatedAt)]++
		srcSignals[s.SourceID]++
		symSignals[s.Symbol]++
	}
	if firstDay > 0 {
		days := int64(time.Since(time.UnixMilli(firstDay)).Hours()/24) + 1
		if days > 0 {
			st.AvgSignalsPerDay = float64(st.TotalSignals) / float64(days)
		}
	}

	// ── 执行记录：成功率 / 达成率 / 盈亏 / 曲线 ──
	dailyPnL := map[string]float64{}
	srcClosed := map[string]int{}
	srcWins := map[string]int{}
	srcPnL := map[string]float64{}
	symClosed := map[string]int{}
	symWins := map[string]int{}
	symPnL := map[string]float64{}
	for _, e := range execs {
		if e.Status == "active" {
			st.Executed++
			continue
		}
		st.Closed++
		st.CumPnL += e.RealizedPnL
		dailyPnL[dayKey(e.ClosedAt)] += e.RealizedPnL
		srcClosed[e.SourceID]++
		srcPnL[e.SourceID] += e.RealizedPnL
		symClosed[e.Symbol]++
		symPnL[e.Symbol] += e.RealizedPnL
		if e.RealizedPnL > 0 {
			st.Wins++
			srcWins[e.SourceID]++
			symWins[e.Symbol]++
		}
		if e.TP1Filled {
			st.Tp1Rate++
		}
		if e.TP2Filled {
			st.Tp2Rate++
		}
		if e.TP3Filled {
			st.Tp3Rate++
		}
		if e.SLTriggered {
			st.SlRate++
		}
	}
	if st.Closed > 0 {
		st.SuccessRate = float64(st.Wins) / float64(st.Closed) * 100
		st.Tp1Rate = st.Tp1Rate / float64(st.Closed) * 100
		st.Tp2Rate = st.Tp2Rate / float64(st.Closed) * 100
		st.Tp3Rate = st.Tp3Rate / float64(st.Closed) * 100
		st.SlRate = st.SlRate / float64(st.Closed) * 100
	}

	// ── 近 30 日曲线（窗口内每天都有点，无数据补零）──
	now := time.Now()
	for i := 29; i >= 0; i-- {
		d := now.AddDate(0, 0, -i)
		key := d.Format("2006-01-02")
		st.Daily = append(st.Daily, executorDailyPoint{Date: key, PnL: dailyPnL[key], Signals: dailySignals[key]})
	}

	// ── 来源分组 ──
	for id, n := range srcSignals {
		name := sourceNames[id]
		if name == "" {
			name = id
		}
		st.BySource = append(st.BySource, executorSourceStat{
			SourceID: id, SourceName: name, Signals: n,
			Closed: srcClosed[id], Wins: srcWins[id], CumPnL: srcPnL[id],
		})
	}
	sort.Slice(st.BySource, func(i, j int) bool { return st.BySource[i].Signals > st.BySource[j].Signals })

	// ── symbol 分组 ──
	for sym, n := range symSignals {
		closed := symClosed[sym]
		stat := executorSymbolStat{
			Symbol: sym, Signals: n, Closed: closed,
			Wins: symWins[sym], CumPnL: symPnL[sym], PnL: symPnL[sym],
		}
		if closed > 0 {
			stat.WinRate = float64(stat.Wins) / float64(closed) * 100
			stat.SuccessRate = stat.WinRate
		}
		st.BySymbol = append(st.BySymbol, stat)
	}
	sort.Slice(st.BySymbol, func(i, j int) bool { return st.BySymbol[i].Signals > st.BySymbol[j].Signals })

	// 前端契约别名字段
	st.TotalPnL = st.CumPnL
	st.PnlCurve = st.Daily

	return st
}

// ExecutorStats godoc
// GET /executor/stats
// 信号统计总览：总数/今日数、成功率、T1/T2/T3 达成率、日均频率、
// 累计盈亏、近 30 日曲线、按来源与 symbol 分组。
// 可选 query：symbol=BTCUSDT 时 BySymbol 只保留该币种。
func ExecutorStats(c *gin.Context) {
	sigRepo := store.NewSignalRepo()
	execRepo := store.NewSignalExecutionRepo()

	signals, err := sigRepo.ListSince(0, 0)
	if err != nil {
		signals = nil
	}
	execs, err := execRepo.ListSince(0, 0)
	if err != nil {
		execs = nil
	}

	sourceNames := map[string]string{}
	if srcs, err := store.NewSignalSourceRepo().List(false); err == nil {
		for _, s := range srcs {
			sourceNames[s.ID] = s.Name
		}
	}

	data := computeExecutorStats(signals, execs, sourceNames)

	if sym := c.Query("symbol"); sym != "" {
		filtered := []executorSymbolStat{}
		for _, s := range data.BySymbol {
			if s.Symbol == sym {
				filtered = append(filtered, s)
			}
		}
		data.BySymbol = filtered
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}
