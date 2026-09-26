package strategy

import (
	"log"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/event"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/protection"
)

// ── 引擎扩展（A7.1 多周期 / A7.2 调度 / A7.3 动态 universe） ──
// 三个能力都通过可选接口接入：策略实现哪个接口就启用哪个能力，
// BaseStrategy 无需任何改动，存量策略行为完全不变。

// Timeframer 可选接口：策略声明主周期之外还需要哪些周期的 K 线
// （如 ["4h","1d"]）。引擎为其订阅 feeder，策略经 BarProvider 读取。
type Timeframer interface {
	Timeframes() []string
}

// PrimaryTimeframer 可选接口：声明主周期。声明后引擎在 OnBar 分发前
// 过滤非主周期 K 线（同一 symbol 的其它周期供给不会污染策略状态），
// 也是 universe 自动订阅使用的周期。
type PrimaryTimeframer interface {
	PrimaryTimeframe() string
}

// UniverseProvider 可选接口：策略每 N 根主周期 K 线返回新的标的池，
// 引擎据此增删 symbol 的事件订阅与 K 线供给。
type UniverseProvider interface {
	OnUniverse(ctx UniverseContext) []string
}

// UniverseRefreshProvider 可选接口：自定义 universe 刷新频率（主周期 K 线根数）。
type UniverseRefreshProvider interface {
	UniverseRefreshBars() int
}

// WatchlistProvider 可选接口：策略的候选标的池。引擎为其全部供给 K 线
// （数据进 MarketData，供 OnUniverse 排名计算），但只有 universe 内的
// symbol 才向策略派发 OnBar。
type WatchlistProvider interface {
	Watchlist() []string
}

// UniverseContext 是 OnUniverse 回调的入参。
type UniverseContext struct {
	Time     time.Time
	Symbols  []string // 当前 universe（只读拷贝）
	BarIndex int      // 主周期 K 线计数
	provider BarProvider
}

// Bars 读取某 symbol 主周期 K 线序列（OnUniverse 内做动量排名等）。
func (c UniverseContext) Bars(symbol, tf string) []model.Bar {
	if c.provider == nil {
		return nil
	}
	return c.provider.GetSeries(symbol, tf)
}

// NewUniverseContext 构造 UniverseContext（引擎与策略单测共用）。
func NewUniverseContext(t time.Time, symbols []string, barIndex int, provider BarProvider) UniverseContext {
	return UniverseContext{Time: t, Symbols: symbols, BarIndex: barIndex, provider: provider}
}

// ── 引擎侧状态 ──

// SymbolFeeder 是引擎对 K 线供给管的最小依赖（market.KlineFeeder 满足该
// 接口；接口化便于测试替身）。Ensure/Release 引用计数、线程安全。
type SymbolFeeder interface {
	EnsureSymbol(symbol, interval string) bool
	ReleaseSymbol(symbol, interval string)
}

// feedHold 记录引擎为某策略持有的 feeder 引用（释放在 Unregister）。
type feedHold struct{ symbol, interval string }

// universeState 跟踪每个策略的动态 universe。
type universeState struct {
	current  map[string]bool                 // 当前 universe（含主 symbol）
	subs     map[string]event.SubscriptionID // 引擎为 universe 追加的订阅（不含 Register 的主订阅）
	barCount int
	refresh  int // 每 N 根主周期 K 线刷新一次
}

// scheduledEntry 是调度器里的一条注册。
type scheduledEntry struct {
	spec *ScheduleSpec
	next time.Time
}

// SetKlineFeeder 注入 K 线供给管（main 启动接线，供多周期/universe 使用）。
// 与 handler 侧各自独立记账：feed 是引用计数的，双方释放互不影响。
func (e *Engine) SetKlineFeeder(f SymbolFeeder) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.feeder = f
}

// MarketData 返回引擎的多周期数据访问器（懒初始化）。
func (e *Engine) MarketData() *MarketData {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.marketData == nil {
		e.marketData = NewMarketData(e.bus)
	}
	return e.marketData
}

// strategyPrimaryTF 取策略主周期（未声明时回退 15m，与 KlineFeeder 默认一致）。
func strategyPrimaryTF(s Strategy) string {
	if p, ok := s.(PrimaryTimeframer); ok {
		if tf := strings.ToLower(strings.TrimSpace(p.PrimaryTimeframe())); tf != "" {
			return tf
		}
	}
	return "15m"
}

// setupExtensions 在策略 Start 成功后启用多周期/调度/universe（幂等，可重入）。
// 注意：s 可能是 NamedStrategy 包装，所有可选接口断言都用解包后的 core。
func (e *Engine) setupExtensions(name string, s Strategy) {
	// 幂等：重复 Start 前先清掉旧的 feeder 持有与调度注册
	e.teardownExtensions(name)

	core := UnwrapStrategy(s)

	e.mu.Lock()
	feeder := e.feeder
	e.mu.Unlock()

	// BarProvider 注入（多周期读取入口；同时完成 MarketData 懒初始化）
	md := e.MarketData()
	if bp, ok := core.(BarProviderSetter); ok {
		bp.SetBarProvider(md)
	}

	// 多周期供给
	if tfm, ok := core.(Timeframer); ok && feeder != nil {
		sym := strings.ToUpper(s.Symbol())
		for _, tf := range tfm.Timeframes() {
			tf = strings.ToLower(strings.TrimSpace(tf))
			if tf == "" || tf == strategyPrimaryTF(core) {
				continue
			}
			if feeder.EnsureSymbol(sym, tf) {
				log.Printf("[StrategyEngine] strategy %s multi-timeframe feed %s %s started", name, sym, tf)
			}
			e.addFeedHold(name, feedHold{sym, tf})
		}
	}

	// 候选池供给（watchlist 全量供给数据，universe 决定哪些派发事件）
	if wl, ok := core.(WatchlistProvider); ok && feeder != nil {
		sym := strings.ToUpper(s.Symbol())
		tf := strategyPrimaryTF(core)
		for _, w := range wl.Watchlist() {
			w = strings.ToUpper(strings.TrimSpace(w))
			if w == "" || w == sym {
				continue
			}
			feeder.EnsureSymbol(w, tf)
			e.addFeedHold(name, feedHold{w, tf})
		}
	}

	// universe 状态初始化
	if _, ok := core.(UniverseProvider); ok {
		refresh := 20
		if rp, ok2 := core.(UniverseRefreshProvider); ok2 {
			if v := rp.UniverseRefreshBars(); v > 0 {
				refresh = v
			}
		}
		e.uniMu.Lock()
		st := e.universes[name]
		if st == nil {
			st = &universeState{
				current: map[string]bool{strings.ToUpper(s.Symbol()): true},
				subs:    make(map[string]event.SubscriptionID),
				refresh: refresh,
			}
			e.universes[name] = st
		}
		e.uniMu.Unlock()
	}

	// 计划调度注册
	if sp, ok := core.(ScheduleProvider); ok {
		raw := strings.TrimSpace(sp.Schedule())
		if raw != "" {
			spec, err := ParseSchedule(raw)
			if err != nil {
				log.Printf("[StrategyEngine] strategy %s schedule %q invalid: %v", name, raw, err)
			} else {
				e.schedMu.Lock()
				e.scheduled[name] = &scheduledEntry{spec: spec, next: spec.NextAfter(time.Now())}
				e.schedMu.Unlock()
				e.ensureSchedulerLoop()
				log.Printf("[StrategyEngine] strategy %s scheduled: %s (next %s UTC)", name, raw, e.scheduledNext(name))
			}
		}
	}
}

func (e *Engine) scheduledNext(name string) time.Time {
	e.schedMu.Lock()
	defer e.schedMu.Unlock()
	if en, ok := e.scheduled[name]; ok {
		return en.next
	}
	return time.Time{}
}

// teardownExtensions 释放策略的全部扩展资源（Unregister / 重入 Start 时调用）。
func (e *Engine) teardownExtensions(name string) {
	e.mu.Lock()
	feeder := e.feeder
	holds := e.feedHolds[name]
	delete(e.feedHolds, name)
	e.mu.Unlock()
	if feeder != nil {
		for _, h := range holds {
			feeder.ReleaseSymbol(h.symbol, h.interval)
		}
	}

	e.schedMu.Lock()
	delete(e.scheduled, name)
	e.schedMu.Unlock()

	e.uniMu.Lock()
	st := e.universes[name]
	delete(e.universes, name)
	e.uniMu.Unlock()
	if st != nil && e.bus != nil {
		for sym, subID := range st.subs {
			e.bus.Unsubscribe(subID)
			log.Printf("[StrategyEngine] universe strategy %s removed %s", name, sym)
		}
	}
}

func (e *Engine) addFeedHold(name string, h feedHold) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.feedHolds[name] = append(e.feedHolds[name], h)
}

// maybeRefreshUniverse 在主周期 K 线分发时驱动 universe 刷新。
// s 用于引擎内状态键（=配置 id），core 是解包后的真实实例（接口断言用）。
func (e *Engine) maybeRefreshUniverse(s, core Strategy, bar model.Bar) {
	name := s.Name()
	e.uniMu.Lock()
	st := e.universes[name]
	if st == nil {
		e.uniMu.Unlock()
		return
	}
	st.barCount++
	if st.barCount%st.refresh != 0 {
		e.uniMu.Unlock()
		return
	}
	current := make([]string, 0, len(st.current))
	for sym := range st.current {
		current = append(current, sym)
	}
	barIndex := st.barCount
	e.uniMu.Unlock()

	prov, ok := core.(UniverseProvider)
	if !ok {
		return
	}
	sym := strings.ToUpper(s.Symbol())
	data := e.MarketData()
	ctx := UniverseContext{
		Time:     time.UnixMilli(bar.Time),
		Symbols:  current,
		BarIndex: barIndex,
		provider: data,
	}
	var next []string
	func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[StrategyEngine] panic in %s OnUniverse (recovered): %v", name, r)
			}
		}()
		next = prov.OnUniverse(ctx)
	}()
	e.applyUniverse(s, core, sym, next)
}

// applyUniverse 按新 universe 增删 symbol 订阅与 feeder 供给。
func (e *Engine) applyUniverse(s, core Strategy, primary string, next []string) {
	name := s.Name()
	tf := strategyPrimaryTF(s)
	e.mu.Lock()
	feeder := e.feeder
	e.mu.Unlock()

	desired := map[string]bool{primary: true} // 主 symbol 不可移除
	for _, sym := range next {
		sym = strings.ToUpper(strings.TrimSpace(sym))
		if sym != "" {
			desired[sym] = true
		}
	}

	e.uniMu.Lock()
	st := e.universes[name]
	if st == nil {
		e.uniMu.Unlock()
		return
	}
	var add, remove []string
	for sym := range desired {
		if !st.current[sym] {
			add = append(add, sym)
		}
	}
	for sym := range st.current {
		if !desired[sym] && sym != primary {
			remove = append(remove, sym)
		}
	}
	// 先改状态，再持锁外做 I/O
	for _, sym := range add {
		st.current[sym] = true
	}
	for _, sym := range remove {
		delete(st.current, sym)
	}
	e.uniMu.Unlock()

	for _, sym := range add {
		if feeder != nil {
			feeder.EnsureSymbol(sym, tf)
		}
		subSym := sym
		subID := e.bus.Subscribe(subSym, event.PrioNormal, func(evt event.Event) {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[StrategyEngine] panic in strategy %s dispatch (recovered): %v", name, r)
				}
			}()
			e.dispatch(s, evt)
		}, event.TypeTick, event.TypeOrderBook, event.TypeBar, event.TypeOrderUpdate)
		e.uniMu.Lock()
		if st2 := e.universes[name]; st2 != nil {
			st2.subs[subSym] = subID
		}
		e.uniMu.Unlock()
		log.Printf("[StrategyEngine] universe strategy %s added %s (%s)", name, subSym, tf)
	}
	for _, sym := range remove {
		e.uniMu.Lock()
		subID, ok := st.subs[sym]
		if ok {
			delete(st.subs, sym)
		}
		e.uniMu.Unlock()
		if ok {
			e.bus.Unsubscribe(subID)
		}
		if feeder != nil {
			feeder.ReleaseSymbol(sym, tf)
		}
		log.Printf("[StrategyEngine] universe strategy %s removed %s", name, sym)
	}
}

// ── 调度器循环 ──

const schedulerTick = 20 * time.Second

// ensureSchedulerLoop 懒启动分钟级调度循环（进程生命周期常驻）。
func (e *Engine) ensureSchedulerLoop() {
	e.schedOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(schedulerTick)
			defer ticker.Stop()
			for range ticker.C {
				e.runDueSchedules()
			}
		}()
	})
}

// runDueSchedules 触发所有到点的计划任务。
func (e *Engine) runDueSchedules() {
	now := time.Now()
	e.schedMu.Lock()
	due := make([]string, 0, len(e.scheduled))
	for name, en := range e.scheduled {
		if !en.next.After(now) {
			due = append(due, name)
			en.next = en.spec.NextAfter(now)
		}
	}
	e.schedMu.Unlock()
	for _, name := range due {
		e.fireSchedule(name, now)
	}
}

// fireSchedule 到点触发：有 OnSchedule 走回调，否则用最近一根主周期 K 线调 OnBar。
func (e *Engine) fireSchedule(name string, now time.Time) {
	e.mu.RLock()
	s, ok := e.strategies[name]
	e.mu.RUnlock()
	if !ok || !s.IsRunning() {
		return
	}
	core := UnwrapStrategy(s)
	if os, ok := core.(OnScheduler); ok {
		var sig *model.Signal
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[StrategyEngine] panic in %s OnSchedule (recovered): %v", name, r)
				}
			}()
			sig, err = os.OnSchedule(now, e.bus)
		}()
		e.emitSignal(s, sig, err)
		return
	}
	// 默认回调：用最近一根 K 线调 OnBar
	if bar, ok := e.MarketData().LastBar(s.Symbol()); ok {
		var sig *model.Signal
		var err error
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[StrategyEngine] panic in %s scheduled OnBar (recovered): %v", name, r)
				}
			}()
			sig, err = s.OnBar(bar, e.bus)
		}()
		e.emitSignal(s, sig, err)
	}
}

// emitSignal 是 dispatch 信号出口的复用：策略名覆盖、风控检查、
// OnSignal / 广播通知。dispatch 与调度回调共用。
func (e *Engine) emitSignal(s Strategy, signal *model.Signal, err error) {
	if err != nil || signal == nil {
		return
	}
	// 用包装后的策略名（=配置 id，如 7bb9a9a6）覆盖策略内部名（如
	// cra_contract）：下游按 signal.Strategy 查配置/维护投入资金，内部名
	// 永远查不到，多个同类型策略按 strategy_type 兜底匹配会张冠李戴。
	signal.Strategy = s.Name()
	// v1.2 契约钩子：CustomStakeAmount 折算默认数量，ConfirmTradeEntry/
	// ConfirmTradeExit 最后一刻否决（钩子 panic 回退默认行为，见 hooks.go）。
	if !e.applyTradeHooks(s, UnwrapStrategy(s), signal) {
		return
	}
	e.mu.RLock()
	onSignal := e.OnSignal
	protectionMgr := e.protectionMgr
	stratProt := e.strategyProtections[s.Name()]
	broadcaster := e.broadcaster
	wsHub := e.wsHub
	e.mu.RUnlock()
	// 策略级 protections（config_json["protections"]）先于全局检查：
	// 策略自己的风控约束未过就直接吞信号，不污染全局判定。
	if stratProt != nil {
		ctx := protectionContextFor(signal)
		if result := stratProt.CheckAll(ctx); result.Blocked {
			log.Printf("[protection] signal blocked for %s (strategy=%s, scope=strategy): %s",
				signal.Symbol, s.Name(), result.Reason)
			if broadcaster != nil {
				broadcaster.Protection("protection", signal.Symbol, "block", "strategy: "+result.Reason, 0)
				if wsHub != nil {
					wsHub.BroadcastProtection("protection", signal.Symbol, "block", "strategy: "+result.Reason)
				}
			}
			return
		}
	}
	if protectionMgr != nil {
		ctx := protectionContextFor(signal)
		result := protectionMgr.CheckAll(ctx)
		if result.Blocked {
			log.Printf("[protection] signal blocked for %s: %s", signal.Symbol, result.Reason)
			if broadcaster != nil {
				broadcaster.Protection("protection", signal.Symbol, "block", result.Reason, 0)
				if wsHub != nil {
					wsHub.BroadcastProtection("protection", signal.Symbol, "block", result.Reason)
				}
			}
			return
		}
	}
	if onSignal != nil {
		onSignal(*signal)
	}
	if broadcaster != nil {
		params := s.GetParameters()
		var paramMap map[string]any
		if params != nil {
			paramMap = params.ToMap()
		}
		broadcaster.Signal(signal.Symbol, signal.Direction, s.Name(), 0, paramMap)
		if wsHub != nil {
			wsHub.BroadcastSignal(*signal)
		}
	}
}

// protectionContextFor 构造风控检查上下文（与 dispatch 原逻辑一致）。
func protectionContextFor(signal *model.Signal) protection.ProtectionContext {
	return protection.ProtectionContext{
		Symbol:      signal.Symbol,
		CurrentTime: time.Now(),
	}
}
