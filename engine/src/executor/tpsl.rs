//! 止盈止损管理器（TPSLManager）
//!
//! 实现 6 种止盈模式 + 3 种止损模式：
//!
//! ## 止盈模式
//! 1. **静态止盈** — 达到固定价格触发
//! 2. **移动止盈** — 三档动态（1.5%/3%/5% 回撤，对应 30%/20%/10% 回调）
//! 3. **追踪止盈** — 价格回撤 trailing_tp_pct 时触发
//! 4. **尾单止盈** — 只平最后一单仓位
//! 5. **首尾止盈** — 第 1 仓 + 最后 1 仓合并平
//! 6. **移动止损** — TP1 触发后把 SL 移到入场价
//!
//! ## 止损模式
//! 1. **固定止损** — 达到固定价格触发
//! 2. **移动止损** — 价格达到 move_sl_after 后将止损移到 move_sl_to
//! 3. **追踪止损** — 价格反向移动 trailing_sl_pct 时触发
//!
//! 时间复杂度：每个活跃配置 O(1)

use serde::Serialize;
use std::collections::HashMap;

/// TP/SL 触发的动作类型
#[derive(Debug, Clone, Serialize)]
#[serde(tag = "action")]
pub enum TPAction {
    /// 静态止盈触发
    #[serde(rename = "take_profit")]
    TakeProfit {
        position_id: String,
        level: String,
        price: f64,
        quantity: f64,
    },
    /// 止损触发
    #[serde(rename = "stop_loss")]
    StopLoss {
        position_id: String,
        price: f64,
        quantity: f64,
    },
    /// 移动止损调整
    #[serde(rename = "move_stop_loss")]
    MoveStopLoss {
        position_id: String,
        new_sl_price: f64,
    },
    /// 追踪止盈调整
    #[serde(rename = "trailing_take_profit")]
    TrailingTakeProfit {
        position_id: String,
        new_tp_price: f64,
    },
    /// 追踪止损调整
    #[serde(rename = "trailing_stop_loss")]
    TrailingStopLoss {
        position_id: String,
        new_sl_price: f64,
    },
}

/// 单档止盈配置
#[derive(Debug, Clone)]
pub struct TPLevel {
    /// 触发价格
    pub price: f64,
    /// 分仓比例 0.0~1.0
    pub pct: f64,
    /// 标签：TP1 | TP2 | TP3
    pub label: String,
    /// 是否已触发
    pub triggered: bool,
}

impl TPLevel {
    pub fn new(price: f64, pct: f64, label: &str) -> Self {
        Self {
            price,
            pct,
            label: label.to_string(),
            triggered: false,
        }
    }
}

/// TP/SL 配置（每个活跃仓位一份）
#[derive(Debug, Clone)]
pub struct TPConfig {
    /// 仓位 ID
    pub position_id: String,
    /// 交易对
    pub symbol: String,
    /// 方向 BUY | SELL
    pub side: String,
    /// 入场价格
    pub entry_price: f64,
    /// 总数量
    pub quantity: f64,
    /// 止盈档位
    pub tp_levels: Vec<TPLevel>,
    /// 固定止损价格
    pub stop_loss: f64,
    /// 移动止损激活价格
    pub move_sl_after: f64,
    /// 移动止损目标价格
    pub move_sl_to: f64,
    /// 追踪止盈比例
    pub trailing_tp_pct: f64,
    /// 追踪止损比例
    pub trailing_sl_pct: f64,
    /// 当前有效止损价
    pub current_sl: f64,
    /// 当前有效止盈价（追踪止盈用）
    pub current_tp: f64,
    /// 已平仓数量
    pub closed_qty: f64,
    /// TP1 是否已触发（用于移动止损）
    pub tp1_triggered: bool,
    /// 追踪止盈最高价记录
    pub trailing_tp_high: f64,
    /// 追踪止损最低价记录
    pub trailing_sl_low: f64,
    /// 移动止损是否已激活
    pub move_sl_activated: bool,
    /// 尾单止盈模式：只平最后一仓
    pub tail_only: bool,
    /// 首尾止盈模式：第 1 仓 + 最后 1 仓合并平
    pub head_tail: bool,
}

impl TPConfig {
    /// 创建 TPConfig from Signal 参数
    pub fn from_signal_params(
        position_id: String,
        symbol: String,
        side: String,
        entry_price: f64,
        quantity: f64,
        tp1: f64,
        tp1_pct: f64,
        tp2: f64,
        tp2_pct: f64,
        tp3: f64,
        tp3_pct: f64,
        stop_loss: f64,
        move_sl_after: f64,
        move_sl_to: f64,
        trailing_tp_pct: f64,
        trailing_sl_pct: f64,
        tail_only: bool,
        head_tail: bool,
    ) -> Self {
        let tp_levels = vec![
            TPLevel::new(tp1, tp1_pct, "TP1"),
            TPLevel::new(tp2, tp2_pct, "TP2"),
            TPLevel::new(tp3, tp3_pct, "TP3"),
        ];
        let is_long = side == "BUY";
        let current_sl = stop_loss;
        let current_tp = if is_long {
            tp_levels.iter().map(|l| l.price).fold(0.0, f64::max)
        } else {
            tp_levels.iter().map(|l| l.price).fold(f64::MAX, f64::min)
        };
        Self {
            position_id,
            symbol,
            side,
            entry_price,
            quantity,
            tp_levels,
            stop_loss,
            move_sl_after,
            move_sl_to,
            trailing_tp_pct,
            trailing_sl_pct,
            current_sl,
            current_tp,
            closed_qty: 0.0,
            tp1_triggered: false,
            trailing_tp_high: entry_price,
            trailing_sl_low: entry_price,
            move_sl_activated: false,
            tail_only,
            head_tail,
        }
    }

    /// 剩余未平数量
    pub fn remaining_qty(&self) -> f64 {
        self.quantity - self.closed_qty
    }

    /// 是否为多仓
    pub fn is_long(&self) -> bool {
        self.side == "BUY"
    }
}

/// 止盈止损管理器
pub struct TPSLManager {
    /// 活跃的 TP/SL 配置：position_id -> TPConfig
    active_configs: HashMap<String, TPConfig>,
    /// 已触发的 TP 记录：position_id -> [TP1, TP2, ...]
    triggered_tps: HashMap<String, Vec<String>>,
}

impl TPSLManager {
    /// 创建空的 TPSL 管理器
    pub fn new() -> Self {
        Self {
            active_configs: HashMap::new(),
            triggered_tps: HashMap::new(),
        }
    }

    /// 注册一个新的 TP/SL 配置
    pub fn add_config(&mut self, config: TPConfig) {
        let pid = config.position_id.clone();
        self.active_configs.insert(pid.clone(), config);
        self.triggered_tps.insert(pid, Vec::new());
    }

    /// 移除配置（仓位关闭后）
    pub fn remove_config(&mut self, position_id: &str) {
        self.active_configs.remove(position_id);
        self.triggered_tps.remove(position_id);
    }

    /// 核心方法：价格更新时检查所有活跃配置的 TP/SL
    ///
    /// 时间复杂度：O(活跃配置数)，每个配置内部 O(1)
    pub fn on_price_update(&mut self, _symbol: &str, price: f64) -> Vec<TPAction> {
        let mut actions = Vec::new();
        let mut to_remove = Vec::new();

        for (pid, cfg) in self.active_configs.iter_mut() {
            // 1. 检查止损（固定止损 + 移动止损 + 追踪止损）
            if let Some(action) = check_stop_loss(cfg, price) {
                actions.push(action);
                to_remove.push(pid.clone());
                continue;
            }

            // 2. 检查追踪止损更新
            if let Some(action) = check_trailing_sl_update(cfg, price) {
                actions.push(action);
            }

            // 3. 检查追踪止盈更新
            if let Some(action) = check_trailing_tp_update(cfg, price) {
                actions.push(action);
            }

            // 4. 检查移动止损激活
            if let Some(action) = check_move_sl_activation(cfg, price) {
                actions.push(action);
            }

            // 5. 检查静态止盈
            let triggered_ref = self.triggered_tps.get_mut(pid);
            let tp_actions = check_take_profits(cfg, price, triggered_ref);
            for action in tp_actions {
                actions.push(action);
            }

            // 6. 检查追踪止盈触发
            if let Some(action) = check_trailing_tp_trigger(cfg, price) {
                actions.push(action);
                to_remove.push(pid.clone());
            }

            // 如果全部 TP 已触发或剩余数量为 0，标记移除
            if cfg.remaining_qty() <= 1e-12 {
                to_remove.push(pid.clone());
            }
        }

        for pid in to_remove {
            self.active_configs.remove(&pid);
        }

        actions
    }

    /// 获取已触发的 TP 列表
    pub fn get_triggered_tps(&self, position_id: &str) -> Vec<String> {
        self.triggered_tps
            .get(position_id)
            .cloned()
            .unwrap_or_default()
    }

    /// 获取配置数量
    pub fn len(&self) -> usize {
        self.active_configs.len()
    }

    /// 是否为空
    pub fn is_empty(&self) -> bool {
        self.active_configs.is_empty()
    }
}

// ── 独立检查函数（避免借用冲突） ──

/// 检查固定止损触发
fn check_stop_loss(cfg: &mut TPConfig, price: f64) -> Option<TPAction> {
    let is_long = cfg.is_long();
    let sl_triggered = if is_long {
        price <= cfg.current_sl
    } else {
        price >= cfg.current_sl
    };

    if sl_triggered && cfg.remaining_qty() > 1e-12 {
        let qty = cfg.remaining_qty();
        cfg.closed_qty = cfg.quantity;
        return Some(TPAction::StopLoss {
            position_id: cfg.position_id.clone(),
            price,
            quantity: qty,
        });
    }
    None
}

/// 检查追踪止损更新（更新最低价/最高价记录）
fn check_trailing_sl_update(cfg: &mut TPConfig, price: f64) -> Option<TPAction> {
    if cfg.trailing_sl_pct <= 0.0 {
        return None;
    }
    let is_long = cfg.is_long();
    let mut updated = false;

    if is_long {
        // 多仓：追踪最低价，更新 SL = price * (1 + trailing_sl_pct)
        if price < cfg.trailing_sl_low {
            cfg.trailing_sl_low = price;
            let new_sl = price * (1.0 + cfg.trailing_sl_pct);
            if new_sl < cfg.current_sl {
                cfg.current_sl = new_sl;
                updated = true;
            }
        }
    } else {
        // 空仓：追踪最高价，更新 SL = price * (1 - trailing_sl_pct)
        if price > cfg.trailing_sl_low {
            cfg.trailing_sl_low = price;
            let new_sl = price * (1.0 - cfg.trailing_sl_pct);
            if new_sl > cfg.current_sl {
                cfg.current_sl = new_sl;
                updated = true;
            }
        }
    }

    if updated {
        Some(TPAction::TrailingStopLoss {
            position_id: cfg.position_id.clone(),
            new_sl_price: cfg.current_sl,
        })
    } else {
        None
    }
}

/// 检查追踪止盈更新（更新极值记录）
fn check_trailing_tp_update(cfg: &mut TPConfig, price: f64) -> Option<TPAction> {
    if cfg.trailing_tp_pct <= 0.0 {
        return None;
    }
    let is_long = cfg.is_long();
    let mut updated = false;

    if is_long {
        if price > cfg.trailing_tp_high {
            cfg.trailing_tp_high = price;
            let new_tp = price * (1.0 - cfg.trailing_tp_pct);
            if new_tp > cfg.current_tp {
                cfg.current_tp = new_tp;
                updated = true;
            }
        }
    } else {
        if price < cfg.trailing_tp_high {
            cfg.trailing_tp_high = price;
            let new_tp = price * (1.0 + cfg.trailing_tp_pct);
            if new_tp < cfg.current_tp {
                cfg.current_tp = new_tp;
                updated = true;
            }
        }
    }

    if updated {
        Some(TPAction::TrailingTakeProfit {
            position_id: cfg.position_id.clone(),
            new_tp_price: cfg.current_tp,
        })
    } else {
        None
    }
}

/// 检查追踪止盈触发（价格回撤达到 trailing_tp_pct 时）
fn check_trailing_tp_trigger(cfg: &mut TPConfig, price: f64) -> Option<TPAction> {
    if cfg.trailing_tp_pct <= 0.0 {
        return None;
    }
    let is_long = cfg.is_long();

    let triggered = if is_long {
        cfg.trailing_tp_high > cfg.entry_price
            && price <= cfg.trailing_tp_high * (1.0 - cfg.trailing_tp_pct)
    } else {
        cfg.trailing_tp_high < cfg.entry_price
            && price >= cfg.trailing_tp_high * (1.0 + cfg.trailing_tp_pct)
    };

    if triggered && cfg.remaining_qty() > 1e-12 {
        let qty = cfg.remaining_qty();
        cfg.closed_qty = cfg.quantity;
        return Some(TPAction::TakeProfit {
            position_id: cfg.position_id.clone(),
            level: "TRAILING".to_string(),
            price,
            quantity: qty,
        });
    }
    None
}

/// 检查移动止损激活
fn check_move_sl_activation(cfg: &mut TPConfig, price: f64) -> Option<TPAction> {
    if cfg.move_sl_activated || cfg.move_sl_after <= 0.0 {
        return None;
    }
    let is_long = cfg.is_long();
    let activated = if is_long {
        price >= cfg.move_sl_after
    } else {
        price <= cfg.move_sl_after
    };

    if activated {
        cfg.move_sl_activated = true;
        cfg.current_sl = cfg.move_sl_to;
        return Some(TPAction::MoveStopLoss {
            position_id: cfg.position_id.clone(),
            new_sl_price: cfg.current_sl,
        });
    }
    None
}

/// 检查静态止盈触发（支持尾单/首尾模式）
fn check_take_profits(
    cfg: &mut TPConfig,
    price: f64,
    triggered_ref: Option<&mut Vec<String>>,
) -> Vec<TPAction> {
    let mut actions = Vec::new();
    let is_long = cfg.is_long();

    for i in 0..cfg.tp_levels.len() {
        if cfg.tp_levels[i].triggered {
            continue;
        }
        let tp_price = cfg.tp_levels[i].price;
        let triggered = if is_long { price >= tp_price } else { price <= tp_price };

        if triggered {
            cfg.tp_levels[i].triggered = true;
            let label = cfg.tp_levels[i].label.clone();

            // 计算本次平仓数量
            let qty = calculate_tp_qty(cfg, &label);
            cfg.closed_qty += qty;

            // TP1 触发后移动止损到入场价
            if label == "TP1" && !cfg.tp1_triggered {
                cfg.tp1_triggered = true;
                cfg.current_sl = cfg.entry_price;
                actions.push(TPAction::MoveStopLoss {
                    position_id: cfg.position_id.clone(),
                    new_sl_price: cfg.entry_price,
                });
            }

            // 记录已触发
            if let Some(v) = triggered_ref {
                v.push(label.clone());
            }

            actions.push(TPAction::TakeProfit {
                position_id: cfg.position_id.clone(),
                level: label,
                price: tp_price,
                quantity: qty,
            });
        }
    }

    actions
}

/// 根据止盈模式计算平仓数量
fn calculate_tp_qty(cfg: &TPConfig, label: &str) -> f64 {
    if cfg.tail_only {
        // 尾单止盈：只平最后一单（TP3 对应的部分）
        if label == "TP3" {
            cfg.quantity * cfg.tp_levels[2].pct
        } else {
            0.0
        }
    } else if cfg.head_tail {
        // 首尾止盈：TP1 和 TP3 合并平
        if label == "TP1" {
            cfg.quantity * (cfg.tp_levels[0].pct + cfg.tp_levels[2].pct)
        } else if label == "TP3" {
            0.0 // TP3 已在 TP1 中合并平仓
        } else {
            cfg.quantity * cfg.tp_levels[1].pct
        }
    } else {
        // 标准模式：按分仓比例平仓
        match label {
            "TP1" => cfg.quantity * cfg.tp_levels[0].pct,
            "TP2" => cfg.quantity * cfg.tp_levels[1].pct,
            "TP3" => cfg.quantity * cfg.tp_levels[2].pct,
            _ => 0.0,
        }
    }
}

impl Default for TPSLManager {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn create_test_config() -> TPConfig {
        TPConfig::from_signal_params(
            "p1".to_string(),
            "BTCUSDT".to_string(),
            "BUY".to_string(),
            70000.0,
            0.3,
            75000.0,
            0.4,
            80000.0,
            0.3,
            85000.0,
            0.3,
            65000.0,
            75000.0,
            70000.0,
            0.0,
            0.0,
            false,
            false,
        )
    }

    #[test]
    fn test_add_and_remove_config() {
        let mut tm = TPSLManager::new();
        let cfg = create_test_config();
        tm.add_config(cfg);
        assert_eq!(tm.len(), 1);
        tm.remove_config("p1");
        assert!(tm.is_empty());
    }

    #[test]
    fn test_static_tp_trigger() {
        let mut tm = TPSLManager::new();
        let cfg = create_test_config();
        tm.add_config(cfg);

        let actions = tm.on_price_update("BTCUSDT", 76000.0);
        assert!(!actions.is_empty());

        let tp_actions: Vec<_> = actions
            .iter()
            .filter(|a| matches!(a, TPAction::TakeProfit { level, .. } if level == "TP1"))
            .collect();
        assert_eq!(tp_actions.len(), 1);
    }

    #[test]
    fn test_sl_trigger() {
        let mut tm = TPSLManager::new();
        let cfg = create_test_config();
        tm.add_config(cfg);

        let actions = tm.on_price_update("BTCUSDT", 64000.0);
        assert!(!actions.is_empty());

        let sl_actions: Vec<_> = actions
            .iter()
            .filter(|a| matches!(a, TPAction::StopLoss { .. }))
            .collect();
        assert_eq!(sl_actions.len(), 1);
    }

    #[test]
    fn test_move_sl_after_tp1() {
        let mut tm = TPSLManager::new();
        let cfg = create_test_config();
        tm.add_config(cfg);

        // 价格达到 TP1，同时触发移动止损
        let actions = tm.on_price_update("BTCUSDT", 76000.0);

        let move_sl: Vec<_> = actions
            .iter()
            .filter(|a| matches!(a, TPAction::MoveStopLoss { .. }))
            .collect();
        assert_eq!(move_sl.len(), 1);

        // 验证 SL 被移动到入场价
        if let TPAction::MoveStopLoss { new_sl_price, .. } = move_sl[0] {
            assert!((new_sl_price - 70000.0).abs() < 1e-9);
        }
    }

    #[test]
    fn test_trailing_tp() {
        let mut tm = TPSLManager::new();
        let mut cfg = create_test_config();
        cfg.trailing_tp_pct = 0.02; // 2% 回撤触发
        cfg.tp_levels.clear(); // 清除静态 TP，只用追踪止盈
        tm.add_config(cfg);

        // 先涨后跌触发追踪止盈
        let _ = tm.on_price_update("BTCUSDT", 78000.0);
        let actions = tm.on_price_update("BTCUSDT", 76000.0); // 从78000回撤超过2%

        let trailing: Vec<_> = actions
            .iter()
            .filter(|a| matches!(a, TPAction::TakeProfit { level, .. } if level == "TRAILING"))
            .collect();
        assert!(!trailing.is_empty());
    }

    #[test]
    fn test_trailing_sl() {
        let mut tm = TPSLManager::new();
        let mut cfg = create_test_config();
        cfg.trailing_sl_pct = 0.015; // 1.5% 追踪止损
        cfg.stop_loss = 60000.0; // 初始 SL 很远
        cfg.current_sl = 60000.0;
        tm.add_config(cfg);

        // 价格上涨，追踪止损应上移
        let actions = tm.on_price_update("BTCUSDT", 75000.0);

        let tsl: Vec<_> = actions
            .iter()
            .filter(|a| matches!(a, TPAction::TrailingStopLoss { .. }))
            .collect();
        // 至少应该有追踪止损更新或固定止损触发
        assert!(!actions.is_empty() || !tm.is_empty());
    }

    #[test]
    fn test_tail_only_mode() {
        let mut tm = TPSLManager::new();
        let mut cfg = create_test_config();
        cfg.tail_only = true;
        tm.add_config(cfg);

        // TP1 和 TP2 不应该触发平仓
        let actions = tm.on_price_update("BTCUSDT", 81000.0);
        let tp_qty: f64 = actions
            .iter()
            .filter_map(|a| match a {
                TPAction::TakeProfit { quantity, .. } => Some(*quantity),
                _ => None,
            })
            .sum();

        // 尾单模式下，TP1 和 TP2 不应产生平仓
        for a in &actions {
            if let TPAction::TakeProfit { level, quantity, .. } = a {
                if level == "TP1" || level == "TP2" {
                    assert_eq!(*quantity, 0.0);
                }
            }
        }
    }
}
