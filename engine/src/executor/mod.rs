//! SignalExecutor — 统一信号执行引擎
//!
//! 接收来自三种机器人（strategy / signal / ai）的交易信号，
//! 统一处理仓位管理、止盈止损计算和执行下单。
//!
//! 通过 FFI 与 Go 层交互，JSON 作为序列化格式。

pub mod execution;
pub mod position;
pub mod signal;
pub mod tpsl;

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

use serde::Serialize;

use execution::ExecutionEngine;
use position::{Position, PositionManager};
use signal::Signal;
use tpsl::{TPAction, TPConfig, TPSLManager};

/// 信号执行记录，通过 FFI 传回 Go
#[derive(Debug, Clone, Serialize)]
pub struct ExecutionRecord {
    /// 信号 ID
    pub signal_id: String,
    /// 机器人 ID
    pub bot_id: String,
    /// 机器人类型
    pub bot_type: String,
    /// 交易对
    pub symbol: String,
    /// 执行状态
    pub status: String,
    /// 实际入场价格
    pub entry_price: f64,
    /// 实际入场数量
    pub entry_qty: f64,
    /// TP1 是否已触发
    pub tp1_filled: bool,
    /// TP1 价格
    pub tp1_price: f64,
    /// TP1 数量
    pub tp1_qty: f64,
    /// TP2 是否已触发
    pub tp2_filled: bool,
    /// TP2 价格
    pub tp2_price: f64,
    /// TP2 数量
    pub tp2_qty: f64,
    /// TP3 是否已触发
    pub tp3_filled: bool,
    /// TP3 价格
    pub tp3_price: f64,
    /// TP3 数量
    pub tp3_qty: f64,
    /// SL 是否已触发
    pub sl_triggered: bool,
    /// SL 价格
    pub sl_price: f64,
    /// 追踪止盈是否激活
    pub trailing_active: bool,
    /// 当前有效止盈价
    pub current_tp: f64,
    /// 当前有效止损价
    pub current_sl: f64,
    /// 已实现盈亏
    pub realized_pnl: f64,
    /// 关仓原因
    pub close_reason: String,
}

impl ExecutionRecord {
    /// 从 Signal 创建初始执行记录
    pub fn from_signal(signal: &Signal) -> Self {
        Self {
            signal_id: signal.id.clone(),
            bot_id: signal.bot_id.clone(),
            bot_type: signal.bot_type.clone(),
            symbol: signal.symbol.clone(),
            status: "pending".to_string(),
            entry_price: signal.entry_price,
            entry_qty: 0.0,
            tp1_filled: false,
            tp1_price: signal.tp1,
            tp1_qty: 0.0,
            tp2_filled: false,
            tp2_price: signal.tp2,
            tp2_qty: 0.0,
            tp3_filled: false,
            tp3_price: signal.tp3,
            tp3_qty: 0.0,
            sl_triggered: false,
            sl_price: signal.stop_loss,
            trailing_active: signal.trailing_tp > 0.0,
            current_tp: signal.tp3,
            current_sl: signal.stop_loss,
            realized_pnl: 0.0,
            close_reason: String::new(),
        }
    }
}

/// 按交易对组织的 SignalExecutor
///
/// 每个 symbol 有独立的 PositionManager，
/// TPSLManager 全局统一管理。
pub struct SignalExecutor {
    /// 每个 symbol 的仓位管理器
    positions: Arc<Mutex<HashMap<String, PositionManager>>>,
    /// 全局止盈止损管理器
    tpsl: Arc<Mutex<TPSLManager>>,
    /// 执行引擎
    execution: ExecutionEngine,
    /// 执行记录缓存：signal_id -> ExecutionRecord
    records: Arc<Mutex<HashMap<String, ExecutionRecord>>>,
}

impl SignalExecutor {
    /// 创建新的 SignalExecutor
    pub fn new() -> Self {
        Self {
            positions: Arc::new(Mutex::new(HashMap::new())),
            tpsl: Arc::new(Mutex::new(TPSLManager::new())),
            execution: ExecutionEngine::new(),
            records: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// 执行一个交易信号
    ///
    /// 流程：
    /// 1. 校验信号
    /// 2. 检查是否过期
    /// 3. 计算仓位大小
    /// 4. 下入场单
    /// 5. 创建仓位和 TP/SL 配置
    /// 6. 返回执行记录
    ///
    /// # 参数
    /// - `signal`: 交易信号
    /// - `available_balance`: 可用余额（用于计算仓位大小）
    pub fn execute_signal(
        &mut self,
        signal: Signal,
        available_balance: f64,
    ) -> Result<ExecutionRecord, String> {
        // 1. 校验
        signal.validate()?;

        // 2. 检查过期
        if signal.is_expired() {
            return Err(format!("signal {} has expired", signal.id));
        }

        // 3. 初始化记录
        let mut record = ExecutionRecord::from_signal(&signal);

        // 4. 获取或创建 symbol 对应的 PositionManager
        let mut positions_guard = self.positions.lock().map_err(|e| e.to_string())?;
        let pm = positions_guard
            .entry(signal.symbol.clone())
            .or_insert_with(PositionManager::new);

        // 5. 计算仓位大小
        let (qty, _notional) = pm.calculate_position_size(&signal, available_balance)?;
        record.entry_qty = qty;

        // 6. 下入场单
        let entry_result = self.execution.place_entry_order(&signal, qty)?;
        record.entry_price = entry_result.fill_price;
        record.status = "filled".to_string();

        // 7. 创建 Position
        let position_id = format!("{}-{}", signal.symbol, signal.id);
        let position = Position::new(
            position_id.clone(),
            signal.id.clone(),
            signal.bot_id.clone(),
            signal.bot_type.clone(),
            signal.symbol.clone(),
            signal.side.clone(),
            entry_result.fill_price,
            entry_result.fill_qty,
            signal.leverage,
        );
        pm.open_position(position);

        // 8. 创建 TP/SL 配置
        let tp_config = TPConfig::from_signal_params(
            position_id.clone(),
            signal.symbol.clone(),
            signal.side.clone(),
            entry_result.fill_price,
            entry_result.fill_qty,
            signal.tp1,
            signal.tp1_pct,
            signal.tp2,
            signal.tp2_pct,
            signal.tp3,
            signal.tp3_pct,
            signal.stop_loss,
            signal.move_sl_after,
            signal.move_sl_to,
            signal.trailing_tp,
            signal.trailing_sl,
            false, // tail_only: 可从 signal 扩展字段
            false, // head_tail: 可从 signal 扩展字段
        );

        {
            let mut tpsl_guard = self.tpsl.lock().map_err(|e| e.to_string())?;
            tpsl_guard.add_config(tp_config);
        }

        // 9. 缓存记录
        {
            let mut records_guard = self.records.lock().map_err(|e| e.to_string())?;
            records_guard.insert(signal.id.clone(), record.clone());
        }

        Ok(record)
    }

    /// 价格更新回调
    ///
    /// 由外部价格推送驱动，检查所有活跃配置的 TP/SL，
    /// 返回需要执行的动作列表。
    ///
    /// 时间复杂度：O(活跃配置数)
    ///
    /// # 参数
    /// - `symbol`: 交易对
    /// - `price`: 最新标记价格
    pub fn on_price_update(&mut self, symbol: &str, price: f64) -> Vec<TPAction> {
        let mut tpsl_guard = match self.tpsl.lock() {
            Ok(g) => g,
            Err(poisoned) => poisoned.into_inner(),
        };

        let actions = tpsl_guard.on_price_update(symbol, price);

        // 同步更新仓位未实现盈亏
        drop(tpsl_guard);

        let mut positions_guard = match self.positions.lock() {
            Ok(g) => g,
            Err(poisoned) => poisoned.into_inner(),
        };

        if let Some(pm) = positions_guard.get_mut(symbol) {
            let open_pids: Vec<String> = pm.all_open_positions()
                .iter()
                .map(|p| p.position_id.clone())
                .collect();
            for pid in open_pids {
                pm.update_unrealized_pnl(&pid, price);
            }
        }

        actions
    }

    /// 获取指定信号的执行记录
    pub fn get_record(&self, signal_id: &str) -> Option<ExecutionRecord> {
        match self.records.lock() {
            Ok(guard) => guard.get(signal_id).cloned(),
            Err(poisoned) => poisoned.into_inner().get(signal_id).cloned(),
        }
    }

    /// 获取所有执行记录
    pub fn get_all_records(&self) -> HashMap<String, ExecutionRecord> {
        match self.records.lock() {
            Ok(guard) => guard.clone(),
            Err(poisoned) => poisoned.into_inner().clone(),
        }
    }

    /// 手动关闭指定仓位
    pub fn close_position(
        &mut self,
        position_id: &str,
        symbol: &str,
        reason: &str,
    ) -> Result<(), String> {
        let mut positions_guard = self.positions.lock().map_err(|e| e.to_string())?;
        if let Some(pm) = positions_guard.get_mut(symbol) {
            pm.close_position(position_id, reason);
        }

        let mut tpsl_guard = self.tpsl.lock().map_err(|e| e.to_string())?;
        tpsl_guard.remove_config(position_id);

        // 更新记录状态
        let mut records_guard = self.records.lock().map_err(|e| e.to_string())?;
        for rec in records_guard.values_mut() {
            // 简单的匹配逻辑：position_id 包含 signal_id
            if position_id.contains(&rec.signal_id) {
                rec.status = "closed".to_string();
                rec.close_reason = reason.to_string();
            }
        }

        Ok(())
    }

    /// 获取指定 symbol 的所有未平仓位
    pub fn get_open_positions(&self, symbol: &str) -> Vec<Position> {
        match self.positions.lock() {
            Ok(guard) => {
                if let Some(pm) = guard.get(symbol) {
                    pm.all_open_positions()
                        .into_iter()
                        .cloned()
                        .collect()
                } else {
                    Vec::new()
                }
            }
            Err(poisoned) => {
                let guard = poisoned.into_inner();
                if let Some(pm) = guard.get(symbol) {
                    pm.all_open_positions()
                        .into_iter()
                        .cloned()
                        .collect()
                } else {
                    Vec::new()
                }
            }
        }
    }

    /// 获取活跃 TP/SL 配置数量
    pub fn active_tpsl_count(&self) -> usize {
        match self.tpsl.lock() {
            Ok(guard) => guard.len(),
            Err(poisoned) => poisoned.into_inner().len(),
        }
    }
}

impl Default for SignalExecutor {
    fn default() -> Self {
        Self::new()
    }
}

/// 将 TPAction 列表序列化为 JSON 字符串
pub fn actions_to_json(actions: &[TPAction]) -> Result<String, serde_json::Error> {
    serde_json::to_string(actions)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_signal() -> Signal {
        Signal {
            id: "sig-001".to_string(),
            bot_id: "bot-001".to_string(),
            bot_type: "strategy".to_string(),
            symbol: "BTCUSDT".to_string(),
            side: "BUY".to_string(),
            direction: "LONG".to_string(),
            entry_price: 70000.0,
            market_order: false,
            slippage_pct: 0.001,
            tp1: 75000.0,
            tp2: 80000.0,
            tp3: 85000.0,
            tp1_pct: 0.4,
            tp2_pct: 0.3,
            tp3_pct: 0.3,
            stop_loss: 65000.0,
            move_sl_after: 75000.0,
            move_sl_to: 70000.0,
            trailing_tp: 0.0,
            trailing_sl: 0.0,
            leverage: 10.0,
            max_stake_pct: 0.1,
            confidence: 0.85,
            ai_reason: "test".to_string(),
            timestamp: 1700000000000,
            expire_at: 1700003600000,
        }
    }

    #[test]
    fn test_execute_signal() {
        let mut executor = SignalExecutor::new();
        let signal = test_signal();
        let record = executor.execute_signal(signal, 10000.0).unwrap();

        assert_eq!(record.signal_id, "sig-001");
        assert_eq!(record.status, "filled");
        assert!(record.entry_qty > 0.0);
    }

    #[test]
    fn test_execute_invalid_signal() {
        let mut executor = SignalExecutor::new();
        let mut signal = test_signal();
        signal.id = "".to_string();
        assert!(executor.execute_signal(signal, 10000.0).is_err());
    }

    #[test]
    fn test_price_update_triggers_tp() {
        let mut executor = SignalExecutor::new();
        let signal = test_signal();
        let _ = executor.execute_signal(signal, 10000.0).unwrap();

        // 价格涨到 TP1 之上，应触发止盈
        let actions = executor.on_price_update("BTCUSDT", 76000.0);
        assert!(!actions.is_empty());
    }

    #[test]
    fn test_actions_to_json() {
        let actions = vec![TPAction::TakeProfit {
            position_id: "p1".to_string(),
            level: "TP1".to_string(),
            price: 75000.0,
            quantity: 0.04,
        }];
        let json = actions_to_json(&actions).unwrap();
        assert!(json.contains("take_profit"));
        assert!(json.contains("TP1"));
    }

    #[test]
    fn test_get_record() {
        let mut executor = SignalExecutor::new();
        let signal = test_signal();
        let _ = executor.execute_signal(signal.clone(), 10000.0).unwrap();

        let record = executor.get_record("sig-001");
        assert!(record.is_some());
        assert_eq!(record.unwrap().signal_id, "sig-001");
    }

    #[test]
    fn test_close_position() {
        let mut executor = SignalExecutor::new();
        let signal = test_signal();
        let record = executor.execute_signal(signal.clone(), 10000.0).unwrap();
        let pos_id = format!("BTCUSDT-sig-001");

        assert!(executor
            .close_position(&pos_id, "BTCUSDT", "manual_close")
            .is_ok());

        let updated = executor.get_record("sig-001").unwrap();
        assert_eq!(updated.status, "closed");
        assert_eq!(updated.close_reason, "manual_close");
    }
}
