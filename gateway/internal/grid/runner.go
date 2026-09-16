// Runner：让网格机器人在网关进程里 7×24 跑起来。
//
// 每个运行中的 bot 独占一条 goroutine，以固定节拍从注入式 PriceSource
// 取价并串行驱动 Engine（引擎非线程安全，绝不跨 goroutine 调用）；
// 成交逐笔落库 grid_bot_trades，累计状态单行 UPDATE 回 grid_bots，
// 周期权益快照落库 grid_bot_snapshots。Runner 的 Start/Stop 与 tick
// goroutine 之间用 map 互斥锁 + 每 bot 的 stop/done channel 隔离。
package grid

import (
	"encoding/json"
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

// Runner 管理一组运行中的网格机器人。
type Runner struct {
	mu    sync.Mutex
	price PriceSource
	repo  *store.GridRepo
	bots  map[string]*botRuntime

	// TickInterval 为取价驱动节拍，SnapshotInterval 为权益快照节拍；
	// NewRunner 赋默认值，测试可调短。
	TickInterval     time.Duration
	SnapshotInterval time.Duration
}

// botRuntime 是一个运行中 bot 的独占状态；engine 只允许其 tick goroutine 触碰。
type botRuntime struct {
	record *store.GridBotRecord
	engine *Engine
	stopCh chan struct{}
	done   chan struct{} // tick goroutine 退出时关闭
}

// NewRunner 创建网格机器人 Runner。priceSource 为注入式行情源
// （生产用 BinanceWS.GetPrice，测试用假源），repo 负责持久化。
func NewRunner(priceSource PriceSource, repo *store.GridRepo) *Runner {
	return &Runner{
		price:            priceSource,
		repo:             repo,
		bots:             make(map[string]*botRuntime),
		TickInterval:     2 * time.Second,
		SnapshotInterval: 60 * time.Second,
	}
}

// StartBot 启动一个机器人：state_json 非空则从保存状态恢复（LoadState），
// 否则按 currentPrice 开网（NewEngine）；随后置 status=running、补记
// started_at，并启动其独占 tick goroutine。
func (r *Runner) StartBot(record *store.GridBotRecord, currentPrice float64) error {
	if record == nil {
		return errors.New("grid runner: nil bot record")
	}
	if currentPrice <= 0 {
		return fmt.Errorf("grid runner: bot %s invalid start price %v", record.ID, currentPrice)
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.bots[record.ID]; ok {
		return fmt.Errorf("grid runner: bot %s already running", record.ID)
	}

	cfg := Config{
		Symbol:       record.Symbol,
		Lower:        record.LowerPrice,
		Upper:        record.UpperPrice,
		GridCount:    record.GridCount,
		Investment:   record.Investment,
		FeeRate:      record.FeeRate,
		CurrentPrice: currentPrice,
	}

	var (
		eng *Engine
		err error
	)
	stateJSON := strings.TrimSpace(record.StateJSON)
	if stateJSON != "" && stateJSON != "{}" {
		var state map[string]any
		if uerr := json.Unmarshal([]byte(stateJSON), &state); uerr != nil {
			return fmt.Errorf("grid runner: bot %s state_json: %w", record.ID, uerr)
		}
		eng, err = LoadState(cfg, state)
		if err != nil {
			return fmt.Errorf("grid runner: bot %s load state: %w", record.ID, err)
		}
	} else {
		eng, _, err = NewEngine(cfg)
		if err != nil {
			return fmt.Errorf("grid runner: bot %s new engine: %w", record.ID, err)
		}
	}

	rt := &botRuntime{
		record: record,
		engine: eng,
		stopCh: make(chan struct{}),
		done:   make(chan struct{}),
	}
	r.bots[record.ID] = rt

	// 初始状态立即落库：保证 state_json 永远是可恢复的完整状态。
	r.persistState(rt)
	if err := r.repo.UpdateStatus(record.ID, "running"); err != nil {
		log.Printf("grid runner: bot %s update status: %v", record.ID, err)
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
			log.Printf("grid runner: stop bot %s: %v", id, err)
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
// 用当前价启动（state_json 非空时按保存状态恢复）。启动价无效（行情源
// 暂无报价）时记录日志并跳过，交由 RetryResume 下轮再试。
func (r *Runner) ResumeRunningBots(list []*store.GridBotRecord) {
	for _, rec := range list {
		if rec == nil || rec.Status != "running" {
			continue
		}
		if r.IsRunning(rec.ID) {
			continue
		}
		price := r.price(normalizeSymbol(rec.Symbol))
		if price <= 0 {
			log.Printf("grid runner: skip resume bot %s (%s): no live price", rec.ID, rec.Symbol)
			continue
		}
		if err := r.StartBot(rec, price); err != nil {
			log.Printf("grid runner: resume bot %s failed: %v", rec.ID, err)
		}
	}
}

// RetryResume 立即执行一次恢复，然后每 interval 重新拉取列表复查一遍：
// 防御进程重启漏恢复、以及上一轮因行情无效被跳过的机器人。
func (r *Runner) RetryResume(fetch func() ([]*store.GridBotRecord, error), interval time.Duration) {
	attempt := func() {
		list, err := fetch()
		if err != nil {
			log.Printf("grid runner: resume fetch: %v", err)
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
	snap := time.NewTicker(r.SnapshotInterval)
	defer tick.Stop()
	defer snap.Stop()

	for {
		select {
		case <-rt.stopCh:
			r.persistState(rt) // 最终持久化：状态停在退出前一刻
			return
		case <-tick.C:
			price := r.price(symbol)
			if price <= 0 {
				continue // 行情源无效（WS 断连/旧值），本轮跳过
			}
			fills := rt.engine.OnPriceTick(price, time.Now().UnixMilli())
			for _, f := range fills {
				if err := r.repo.InsertTrade(rt.record.ID, f.Level, string(f.Side),
					f.Price, f.Qty, f.QuoteQty, f.Fee, f.Pnl, f.Ts); err != nil {
					log.Printf("grid runner: bot %s insert trade: %v", rt.record.ID, err)
				}
			}
			if len(fills) > 0 {
				r.persistState(rt)
			}
		case <-snap.C:
			price := r.price(symbol)
			if price <= 0 {
				continue // 无有效标记价，权益无意义，跳过本轮快照
			}
			equity, realized, openOrders := rt.engine.Snapshot(price, time.Now().UnixMilli())
			if err := r.repo.InsertSnapshot(rt.record.ID, equity, price, realized, openOrders, time.Now().UnixMilli()); err != nil {
				log.Printf("grid runner: bot %s insert snapshot: %v", rt.record.ID, err)
			}
		}
	}
}

// persistState 把引擎当前状态导出为 state_json 并单行 UPDATE 回主表。
func (r *Runner) persistState(rt *botRuntime) {
	state, err := json.Marshal(rt.engine.ExportState())
	if err != nil {
		log.Printf("grid runner: bot %s export state: %v", rt.record.ID, err)
		return
	}
	if err := r.repo.UpdateState(rt.record.ID, string(state), rt.engine.BaseQty(),
		rt.engine.QuoteBalance(), rt.engine.RealizedPnL(), rt.engine.TotalTrades()); err != nil {
		log.Printf("grid runner: bot %s persist state: %v", rt.record.ID, err)
	}
}

// normalizeSymbol 把 "BTC/USDT" 规范成 Binance WS 缓存键 "BTCUSDT"。
func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.ReplaceAll(symbol, "/", ""))
}
