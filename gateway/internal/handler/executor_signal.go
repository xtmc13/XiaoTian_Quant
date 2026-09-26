package handler

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"github.com/xiaotian-quant/gateway/internal/model"
	"github.com/xiaotian-quant/gateway/internal/store"
)

// ── 信号机器人：信号落库 + 执行入口（保证金模式 / hedge 双腿）──
//
// webhook 收到信号后先落 SignalRepo（补齐此前缺失的持久化链路），
// 带保证金/止盈止损字段的信号走 executeSignalRecord：按 margin_mode
// 构造合约单（hedge 用 positionSide=LONG/SHORT 双腿记账，同 symbol
// 多空双开互不覆盖），成交后挂阶梯 TP/SL 执行记录。

// 支持的保证金模式（对齐 CryptoRobotics）：
//   - isolated 逐仓单腿
//   - cross    全仓单腿
//   - hedge    双向持仓：同一 symbol 可多空双开，双腿各自独立的
//     positionSide + 阶梯 TP/SL（本地组合持仓按 symbol-LONG/SHORT 双键记账）
const (
	marginModeIsolated = "isolated"
	marginModeCross    = "cross"
	marginModeHedge    = "hedge"
)

var (
	executorSignalRepo = store.NewSignalRepo()
	executorExecRepo   = store.NewSignalExecutionRepo()
	executorSourceRepo = store.NewSignalSourceRepo()
	executorSourceOnce sync.Once
)

// ensureExecutorSources 懒初始化默认信号源（webhook 落库的回退归属）。
func ensureExecutorSources() string {
	var id string
	executorSourceOnce.Do(func() {
		id = executorSourceRepo.EnsureDefaultSource()
	})
	if id == "" {
		id = "default"
	}
	return id
}

// resolveSignalSourceID 解析信号归属源：body 里的 source_id / source
// 能匹配已配置信号源则用之，否则落默认源。
func resolveSignalSourceID(body map[string]any) string {
	ensureExecutorSources()
	if sid := getString(body, "source_id", ""); sid != "" {
		if _, err := executorSourceRepo.GetByID(sid); err == nil {
			return sid
		}
	}
	if name := getString(body, "source", ""); name != "" {
		if srcs, err := executorSourceRepo.List(false); err == nil {
			for _, s := range srcs {
				if s.Name == name || s.ID == name {
					return s.ID
				}
			}
		}
	}
	return "default"
}

// normalizeMarginMode 校验并归一保证金模式；spot 下 hedge/isolated 非法。
func normalizeMarginMode(raw, marketType string) (string, error) {
	mode := strings.ToLower(strings.TrimSpace(raw))
	if mode == "" {
		mode = marginModeCross
	}
	switch mode {
	case marginModeCross, marginModeIsolated, marginModeHedge:
	default:
		return "", fmt.Errorf("unknown margin_mode: %s (expect isolated|cross|hedge)", raw)
	}
	if marketType != string(model.MarketSwap) && mode != marginModeCross {
		return "", fmt.Errorf("margin_mode %s requires market_type=swap", mode)
	}
	return mode, nil
}

// recordSignalFromWebhook 把 webhook 收到的信号落库（此前链路缺失的环节）。
// 返回落库记录；调用方决定走普通成交流程还是信号执行流程。
func recordSignalFromWebhook(body map[string]any, symbol, direction, sourceID string) *store.SignalRecord {
	side := strings.ToUpper(direction)
	sig := &store.SignalRecord{
		Symbol:       strings.ToUpper(symbol),
		Direction:    side,
		Strength:     getFloat(body, "strength", 0),
		Strategy:     getString(body, "strategy", "webhook"),
		Reason:       getString(body, "reason", ""),
		EntryPrice:   getFloat(body, "price", 0),
		StopLoss:     getFloat(body, "stop_loss", getFloat(body, "sl", 0)),
		TakeProfit:   getFloat(body, "take_profit", getFloat(body, "tp", 0)),
		PositionSize: getFloat(body, "quantity", getFloat(body, "qty", 0)),
		Status:       "PENDING",
		SourceID:     sourceID,
		MarginMode:   strings.ToLower(getString(body, "margin_mode", marginModeCross)),
		TP1:          getFloat(body, "tp1", 0),
		TP2:          getFloat(body, "tp2", 0),
		TP3:          getFloat(body, "tp3", 0),
		TP1Pct:       getFloat(body, "tp1_pct", 0),
		TP2Pct:       getFloat(body, "tp2_pct", 0),
		TP3Pct:       getFloat(body, "tp3_pct", 0),
	}
	if sig.Direction == "BUY" || sig.Direction == "LONG" {
		sig.Direction = "LONG"
	} else if sig.Direction == "SELL" || sig.Direction == "SHORT" {
		sig.Direction = "SHORT"
	}
	if err := executorSignalRepo.Create(sig); err != nil {
		log.Printf("[executor] signal persist failed: %v", err)
		return nil
	}
	return sig
}

// isSignalStyleBody 判断 payload 是否信号式（带保证金模式或止盈止损），
// 是则走信号执行管线（阶梯 TP/SL），否则保持原 webhook 直接成交流程。
func isSignalStyleBody(body map[string]any) bool {
	if raw := getString(body, "margin_mode", ""); raw != "" {
		return true
	}
	for _, k := range []string{"stop_loss", "sl", "take_profit", "tp", "tp1", "tp2", "tp3"} {
		if getFloat(body, k, 0) > 0 {
			return true
		}
	}
	return false
}

// executeSignalRecord 执行一条已落库的信号：
//  1. 校验保证金模式（hedge/isolated 限合约）
//  2. 构造入场单（hedge → positionSide 双腿；isolated/cross → 标注 + 校验）
//  3. 成交后生成阶梯 TP/SL 执行记录并挂监控
func executeSignalRecord(sig *store.SignalRecord, body map[string]any) (*store.SignalExecution, error) {
	if sig == nil {
		return nil, fmt.Errorf("signal is nil")
	}
	marketType := getString(body, "market_mode", getString(body, "market_type", "swap"))
	mode, err := normalizeMarginMode(sig.MarginMode, marketType)
	if err != nil {
		sig.Status = "FAILED"
		_ = executorSignalRepo.Update(sig)
		return nil, err
	}
	sig.MarginMode = mode

	price := sig.EntryPrice
	if price <= 0 {
		price = getFloat(body, "price", 0)
	}
	qty := sig.PositionSize
	if qty <= 0 {
		return nil, fmt.Errorf("signal %d: quantity required", sig.ID)
	}
	if price <= 0 {
		return nil, fmt.Errorf("signal %d: price required for signal execution", sig.ID)
	}

	// 双腿标识：LONG/SHORT 与方向一致（hedge 模式下双腿仓位键 symbol-LONG/SHORT 互不覆盖）
	positionSide := model.PositionLong
	side := "BUY"
	if sig.Direction == "SHORT" {
		positionSide = model.PositionShort
		side = "SELL"
	}
	// 账本 margin_mode 只有 cross/isolated：hedge 在账本侧等价全仓双腿
	bookMarginMode := mode
	if bookMarginMode == marginModeHedge {
		bookMarginMode = marginModeCross
	}
	leverage := getFloat(body, "leverage", 1)
	if leverage <= 0 {
		leverage = 1
	}

	order := map[string]any{
		"symbol":        sig.Symbol,
		"side":          side,
		"type":          "MARKET",
		"price":         price,
		"quantity":      qty,
		"source":        "signal_executor",
		"strategy":      sig.Strategy,
		"market_type":   marketType,
		"position_side": string(positionSide),
		"leverage":      leverage,
		"margin_mode":   bookMarginMode,
	}
	fillOrderAndUpdatePortfolio(order)
	if order["status"] != "FILLED" {
		sig.Status = "FAILED"
		_ = executorSignalRepo.Update(sig)
		return nil, fmt.Errorf("signal %d: entry order not filled", sig.ID)
	}

	// 阶梯档位：信号显式 tp1/2/3 优先；否则单档止盈用 take_profit 当 TP3
	tp1, tp2, tp3 := sig.TP1, sig.TP2, sig.TP3
	if tp1 == 0 && tp2 == 0 && tp3 == 0 && sig.TakeProfit > 0 {
		tp3 = sig.TakeProfit
	}
	pct1, pct2, pct3 := sig.TP1Pct, sig.TP2Pct, sig.TP3Pct
	if pct1 <= 0 && pct2 <= 0 && pct3 <= 0 {
		pct1, pct2, pct3 = 0.4, 0.3, 0.3
	}
	// 未填比例归一到剩余仓位
	if pct1+pct2+pct3 <= 0 {
		pct1 = 1
	}

	now := time.Now().UnixMilli()
	exec := &store.SignalExecution{
		SignalID:     sig.ID,
		SourceID:     sig.SourceID,
		Symbol:       sig.Symbol,
		Direction:    string(positionSide),
		MarginMode:   mode,
		PositionSide: string(positionSide),
		PositionID:   sig.Symbol + "-" + string(positionSide),
		Status:       "active",
		EntryPrice:   price,
		EntryQty:     qty,
		EntryTime:    now,
		TP1Price:     tp1,
		TP1Qty:       qty * pct1,
		TP2Price:     tp2,
		TP2Qty:       qty * pct2,
		TP3Price:     tp3,
		TP3Qty:       qty * pct3,
		SLPrice:      sig.StopLoss,
		CurrentSL:    sig.StopLoss,
		MoveSLAfter:  getFloat(body, "move_sl_after", 0),
		MoveSLTo:     getFloat(body, "move_sl_to", 0),
		TrailingPct:  getFloat(body, "trailing_pct", getFloat(body, "trailing_sl", 0)),
		RemainingQty: qty,
		CreatedAt:    now,
	}
	// 当前有效 TP = 第一个未触发档位
	for _, lv := range []float64{tp1, tp2, tp3} {
		if lv > 0 {
			exec.CurrentTP = lv
			break
		}
	}

	if err := executorExecRepo.Create(exec); err != nil {
		log.Printf("[executor] execution record create failed: %v", err)
		sig.Status = "FAILED"
		_ = executorSignalRepo.Update(sig)
		return nil, err
	}

	// 信号记录推进到 EXECUTED
	sig.Status = "EXECUTED"
	sig.ExecutedOrderID = exec.ID
	if err := executorSignalRepo.Update(sig); err != nil {
		log.Printf("[executor] signal %d update failed: %v", sig.ID, err)
	}
	sigTPSLManager.attach(exec)
	startTPSLLoop()
	log.Printf("[executor] signal %d executed: %s %s %s qty=%.4f price=%.2f mode=%s",
		sig.ID, sig.Strategy, exec.Direction, sig.Symbol, qty, price, mode)
	return exec, nil
}
