package social

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"
)

// ── 利润分成结算引擎 ─────────────────────────────────────────────
//
// 对标 CryptoRobotics 的核心商业模式：provider 免费/低价入驻，平台按跟单盈利
// 抽成（profit_share_pct，10%-30%）。结算按自然日窗口跑批：
//
//   - 对每个 profit_share/hybrid 的 active 订阅，汇总该 follower 当日归因到该
//     provider 的已实现跟单盈亏（xt_social_copy_pnl，由跟单执行层平仓时写入）。
//   - copied_pnl > 0：share_amount = pnl × pct%，写快照 status=pending，
//     locked_until = 窗口日结束 + 3 天（T+3 冲抵保护期）。
//   - copied_pnl < 0：回撤冲抵——把该 follower 对该 provider 的 pending 快照
//     按窗口日由旧到新整单 void，直到亏损失效金额被完全冲抵。
//     简化规则：只冲抵未 locked 的 pending，locked/payable/paid 不追溯；
//     不做部分冲抵，未覆盖的亏损不向后结转。
//   - 状态机：pending →（locked_until 到期）locked →（+payableDelay）payable
//     →（提现打款时由提现流水扣减，快照本身不再流转）；
//     void 为冲抵终态。payable 余额 = Σ payable 快照 − 在途/已付提现（实时汇总，不落总账）。
//
// 调度：Start/Stop 生命周期与 reconcile.Service 对齐（cmd/server/main.go 集成），
// 每 interval 跑一轮 RunDue：补齐未结算窗口 → 到期状态迁移。

const (
	// DefaultSettleInterval 结算轮询周期。
	DefaultSettleInterval = time.Hour
	// DefaultLockWindow T+3 冲抵保护期（locked_until = 窗口日结束 + 72h）。
	DefaultLockWindow = 72 * time.Hour
	// DefaultPayableDelay locked → payable 的放款准备期（额外 24h）。
	DefaultPayableDelay = 24 * time.Hour
	// MaxBackfillDays 单次补齐未结算窗口的最大天数（防失控回填）。
	MaxBackfillDays = 31
)

// SettlementEngine 每日利润分成结算器。
type SettlementEngine struct {
	market *MarketService

	interval     time.Duration
	lockWindow   time.Duration
	payableDelay time.Duration

	logf func(format string, args ...any)

	mu             sync.Mutex
	running        bool
	lastSettledDay string // 内存态：最近已结算窗口日（重启后从快照表恢复）
	stopCh         chan struct{}
	doneCh         chan struct{}
}

// NewSettlementEngine 组装结算引擎；nil market 视为无 DB（轮询空转，测试可用）。
func NewSettlementEngine(market *MarketService) *SettlementEngine {
	e := &SettlementEngine{
		market:       market,
		interval:     DefaultSettleInterval,
		lockWindow:   DefaultLockWindow,
		payableDelay: DefaultPayableDelay,
		logf:         log.Printf,
	}
	if v := os.Getenv("SOCIAL_SETTLE_INTERVAL_SEC"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			e.interval = time.Duration(n) * time.Second
		}
	}
	if v := os.Getenv("SOCIAL_PROFIT_PAYABLE_DELAY_H"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			e.payableDelay = time.Duration(n) * time.Hour
		}
	}
	return e
}

// SetLogf 覆盖日志输出（测试注入捕获用）。
func (e *SettlementEngine) SetLogf(fn func(format string, args ...any)) {
	if fn != nil {
		e.logf = fn
	}
}

// Start 启动调度循环（立即跑一轮，之后按周期跑），进程退出前调 Stop。
func (e *SettlementEngine) Start() {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.doneCh = make(chan struct{})
	e.mu.Unlock()
	go e.loop()
}

// Stop 优雅停止：等当前轮跑完。
func (e *SettlementEngine) Stop() {
	e.mu.Lock()
	if !e.running {
		e.mu.Unlock()
		return
	}
	e.running = false
	close(e.stopCh)
	done := e.doneCh
	e.mu.Unlock()
	<-done
}

// IsRunning 调度循环是否存活。
func (e *SettlementEngine) IsRunning() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.running
}

func (e *SettlementEngine) loop() {
	defer close(e.doneCh)
	if err := e.RunDue(time.Now()); err != nil {
		e.logf("[social-settle] initial run: %v", err)
	}
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case now := <-ticker.C:
			if err := e.RunDue(now); err != nil {
				e.logf("[social-settle] run: %v", err)
			}
		}
	}
}

// RunDue 结算到昨日为止的全部未结算窗口，并做到期状态迁移。
// 重启恢复：lastSettledDay 取内存值，缺失时从快照表 MAX(window_date) 恢复，
// 仍无记录则以"前天"为基线，首轮把昨日窗口结掉（不做更早的历史回填）。
func (e *SettlementEngine) RunDue(now time.Time) error {
	if e.market == nil {
		return nil
	}
	yesterdayStart := now.Add(-24 * time.Hour)

	e.mu.Lock()
	last := e.lastSettledDay
	e.mu.Unlock()
	if last == "" {
		if d, ok, err := e.market.LatestSnapshotDate(); err == nil && ok {
			last = d
		} else {
			// 无结算历史：基线设为前天，首轮把昨日窗口结掉（不做更早的历史回填）。
			last = now.Add(-48 * time.Hour).Format("2006-01-02")
		}
	}

	start, err := time.ParseInLocation("2006-01-02", last, time.Local)
	if err != nil {
		return err
	}
	start = start.Add(24 * time.Hour) // 从 last 之后一天开始

	for d := start; !d.After(yesterdayStart); d = d.Add(24 * time.Hour) {
		day := d.Format("2006-01-02")
		if err := e.SettleWindow(day); err != nil {
			return fmt.Errorf("settle %s: %w", day, err)
		}
		e.mu.Lock()
		e.lastSettledDay = day
		e.mu.Unlock()
	}

	_, _, err = e.Sweep(now)
	return err
}

// SettleWindow 结算单个自然日窗口（YYYY-MM-DD，本地时区）。
func (e *SettlementEngine) SettleWindow(day string) error {
	if e.market == nil {
		return nil
	}
	dayStart, err := time.ParseInLocation("2006-01-02", day, time.Local)
	if err != nil {
		return fmt.Errorf("bad window date %q: %w", day, err)
	}
	dayEnd := dayStart.Add(24 * time.Hour)
	now := time.Now()

	subs, err := e.market.ListActiveShareSubscriptions()
	if err != nil {
		return err
	}

	// provider 属主 → 当日应付合计：只进内存/日志，不落总账
	// （提现可用额由 Earnings() 基于快照 + 提现流水实时汇总）。
	dailyTotals := make(map[int64]float64)
	var settled, offsetCount int

	for _, sub := range subs {
		pnl, cnt, err := e.market.SumCopyPnL(sub.ProviderID, sub.FollowerUserID,
			dayStart.UnixMilli(), dayEnd.UnixMilli())
		if err != nil {
			return err
		}
		if cnt == 0 {
			continue // 当日无归因盈亏记录，不产生快照
		}
		switch {
		case pnl > 0:
			share := pnl * sub.ProfitSharePct / 100
			snap := &ProfitSnapshot{
				ID:             snapshotID(sub.ProviderID, sub.FollowerUserID, day),
				ProviderID:     sub.ProviderID,
				FollowerUserID: sub.FollowerUserID,
				WindowDate:     day,
				CopiedPnl:      pnl,
				SharePct:       sub.ProfitSharePct,
				ShareAmount:    share,
				Status:         SnapStatusPending,
				LockedUntil:    dayEnd.Add(e.lockWindow).UnixMilli(),
				CreatedAt:      now.UnixMilli(),
			}
			if err := e.market.InsertSnapshot(snap); err != nil {
				return err
			}
			dailyTotals[sub.ProviderUserID] += share
			settled++
		case pnl < 0:
			n, err := e.offsetPending(sub.ProviderID, sub.FollowerUserID, -pnl)
			if err != nil {
				return err
			}
			offsetCount += n
		}
	}

	for providerUserID, total := range dailyTotals {
		e.logf("[social-settle] window=%s provider_user=%d daily_share=%.4f (snapshots=%d, offsets=%d)",
			day, providerUserID, total, settled, offsetCount)
	}
	return nil
}

// offsetPending 回撤冲抵：把 pending 快照按窗口日由旧到新整单 void，
// 直到亏损失效金额（loss）被完全冲抵。返回 void 的快照数。
// 简化规则：不部分冲抵、不冲抵 locked 及以后状态、未覆盖亏损不结转。
func (e *SettlementEngine) offsetPending(providerID, followerUserID int64, loss float64) (int, error) {
	if loss <= 0 {
		return 0, nil
	}
	pendings, err := e.market.PendingSnapshots(providerID, followerUserID)
	if err != nil {
		return 0, err
	}
	voided := 0
	remaining := loss
	for _, snap := range pendings {
		if remaining < snap.ShareAmount {
			break // 整单冲抵：剩余亏损不足以覆盖下一单时停止
		}
		if err := e.market.VoidSnapshot(snap.ID); err != nil {
			return voided, err
		}
		e.logf("[social-settle] offset void snapshot=%s provider=%d follower=%d window=%s amount=%.4f remaining=%.4f",
			snap.ID, providerID, followerUserID, snap.WindowDate, snap.ShareAmount, remaining-snap.ShareAmount)
		remaining -= snap.ShareAmount
		voided++
	}
	return voided, nil
}

// Sweep 到期状态迁移：pending→locked（locked_until 到期），
// locked→payable（locked_until + payableDelay 到期）。返回两次迁移的条数。
func (e *SettlementEngine) Sweep(now time.Time) (int64, int64, error) {
	if e.market == nil {
		return 0, 0, nil
	}
	toLocked, err := e.market.AgePendingToLocked(now.UnixMilli())
	if err != nil {
		return 0, 0, err
	}
	toPayable, err := e.market.AgeLockedToPayable(now.Add(-e.payableDelay).UnixMilli())
	if err != nil {
		return toLocked, 0, err
	}
	return toLocked, toPayable, nil
}

func snapshotID(providerID, followerUserID int64, day string) string {
	return fmt.Sprintf("ps-%d-%d-%s", providerID, followerUserID, day)
}
