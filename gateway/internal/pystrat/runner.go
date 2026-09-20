// Runner：让契约化 Python 策略在网关进程里 7×24 跑起来。
//
// 与 dca/grid runner 同一模式：每个运行中的策略独占一条 goroutine 与一个
// python 沙箱子进程，串行消费 K 线（bar 经 BarSource 订阅）；on_bar 抛错
// 按错误契约计数，连续 10 次 → 置 paused + 告警退出。成交走注入式 Executor
// （生产为 handler.OMSBotExecutor，client_oid 前缀 "pystrat:<id>"）。
package pystrat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// MaxConsecutiveErrors 是错误契约阈值：回调连续抛错达到该值 → 暂停 + 告警。
const MaxConsecutiveErrors = 10

// SandboxFactory 创建一个新的沙箱实例（生产为 NewSubprocessSandbox）。
type SandboxFactory func() Sandbox

// BarSource 是闭合 K 线订阅源（生产接事件总线 + KlineFeeder，测试用 channel fake）。
type BarSource interface {
	// Subscribe 订阅 symbol+interval 的闭合 K 线；返回接收 channel 与取消函数。
	Subscribe(symbol, interval string) (<-chan model.Bar, func(), error)
}

// Executor 是下单窄接口（生产为 handler.OMSBotExecutor）。
type Executor interface {
	BuySpot(botID, symbol, exchange string, userID int64, quoteAmount, refPrice float64) (filledQty, avgPrice float64, err error)
	SellSpot(botID, symbol, exchange string, userID int64, baseQty, refPrice float64) (filledQty, avgPrice float64, err error)
}

// Accounts 是持仓/权益视图（生产读 portfolio manager 的 paper 账本）。
type Accounts interface {
	// Position 返回该 symbol 当前净持仓；无持仓 ok=false。
	Position(symbol string) (qty, avgPrice float64, side string, ok bool)
	// Equity 返回当前账户权益（USDT 口径）。
	Equity() float64
}

// Protector 把 set_stop_loss/set_take_profit 映射到现有条件单设施
// （生产接 order.ConditionalEngine.RegisterTPSL；nil 时记录日志并跳过）。
type Protector interface {
	// SetBracket 在成交后挂 TP/SL 条件单；tpPct/slPct 为 0 表示不挂。
	SetBracket(strategyID, symbol, exchange string, userID int64, qty, entryPrice, tpPct, slPct float64) error
}

// Runner 管理一组运行中的 Python 策略。
type Runner struct {
	mu      sync.Mutex
	repo    *store.PyStrategyRepo
	factory SandboxFactory
	bars    BarSource
	exec    Executor
	accts   Accounts
	gate    func(exchange string) error
	prot    Protector
	alert   func(title, content string)

	// LiveExchange 是 paper=0 时下单/过闸用的交易所（v1 固定 binance）。
	LiveExchange string

	bots map[string]*botRuntime
}

// botRuntime 是一个运行中策略的独占状态；只允许其 bar goroutine 触碰
// （status 字段除外，Status() 走 rt.mu 并发读）。
type botRuntime struct {
	record   *store.PyStrategyRecord
	manifest *Manifest
	sandbox  Sandbox
	barCh    <-chan model.Bar
	params   map[string]any
	unsub    func()
	stopCh   chan struct{}
	done     chan struct{}
	logs     *ringLog

	exchange  string // 实际执行交易所（paper / LiveExchange）
	startTime int64  // 启动时间：早于此的回补 bar 只暖机不下单

	mu           sync.Mutex // 守护 status 字段（Status API 并发读）
	slPct        float64
	tpPct        float64
	hasSL        bool
	hasTP        bool
	consecErrors int
	lastErr      string
	restarts     int
}

// NewRunner 创建 Python 策略 Runner（依赖全注入，测试给 fake）。
func NewRunner(repo *store.PyStrategyRepo, factory SandboxFactory, bars BarSource, exec Executor, accts Accounts) *Runner {
	return &Runner{
		repo:         repo,
		factory:      factory,
		bars:         bars,
		exec:         exec,
		accts:        accts,
		LiveExchange: "binance",
		bots:         make(map[string]*botRuntime),
	}
}

// SetLiveGate 注入实盘闸（生产为 handler.canPlaceLiveOrder 闭包）。
func (r *Runner) SetLiveGate(gate func(exchange string) error) { r.gate = gate }

// SetProtector 注入 TP/SL 条件单映射（可为 nil，nil 即记录并跳过）。
func (r *Runner) SetProtector(p Protector) { r.prot = p }

// SetAlert 注入告警通道（生产为 notify.Manager.Send）。
func (r *Runner) SetAlert(fn func(title, content string)) { r.alert = fn }

// Start 校验并启动一个策略：静态校验 → 实盘闸（paper=0）→ 建沙箱加载
// → 订阅 K 线 → 起 goroutine。任何一步失败都不留残态。
func (r *Runner) Start(rec *store.PyStrategyRecord) error {
	if rec == nil {
		return errors.New("pystrat runner: nil record")
	}
	if issues := ValidateStatic(rec.Code); len(issues) > 0 {
		return fmt.Errorf("pystrat runner: 静态校验未通过: %s", FormatIssues(issues))
	}

	r.mu.Lock()
	if _, ok := r.bots[rec.ID]; ok {
		r.mu.Unlock()
		return fmt.Errorf("pystrat runner: strategy %s already running", rec.ID)
	}
	r.mu.Unlock()

	exchange := "paper"
	if !rec.Paper {
		exchange = r.LiveExchange
		if r.gate == nil {
			return errors.New("pystrat runner: live gate not configured")
		}
		if err := r.gate(exchange); err != nil {
			return fmt.Errorf("pystrat runner: 实盘闸未放行: %w", err)
		}
	}
	if r.factory == nil || r.bars == nil || r.exec == nil || r.accts == nil {
		return errors.New("pystrat runner: dependencies not fully injected")
	}

	var params map[string]any
	if rec.ParamsJSON != "" {
		if err := json.Unmarshal([]byte(rec.ParamsJSON), &params); err != nil {
			return fmt.Errorf("pystrat runner: params_json: %w", err)
		}
	}

	sandbox := r.factory()
	manifest, err := sandbox.Load(context.Background(), rec.Code, params, normalizeSymbol(rec.Symbol), rec.Interval)
	if err != nil {
		sandbox.Close()
		return fmt.Errorf("pystrat runner: sandbox load: %w", err)
	}
	if msg := manifest.Validate(); msg != "" {
		sandbox.Close()
		return fmt.Errorf("pystrat runner: manifest 校验: %s", msg)
	}

	barCh, unsub, err := r.bars.Subscribe(normalizeSymbol(rec.Symbol), manifest.Interval)
	if err != nil {
		sandbox.Close()
		return fmt.Errorf("pystrat runner: subscribe bars: %w", err)
	}

	rt := &botRuntime{
		record:    rec,
		manifest:  manifest,
		sandbox:   sandbox,
		barCh:     barCh,
		params:    manifest.EffectiveParams(params),
		unsub:     unsub,
		stopCh:    make(chan struct{}),
		done:      make(chan struct{}),
		logs:      newRingLog(200),
		exchange:  exchange,
		startTime: time.Now().UnixMilli(),
	}
	rt.logf("info", "策略已加载 manifest=%s symbol=%s interval=%s direction=%s exchange=%s",
		manifest.Name, manifest.Symbol, manifest.Interval, manifest.Direction, exchange)

	r.mu.Lock()
	r.bots[rec.ID] = rt
	r.mu.Unlock()

	if r.repo != nil {
		if err := r.repo.UpdateStatus(rec.ID, store.PyStratStatusActive, ""); err != nil {
			log.Printf("pystrat runner: update status: %v", err)
		} else {
			rec.Status = store.PyStratStatusActive
		}
	}
	go r.run(rt)
	return nil
}

// Stop 停止指定策略：退订 K 线、杀沙箱子进程、置 paused。
func (r *Runner) Stop(id string) error {
	r.mu.Lock()
	rt, ok := r.bots[id]
	if ok {
		delete(r.bots, id)
	}
	r.mu.Unlock()
	if !ok {
		return nil
	}
	close(rt.stopCh)
	<-rt.done
	if r.repo != nil {
		return r.repo.UpdateStatus(id, store.PyStratStatusPaused, "")
	}
	return nil
}

// StopAll 停止所有运行中的策略（优雅退出用）。
func (r *Runner) StopAll() {
	for _, id := range r.ListRunning() {
		if err := r.Stop(id); err != nil {
			log.Printf("pystrat runner: stop %s: %v", id, err)
		}
	}
}

// IsRunning 报告策略是否由本 Runner 在内存中驱动。
func (r *Runner) IsRunning(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.bots[id]
	return ok
}

// ListRunning 返回所有运行中的策略 ID。
func (r *Runner) ListRunning() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.bots))
	for id := range r.bots {
		ids = append(ids, id)
	}
	return ids
}

// StrategyStatus 是状态接口的运行时视图。
type StrategyStatus struct {
	Running      bool   `json:"running"`
	ConsecErrors int    `json:"consec_errors"`
	LastError    string `json:"last_error"`
	Restarts     int    `json:"restarts"`
	SandboxAlive bool   `json:"sandbox_alive"`
}

// Status 返回运行状态；未运行返回 {Running:false}。
func (r *Runner) Status(id string) StrategyStatus {
	r.mu.Lock()
	rt, ok := r.bots[id]
	r.mu.Unlock()
	if !ok {
		return StrategyStatus{}
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return StrategyStatus{
		Running:      true,
		ConsecErrors: rt.consecErrors,
		LastError:    rt.lastErr,
		Restarts:     rt.restarts,
		SandboxAlive: rt.sandbox != nil && rt.sandbox.Alive(),
	}
}

// Logs 返回最近运行日志（最新在前，内存环形缓冲；未运行返回 nil）。
func (r *Runner) Logs(id string) []LogEntry {
	r.mu.Lock()
	rt, ok := r.bots[id]
	r.mu.Unlock()
	if !ok || rt.logs == nil {
		return nil
	}
	return rt.logs.snapshot()
}

// ResumeRunningBots 恢复 status='active' 且本机未驱动的策略（进程重启恢复）。
func (r *Runner) ResumeRunningBots(list []*store.PyStrategyRecord) {
	for _, rec := range list {
		if rec == nil || rec.Status != store.PyStratStatusActive {
			continue
		}
		if r.IsRunning(rec.ID) {
			continue
		}
		if err := r.Start(rec); err != nil {
			log.Printf("pystrat runner: resume %s failed: %v", rec.ID, err)
		}
	}
}

// RetryResume 立即恢复一次，然后每 interval 复查（防漏恢复）。
func (r *Runner) RetryResume(fetch func() ([]*store.PyStrategyRecord, error), interval time.Duration) {
	attempt := func() {
		list, err := fetch()
		if err != nil {
			log.Printf("pystrat runner: resume fetch: %v", err)
			return
		}
		r.ResumeRunningBots(list)
	}
	attempt()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		attempt()
	}
}

// run 是单策略的 bar goroutine：串行消费 K 线 → 沙箱 on_bar → 动作执行。
func (r *Runner) run(rt *botRuntime) {
	defer close(rt.done)
	defer rt.unsub()
	defer rt.sandbox.Close()

	for {
		select {
		case <-rt.stopCh:
			return
		case bar, ok := <-rt.barCh:
			if !ok {
				rt.logf("error", "K 线订阅已关闭，策略退出")
				r.mu.Lock()
				delete(r.bots, rt.record.ID)
				r.mu.Unlock()
				r.pause(rt, "bar subscription closed")
				return
			}
			if err := r.onBar(rt, bar); err != nil {
				rt.mu.Lock()
				rt.consecErrors++
				rt.lastErr = err.Error()
				n := rt.consecErrors
				rt.mu.Unlock()
				rt.logf("error", "第 %d/%d 次连续错误: %v", n, MaxConsecutiveErrors, err)
				if n >= MaxConsecutiveErrors {
					r.mu.Lock()
					delete(r.bots, rt.record.ID)
					r.mu.Unlock()
					r.pause(rt, fmt.Sprintf("连续 %d 次回调错误，最后错误: %v", MaxConsecutiveErrors, err))
					return
				}
			} else {
				rt.mu.Lock()
				rt.consecErrors = 0
				rt.mu.Unlock()
			}
		}
	}
}

// onBar 驱动一根 K 线并执行动作。warmup（早于启动时间的回补 bar）只跑
// 回调暖机、不下单。
func (r *Runner) onBar(rt *botRuntime, bar model.Bar) error {
	qty, avgPrice, side, hasPos := r.accts.Position(normalizeSymbol(rt.manifest.Symbol))
	state := State{
		PositionQty:      qty,
		PositionAvgPrice: avgPrice,
		PositionSide:     side,
		HasPosition:      hasPos,
		Equity:           r.accts.Equity(),
	}
	result, err := rt.sandbox.OnBar(context.Background(), bar, state)
	if err != nil {
		if errors.Is(err, ErrSandboxDead) {
			// 超时/崩溃：重建沙箱重载一次（模块内状态丢失，文档已注明）。
			if rebuildErr := r.rebuildSandbox(rt); rebuildErr != nil {
				return fmt.Errorf("沙箱死亡且重建失败: %v (原错误: %w)", rebuildErr, err)
			}
			rt.mu.Lock()
			n := rt.restarts
			rt.mu.Unlock()
			rt.logf("info", "沙箱已重建并重载策略（第 %d 次）", n)
			return nil // 本轮跳过，不计错误
		}
		return err
	}
	for _, line := range result.Logs {
		rt.logf("info", "%s", line)
	}
	for _, p := range result.Prints {
		rt.logf("info", "print: %s", p)
	}
	warmup := bar.Time > 0 && bar.Time < rt.startTime
	if warmup && len(result.Actions) > 0 {
		rt.logf("info", "暖机 bar 跳过 %d 个交易动作", len(result.Actions))
		return nil
	}
	for _, act := range result.Actions {
		if err := r.execAction(rt, bar, act, state); err != nil {
			rt.logf("error", "动作 %s 执行失败: %v", act.Type, err)
		}
	}
	return nil
}

// rebuildSandbox 用同一策略代码重造沙箱并重新加载。
func (r *Runner) rebuildSandbox(rt *botRuntime) error {
	rt.sandbox.Close()
	sandbox := r.factory()
	manifest, err := sandbox.Load(context.Background(), rt.record.Code, rt.params, normalizeSymbol(rt.manifest.Symbol), rt.manifest.Interval)
	if err != nil {
		sandbox.Close()
		return err
	}
	rt.mu.Lock()
	rt.restarts++
	rt.mu.Unlock()
	rt.sandbox = sandbox
	rt.manifest = manifest
	return nil
}

// execAction 把一个 context 动作转成 OMS 下单 / 条件单。
func (r *Runner) execAction(rt *botRuntime, bar model.Bar, act Action, state State) error {
	symbol := normalizeSymbol(rt.manifest.Symbol)
	refPrice := bar.Close
	if act.Price > 0 {
		rt.logf("info", "v1 为市价执行，忽略限价 %.4f", act.Price)
	}

	switch act.Type {
	case "buy":
		quote := act.Amount
		if quote <= 0 && act.Qty > 0 {
			quote = act.Qty * refPrice
		}
		if quote <= 0 {
			return errors.New("buy: 数量无效")
		}
		if !r.directionAllows(rt.manifest.Direction, true) {
			return fmt.Errorf("direction=%s 不允许买入开仓", rt.manifest.Direction)
		}
		if err := r.checkPositionLimit(rt, state, quote, refPrice); err != nil {
			rt.logf("info", "仓位上限拦截买入: %v", err)
			return nil // 风控拦截记日志不算错误
		}
		filled, avg, err := r.exec.BuySpot(rt.record.ID, rt.record.Symbol, rt.exchange, rt.record.UserID, quote, refPrice)
		if err != nil {
			return err
		}
		if filled <= 0 {
			rt.logf("info", "买入未成交 quote=%.2f @ %.4f", quote, refPrice)
			return nil
		}
		rt.logf("action", "买入成交 qty=%.6f avg=%.4f", filled, avg)
		return r.applyBracket(rt, symbol, filled, avg)

	case "sell":
		qty := act.Qty
		if qty <= 0 && act.Amount > 0 && refPrice > 0 {
			qty = act.Amount / refPrice
		}
		if qty <= 0 {
			return errors.New("sell: 数量无效")
		}
		if !state.HasPosition {
			rt.logf("info", "无持仓，跳过卖出 qty=%.6f", qty)
			return nil
		}
		filled, avg, err := r.exec.SellSpot(rt.record.ID, rt.record.Symbol, rt.exchange, rt.record.UserID, qty, refPrice)
		if err != nil {
			return err
		}
		if filled <= 0 {
			rt.logf("info", "卖出未成交 qty=%.6f @ %.4f", qty, refPrice)
			return nil
		}
		rt.logf("action", "卖出成交 qty=%.6f avg=%.4f", filled, avg)
		return nil

	case "close_position":
		if !state.HasPosition {
			rt.logf("info", "无持仓，close_position 空操作")
			return nil
		}
		filled, avg, err := r.exec.SellSpot(rt.record.ID, rt.record.Symbol, rt.exchange, rt.record.UserID, state.PositionQty, refPrice)
		if err != nil {
			return err
		}
		if filled > 0 {
			rt.logf("action", "平仓成交 qty=%.6f avg=%.4f", filled, avg)
		}
		return nil

	case "set_stop_loss":
		if act.Pct <= 0 || act.Pct >= 1 {
			return fmt.Errorf("set_stop_loss: pct=%.4f 越界", act.Pct)
		}
		rt.mu.Lock()
		rt.slPct, rt.hasSL = act.Pct, true
		rt.mu.Unlock()
		rt.logf("action", "止损设置为 %.2f%%", act.Pct*100)
		return nil

	case "set_take_profit":
		if act.Pct <= 0 || act.Pct >= 1 {
			return fmt.Errorf("set_take_profit: pct=%.4f 越界", act.Pct)
		}
		rt.mu.Lock()
		rt.tpPct, rt.hasTP = act.Pct, true
		rt.mu.Unlock()
		rt.logf("action", "止盈设置为 %.2f%%", act.Pct*100)
		return nil
	}
	return fmt.Errorf("未知动作类型 %q", act.Type)
}

// directionAllows：v1 现货执行，short 方向不允许买入开仓（无现货空头），
// long 方向允许买/卖（卖为减仓），both 全允许。
func (r *Runner) directionAllows(direction string, isBuy bool) bool {
	switch direction {
	case DirectionShort:
		return !isBuy
	case DirectionLong, DirectionBoth:
		return true
	}
	return true
}

// checkPositionLimit 校验买入后仓位名义价值不超权益 × max_position_pct。
// 0 表示不限制。 equity<=0 时无法判断，放行（OMS 余额检查兜底）。
func (r *Runner) checkPositionLimit(rt *botRuntime, state State, quote, refPrice float64) error {
	limit := rt.manifest.Risk.MaxPositionPct
	if limit <= 0 || state.Equity <= 0 {
		return nil
	}
	current := 0.0
	if state.HasPosition {
		current = state.PositionQty * refPrice
	}
	if (current+quote)/state.Equity > limit {
		return fmt.Errorf("仓位 %.2fU + 买入 %.2fU 超过权益 %.2fU 的 %.0f%% 上限",
			current, quote, state.Equity, limit*100)
	}
	return nil
}

// applyBracket 买入成交后按 manifest.risk 与策略覆盖挂 TP/SL 条件单。
func (r *Runner) applyBracket(rt *botRuntime, symbol string, qty, entryPrice float64) error {
	rt.mu.Lock()
	slPct, tpPct := rt.slPct, rt.tpPct
	hasSL, hasTP := rt.hasSL, rt.hasTP
	rt.mu.Unlock()
	if !hasSL {
		slPct = rt.manifest.Risk.StopLossPct
	}
	if !hasTP {
		tpPct = rt.manifest.Risk.TakeProfitPct
	}
	if slPct <= 0 && tpPct <= 0 {
		return nil
	}
	if r.prot == nil {
		rt.logf("info", "TP/SL 条件单未配置（Protector=nil），跳过 tp=%.2f%% sl=%.2f%%", tpPct*100, slPct*100)
		return nil
	}
	if err := r.prot.SetBracket(rt.record.ID, symbol, rt.exchange, rt.record.UserID, qty, entryPrice, tpPct, slPct); err != nil {
		return fmt.Errorf("挂 TP/SL 条件单: %w", err)
	}
	rt.logf("action", "TP/SL 条件单已挂 qty=%.6f entry=%.4f tp=%.2f%% sl=%.2f%%", qty, entryPrice, tpPct*100, slPct*100)
	return nil
}

// pause 把策略置 paused（error 列带原因）并退出 goroutine。
func (r *Runner) pause(rt *botRuntime, reason string) {
	rt.mu.Lock()
	rt.lastErr = reason
	rt.mu.Unlock()
	rt.logf("error", "策略暂停: %s", reason)
	if r.repo != nil {
		if err := r.repo.UpdateStatus(rt.record.ID, store.PyStratStatusPaused, reason); err != nil {
			log.Printf("pystrat runner: pause %s: %v", rt.record.ID, err)
		}
	}
	if r.alert != nil {
		r.alert("Python 策略已暂停", fmt.Sprintf("策略 %s（%s）已暂停：%s", rt.record.Name, rt.record.ID, reason))
	}
}

func (rt *botRuntime) logf(level, format string, args ...any) {
	if rt.logs == nil {
		return
	}
	rt.logs.append(LogEntry{TS: time.Now().UnixMilli(), Level: level, Message: fmt.Sprintf(format, args...)})
}
