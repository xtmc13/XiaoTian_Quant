package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/xiaotian-quant/gateway/internal/app"
	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/notify"
	"github.com/xiaotian-quant/gateway/internal/order"
	"github.com/xiaotian-quant/gateway/internal/portfolio"
	"github.com/xiaotian-quant/gateway/internal/pystrat"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── Python 策略运行时生产接线 ──
// main 启动时：runner := handler.NewPyStratRunner() → handler.SetPyStratService(runner)
// → go runner.RetryResume(fetch, 60s)。路由片段见路由集成说明（/api/pystrategies 组）。

// pyStratSandboxFactory 创建校验用沙箱；测试可替换为 fake（不经 python 子进程）。
var pyStratSandboxFactory = func() pystrat.Sandbox {
	return pystrat.NewSubprocessSandbox()
}

// normalizePyStratSymbol 规范 symbol 到 Binance 缓存键形式。
func normalizePyStratSymbol(symbol string) string {
	s := strings.ToUpper(strings.TrimSpace(symbol))
	s = strings.ReplaceAll(s, "/", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	return s
}

// NewPyStratRunner 组装生产版 pystrat.Runner：
//   - BarSource:     KlineFeeder（引用计数轮询）→ 事件总线 TypeBar
//   - Executor:      OMSBotExecutor（kind="pystrat"，走 OMS 统一执行层，
//     paper 即时成交，live 过 canPlaceLiveOrder 实盘闸）
//   - Accounts:      portfolio manager 的 paper 账本持仓/权益
//   - LiveGate:      canPlaceLiveOrder（paper=0 启动前强制过闸）
//   - Protector:     条件单引擎 RegisterTPSL（context.set_stop_loss/take_profit 映射）
//   - Alert:         notify.Manager（连续 10 次错误暂停时告警）
func NewPyStratRunner() *pystrat.Runner {
	repo := store.NewPyStrategyRepo()
	src := &pystrat.EventBarSource{
		Ensure: func(symbol, interval string) (bool, error) {
			f := KlineFeederForEngine()
			if f == nil {
				return false, errors.New("kline feeder 未初始化")
			}
			return f.EnsureSymbol(symbol, interval), nil
		},
		Release: func(symbol, interval string) {
			if f := KlineFeederForEngine(); f != nil {
				f.ReleaseSymbol(symbol, interval)
			}
		},
		RecentBars: func(symbol, interval string, limit int) ([]model.Bar, error) {
			f := KlineFeederForEngine()
			if f == nil {
				return nil, errors.New("kline feeder 未初始化")
			}
			return f.RecentClosedBars(symbol, interval, limit)
		},
	}
	if appCtx := app.Get(); appCtx != nil {
		src.Bus = appCtx.EventBus
	}
	runner := pystrat.NewRunner(
		repo,
		func() pystrat.Sandbox { return pystrat.NewSubprocessSandbox() },
		src,
		NewKindOMSBotExecutor("pystrat"),
		pyStratAccounts{},
	)
	if appCtx := app.Get(); appCtx != nil && appCtx.EventBus != nil {
		// v1.1 on_order 落地：OMS OnOrderUpdate → 事件总线 TypeOrderUpdate，
		// 这里按 client_oid 前缀 "pystrat:<id>" 过滤后驱动沙箱 on_order。
		runner.SetOrderSource(&pystrat.EventOrderSource{Bus: appCtx.EventBus})
	}
	runner.SetLiveGate(func(exchange string) error { return canPlaceLiveOrder(exchange, true) })
	runner.SetProtector(pyStratProtector{})
	runner.SetAlert(func(title, content string) {
		notify.GetManager().Send(notify.Message{
			Title:   title,
			Content: content,
			Level:   "WARN",
		})
	})
	return runner
}

// pyStratAccounts 生产持仓/权益视图：读 portfolio manager "default" 账户
// （paper 单与真实成交都回填这里）。现货多仓 key 为 SYMBOL-BUY，
// 合约多仓为 SYMBOL-LONG；优先返回多仓。
type pyStratAccounts struct{}

func (pyStratAccounts) Position(symbol string) (qty, avgPrice float64, side string, ok bool) {
	pm := portfolio.GetManager()
	if pm == nil {
		return 0, 0, "", false
	}
	acct := pm.GetAccount("default")
	if acct == nil {
		return 0, 0, "", false
	}
	symbol = normalizePyStratSymbol(symbol)
	for _, suffix := range []string{"BUY", "LONG"} {
		pos, exists := acct.Positions[symbol+"-"+suffix]
		if !exists || pos.Quantity <= 0 {
			continue
		}
		negSide := "long"
		if pos.Side == string(model.SideSell) || pos.PositionSide == model.PositionShort {
			negSide = "short"
		}
		return pos.Quantity, pos.AvgEntryPrice, negSide, true
	}
	return 0, 0, "", false
}

func (pyStratAccounts) Equity() float64 {
	pm := portfolio.GetManager()
	if pm == nil {
		return 0
	}
	// paper 优先：paper 单的风控分母应是模拟权益（与 OMS risk context 一致）。
	if pe := pm.PaperEquity(); pe > 0 {
		return pe
	}
	return pm.TotalEquity()
}

// pyStratProtector 把成交后的 TP/SL 挂到条件单引擎（trigger 后市价平仓单
// 经 OMS 执行；价格监控由 wireConditionalEngine 的 BinanceWS 回调驱动）。
type pyStratProtector struct{}

func (pyStratProtector) SetBracket(strategyID, symbol, exchange string, userID int64, qty, entryPrice, tpPct, slPct float64) error {
	if qty <= 0 || entryPrice <= 0 {
		return errors.New("invalid qty/entryPrice for bracket")
	}
	parent := &model.OrderData{
		ID:           strategyID + "-entry",
		Symbol:       symbol,
		Side:         model.SideBuy, // 开仓方向；trigger 时条件单引擎反向平仓
		OrderType:    model.TypeMarket,
		Quantity:     qty,
		Exchange:     exchange,
		PositionSide: model.PositionLong,
		Price:        entryPrice,
		UserID:       uint64(userID),
		CreatedAt:    time.Now().UnixMilli(),
		UpdatedAt:    time.Now().UnixMilli(),
	}
	tpPrice, slPrice := 0.0, 0.0
	if tpPct > 0 {
		tpPrice = entryPrice * (1 + tpPct)
	}
	if slPct > 0 {
		slPrice = entryPrice * (1 - slPct)
	}
	order.GetConditionalEngine().RegisterTPSL(parent, tpPrice, slPrice)
	return nil
}
