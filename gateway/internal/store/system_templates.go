package store

import (
	"encoding/json"
	"time"
)

// ── 系统预设策略模板（A1.5 CTA/组合策略模板）──
//
// 12 个 QuantDinger 对标的系统预设：8 个 CTA 单策略 + 4 个组合策略。
// user_id=0 表示系统模板，对全部用户可见（StrategyTemplateRepo.List 改为
// user_id=? OR user_id=0）；用户模板删除条件 user_id=? 天然保护系统模板
// 不被普通用户删除。ID 固定（tpl-cta-* / tpl-combo-*），INSERT OR IGNORE
// 幂等，重启/重复初始化不会重复插入，也不覆盖用户已改过名的同 ID 记录。

// SystemTemplates 返回 12 个系统预设模板的完整定义。
func SystemTemplates() []StrategyTemplateRecord {
	cta := func(id, name, stype, desc string, cfg map[string]any) StrategyTemplateRecord {
		raw, _ := json.Marshal(cfg)
		return StrategyTemplateRecord{
			ID:                id,
			UserID:            0,
			Name:              name,
			Category:          "contract",
			StrategyType:      stype,
			Description:       desc,
			DefaultConfigJSON: string(raw),
		}
	}
	combo := func(id, name, desc string, cfg map[string]any) StrategyTemplateRecord {
		raw, _ := json.Marshal(cfg)
		return StrategyTemplateRecord{
			ID:                id,
			UserID:            0,
			Name:              name,
			Category:          "contract",
			StrategyType:      "combo",
			Description:       desc,
			DefaultConfigJSON: string(raw),
		}
	}
	return []StrategyTemplateRecord{
		// ── 8 个 CTA 策略（strategy_type 对齐内置策略工厂，可直接实例化）──
		cta("tpl-cta-ema-cross", "双均线交叉 (CTA)", "ema_cross",
			"经典 CTA 趋势跟踪：快/慢 EMA 金叉做多、死叉平仓，默认 12/26 参数。",
			map[string]any{
				"symbol": "BTCUSDT", "timeframe": "1h", "trade_direction": "dual",
				"fast_period": 12, "slow_period": 26,
				"stop_loss_pct": 0.02, "take_profit_pct": 0.04, "position_size": 500,
			}),
		cta("tpl-cta-macd-trend", "MACD 趋势 (CTA)", "macd",
			"MACD 柱状线趋势策略：DIF 上穿 DEA 进场，反向信号离场，适合中周期趋势。",
			map[string]any{
				"symbol": "BTCUSDT", "timeframe": "1h", "trade_direction": "dual",
				"fast_period": 12, "slow_period": 26, "signal_period": 9,
				"stop_loss_pct": 0.02, "take_profit_pct": 0.06, "position_size": 500,
			}),
		cta("tpl-cta-rsi-reversal", "RSI 反转 (CTA)", "rsi",
			"均值回归型 CTA：RSI 超卖反弹做多、超买回落做空，默认 14 周期 30/70 阈值。",
			map[string]any{
				"symbol": "BTCUSDT", "timeframe": "1h", "trade_direction": "dual",
				"period": 14, "overbought": 70, "oversold": 30,
				"stop_loss_pct": 0.02, "take_profit_pct": 0.04, "position_size": 500,
			}),
		cta("tpl-cta-bollinger-breakout", "布林突破 (CTA)", "bollinger_bands",
			"波动率通道突破：价格上/下轨外突破进场，回到通道内离场。",
			map[string]any{
				"symbol": "BTCUSDT", "timeframe": "1h", "trade_direction": "dual",
				"period": 20, "std_dev": 2.0,
				"stop_loss_pct": 0.02, "take_profit_pct": 0.05, "position_size": 500,
			}),
		cta("tpl-cta-momentum-rotation", "动量轮动 (CTA)", "dual_thrust",
			"Dual Thrust 动量区间突破：按 lookback 区间幅度×系数挂突破单；多标的轮动可为每个标的实例化一份。",
			map[string]any{
				"symbol": "BTCUSDT", "timeframe": "1h", "trade_direction": "dual",
				"lookback_period": 4, "k1": 0.5, "k2": 0.4,
				"position_size": 500,
			}),
		cta("tpl-cta-atr-channel", "ATR 通道突破 (CTA)", "atr_trailing_stop",
			"ATR 通道趋势策略：entry_period 高低价通道突破进场，ATR 吊灯止损离场。",
			map[string]any{
				"symbol": "BTCUSDT", "timeframe": "1h", "trade_direction": "dual",
				"atr_period": 14, "atr_multiplier": 2.0, "entry_period": 20,
				"position_size": 500,
			}),
		cta("tpl-cta-ema-resonance", "EMA 多周期共振 (CTA)", "ema_follow_trend",
			"多周期共振近似：短周期 EMA(12) 与长周期 EMA(48) 同向确认进场，过滤单周期噪音。",
			map[string]any{
				"symbol": "BTCUSDT", "timeframe": "4h", "trade_direction": "dual",
				"fast_period": 12, "slow_period": 48,
				"stop_loss_pct": 0.02, "take_profit_pct": 0.06, "position_size": 500,
			}),
		cta("tpl-cta-donchian-breakout", "Donchian 通道突破 (CTA)", "breakout",
			"唐奇安通道突破：lookback 周期最高价上破做多/最低价下破做空，海龟交易法则内核。",
			map[string]any{
				"symbol": "BTCUSDT", "timeframe": "4h", "trade_direction": "dual",
				"lookback": 20, "buffer_pct": 0.002,
				"stop_loss_pct": 0.02, "take_profit_pct": 0.08, "position_size": 500,
			}),

		// ── 4 个组合策略（payload 对齐 POST /combos，可直接实例化）──
		combo("tpl-combo-equal-weight", "多策略等权组合",
			"四个经典 CTA 等权投票：双均线/MACD/RSI/布林任一给出信号即进场（vote 聚合），分散单策略失效风险。",
			map[string]any{
				"name": "多策略等权组合", "symbol": "BTCUSDT", "aggregation_mode": "vote",
				"members": []map[string]any{
					{"strategy_name": "ema_cross", "weight": 0.25, "enabled": true},
					{"strategy_name": "macd", "weight": 0.25, "enabled": true},
					{"strategy_name": "rsi", "weight": 0.25, "enabled": true},
					{"strategy_name": "bollinger_bands", "weight": 0.25, "enabled": true},
				},
			}),
		combo("tpl-combo-trend-grid", "趋势+网格组合",
			"趋势腿(EMA 交叉)抓单边、网格腿在震荡中收割，vote 聚合两种市况都有腿在工作。",
			map[string]any{
				"name": "趋势+网格组合", "symbol": "BTCUSDT", "aggregation_mode": "vote",
				"members": []map[string]any{
					{"strategy_name": "ema_follow_trend", "weight": 0.5, "enabled": true},
					{"strategy_name": "grid_trading", "weight": 0.5, "enabled": true},
				},
			}),
		combo("tpl-combo-rotation", "多标的轮动组合",
			"动量三剑客加权投票（Dual Thrust/Donchian/EMA 共振）；多标的轮动为每个标的实例化一份并按强势度分配资金。",
			map[string]any{
				"name": "多标的轮动组合", "symbol": "BTCUSDT", "aggregation_mode": "weighted",
				"members": []map[string]any{
					{"strategy_name": "dual_thrust", "weight": 0.4, "enabled": true},
					{"strategy_name": "breakout", "weight": 0.3, "enabled": true},
					{"strategy_name": "ema_follow_trend", "weight": 0.3, "enabled": true},
				},
			}),
		combo("tpl-combo-risk-parity", "保守/激进风险平价组合",
			"风险平价思路的保守配置：低相关腿（ATR 通道/RSI 反转/布林突破）按波动反向加权，激进用法可提高仓位上限并加入动量腿。",
			map[string]any{
				"name": "保守/激进风险平价组合", "symbol": "BTCUSDT", "aggregation_mode": "weighted",
				"members": []map[string]any{
					{"strategy_name": "atr_trailing_stop", "weight": 0.4, "enabled": true},
					{"strategy_name": "rsi", "weight": 0.4, "enabled": true},
					{"strategy_name": "bollinger_bands", "weight": 0.2, "enabled": true},
				},
			}),
	}
}

// EnsureSystemTemplates 幂等注册系统预设模板（INSERT OR IGNORE，固定 ID）。
// 在 InitDB 的模板迁移后调用；已存在的同 ID 记录（含用户改名版）不覆盖。
func EnsureSystemTemplates() error {
	if db == nil {
		return nil
	}
	now := time.Now().UnixMilli()
	for _, tpl := range SystemTemplates() {
		_, err := db.Exec(`INSERT OR IGNORE INTO strategy_templates
			(id, user_id, name, category, strategy_type, description, default_config_json, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?)`,
			tpl.ID, tpl.UserID, tpl.Name, tpl.Category, tpl.StrategyType, tpl.Description,
			tpl.DefaultConfigJSON, now, now,
		)
		if err != nil {
			return err
		}
	}
	return nil
}
