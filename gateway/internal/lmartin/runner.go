// Runner：让分层马丁格尔机器人在网关进程里 7×24 跑起来。
//
// 与 grid/dca Runner 同一模式：每个运行中的 bot 独占一条 goroutine，以固定
// 节拍从注入式 PriceSource 取价并串行驱动 Engine；成交逐笔落库
// layered_martin_orders，每组状态落库 layered_martin_groups，主表累计字段
// 单行 UPDATE 回 layered_martin_bots。全部组到达终态后 bot 置 finished 并
// 退出运行表。Start/Stop 与 tick goroutine 之间用 map 互斥锁 + 每 bot 的
// stop/done channel 隔离。
package lmartin

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/store"
)

// PriceSource 注入式实时价格源：返回某 symbol 的最新价；0 或负值表示
// 当前无有效行情（如 WS 断连），调用方应跳过本轮驱动。
type PriceSource func(symbol string) float64

// Executor 执行现货下单并回报确认成交量：只有返回的 filledQty>0 才算
// 成交，引擎才推进状态（未成交不推进）。生产实现走 order.OMS（paper/live
// 均经过现有实盘安全闸），测试用 fake。
type Executor interface {
	// BuySpot 按约 quoteAmount 计价币买入现货，refPrice 为参考价（数量换算）。
	// botID 用于订单 client_oid 标记（"lmartin:<botID>"），供 A8.2 成交恢复路由回填。
	BuySpot(botID, symbol, exchange string, userID int64, quoteAmount, refPrice float64) (filledQty, avgPrice float64, err error)
	// SellSpot 卖出 baseQty 个现货。
	SellSpot(botID, symbol, exchange string, userID int64, baseQty, refPrice float64) (filledQty, avgPrice float64, err error)
}

// Runner 管理一组运行中的分层马丁格尔机器人。
type Runner struct {
	mu    sync.Mutex
	price PriceSource
	repo  *store.LayeredMartinRepo
	exec  Executor
	bots  map[string]*botRuntime

	// TickInterval 为取价驱动节拍，NewRunner 赋默认值，测试可调短。
	TickInterval time.Duration
}

// botRuntime 是一个运行中 bot 的独占状态；engine 只允许其 tick goroutine 触碰。
type botRuntime struct {
	record *store.LayeredMartinBotRecord
	engine *Engine
	groups []*store.LayeredMartinGroupRecord // 与 engine 组下标一一对应
	stopCh chan struct{}
	done   chan struct{} // tick goroutine 退出时关闭
}

// NewRunner 创建分层马丁格尔机器人 Runner。priceSource 为注入式行情源
// （生产用 BinanceWS.GetPrice，测试用假源），repo 负责持久化，
// exec 负责下单（生产接 OMS，测试用 fake）。
func NewRunner(priceSource PriceSource, repo *store.LayeredMartinRepo, exec Executor) *Runner {
	return &Runner{
		price:        priceSource,
		repo:         repo,
		exec:         exec,
		bots:         make(map[string]*botRuntime),
		TickInterval: 2 * time.Second,
	}
}

func configFromRecord(rec *store.LayeredMartinBotRecord, groups []*store.LayeredMartinGroupRecord) Config {
	cfg := Config{
		Symbol:          rec.Symbol,
		DeviationPct:    rec.PriceDeviationPct,
		TakeProfitPct:   rec.TakeProfitPct,
		StopLossPct:     rec.StopLossPct,
		TrailingEnabled: rec.TrailingEnabled,
	}
	for _, g := range groups {
		if g == nil {
			continue
		}
		cfg.Groups = append(cfg.Groups, GroupConfig{
			QuoteAmount: g.QuoteAmount,
			Multiplier:  g.Multiplier,
			MaxLayers:   g.MaxLayers,
			BudgetCap:   g.BudgetCap,
		})
	}
	return cfg
}

// StartBot 启动一个机器人：按组的落库状态恢复引擎（层数/投入/持仓/在途层），
// 随后置 status=running 并启动独占 tick goroutine。无任何运行状态的组
// （新建/未曾启动）由引擎在首个 Tick 立即开首层。
func (r *Runner) StartBot(record *store.LayeredMartinBotRecord, groups []*store.LayeredMartinGroupRecord, currentPrice float64) error {
	if record == nil {
		return errors.New("lmartin runner: nil bot record")
	}
	if currentPrice <= 0 {
		return fmt.Errorf("lmartin runner: bot %s invalid start price %v", record.ID, currentPrice)
	}
	if len(groups) == 0 {
		return fmt.Errorf("lmartin runner: bot %s has no groups", record.ID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.bots[record.ID]; ok {
		return fmt.Errorf("lmartin runner: bot %s already running", record.ID)
	}

	eng := NewEngine(configFromRecord(record, groups), currentPrice)
	// 恢复每组运行状态（pending 订单视为已失效——重启后在途单不可追溯，清
	// 除后由价格条件重新触发，宁可不重加也不重复加仓）。
	for i, grec := range groups {
		if grec == nil || i >= eng.GroupCount() {
			continue
		}
		gs := eng.Group(i)
		restoreGroupState(gs, grec)
	}

	rt := &botRuntime{
		record: record,
		engine: eng,
		groups: groups,
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	r.bots[record.ID] = rt

	r.persistGroups(rt)
	if err := r.repo.UpdateStatus(record.ID, "running"); err != nil {
		log.Printf("lmartin runner: bot %s update status: %v", record.ID, err)
	} else {
		record.Status = "running"
	}

	go r.runBot(rt)
	return nil
}

// restoreGroupState 把落库组状态灌入引擎组（在途层清零，见 StartBot 注释）。
func restoreGroupState(gs *GroupState, rec *store.LayeredMartinGroupRecord) {
	gs.SetState(rec.Layer, rec.TotalInvested, rec.BaseQty, rec.AvgPrice, rec.EntryPrice, rec.HighestPrice, rec.PendingLayer, rec.Status)
}

// StopBot 停止指定机器人：通知其 tick goroutine 退出并等待最终持久化
// 完成，然后将 status 置为 stopped/finished。对已停止的 bot 是 no-op。
func (r *Runner) StopBot(botID string, status string) error {
	r.mu.Lock()
	rt, ok := r.bots[botID]
	if ok {
		delete(r.bots, botID)
	}
	r.mu.Unlock()
	if !ok {
		return nil
	}
	if status == "" {
		status = "stopped"
	}

	close(rt.stopCh)
	<-rt.done // goroutine 退出前会完成最终一次组状态持久化
	return r.repo.UpdateStatus(botID, status)
}

// StopAll 停止所有运行中的机器人（优雅退出用）。
func (r *Runner) StopAll() {
	for _, id := range r.ListRunning() {
		if err := r.StopBot(id, "stopped"); err != nil {
			log.Printf("lmartin runner: stop bot %s: %v", id, err)
		}
	}
}

// IsRunning 报告机器人是否由本 Runner 在内存中驱动。
func (r *Runner) IsRunning(botID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.bots[botID]
	return ok
}

// ListRunning 返回所有运行中的机器人 ID。
func (r *Runner) ListRunning() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	ids := make([]string, 0, len(r.bots))
	for id := range r.bots {
		ids = append(ids, id)
	}
	return ids
}

// ResumeRunningBots 对列表中 status='running' 且本机未驱动的机器人逐个
// 恢复（含每组状态）。启动价无效时记录日志并跳过，交由 RetryResume 下轮再试。
func (r *Runner) ResumeRunningBots(list []*store.LayeredMartinBotRecord, fetchGroups func(botID string) ([]*store.LayeredMartinGroupRecord, error)) {
	for _, rec := range list {
		if rec == nil || rec.Status != "running" {
			continue
		}
		if r.IsRunning(rec.ID) {
			continue
		}
		price := r.price(normalizeSymbol(rec.Symbol))
		if price <= 0 {
			log.Printf("lmartin runner: skip resume bot %s (%s): no live price", rec.ID, rec.Symbol)
			continue
		}
		groups, err := fetchGroups(rec.ID)
		if err != nil {
			log.Printf("lmartin runner: bot %s load groups: %v", rec.ID, err)
			continue
		}
		if err := r.StartBot(rec, groups, price); err != nil {
			log.Printf("lmartin runner: resume bot %s failed: %v", rec.ID, err)
		}
	}
}

// RetryResume 立即执行一次恢复，然后每 interval 重新拉取列表复查一遍：
// 防御进程重启漏恢复、以及上一轮因行情无效被跳过的机器人。
func (r *Runner) RetryResume(fetch func() ([]*store.LayeredMartinBotRecord, error), fetchGroups func(botID string) ([]*store.LayeredMartinGroupRecord, error), interval time.Duration) {
	attempt := func() {
		list, err := fetch()
		if err != nil {
			log.Printf("lmartin runner: resume fetch: %v", err)
			return
		}
		r.ResumeRunningBots(list, fetchGroups)
	}
	attempt()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for range ticker.C {
		attempt()
	}
}

// runBot 是单个 bot 的独占 tick goroutine：串行驱动引擎，禁止他处触碰 rt.engine。
func (r *Runner) runBot(rt *botRuntime) {
	defer close(rt.done)
	symbol := normalizeSymbol(rt.record.Symbol)
	tick := time.NewTicker(r.TickInterval)
	defer tick.Stop()

	for {
		select {
		case <-rt.stopCh:
			r.persistGroups(rt) // 最终持久化：状态停在退出前一刻
			return
		case now := <-tick.C:
			price := r.price(symbol)
			if price <= 0 {
				continue // 行情源无效（WS 断连/旧值），本轮跳过
			}
			r.drive(rt, price, now)
		}
	}
}

// drive 执行一轮决策 + 下单 + 成交回报，并持久化组状态。
func (r *Runner) drive(rt *botRuntime, price float64, now time.Time) {
	action := rt.engine.Tick(price)

	// 先卖后买：止盈/止损优先于加仓。
	for _, s := range action.Sells {
		qty := rt.engine.Group(s.Group).BaseQty()
		if qty <= 0 {
			continue
		}
		filled, avg, err := r.exec.SellSpot(rt.record.ID, rt.record.Symbol, rt.record.Exchange,
			rt.record.UserID, qty, price)
		if err != nil {
			log.Printf("lmartin runner: bot %s group %d sell: %v", rt.record.ID, s.Group, err)
			continue
		}
		if filled > 0 {
			if err := rt.engine.ApplySellFill(s.Group, filled, avg); err != nil {
				log.Printf("lmartin runner: bot %s group %d apply sell fill: %v", rt.record.ID, s.Group, err)
				continue
			}
			if err := r.repo.InsertOrder(rt.record.ID, s.Group, "sell", 0, avg, filled, filled*avg, s.Reason, now.UnixMilli()); err != nil {
				log.Printf("lmartin runner: bot %s insert order: %v", rt.record.ID, err)
			}
		}
	}

	for _, b := range action.Buys {
		if err := rt.engine.MarkPending(b.Group, b.Layer); err != nil {
			log.Printf("lmartin runner: bot %s group %d mark pending: %v", rt.record.ID, b.Group, err)
			continue
		}
		filled, avg, err := r.exec.BuySpot(rt.record.ID, rt.record.Symbol, rt.record.Exchange,
			rt.record.UserID, b.QuoteAmount, price)
		if err != nil {
			rt.engine.CancelPending(b.Group)
			log.Printf("lmartin runner: bot %s group %d layer %d buy: %v", rt.record.ID, b.Group, b.Layer, err)
			continue
		}
		if filled > 0 {
			if err := rt.engine.ApplyBuyFill(b.Group, b.Layer, filled, avg); err != nil {
				log.Printf("lmartin runner: bot %s group %d apply buy fill: %v", rt.record.ID, b.Group, err)
				continue
			}
			if err := r.repo.InsertOrder(rt.record.ID, b.Group, "buy", b.Layer, avg, filled, filled*avg, "layer_entry", now.UnixMilli()); err != nil {
				log.Printf("lmartin runner: bot %s insert order: %v", rt.record.ID, err)
			}
		}
		// filled==0：保留 pending，成交回报（含 0 成交）前不推进层数。
	}

	// 无成交的轮次不写库（与 grid 一致，避免空转热点）；终态/停止时另行持久化。
	if len(action.Sells)+len(action.Buys) > 0 {
		r.persistGroups(rt)
		r.persistBotState(rt)
	}

	if action.Finished {
		r.finish(rt)
	}
}

// finish 将 bot 置为 finished 并退出其 tick goroutine。若 bot 已不在
// 运行表（并发 StopBot 进行中），则不接管状态/通道，交由 StopBot 收尾。
func (r *Runner) finish(rt *botRuntime) {
	r.mu.Lock()
	_, owned := r.bots[rt.record.ID]
	if owned {
		delete(r.bots, rt.record.ID)
	}
	r.mu.Unlock()
	r.persistGroups(rt)
	r.persistBotState(rt)
	if !owned {
		return
	}
	if err := r.repo.UpdateStatus(rt.record.ID, "finished"); err != nil {
		log.Printf("lmartin runner: bot %s finish: %v", rt.record.ID, err)
	} else {
		rt.record.Status = "finished"
	}
	close(rt.stopCh)
}

// persistGroups 把引擎各组状态 UPDATE 回 groups 表（按组主键 id）。
func (r *Runner) persistGroups(rt *botRuntime) {
	for i, grec := range rt.groups {
		if grec == nil || i >= rt.engine.GroupCount() {
			continue
		}
		gs := rt.engine.Group(i)
		grec.Layer = gs.Layer()
		grec.TotalInvested = gs.TotalInvested()
		grec.BaseQty = gs.BaseQty()
		grec.AvgPrice = gs.AvgPrice()
		grec.EntryPrice = gs.EntryPrice()
		grec.HighestPrice = gs.HighestPrice()
		grec.PendingLayer = gs.PendingLayer()
		grec.Status = gs.Status()
		if err := r.repo.UpdateGroup(grec); err != nil {
			log.Printf("lmartin runner: bot %s group %d persist: %v", rt.record.ID, i, err)
		}
	}
}

// persistBotState 把累计盈亏/成交数单行 UPDATE 回主表。
func (r *Runner) persistBotState(rt *botRuntime) {
	if err := r.repo.UpdateState(rt.record.ID, rt.engine.RealizedPnL(), rt.engine.TotalTrades()); err != nil {
		log.Printf("lmartin runner: bot %s persist state: %v", rt.record.ID, err)
	}
}

// normalizeSymbol 把 "BTC/USDT" 规范成 Binance WS 缓存键 "BTCUSDT"。
func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.ReplaceAll(symbol, "/", ""))
}

// ApplyRecoveredFill A8.2 成交恢复：回填运行中引擎的在途层成交。
// 目标层选择：优先组内 pendingLayer（在途订单），否则该组下一层（layer+1）。
// bot 未运行或没有可用组时返回错误，由调用方记录后下轮重试。
func (r *Runner) ApplyRecoveredFill(botID, side string, qty, price float64) error {
	r.mu.Lock()
	rt, ok := r.bots[botID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("lmartin bot %s not running", botID)
	}

	group, layer, found := -1, 0, false
	for i := 0; i < rt.engine.GroupCount(); i++ {
		gs := rt.engine.Group(i)
		if gs.Finished() {
			continue
		}
		if gs.PendingLayer() > 0 {
			group, layer, found = i, gs.PendingLayer(), true
			break
		}
		if !found {
			group, layer, found = i, gs.Layer()+1, true
		}
	}
	if !found {
		return fmt.Errorf("lmartin bot %s has no active group", botID)
	}

	now := time.Now()
	if strings.EqualFold(side, "SELL") {
		if err := rt.engine.ApplySellFill(group, qty, price); err != nil {
			return err
		}
		if err := r.repo.InsertOrder(rt.record.ID, group, "sell", layer, price, qty, qty*price, "recovered", now.UnixMilli()); err != nil {
			log.Printf("lmartin runner: bot %s recovered order record: %v", rt.record.ID, err)
		}
	} else {
		if err := rt.engine.ApplyBuyFill(group, layer, qty, price); err != nil {
			return err
		}
		if err := r.repo.InsertOrder(rt.record.ID, group, "buy", layer, price, qty, qty*price, "recovered", now.UnixMilli()); err != nil {
			log.Printf("lmartin runner: bot %s recovered order record: %v", rt.record.ID, err)
		}
	}
	r.persistGroups(rt)
	r.persistBotState(rt)
	return nil
}
