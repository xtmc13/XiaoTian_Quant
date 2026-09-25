package handler

import (
	"log"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// 权益曲线异常点过滤（2026-09-16 权益改真实数据后，旧 10 万假 paper 种子
// 残留的快照会让曲线出现一次性陡降）：
//
// 相邻快照变动超过阈值（默认单日 50%，EQUITY_OUTLIER_PCT 百分数可调）且
// 窗口内无任何成交记录 → 记为"断裂"，把曲线切成若干段；短于相邻段的整段
// 视为假数据/口径切换残留剔除并记日志（平局剔除较旧一侧——最新快照与组合
// 当前权益一致，是最可信锚点）。有成交解释的真实波动（爆仓/大胜）一律保留。
// 注意口径：入金/出金没有成交记录，超过阈值的出入金会被误剔（可观测、可调阈值）。

func filterEquityOutliers(snapshots []model.PortfolioSnapshot) []model.PortfolioSnapshot {
	if len(snapshots) < 2 || store.GetDB() == nil {
		return snapshots
	}
	threshold := equityOutlierThreshold()
	trades := store.NewTradeRepo()
	pts := append([]model.PortfolioSnapshot(nil), snapshots...)
	for iter := 0; iter <= len(snapshots); iter++ {
		segs := splitEquitySegments(pts, threshold, trades)
		if len(segs) <= 1 {
			return pts
		}
		idx := pickOutlierSegment(segs)
		if idx < 0 {
			return pts
		}
		drop := segs[idx]
		level := pts[drop.start].TotalEquity
		neighbor := 0.0
		if idx+1 < len(segs) {
			neighbor = pts[segs[idx+1].start].TotalEquity
		} else {
			neighbor = pts[segs[idx-1].end-1].TotalEquity
		}
		log.Printf("[equity] 剔除权益曲线异常段 %d 点（level≈%.4f @%s，相邻段 level≈%.4f，变动超阈值且窗口无成交）",
			drop.end-drop.start, level,
			time.UnixMilli(pts[drop.start].Timestamp).Format(time.RFC3339), neighbor)
		pts = append(pts[:drop.start], pts[drop.end:]...)
	}
	return pts
}

func equityOutlierThreshold() float64 {
	if v := os.Getenv("EQUITY_OUTLIER_PCT"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f / 100
		}
	}
	return 0.5
}

type eqSegment struct{ start, end int } // pts 下标 [start, end)

func (s eqSegment) len() int { return s.end - s.start }

// splitEquitySegments 在"断裂"处切分曲线：相邻点变动超阈值且窗口无成交。
func splitEquitySegments(pts []model.PortfolioSnapshot, threshold float64, trades *store.TradeRepo) []eqSegment {
	segs := make([]eqSegment, 0, 2)
	start := 0
	for i := 1; i < len(pts); i++ {
		if isEquityBreak(pts[i-1], pts[i], threshold, trades) {
			segs = append(segs, eqSegment{start, i})
			start = i
		}
	}
	return append(segs, eqSegment{start, len(pts)})
}

// pickOutlierSegment 选出应剔除的假数据段：内部段须严格短于两侧邻居
// （尖峰特征）；边缘段不严格短于唯一邻居即可（平局剔除较旧一侧）。
func pickOutlierSegment(segs []eqSegment) int {
	best, bestLen := -1, 0
	for i, s := range segs {
		l := s.len()
		var droppable bool
		switch i {
		case 0:
			droppable = l <= segs[1].len()
		case len(segs) - 1:
			droppable = l <= segs[i-1].len()
		default:
			droppable = l < segs[i-1].len() && l < segs[i+1].len()
		}
		if droppable && (best < 0 || l < bestLen) {
			best, bestLen = i, l
		}
	}
	return best
}

// isEquityBreak 判断相邻点是否构成"无成交解释的超阈值变动"。
// 间隔超过一天时按单日均值归一（渐变跨天不误杀）。
func isEquityBreak(prev, cur model.PortfolioSnapshot, threshold float64, trades *store.TradeRepo) bool {
	if prev.TotalEquity <= 0 {
		return false // 基线为 0 变动率无意义，不误杀
	}
	change := math.Abs(cur.TotalEquity-prev.TotalEquity) / prev.TotalEquity
	gapDays := float64(cur.Timestamp-prev.Timestamp) / 86_400_000
	if gapDays < 1 {
		gapDays = 1
	}
	if change/gapDays < threshold {
		return false
	}
	return trades.CountBetween(prev.Timestamp, cur.Timestamp) == 0
}
