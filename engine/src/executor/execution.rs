//! 执行引擎
//!
//! ExecutionEngine 负责将信号转换为实际订单并执行，
//! 包括入场单、止盈单和止损单的下单操作。
//!
//! 当前为模拟实现，生产环境应接入交易所 API。

use super::signal::Signal;
use std::sync::atomic::{AtomicU64, Ordering};

/// 入场订单执行结果
#[derive(Debug)]
pub struct EntryResult {
    /// 实际成交价格
    pub fill_price: f64,
    /// 实际成交数量
    pub fill_qty: f64,
    /// 订单 ID
    pub order_id: String,
    /// 滑点成本
    pub slippage_cost: f64,
}

/// 执行引擎
pub struct ExecutionEngine {
    order_counter: AtomicU64,
}

impl ExecutionEngine {
    /// 创建新的执行引擎
    pub fn new() -> Self {
        Self {
            order_counter: AtomicU64::new(1),
        }
    }

    /// 下入场单
    ///
    /// 根据信号参数计算实际成交价格（考虑滑点），
    /// 返回 `EntryResult` 包含成交详情。
    ///
    /// # 参数
    /// - `signal`: 交易信号
    /// - `qty`: 下单数量
    ///
    /// # 错误
    /// - 数量为 0 或负数时返回 Err
    pub fn place_entry_order(&self, signal: &Signal, qty: f64) -> Result<EntryResult, String> {
        if qty <= 0.0 {
            return Err(format!("invalid quantity: {}", qty));
        }

        let order_id = self.next_order_id();
        let slippage = signal.slippage_pct;

        // 计算实际成交价格（考虑滑点）
        let (fill_price, slippage_cost) = if signal.market_order {
            // 市价单：按当前价格 + 滑点成交
            let slip = signal.entry_price * slippage;
            let fp = if signal.side == "BUY" {
                signal.entry_price * (1.0 + slippage)
            } else {
                signal.entry_price * (1.0 - slippage)
            };
            (fp, slip * qty)
        } else {
            // 限价单：按信号价格成交（假设完全成交）
            (signal.entry_price, 0.0)
        };

        Ok(EntryResult {
            fill_price,
            fill_qty: qty,
            order_id,
            slippage_cost,
        })
    }

    /// 下止盈单
    ///
    /// # 参数
    /// - `position_id`: 仓位 ID
    /// - `level`: 止盈档位标签（TP1/TP2/TP3/TRAILING）
    /// - `price`: 目标价格
    /// - `qty`: 平仓数量
    pub fn place_tp_order(
        &self,
        position_id: &str,
        level: &str,
        price: f64,
        qty: f64,
    ) -> Result<(), String> {
        if qty <= 0.0 {
            return Err(format!(
                "TP order quantity must be > 0, got {} for {} {}",
                qty, position_id, level
            ));
        }
        if price <= 0.0 {
            return Err(format!(
                "TP order price must be > 0, got {} for {} {}",
                price, position_id, level
            ));
        }

        let order_id = self.next_order_id();
        log::debug!(
            "[ExecutionEngine] TP order placed: oid={} pos={} level={} price={} qty={}",
            order_id,
            position_id,
            level,
            price,
            qty
        );

        // 模拟下单成功，生产环境应调用交易所 API
        let _ = order_id;
        Ok(())
    }

    /// 下止损单
    ///
    /// # 参数
    /// - `position_id`: 仓位 ID
    /// - `price`: 止损价格
    /// - `qty`: 平仓数量（通常是剩余全部仓位）
    pub fn place_sl_order(
        &self,
        position_id: &str,
        price: f64,
        qty: f64,
    ) -> Result<(), String> {
        if qty <= 0.0 {
            return Err(format!(
                "SL order quantity must be > 0, got {} for {}",
                qty, position_id
            ));
        }
        if price <= 0.0 {
            return Err(format!(
                "SL order price must be > 0, got {} for {}",
                price, position_id
            ));
        }

        let order_id = self.next_order_id();
        log::debug!(
            "[ExecutionEngine] SL order placed: oid={} pos={} price={} qty={}",
            order_id,
            position_id,
            price,
            qty
        );

        let _ = order_id;
        Ok(())
    }

    /// 取消订单
    ///
    /// # 参数
    /// - `order_id`: 要取消的订单 ID
    pub fn cancel_order(&self, order_id: &str) -> Result<(), String> {
        log::debug!("[ExecutionEngine] Order cancelled: oid={}", order_id);
        Ok(())
    }

    /// 获取下一个订单 ID
    fn next_order_id(&self) -> String {
        let id = self.order_counter.fetch_add(1, Ordering::SeqCst);
        format!("ORD-{}", id)
    }
}

impl Default for ExecutionEngine {
    fn default() -> Self {
        Self::new()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn test_signal() -> Signal {
        Signal {
            id: "s1".to_string(),
            bot_id: "b1".to_string(),
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
            confidence: 0.0,
            ai_reason: "".to_string(),
            timestamp: 0,
            expire_at: 9999999999999,
        }
    }

    #[test]
    fn test_place_entry_limit() {
        let engine = ExecutionEngine::new();
        let sig = test_signal();
        let result = engine.place_entry_order(&sig, 0.1).unwrap();
        assert!((result.fill_price - 70000.0).abs() < 1e-9);
        assert!((result.fill_qty - 0.1).abs() < 1e-12);
        assert!(result.order_id.starts_with("ORD-"));
    }

    #[test]
    fn test_place_entry_market() {
        let mut sig = test_signal();
        sig.market_order = true;
        let engine = ExecutionEngine::new();
        let result = engine.place_entry_order(&sig, 0.1).unwrap();
        // 市价单 BUY 应该有正滑点
        assert!(result.fill_price > 70000.0);
    }

    #[test]
    fn test_place_entry_invalid_qty() {
        let engine = ExecutionEngine::new();
        let sig = test_signal();
        assert!(engine.place_entry_order(&sig, 0.0).is_err());
        assert!(engine.place_entry_order(&sig, -1.0).is_err());
    }

    #[test]
    fn test_place_tp_order() {
        let engine = ExecutionEngine::new();
        assert!(engine.place_tp_order("p1", "TP1", 75000.0, 0.04).is_ok());
    }

    #[test]
    fn test_place_tp_invalid() {
        let engine = ExecutionEngine::new();
        assert!(engine.place_tp_order("p1", "TP1", 0.0, 0.04).is_err());
        assert!(engine.place_tp_order("p1", "TP1", 75000.0, 0.0).is_err());
    }

    #[test]
    fn test_place_sl_order() {
        let engine = ExecutionEngine::new();
        assert!(engine.place_sl_order("p1", 65000.0, 0.1).is_ok());
    }

    #[test]
    fn test_cancel_order() {
        let engine = ExecutionEngine::new();
        assert!(engine.cancel_order("ORD-1").is_ok());
    }

    #[test]
    fn test_sell_market_slippage() {
        let mut sig = test_signal();
        sig.side = "SELL".to_string();
        sig.market_order = true;
        let engine = ExecutionEngine::new();
        let result = engine.place_entry_order(&sig, 0.1).unwrap();
        // 市价单 SELL 应该有负滑点
        assert!(result.fill_price < 70000.0);
    }
}
