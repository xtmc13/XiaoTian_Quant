package backtest

import (
	"fmt"
	"html"
	"os"
	"time"
)

// SaveHTML writes a PerformanceReport as a standalone HTML file.
func (r *PerformanceReport) SaveHTML(path string, diagnostics *AnalysisBundle) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	start := time.UnixMilli(r.StartTime).Format("2006-01-02")
	end := time.UnixMilli(r.EndTime).Format("2006-01-02")

	var diagnosticSection string
	if diagnostics != nil {
		diagnosticSection = fmt.Sprintf(`
		<section>
			<h2>诊断分析</h2>
			<p><strong>综合结论：</strong>%s</p>
			<h3>前视偏差</h3>
			<p>疑似前视偏差：%v</p>
			<ul>%s</ul>
			<h3>递归/未来函数</h3>
			<p>疑似递归：%v</p>
			<ul>%s</ul>
			<h3>过拟合风险</h3>
			<p>风险等级：%s</p>
			<ul>%s</ul>
		</section>`,
			html.EscapeString(diagnostics.Summary),
			diagnostics.Lookahead.HasLookahead,
			listItems(diagnostics.Lookahead.Signals),
			diagnostics.Recursive.HasRecursion,
			listItems(diagnostics.Recursive.Signals),
			diagnostics.Overfit.RiskLevel,
			listItems(diagnostics.Overfit.Signals),
		)
	}

	page := fmt.Sprintf(`<!DOCTYPE html>
<html lang="zh-CN">
<head>
<meta charset="UTF-8">
<title>回测报告 %s</title>
<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 40px; background: #f5f5f5; color: #333; }
.container { max-width: 900px; margin: 0 auto; background: #fff; padding: 30px; border-radius: 8px; box-shadow: 0 2px 8px rgba(0,0,0,0.1); }
h1 { border-bottom: 2px solid #2196F3; padding-bottom: 10px; }
h2 { color: #2196F3; margin-top: 30px; }
.metric { display: flex; justify-content: space-between; padding: 8px 0; border-bottom: 1px solid #eee; }
.metric span:first-child { font-weight: 500; }
.metric span:last-child { font-family: monospace; }
section { margin-bottom: 30px; }
ul { line-height: 1.8; }
.risk-high { color: #d32f2f; }
.risk-medium { color: #f57c00; }
.risk-low { color: #388e3c; }
</style>
</head>
<body>
<div class="container">
<h1>回测报告</h1>
<p><strong>策略：</strong>%s</p>
<p><strong>交易对：</strong>%s</p>
<p><strong>周期：</strong>%s → %s</p>

<section>
<h2>收益指标</h2>
<div class="metric"><span>总收益</span><span>%.2f (%.2f%%)</span></div>
<div class="metric"><span>最大回撤</span><span>%.2f%%</span></div>
<div class="metric"><span>夏普比率</span><span>%.2f</span></div>
<div class="metric"><span>索提诺比率</span><span>%.2f</span></div>
<div class="metric"><span>卡尔玛比率</span><span>%.2f</span></div>
<div class="metric"><span>盈亏比</span><span>%.2f</span></div>
</section>

<section>
<h2>交易统计</h2>
<div class="metric"><span>总交易</span><span>%d</span></div>
<div class="metric"><span>盈利交易</span><span>%d (%.1f%%)</span></div>
<div class="metric"><span>亏损交易</span><span>%d</span></div>
<div class="metric"><span>平均盈利</span><span>%.2f</span></div>
<div class="metric"><span>平均亏损</span><span>%.2f</span></div>
<div class="metric"><span>最佳交易</span><span>%.2f</span></div>
<div class="metric"><span>最差交易</span><span>%.2f</span></div>
</section>

<section>
<h2>风险指标</h2>
<div class="metric"><span>VaR (95%%)</span><span>%.2f%%</span></div>
<div class="metric"><span>CVaR (95%%)</span><span>%.2f%%</span></div>
<div class="metric"><span>年化波动率</span><span>%.2f%%</span></div>
</section>

%s
</div>
</body>
</html>`,
		html.EscapeString(r.RunID),
		html.EscapeString(r.Strategy),
		html.EscapeString(r.Symbol),
		start, end,
		r.TotalReturn, r.TotalReturnPct,
		r.MaxDrawdownPct,
		r.SharpeRatio, r.SortinoRatio, r.CalmarRatio, r.ProfitFactor,
		r.TotalTrades, r.WinningTrades, r.WinRate, r.LosingTrades,
		r.AvgWin, r.AvgLoss, r.BestTrade, r.WorstTrade,
		r.VaR95*100, r.CVaR95*100, r.Volatility*100,
		diagnosticSection,
	)

	_, err = f.WriteString(page)
	return err
}

func listItems(items []string) string {
	if len(items) == 0 {
		return "<li>无</li>"
	}
	var sb string
	for _, item := range items {
		sb += fmt.Sprintf("<li>%s</li>", html.EscapeString(item))
	}
	return sb
}
