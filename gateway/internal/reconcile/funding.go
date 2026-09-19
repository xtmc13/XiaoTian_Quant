package reconcile

import (
	"fmt"
	"log"
	"time"

	"github.com/xiaotian-quant/gateway/internal/metrics"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// A8.3 资金费对账：周期性查交易所合约资金费流水，与本地镜像比对，
// 本地未记录的资金费收支落差异表并通知（同一笔只报一次）。

// FundingQuerier 查询交易所资金费流水（adapter 可选实现）。
type FundingQuerier interface {
	GetFundingIncomes(symbol string, startMs int64, limit int) ([]FundingIncome, error)
}

// FundingIncome 归一化资金费流水条目。
type FundingIncome struct {
	Symbol     string
	IncomeType string // FUNDING_FEE 等
	Asset      string
	Income     float64
	Time       int64  // 毫秒
	TradeID    string // 交易所侧关联 id（可能为空）
	Info       string // 原始 info 字段
}

// FundingReconciler A8.3 资金费对账任务。
type FundingReconciler struct {
	repo        *store.ReconcileRepo
	exchangeFor func(name string) any
	lookback    func() time.Duration
}

// NewFundingReconciler exchangeFor 注入带凭证的 adapter（cmd/server），返回 nil 表示未配置。
func NewFundingReconciler(repo *store.ReconcileRepo, exchangeFor func(name string) any) *FundingReconciler {
	return &FundingReconciler{repo: repo, exchangeFor: exchangeFor}
}

// Run 拉取近 lookback 小时的资金费流水并比对本地镜像。
func (r *FundingReconciler) Run() (string, error) {
	lookbackH := DefaultFundingLookback
	startMs := time.Now().Add(-time.Duration(lookbackH) * time.Hour).UnixMilli()

	newRecords := 0
	for _, exName := range liveExchangeNames {
		exAny := r.exchangeFor(exName)
		if exAny == nil {
			continue
		}
		q, ok := exAny.(FundingQuerier)
		if !ok {
			continue // 该 adapter 未实现资金费流水查询
		}
		incomes, err := q.GetFundingIncomes("", startMs, 1000)
		if err != nil {
			// 网络/权限错误只记日志，下轮重试，不误报。
			log.Printf("[reconcile] %s GetFundingIncomes 失败: %v", exName, err)
			continue
		}
		for _, inc := range incomes {
			inserted, err := r.repo.InsertFundingMirror(&store.ReconcileFundingRecord{
				Exchange:   exName,
				Symbol:     inc.Symbol,
				IncomeType: inc.IncomeType,
				Asset:      inc.Asset,
				Amount:     inc.Income,
				IncomeTime: inc.Time,
				Extra:      inc.Info,
			})
			if err != nil {
				log.Printf("[reconcile] 资金费镜像落库失败: %v", err)
				continue
			}
			if !inserted {
				continue // 本地已有记录，非差异
			}
			newRecords++
			if err := r.reportFundingDiff(exName, inc); err != nil {
				log.Printf("[reconcile] 资金费差异落表失败: %v", err)
			}
		}
	}
	return fmt.Sprintf("new_funding=%d", newRecords), nil
}

// reportFundingDiff 本地未记录的资金费收支 → 差异表 + 通知（只报一次）。
func (r *FundingReconciler) reportFundingDiff(exName string, inc FundingIncome) error {
	amount := inc.Income
	level := "INFO"
	if amount < 0 {
		level = "WARN" // 支出相对醒目
	}
	diff := &store.ReconcileDiff{
		Exchange: exName,
		Symbol:   inc.Symbol,
		DiffType: "funding",
		Asset:    inc.Asset,
		Amount:   amount,
		Detail: fmt.Sprintf("资金费%s %.8f %s (income_time=%d %s)",
			map[bool]string{true: "收入", false: "支出"}[amount >= 0], amount, inc.Asset, inc.Time, inc.IncomeType),
	}
	id, created, err := r.repo.CreateDiff(diff)
	if err != nil {
		return err
	}
	if !created {
		return nil // open 差异已存在
	}
	metrics.RecordReconcileDiff(diff.DiffType, exName)
	notifyDiff(
		fmt.Sprintf("资金费对账: %s %s", exName, inc.Symbol),
		diff.Detail, level, "funding", "funding_diff",
		map[string]any{"diff_id": id, "exchange": exName, "symbol": inc.Symbol, "amount": amount},
	)
	return nil
}
