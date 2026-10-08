package cra

import (
	"fmt"
	"strings"

	"github.com/xiaotian-quant/gateway/internal/model"
)

// ── G1：运行时手动操控四件套（币富名词解释 #23/#24/#25/#28）──
//
// 币富 CRA 对运行中的任务有四个手动操控，本实现全程复用既有安全语义，不开
// 新通道：动作在策略侧校验状态后产出普通 model.Signal（Tag="manual"），经
// 引擎 emitSignal 走与自动信号完全相同的下单链路（execution_mode paper 防线、
// OMS 风控/余额锁、paper 背书钳制、成交回报按 client_oid "sig:<id>:manual:"
// 路由回本策略 OnOrderUpdate）。状态演进只在成交确认时发生（与自动信号同
// 哲学：信号发出≠成交）。
//
// 四动作语义与引擎消费口径：
//
//   - close_all（#23 清仓卖出）：市价全平当前持仓 + EntryPaused 暂停新首单
//     （币富"需先暂停订单方可清仓"的合并落地）。策略保持 running：成交确认
//     后空仓等待、不再自动开新循环（用户恐慌全平后立即重新入场是最大的二次
//     伤害源）；恢复交易=重启策略（Start 清态即用户明确恢复意图）。平仓单
//     被拒/撤/过期清除在途标记可重试（EntryPaused 保持置位——币富暂停是独立
//     状态，RuntimeStatus entry_paused 如实透出）。
//   - add_position（#24 一键补仓）：按输入保证金金额立即市价买入（合约 qty=
//     amount×leverage/price，现货 qty=amount/price，与首单 entryQty 同口径）。
//     成交入 CRA 分档（Manual 档，真实成本计入总量/均价/档级止盈），但不推进
//     自动补仓阶梯——PositionCount/PeakAddCount 不计手动单（币富语义：一键
//     补仓是自动阶梯之外的额外手动单）。重启重建经账本 ":manual:" 标记恢复
//     Manual 档，口径重启存续。
//   - toggle_add_position（#25 关闭/开启补仓）：翻转 CRAState.AddPositionDisabled
//     运行时开关；引擎读它跳过 OnBar 自动补仓评估分支，止盈/止损/反向出场/
//     燃烧照常（各自独立分支）。重启清零恢复配置默认（运行时开关不落库、不改
//     配置——极端行情临时干预不应静默改写策略配置）。
//   - reduce_position（#28 自定义减仓）：按数量或比例（0<ratio<1）市价减部分
//     仓位，成交走 ApplyCloseFill FIFO 从首档核销（币富"最后一仓占比 50%/
//     倒数第二 25%"是倍投结构下的建议话术，本实现不强制——用户主观判断止损
//     哪部分仓位由 qty/ratio 自由表达）。减全量请用 close_all。
//
// ManualAction 是引擎 strategy.ManualActioner 可选接口的 CRA 实现。

// ManualAction 校验当前状态并产出操控信号（toggle 无信号）。返回的 detail
// 含 human-readable "message"（端点 detail 透出/前端 toast）与结构化字段。
func (s *BaseCRAStrategy) ManualAction(req map[string]any) (*model.Signal, map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.params == nil || s.state == nil {
		return nil, nil, fmt.Errorf("策略未在运行")
	}
	action := strings.ToLower(strings.TrimSpace(strVal(req, "action", "")))
	st := s.state
	p := s.params

	switch action {
	case "toggle_add_position":
		// enabled 缺省=翻转；显式传入=置为目标态（幂等）。
		if v, ok := req["enabled"].(bool); ok {
			st.AddPositionDisabled = !v
		} else {
			st.AddPositionDisabled = !st.AddPositionDisabled
		}
		enabled := !st.AddPositionDisabled
		msg := "已开启自动补仓"
		if !enabled {
			msg = "已关闭自动补仓（止盈/止损照常；重启策略恢复配置默认）"
		}
		s.logger.Info("cra manual toggle add position", "symbol", s.symbol, "add_position_enabled", enabled)
		return nil, map[string]any{
			"action": action, "add_position_enabled": enabled, "message": msg,
		}, nil

	case "add_position":
		if !st.InPosition {
			return nil, nil, fmt.Errorf("当前无持仓，无法补仓（一键补仓是对持仓中的策略追加保证金）")
		}
		if st.PendingCloseKind != "" {
			return nil, nil, fmt.Errorf("有在途平仓单（%s），待成交终态后再操作", st.PendingCloseKind)
		}
		amount := numFloat(req, "amount", 0)
		if amount <= 0 {
			return nil, nil, fmt.Errorf("补仓金额（amount）必须大于 0")
		}
		price := s.lastBarClose()
		if price <= 0 {
			return nil, nil, fmt.Errorf("暂无行情数据（尚未收到 K 线），无法折算补仓数量")
		}
		// 与首单 entryQty 同口径：合约名义=保证金×杠杆，现货名义=金额本身。
		qty := amount / price
		if s.isContract {
			qty = amount * p.Leverage / price
		}
		qty = RoundQty(qty)
		if qty <= 0 {
			return nil, nil, fmt.Errorf("补仓金额过小，折算数量≈0")
		}
		sig := s.signal(st.SignalDirection(), qty, "cra manual add position")
		sig.Tag = model.SignalTagManual
		s.logger.Info("cra manual add position", "symbol", s.symbol, "amount", amount,
			"qty", qty, "price", price, "side", st.Side)
		return sig, map[string]any{
			"action": action, "qty": qty, "amount": amount, "price": price,
			"message": fmt.Sprintf("补仓信号已发出：市价%s %.6f %s（保证金 %.2f USDT，计入仓位与均价，不占自动补仓梯档）",
				sideVerb(st.Side), qty, baseOf(s.symbol), amount),
		}, nil

	case "reduce_position":
		if !st.InPosition || st.TotalQty <= 0 {
			return nil, nil, fmt.Errorf("当前无持仓，无法减仓")
		}
		if st.PendingCloseKind != "" {
			return nil, nil, fmt.Errorf("有在途平仓单（%s），待成交终态后再操作", st.PendingCloseKind)
		}
		qty := numFloat(req, "qty", 0)
		ratio := numFloat(req, "ratio", 0)
		if qty > 0 && ratio > 0 {
			return nil, nil, fmt.Errorf("数量（qty）与比例（ratio）只能填一个")
		}
		if ratio > 0 {
			if ratio >= 1 {
				return nil, nil, fmt.Errorf("减仓比例必须小于 1（全平请用清仓卖出）")
			}
			qty = st.TotalQty * ratio
		}
		if qty <= 0 {
			return nil, nil, fmt.Errorf("减仓数量（qty）或比例（ratio）必须大于 0")
		}
		qty = RoundQty(qty)
		if qty <= 0 {
			return nil, nil, fmt.Errorf("减仓数量过小，折算后≈0")
		}
		if qty >= st.TotalQty {
			return nil, nil, fmt.Errorf("减仓数量 %.6f ≥ 当前持仓 %.6f，全平请用清仓卖出", qty, st.TotalQty)
		}
		st.PendingCloseKind = "manual_reduce"
		st.PendingCloseQty = qty
		sig := s.signal("CLOSE", qty, "cra manual reduce")
		sig.Tag = model.SignalTagManual
		s.logger.Info("cra manual reduce", "symbol", s.symbol, "qty", qty, "total", st.TotalQty)
		return sig, map[string]any{
			"action": action, "qty": qty, "remaining": st.TotalQty - qty,
			"message": fmt.Sprintf("减仓信号已发出：市价卖出 %.6f %s（持仓占比 %.1f%%，FIFO 从首档核销）",
				qty, baseOf(s.symbol), qty/st.TotalQty*100),
		}, nil

	case "close_all":
		if !st.InPosition || st.TotalQty <= 0 {
			return nil, nil, fmt.Errorf("当前无持仓，无需清仓")
		}
		if st.PendingCloseKind != "" {
			return nil, nil, fmt.Errorf("有在途平仓单（%s），待成交终态后再操作", st.PendingCloseKind)
		}
		st.PendingCloseKind = "manual_close"
		st.PendingCloseQty = st.TotalQty
		// 币富 #23"需先暂停订单方可清仓"的合并落地：全平 + 暂停新首单。
		st.EntryPaused = true
		// CLOSE 不带数量=全平（与止损/全仓止盈的历史出场形态一致）。
		sig := s.signal("CLOSE", 0, "cra manual close all")
		sig.Tag = model.SignalTagManual
		s.logger.Info("cra manual close all", "symbol", s.symbol, "qty", st.TotalQty, "side", st.Side)
		return sig, map[string]any{
			"action": action, "qty": st.TotalQty,
			"message": fmt.Sprintf("清仓信号已发出：市价全平 %.6f %s；策略保持运行并暂停新开仓（重启策略恢复交易）",
				st.TotalQty, baseOf(s.symbol)),
		}, nil
	}
	return nil, nil, fmt.Errorf("未知动作 %q（支持 close_all/add_position/toggle_add_position/reduce_position）", action)
}

// lastBarClose 最近一根工作 K 线收盘价（手动补仓折算数量的价格基准）。
func (s *BaseCRAStrategy) lastBarClose() float64 {
	if len(s.bars) == 0 {
		return 0
	}
	return s.bars[len(s.bars)-1].Close
}

// sideVerb 持仓方向的中文操作词（toast 文案）。
func sideVerb(side PositionSide) string {
	if side == SideShort {
		return "卖出开空"
	}
	return "买入"
}

// baseOf 从交易对名拆出基础币（"BTCUSDT"→"BTC"），仅用于展示文案。
func baseOf(symbol string) string {
	sym := strings.ToUpper(strings.TrimSpace(symbol))
	for _, q := range []string{"USDT", "USDC", "USD"} {
		if strings.HasSuffix(sym, q) && len(sym) > len(q) {
			return strings.TrimSuffix(sym, q)
		}
	}
	return sym
}
