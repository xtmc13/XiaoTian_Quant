package handler

import (
	"fmt"
	"testing"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

func mkSnaps(equities []float64, startMs, stepMs int64) []model.PortfolioSnapshot {
	out := make([]model.PortfolioSnapshot, len(equities))
	for i, e := range equities {
		out[i] = model.PortfolioSnapshot{TotalEquity: e, Timestamp: startMs + int64(i)*stepMs}
	}
	return out
}

func equitiesOf(snaps []model.PortfolioSnapshot) []float64 {
	out := make([]float64, len(snaps))
	for i, s := range snaps {
		out[i] = s.TotalEquity
	}
	return out
}

// 旧假数据前缀（10 万假种子）→ 口径切换后真实权益：假数据段整段剔除，陡降消失。
func TestEquityOutlierFilterDropsFakePrefix(t *testing.T) {
	base := time.Now().UnixMilli() - 3600_000
	snaps := mkSnaps([]float64{100000, 100001, 100002, 0.2066, 0.21, 0.22}, base, 60_000)
	got := equitiesOf(filterEquityOutliers(snaps))
	want := []float64{0.2066, 0.21, 0.22}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("假数据前缀应整段剔除: got %v want %v", got, want)
	}
}

// 末尾突变假点（如种子配置时序 bug 复活）同样剔除。
func TestEquityOutlierFilterDropsFakeSuffix(t *testing.T) {
	base := time.Now().UnixMilli() - 3600_000
	snaps := mkSnaps([]float64{0.20, 0.21, 0.22, 0.21, 100000, 100001}, base, 60_000)
	got := equitiesOf(filterEquityOutliers(snaps))
	want := []float64{0.20, 0.21, 0.22, 0.21}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("假数据后缀应整段剔除: got %v want %v", got, want)
	}
}

// 中段尖峰（短段夹在两段真实数据之间）剔除中段。
func TestEquityOutlierFilterDropsSpike(t *testing.T) {
	base := time.Now().UnixMilli() - 3600_000
	snaps := mkSnaps([]float64{100, 101, 100, 100000, 100001, 100, 101, 102}, base, 60_000)
	got := equitiesOf(filterEquityOutliers(snaps))
	want := []float64{100, 101, 100, 100, 101, 102}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("中段尖峰应剔除: got %v want %v", got, want)
	}
}

// 有成交记录解释的大幅波动是真实波动（爆仓/大胜），必须保留。
func TestEquityOutlierFilterKeepsRealCrashWithTrades(t *testing.T) {
	base := time.Now().UnixMilli() - 3600_000
	snaps := mkSnaps([]float64{1000, 1001, 1002, 200, 201, 202}, base, 60_000)
	// 在 1002→200 的窗口内落一笔成交。
	tr := store.NewTradeRepo()
	if err := tr.Create(&store.TradeRecord{
		ID: "eqfilter-real-crash", Symbol: "BTCUSDT", Side: "SELL",
		Price: 50000, Quantity: 0.1, Exchange: "paper",
		CreatedAt: base + 2*60_000 + 1000,
	}); err != nil {
		t.Fatalf("seed trade: %v", err)
	}
	got := equitiesOf(filterEquityOutliers(snaps))
	if len(got) != 6 {
		t.Fatalf("有成交解释的真实波动不得剔除: got %v", got)
	}
}

// 正常波动的曲线原样保留。
func TestEquityOutlierFilterKeepsNormalCurve(t *testing.T) {
	base := time.Now().UnixMilli() - 3600_000
	snaps := mkSnaps([]float64{1000, 1001, 999, 1002, 1003}, base, 60_000)
	got := filterEquityOutliers(snaps)
	if len(got) != len(snaps) {
		t.Fatalf("正常曲线不得改动: got %v", equitiesOf(got))
	}
}

// 两点歧义时按"旧假数据"先验剔除较旧一侧（最新快照与组合当前权益一致，
// 是最可信锚点）。
func TestEquityOutlierFilterTwoPointsDropsStalePrefix(t *testing.T) {
	base := time.Now().UnixMilli() - 120_000
	snaps := mkSnaps([]float64{100000, 0.2}, base, 60_000)
	got := equitiesOf(filterEquityOutliers(snaps))
	if len(got) != 1 || got[0] != 0.2 {
		t.Fatalf("两点歧义应剔除较旧的假数据侧: got %v", got)
	}
}

// 跨天长窗口的渐变不按单日口径误杀（每天变动未超阈值）。
func TestEquityOutlierFilterLongGapGradual(t *testing.T) {
	base := time.Now().UnixMilli() - 30*24*3600_000
	dayMs := int64(24 * 3600_000)
	// 30 天内从 1000 渐变到 400：单日变动 ~2%，总变动 60%，不得误杀。
	snaps := mkSnaps([]float64{1000, 900, 800, 700, 600, 500, 400}, base, 5*dayMs)
	if got := filterEquityOutliers(snaps); len(got) != len(snaps) {
		t.Fatalf("跨天渐变不得误杀: got %v", equitiesOf(got))
	}
}
