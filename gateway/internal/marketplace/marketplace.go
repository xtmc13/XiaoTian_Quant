// Package marketplace 实现机器人/信号市场上架准入机制（对标 CryptoRobotics
// 创作者市场）：作者提交自有机器人对象进入强制考核期（probation），
// 标准化统计达标后经人工审核方可上架出售；市场卡片展示统一的透明统计。
//
// 上架对象（0058 通用对象模型，object_kind）：
//   - ai_bot   AI 机器人实例（ai_bot_instances，统计取 ai_bot_snapshots/ai_bot_trades）
//   - grid_bot 网格机器人（grid_bots，统计取 grid_bot_snapshots/grid_bot_trades）
//
// 状态机：
//
//	draft ──submit──▶ probation ──达标(cron)──▶ pending_review ──approve──▶ listed
//	                  ▲  │cancel                   │reject(reason)             │delist(reason)
//	                  │  ▼                         ▼                           ▼
//	                  │ draft                    rejected ──resubmit──┐    delisted
//	                  └──────────────resubmit──────────────┴────────────┴──▶ probation
//
// 统计口径：权益曲线(snapshots)+已平仓交易(trades) →
// backtest.MetricsFromEquity（与回测完全同口径的 Sharpe/回撤/胜率/盈亏比），
// 不新造指标算法。月化/年化由总收益率按运行天数线性折算。
package marketplace

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strconv"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/backtest"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 状态机 ──

const (
	StatusDraft         = "draft"
	StatusProbation     = "probation"
	StatusPendingReview = "pending_review"
	StatusListed        = "listed"
	StatusRejected      = "rejected"
	StatusDelisted      = "delisted"
)

// transitions 合法迁移表（from → 允许的 to 集合）。
var transitions = map[string]map[string]bool{
	StatusDraft:         {StatusProbation: true},
	StatusProbation:     {StatusPendingReview: true, StatusDraft: true},
	StatusPendingReview: {StatusListed: true, StatusRejected: true},
	StatusRejected:      {StatusProbation: true},
	StatusListed:        {StatusDelisted: true},
	StatusDelisted:      {StatusProbation: true},
}

// CanTransition 判定 from→to 是否为合法迁移。
func CanTransition(from, to string) bool {
	return transitions[from][to]
}

// ErrIllegalTransition 非法状态迁移（API 层映射 409）。
var ErrIllegalTransition = errors.New("illegal listing status transition")

// ── 上架对象类型（0058 通用对象模型）──

const (
	// ObjectKindAIBot AI 机器人实例（数据源 ai_bot_snapshots/ai_bot_trades）。
	ObjectKindAIBot = "ai_bot"
	// ObjectKindGridBot 网格机器人（数据源 grid_bot_snapshots/grid_bot_trades）。
	ObjectKindGridBot = "grid_bot"
)

func transition(l *store.MarketListingRecord, to string) error {
	if !CanTransition(l.Status, to) {
		return fmt.Errorf("%w: %s → %s", ErrIllegalTransition, l.Status, to)
	}
	l.Status = to
	return nil
}

// ── 考核规则 ──

// Rules 上架考核规则（全局可配，提交考核时快照进条目）。
type Rules struct {
	MinDays        int     `json:"min_days"`         // 最少考核天数（默认 30，管理员可缩短做演示）
	MinTrades      int     `json:"min_trades"`       // 窗口内最少已平仓交易数（默认 10）
	MaxDrawdownPct float64 `json:"max_drawdown_pct"` // 窗口内最大回撤红线（默认 50，统计值须 < 该值）
}

const (
	settingMinDays     = "min_days"
	settingMinTrades   = "min_trades"
	settingMaxDrawdown = "max_drawdown_pct"
)

// DefaultRules 默认考核规则。
func DefaultRules() Rules {
	return Rules{MinDays: 30, MinTrades: 10, MaxDrawdownPct: 50}
}

// LoadRules 从 xt_market_settings 读取，缺省回落默认值并校验范围。
func LoadRules() Rules {
	d := DefaultRules()
	if v := store.GetMarketSetting(settingMinDays); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			d.MinDays = n
		}
	}
	if v := store.GetMarketSetting(settingMinTrades); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			d.MinTrades = n
		}
	}
	if v := store.GetMarketSetting(settingMaxDrawdown); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			d.MaxDrawdownPct = f
		}
	}
	return d
}

// Validate 规则范围校验（保存前调用）。
func (r Rules) Validate() error {
	if r.MinDays < 1 || r.MinDays > 365 {
		return fmt.Errorf("min_days 必须在 1-365 之间")
	}
	if r.MinTrades < 0 || r.MinTrades > 100000 {
		return fmt.Errorf("min_trades 必须在 0-100000 之间")
	}
	if r.MaxDrawdownPct <= 0 || r.MaxDrawdownPct > 100 {
		return fmt.Errorf("max_drawdown_pct 必须在 (0,100] 之间")
	}
	return nil
}

// SaveRules 校验并持久化规则。
func SaveRules(r Rules) error {
	if err := r.Validate(); err != nil {
		return err
	}
	if err := store.SetMarketSetting(settingMinDays, strconv.Itoa(r.MinDays)); err != nil {
		return err
	}
	if err := store.SetMarketSetting(settingMinTrades, strconv.Itoa(r.MinTrades)); err != nil {
		return err
	}
	return store.SetMarketSetting(settingMaxDrawdown, strconv.FormatFloat(r.MaxDrawdownPct, 'f', -1, 64))
}

// ── Service ──

// Service 上架生命周期服务（无状态，直接走 store repo）。
type Service struct {
	repo     *store.MarketListingRepo
	gridRepo *store.GridRepo
}

func NewService() *Service {
	return &Service{repo: store.NewMarketListingRepo(), gridRepo: store.NewGridRepo()}
}

// Repo 暴露底层 repo（handler 列表查询用）。
func (s *Service) Repo() *store.MarketListingRepo { return s.repo }

// Create 创建 draft 条目；objectKind 分派属主校验：ai_bot 查 ai_bot_instances、
// grid_bot 查 grid_bots（均必须属于作者）；空 objectKind 兼容按 ai_bot 处理。
func (s *Service) Create(authorUserID int64, objectKind, objectID, kind, name, description, feeModel string, feePercent, monthlyFee float64) (*store.MarketListingRecord, error) {
	if objectKind == "" {
		objectKind = ObjectKindAIBot
	}
	var botInstanceID, defaultName string
	switch objectKind {
	case ObjectKindAIBot:
		inst := store.GetAIBotInstanceByID(objectID, int(authorUserID))
		if inst == nil {
			return nil, fmt.Errorf("probation instance not found or not owned by author")
		}
		botInstanceID = objectID
		defaultName = getString(inst, "name", "Untitled")
	case ObjectKindGridBot:
		bot, err := s.gridRepo.GetByIDForUser(objectID, authorUserID)
		if err != nil {
			return nil, err
		}
		if bot == nil {
			return nil, fmt.Errorf("probation grid bot not found or not owned by author")
		}
		defaultName = bot.Name
	default:
		return nil, fmt.Errorf("unsupported object_kind %q (want ai_bot|grid_bot)", objectKind)
	}
	if kind != "robot" && kind != "signal" {
		kind = "robot"
	}
	switch feeModel {
	case "free", "monthly", "profit_share":
	default:
		feeModel = "free"
	}
	if name == "" {
		name = defaultName
	}
	if name == "" {
		name = "Untitled"
	}
	rec := &store.MarketListingRecord{
		ID:            fmt.Sprintf("ml-%d", time.Now().UnixNano()/int64(time.Millisecond)),
		AuthorUserID:  authorUserID,
		BotInstanceID: botInstanceID,
		ObjectKind:    objectKind,
		ObjectID:      objectID,
		Kind:          kind,
		Name:          name,
		Description:   description,
		FeeModel:      feeModel,
		FeePercent:    feePercent,
		MonthlyFee:    monthlyFee,
		Status:        StatusDraft,
	}
	if err := s.repo.Create(rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// Submit 提交考核：draft/rejected/delisted → probation，记录开始时间并快照当前规则。
func (s *Service) Submit(l *store.MarketListingRecord, now time.Time) error {
	if err := transition(l, StatusProbation); err != nil {
		return err
	}
	rules := LoadRules()
	l.ProbationStartedAt = now.Unix()
	l.RuleMinDays = rules.MinDays
	l.RuleMinTrades = rules.MinTrades
	l.RuleMaxDrawdownPct = rules.MaxDrawdownPct
	l.RejectReason = ""
	l.DelistReason = ""
	l.ReviewedBy = 0
	l.ReviewedAt = 0
	return s.repo.Update(l)
}

// Cancel 作者撤回考核：probation → draft。
func (s *Service) Cancel(l *store.MarketListingRecord) error {
	if err := transition(l, StatusDraft); err != nil {
		return err
	}
	return s.repo.Update(l)
}

// Approve 管理员通过：pending_review → listed，同步 ai_bot_catalog（可订阅），
// 并保证作者在 xt_social_providers 有建档（利润分成归因的收款方）；建档失败
// 不阻断 approve，provider_id 留 0 由 Engine.RunOnce 小时级补建。
func (s *Service) Approve(l *store.MarketListingRecord, reviewerID int64, now time.Time) error {
	if err := transition(l, StatusListed); err != nil {
		return err
	}
	l.ReviewedBy = reviewerID
	l.ReviewedAt = now.Unix()
	l.ListedAt = now.Unix()
	l.RejectReason = ""
	strategyType, marketType := "", ""
	if l.ObjectKind == ObjectKindGridBot {
		strategyType, marketType = "grid", "spot"
	} else if inst := store.GetAIBotInstanceByID(l.BotInstanceID, int(l.AuthorUserID)); inst != nil {
		strategyType = getString(inst, "strategy_type", "")
		marketType = getString(inst, "market_type", "")
	}
	if err := store.UpsertMarketCatalogEntry(l, strategyType, marketType, now.Unix()); err != nil {
		return err
	}
	if _, err := EnsureListingProvider(l); err != nil {
		log.Printf("[marketplace] ERROR approve %s: provider ensure failed (retry on engine tick): %v", l.ID, err)
	}
	return s.repo.Update(l)
}

// Reject 管理员驳回：pending_review → rejected（原因必填，作者可见）。
func (s *Service) Reject(l *store.MarketListingRecord, reviewerID int64, reason string, now time.Time) error {
	if reason == "" {
		return fmt.Errorf("reject reason required")
	}
	if err := transition(l, StatusRejected); err != nil {
		return err
	}
	l.ReviewedBy = reviewerID
	l.ReviewedAt = now.Unix()
	l.RejectReason = reason
	return s.repo.Update(l)
}

// Delist 管理员强制下架：listed → delisted（原因必填），catalog 同步隐藏。
func (s *Service) Delist(l *store.MarketListingRecord, reviewerID int64, reason string, now time.Time) error {
	if reason == "" {
		return fmt.Errorf("delist reason required")
	}
	if err := transition(l, StatusDelisted); err != nil {
		return err
	}
	l.ReviewedBy = reviewerID
	l.ReviewedAt = now.Unix()
	l.DelistReason = reason
	store.DeactivateMarketCatalogEntry(l.ID)
	return s.repo.Update(l)
}

// ── 标准化统计聚合 ──

// maxProfitFactor 无亏损交易时 profit factor 的存储上限（数学上为 +Inf，
// 落库/JSON 用有限值表示，展示层按 ">=999" 解读）。
const maxProfitFactor = 999.0

// Aggregate 计算条目考核窗口 [probation_started_at, now] 的标准化统计。
// 数据源按 object_kind 分派：ai_bot → ai_bot_snapshots + ai_bot_trades；
// grid_bot → grid_bot_snapshots + grid_bot_trades；跟踪者数取 ai_bot_subscriptions。
// 指标口径与 backtest.Runner 完全一致（统一走 MetricsFromEquity）。
func (s *Service) Aggregate(l *store.MarketListingRecord, now time.Time) (*store.MarketListingStatsRecord, error) {
	var initialBalance float64
	var since int64 // 窗口起点（Unix 秒）
	var equity []backtest.EquityPoint
	var positions []backtest.Position
	var err error
	switch l.ObjectKind {
	case "", ObjectKindAIBot:
		initialBalance, since, equity, positions, err = s.aggregateAIBot(l, now)
	case ObjectKindGridBot:
		initialBalance, since, equity, positions, err = s.aggregateGrid(l)
	default:
		return nil, fmt.Errorf("unsupported object_kind %q", l.ObjectKind)
	}
	if err != nil {
		return nil, err
	}

	res := backtest.MetricsFromEquity(equity, initialBalance, 0.02, positions, 0)

	runningDays := int((now.Unix() - since) / 86400)
	if runningDays < 0 {
		runningDays = 0
	}
	daysForRates := runningDays
	if daysForRates < 1 {
		daysForRates = 1
	}
	profitFactor := res.ProfitFactor
	if math.IsInf(profitFactor, 1) {
		profitFactor = maxProfitFactor
	}

	return &store.MarketListingStatsRecord{
		ListingID:           l.ID,
		Date:                now.UTC().Format("2006-01-02"),
		TotalReturnPct:      res.TotalReturnPct,
		AnnualizedReturnPct: res.TotalReturnPct / float64(daysForRates) * 365,
		MaxDrawdownPct:      res.MaxDrawdownPct,
		WinRate:             res.WinRate,
		ProfitFactor:        profitFactor,
		SharpeRatio:         res.SharpeRatio,
		TotalTrades:         res.TotalTrades,
		MonthlyReturnPct:    res.TotalReturnPct / float64(daysForRates) * 30,
		Followers:           s.repo.CountActiveFollowers(l.ID),
		RunningDays:         runningDays,
	}, nil
}

// aggregateAIBot 汇总 ai_bot 条目的考核窗口数据：ai_bot_snapshots 权益曲线
// （秒级时间戳）+ ai_bot_trades 已平仓交易。
func (s *Service) aggregateAIBot(l *store.MarketListingRecord, now time.Time) (initialBalance float64, since int64, equity []backtest.EquityPoint, positions []backtest.Position, err error) {
	inst := store.GetAIBotInstanceByID(l.BotInstanceID, int(l.AuthorUserID))
	if inst == nil {
		return 0, 0, nil, nil, fmt.Errorf("probation instance %s not found", l.BotInstanceID)
	}
	initialBalance = getFloat(inst, "initial_balance", 10000)
	if initialBalance <= 0 {
		initialBalance = 10000
	}
	since = l.ProbationStartedAt
	if since <= 0 {
		since = getInt64(inst, "created_at", now.Unix())
	}

	// 权益曲线：窗口起点补 initial_balance 锚点，之后接快照（快照可能
	// 与起点同秒——跳过，保证曲线严格递增且无重复锚点）。
	equity = []backtest.EquityPoint{{Timestamp: since * 1000, Equity: initialBalance}}
	for _, snap := range store.GetAIBotSnapshotsSince(l.BotInstanceID, since) {
		ts := getInt64(snap, "timestamp", 0)
		eq := getFloat(snap, "total_equity", 0)
		if ts <= since || eq <= 0 {
			continue
		}
		equity = append(equity, backtest.EquityPoint{Timestamp: ts * 1000, Equity: eq})
	}

	for _, t := range store.GetAIBotTradesSince(l.BotInstanceID, since) {
		positions = append(positions, backtest.Position{
			Symbol:      getString(t, "symbol", ""),
			EntryPrice:  getFloat(t, "entry_price", 0),
			ExitPrice:   getFloat(t, "exit_price", 0),
			Quantity:    getFloat(t, "quantity", 0),
			EntryTime:   getInt64(t, "opened_at", 0) * 1000,
			ExitTime:    getInt64(t, "closed_at", 0) * 1000,
			RealizedPnL: getFloat(t, "pnl", 0),
			IsClosed:    true,
			ExitReason:  getString(t, "close_reason", ""),
		})
	}
	return initialBalance, since, equity, positions, nil
}

// aggregateGrid 汇总 grid_bot 条目的考核窗口数据：grid_bot_snapshots 权益曲线
// + grid_bot_trades 成交（毫秒级时间戳）。网格成交是单格 fills，只有落袋盈亏
// 的腿（pnl≠0，long 模式的 SELL / short 模式的 BUY）计入平仓口径。
func (s *Service) aggregateGrid(l *store.MarketListingRecord) (initialBalance float64, since int64, equity []backtest.EquityPoint, positions []backtest.Position, err error) {
	bot, err := s.gridRepo.GetByIDForUser(l.ObjectID, l.AuthorUserID)
	if err != nil {
		return 0, 0, nil, nil, err
	}
	if bot == nil {
		return 0, 0, nil, nil, fmt.Errorf("probation grid bot %s not found", l.ObjectID)
	}
	initialBalance = bot.InitialEquity
	if initialBalance <= 0 {
		initialBalance = bot.Investment
	}
	if initialBalance <= 0 {
		initialBalance = 10000
	}
	since = l.ProbationStartedAt
	if since <= 0 {
		since = bot.CreatedAt / 1000
	}
	sinceMs := since * 1000

	equity = []backtest.EquityPoint{{Timestamp: sinceMs, Equity: initialBalance}}
	snaps, err := s.gridRepo.GetSnapshotsAsc(l.ObjectID, sinceMs)
	if err != nil {
		return 0, 0, nil, nil, err
	}
	for _, snap := range snaps {
		if snap.Ts <= sinceMs || snap.Equity <= 0 {
			continue
		}
		equity = append(equity, backtest.EquityPoint{Timestamp: snap.Ts, Equity: snap.Equity})
	}

	trades, err := s.gridRepo.GetTradesSinceAsc(l.ObjectID, sinceMs)
	if err != nil {
		return 0, 0, nil, nil, err
	}
	for _, tr := range trades {
		if tr.PnL == 0 {
			continue
		}
		positions = append(positions, backtest.Position{
			Symbol:      bot.Symbol,
			EntryPrice:  tr.Price,
			ExitPrice:   tr.Price,
			Quantity:    tr.Quantity,
			EntryTime:   tr.Ts,
			ExitTime:    tr.Ts,
			RealizedPnL: tr.PnL,
			IsClosed:    true,
			ExitReason:  tr.Leg,
		})
	}
	return initialBalance, since, equity, positions, nil
}

// ProbationProgress 考核进度（作者视图 + 达标判定共用）。
type ProbationProgress struct {
	DaysElapsed      int     `json:"days_elapsed"`
	MinDays          int     `json:"min_days"`
	RemainingDays    int     `json:"remaining_days"`
	TradesInWindow   int     `json:"trades_in_window"`
	MinTrades        int     `json:"min_trades"`
	RemainingTrades  int     `json:"remaining_trades"`
	MaxDrawdownPct   float64 `json:"max_drawdown_pct"`
	MaxDrawdownLimit float64 `json:"max_drawdown_limit"`
	DaysOK           bool    `json:"days_ok"`
	TradesOK         bool    `json:"trades_ok"`
	DrawdownOK       bool    `json:"drawdown_ok"`
	Passed           bool    `json:"passed"`
}

// EvaluateProbation 按条目快照的规则判定考核是否达标。
func EvaluateProbation(l *store.MarketListingRecord, stats *store.MarketListingStatsRecord) ProbationProgress {
	p := ProbationProgress{
		MinDays:          l.RuleMinDays,
		MinTrades:        l.RuleMinTrades,
		MaxDrawdownLimit: l.RuleMaxDrawdownPct,
	}
	if stats != nil {
		p.DaysElapsed = stats.RunningDays
		p.TradesInWindow = stats.TotalTrades
		p.MaxDrawdownPct = stats.MaxDrawdownPct
	}
	if p.RemainingDays = p.MinDays - p.DaysElapsed; p.RemainingDays < 0 {
		p.RemainingDays = 0
	}
	if p.RemainingTrades = p.MinTrades - p.TradesInWindow; p.RemainingTrades < 0 {
		p.RemainingTrades = 0
	}
	p.DaysOK = p.DaysElapsed >= p.MinDays
	p.TradesOK = p.TradesInWindow >= p.MinTrades
	p.DrawdownOK = p.MaxDrawdownPct < p.MaxDrawdownLimit
	p.Passed = p.DaysOK && p.TradesOK && p.DrawdownOK
	return p
}

// ── 周期引擎（日快照 + 达标自动转 pending_review） ──

// Engine 周期性聚合市场条目统计并推进考核状态机。
// RunOnce(now) 与 ticker 解耦，测试可直接驱动。
type Engine struct {
	svc      *Service
	interval time.Duration
	logf     func(format string, args ...any)

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
}

func NewEngine(svc *Service) *Engine {
	return &Engine{
		svc:      svc,
		interval: time.Hour, // 快照按日幂等 upsert；小时级 tick 让达标判定更及时
		logf:     log.Printf,
	}
}

// SetInterval 覆盖 tick 间隔（测试用）。
func (e *Engine) SetInterval(d time.Duration) { e.interval = d }

// SetLogf 覆盖日志函数（测试静默用）。
func (e *Engine) SetLogf(fn func(format string, args ...any)) { e.logf = fn }

func (e *Engine) Start() {
	e.mu.Lock()
	if e.running {
		e.mu.Unlock()
		return
	}
	e.running = true
	e.stopCh = make(chan struct{})
	e.mu.Unlock()
	go e.loop()
}

func (e *Engine) Stop() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return
	}
	e.running = false
	close(e.stopCh)
}

func (e *Engine) loop() {
	ticker := time.NewTicker(e.interval)
	defer ticker.Stop()
	for {
		select {
		case <-e.stopCh:
			return
		case now := <-ticker.C:
			if err := e.RunOnce(now); err != nil {
				e.logf("[marketplace] run error: %v", err)
			}
		}
	}
}

// RunOnce 对 probation/pending_review/listed 条目：聚合当日统计快照；
// probation 达标自动转 pending_review（待人工审核）。
// 同时做市场条目订阅到期治理：expires_at 已过的 active 订阅置 expired。
func (e *Engine) RunOnce(now time.Time) error {
	var firstErr error
	for _, status := range []string{StatusProbation, StatusPendingReview, StatusListed} {
		for _, l := range e.svc.repo.ListByStatus(status) {
			stats, err := e.svc.Aggregate(l, now)
			if err != nil {
				if firstErr == nil {
					firstErr = err
				}
				continue
			}
			if err := e.svc.repo.UpsertStats(stats); err != nil && firstErr == nil {
				firstErr = err
			}
			if l.Status == StatusProbation && EvaluateProbation(l, stats).Passed {
				if err := transition(l, StatusPendingReview); err == nil {
					if err := e.svc.repo.Update(l); err != nil && firstErr == nil {
						firstErr = err
					}
				}
			}
			// listed 条目补建 provider 建档（approve 时失败的兜底重试）。
			if l.Status == StatusListed && l.ProviderID == 0 {
				if _, err := EnsureListingProvider(l); err != nil {
					e.logf("[marketplace] listing %s provider ensure retry failed: %v", l.ID, err)
				}
			}
		}
	}
	if n, err := e.svc.ExpireSubscriptions(now.Unix()); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	} else if n > 0 {
		e.logf("[marketplace] expired %d listing subscription(s)", n)
	}
	return firstErr
}

// ── map 读取小工具（store 的 ai_bot map 风格数据） ──

func getString(m map[string]any, key, def string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func getFloat(m map[string]any, key string, def float64) float64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case float64:
			return n
		case int:
			return float64(n)
		case int64:
			return float64(n)
		}
	}
	return def
}

func getInt64(m map[string]any, key string, def int64) int64 {
	if v, ok := m[key]; ok {
		switch n := v.(type) {
		case int64:
			return n
		case int:
			return int64(n)
		case float64:
			return int64(n)
		}
	}
	return def
}
