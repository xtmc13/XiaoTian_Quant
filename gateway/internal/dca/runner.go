// Runner：让 DCA 定投机器人在网关进程里 7×24 跑起来。
//
// 与 grid.Runner 同一模式：每个运行中的 bot 独占一条 goroutine，以固定节拍
// 从注入式 PriceSource 取价并串行驱动 Engine（引擎非线程安全，绝不跨
// goroutine 调用）；成交逐笔落库 dca_bot_orders，累计状态单行 UPDATE 回
// dca_bots。Start/Stop 与 tick goroutine 之间用 map 互斥锁 + 每 bot 的
// stop/done channel 隔离。
package dca

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
	// botID 用于订单 client_oid 标记（"dca:<botID>"），供 A8.2 成交恢复路由回填。
	BuySpot(botID, symbol, exchange string, userID int64, quoteAmount, refPrice float64) (filledQty, avgPrice float64, err error)
	// SellSpot 卖出 baseQty 个现货。
	SellSpot(botID, symbol, exchange string, userID int64, baseQty, refPrice float64) (filledQty, avgPrice float64, err error)
}

// Runner 管理一组运行中的 DCA 机器人。
type Runner struct {
	mu    sync.Mutex
	price PriceSource
	repo  *store.DCARepo
	exec  Executor
	bots  map[string]*botRuntime

	// TickInterval 为取价驱动节拍，NewRunner 赋默认值，测试可调短。
	TickInterval time.Duration
}

// botRuntime 是一个运行中 bot 的独占状态；engine 只允许其 tick goroutine 触碰。
type botRuntime struct {
	record *store.DCABotRecord
	engine *Engine
	stopCh chan struct{}
	done   chan struct{} // tick goroutine 退出时关闭
}

// NewRunner 创建 DCA 机器人 Runner。priceSource 为注入式行情源
// （生产用 BinanceWS.GetPrice，测试用假源），repo 负责持久化，
// exec 负责下单（生产接 OMS，测试用 fake）。
func NewRunner(priceSource PriceSource, repo *store.DCARepo, exec Executor) *Runner {
	return &Runner{
		price:        priceSource,
		repo:         repo,
		exec:         exec,
		bots:         make(map[string]*botRuntime),
		TickInterval: 2 * time.Second,
	}
}

func configFromRecord(rec *store.DCABotRecord) Config {
	return Config{
		Symbol:          rec.Symbol,
		QuoteAmount:     rec.QuoteAmount,
		IntervalMinutes: rec.IntervalMinutes,
		MaxOrders:       rec.MaxOrders,
		PeriodBudget:    rec.PeriodBudget,
		TakeProfitPct:   rec.TakeProfitPct,
		StopLossPct:     rec.StopLossPct,
		TrailingEnabled: rec.TrailingEnabled,
	}
}

func hasPersistedState(rec *store.DCABotRecord) bool {
	return rec.FilledOrders > 0 || rec.TotalInvested > 0 || rec.BaseQty > 0 || rec.LastBuyAt > 0
}

// StartBot 启动一个机器人：已有运行时状态则从落库列恢复（Restore），
// 否则按 currentPrice 新建引擎；随后置 status=running 并启动独占 tick goroutine。
func (r *Runner) StartBot(record *store.DCABotRecord, currentPrice float64) error {
	if record == nil {
		return errors.New("dca runner: nil bot record")
	}
	if currentPrice <= 0 {
		return fmt.Errorf("dca runner: bot %s invalid start price %v", record.ID, currentPrice)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.bots[record.ID]; ok {
		return fmt.Errorf("dca runner: bot %s already running", record.ID)
	}

	cfg := configFromRecord(record)
	var eng *Engine
	if hasPersistedState(record) {
		eng = Restore(cfg, record.FilledOrders, record.TotalInvested, record.BaseQty,
			record.AvgPrice, record.RealizedPnL, record.LastBuyAt, record.HighestPrice, currentPrice)
	} else {
		eng = NewEngine(cfg, currentPrice)
	}

	rt := &botRuntime{
		record: record,
		engine: eng,
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	r.bots[record.ID] = rt

	r.persistState(rt)
	if err := r.repo.UpdateStatus(record.ID, "running"); err != nil {
		log.Printf("dca runner: bot %s update status: %v", record.ID, err)
	} else {
		record.Status = "running"
	}

	go r.runBot(rt)
	return nil
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
	<-rt.done // goroutine 退出前会完成最终一次状态持久化
	return r.repo.UpdateStatus(botID, status)
}

// StopAll 停止所有运行中的机器人（优雅退出用）。
func (r *Runner) StopAll() {
	for _, id := range r.ListRunning() {
		if err := r.StopBot(id, "stopped"); err != nil {
			log.Printf("dca runner: stop bot %s: %v", id, err)
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
// 用当前价启动（有运行时状态则按落库列恢复）。启动价无效时记录日志并跳过，
// 交由 RetryResume 下轮再试。
func (r *Runner) ResumeRunningBots(list []*store.DCABotRecord) {
	for _, rec := range list {
		if rec == nil || rec.Status != "running" {
			continue
		}
		if r.IsRunning(rec.ID) {
			continue
		}
		price := r.price(normalizeSymbol(rec.Symbol))
		if price <= 0 {
			log.Printf("dca runner: skip resume bot %s (%s): no live price", rec.ID, rec.Symbol)
			continue
		}
		if err := r.StartBot(rec, price); err != nil {
			log.Printf("dca runner: resume bot %s failed: %v", rec.ID, err)
		}
	}
}

// RetryResume 立即执行一次恢复，然后每 interval 重新拉取列表复查一遍：
// 防御进程重启漏恢复、以及上一轮因行情无效被跳过的机器人。
func (r *Runner) RetryResume(fetch func() ([]*store.DCABotRecord, error), interval time.Duration) {
	attempt := func() {
		list, err := fetch()
		if err != nil {
			log.Printf("dca runner: resume fetch: %v", err)
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

// runBot 是单个 bot 的独占 tick goroutine：串行驱动引擎，禁止他处触碰 rt.engine。
func (r *Runner) runBot(rt *botRuntime) {
	defer close(rt.done)
	symbol := normalizeSymbol(rt.record.Symbol)
	tick := time.NewTicker(r.TickInterval)
	defer tick.Stop()

	for {
		select {
		case <-rt.stopCh:
			r.persistState(rt) // 最终持久化：状态停在退出前一刻
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

// drive 执行一轮决策 + 下单 + 成交回报，并持久化状态。
func (r *Runner) drive(rt *botRuntime, price float64, now time.Time) {
	action := rt.engine.Tick(price, now)
	switch {
	case action.Buy != nil:
		filled, avg, err := r.exec.BuySpot(rt.record.ID, rt.record.Symbol, rt.record.Exchange,
			rt.record.UserID, action.Buy.QuoteAmount, price)
		if err != nil {
			log.Printf("dca runner: bot %s buy: %v", rt.record.ID, err)
			return
		}
		if filled > 0 {
			if err := rt.engine.ApplyBuyFill(filled, avg, now); err != nil {
				log.Printf("dca runner: bot %s apply buy fill: %v", rt.record.ID, err)
				return
			}
			if err := r.repo.InsertOrder(rt.record.ID, "buy", avg, filled, filled*avg, "schedule", now.UnixMilli()); err != nil {
				log.Printf("dca runner: bot %s insert order: %v", rt.record.ID, err)
			}
			r.persistState(rt)
		}
	case action.Sell != nil:
		qty := rt.engine.BaseQty()
		if qty <= 0 {
			return
		}
		filled, avg, err := r.exec.SellSpot(rt.record.ID, rt.record.Symbol, rt.record.Exchange,
			rt.record.UserID, qty, price)
		if err != nil {
			log.Printf("dca runner: bot %s sell: %v", rt.record.ID, err)
			return
		}
		if filled > 0 {
			if err := rt.engine.ApplySellFill(filled, avg); err != nil {
				log.Printf("dca runner: bot %s apply sell fill: %v", rt.record.ID, err)
				return
			}
			if err := r.repo.InsertOrder(rt.record.ID, "sell", avg, filled, filled*avg, action.Sell.Reason, now.UnixMilli()); err != nil {
				log.Printf("dca runner: bot %s insert order: %v", rt.record.ID, err)
			}
			r.persistState(rt)
			// 止盈/止损卖出后本轮定投周期结束 → finished。
			r.finish(rt)
		}
	case action.Done:
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
	r.persistState(rt)
	if !owned {
		return
	}
	if err := r.repo.UpdateStatus(rt.record.ID, "finished"); err != nil {
		log.Printf("dca runner: bot %s finish: %v", rt.record.ID, err)
	} else {
		rt.record.Status = "finished"
	}
	close(rt.stopCh)
}

// persistState 把引擎当前状态单行 UPDATE 回主表。
func (r *Runner) persistState(rt *botRuntime) {
	var lastBuyAt int64
	if t := rt.engine.LastBuyAt(); !t.IsZero() {
		lastBuyAt = t.UnixMilli()
	}
	if err := r.repo.UpdateState(rt.record.ID, rt.engine.FilledOrders(), rt.engine.TotalInvested(),
		rt.engine.BaseQty(), rt.engine.AvgPrice(), rt.engine.RealizedPnL(),
		lastBuyAt, rt.engine.HighestPrice()); err != nil {
		log.Printf("dca runner: bot %s persist state: %v", rt.record.ID, err)
	}
}

// normalizeSymbol 把 "BTC/USDT" 规范成 Binance WS 缓存键 "BTCUSDT"。
func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.ReplaceAll(symbol, "/", ""))
}

// ApplyRecoveredFill A8.2 成交恢复：把对账任务补录的成交回填给运行中的引擎
// （引擎 ApplyBuyFill/ApplySellFill 推进状态），并补记订单流水后持久化。
// bot 未在运行时返回错误（由调用方记录，下轮重试）。
func (r *Runner) ApplyRecoveredFill(botID, side string, qty, price float64) error {
	r.mu.Lock()
	rt, ok := r.bots[botID]
	r.mu.Unlock()
	if !ok {
		return fmt.Errorf("dca bot %s not running", botID)
	}
	now := time.Now()
	if strings.EqualFold(side, "SELL") {
		if err := rt.engine.ApplySellFill(qty, price); err != nil {
			return err
		}
		if err := r.repo.InsertOrder(rt.record.ID, "sell", price, qty, qty*price, "recovered", now.UnixMilli()); err != nil {
			log.Printf("dca runner: bot %s recovered order record: %v", rt.record.ID, err)
		}
	} else {
		if err := rt.engine.ApplyBuyFill(qty, price, now); err != nil {
			return err
		}
		if err := r.repo.InsertOrder(rt.record.ID, "buy", price, qty, qty*price, "recovered", now.UnixMilli()); err != nil {
			log.Printf("dca runner: bot %s recovered order record: %v", rt.record.ID, err)
		}
	}
	r.persistState(rt)
	return nil
}
