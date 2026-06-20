//! 仓位管理器
//!
//! PositionManager 负责维护所有活跃仓位的生命周期，
//! 包括开仓、关仓、未实现盈亏计算和仓位大小计算。

use std::collections::HashMap;

use super::signal::Signal;

/// 单个仓位的完整状态
#[derive(Debug, Clone)]
pub struct Position {
    /// 仓位唯一标识
    pub position_id: String,
    /// 关联的信号 ID
    pub signal_id: String,
    /// 机器人 ID
    pub bot_id: String,
    /// 机器人类型
    pub bot_type: String,
    /// 交易对
    pub symbol: String,
    /// 买卖方向：BUY | SELL
    pub side: String,
    /// 实际入场价格
    pub entry_price: f64,
    /// 持仓数量
    pub quantity: f64,
    /// 杠杆倍数
    pub leverage: f64,
    /// 已实现盈亏
    pub realized_pnl: f64,
    /// 未实现盈亏
    pub unrealized_pnl: f64,
    /// 仓位状态：open | partial | closed
    pub status: String,
    /// 仓位创建时间戳（毫秒）
    pub created_at: i64,
    /// 仓位更新时间戳（毫秒）
    pub updated_at: i64,
    /// 已平数量（用于追踪部分平仓）
    pub closed_qty: f64,
    /// 关仓原因
    pub close_reason: String,
}

impl Position {
    /// 创建一个新的仓位
    pub fn new(
        position_id: String,
        signal_id: String,
        bot_id: String,
        bot_type: String,
        symbol: String,
        side: String,
        entry_price: f64,
        quantity: f64,
        leverage: f64,
    ) -> Self {
        let now = now_ms();
        Self {
            position_id,
            signal_id,
            bot_id,
            bot_type,
            symbol,
            side,
            entry_price,
            quantity,
            leverage,
            realized_pnl: 0.0,
            unrealized_pnl: 0.0,
            status: "open".to_string(),
            created_at: now,
            updated_at: now,
            closed_qty: 0.0,
            close_reason: String::new(),
        }
    }

    /// 计算当前未实现盈亏（基于标记价格）
    pub fn calc_unrealized_pnl(&self, mark_price: f64) -> f64 {
        let direction = if self.side == "BUY" { 1.0 } else { -1.0 };
        (mark_price - self.entry_price) * self.quantity * direction * self.leverage
    }

    /// 剩余未平数量
    pub fn remaining_qty(&self) -> f64 {
        self.quantity - self.closed_qty
    }
}

/// 仓位管理器，管理所有活跃仓位
pub struct PositionManager {
    positions: HashMap<String, Position>,
}

impl PositionManager {
    /// 创建空的仓位管理器
    pub fn new() -> Self {
        Self {
            positions: HashMap::new(),
        }
    }

    /// 开仓：将仓位加入管理
    ///
    /// 返回 position_id 以便后续引用
    pub fn open_position(&mut self, pos: Position) -> String {
        let id = pos.position_id.clone();
        self.positions.insert(id.clone(), pos);
        id
    }

    /// 关仓：标记仓位为 closed 并记录原因
    pub fn close_position(&mut self, position_id: &str, reason: &str) {
        if let Some(pos) = self.positions.get_mut(position_id) {
            pos.status = "closed".to_string();
            pos.close_reason = reason.to_string();
            pos.updated_at = now_ms();
            pos.closed_qty = pos.quantity;
        }
    }

    /// 部分平仓：减少仓位数量
    pub fn partial_close(&mut self, position_id: &str, qty: f64, pnl: f64) {
        if let Some(pos) = self.positions.get_mut(position_id) {
            pos.closed_qty += qty;
            pos.realized_pnl += pnl;
            pos.updated_at = now_ms();
            if (pos.remaining_qty() - 0.0).abs() < 1e-12 {
                pos.status = "closed".to_string();
            } else {
                pos.status = "partial".to_string();
            }
        }
    }

    /// 获取仓位引用
    pub fn get_position(&self, position_id: &str) -> Option<&Position> {
        self.positions.get(position_id)
    }

    /// 获取可变仓位引用
    pub fn get_position_mut(&mut self, position_id: &str) -> Option<&mut Position> {
        self.positions.get_mut(position_id)
    }

    /// 更新指定仓位的未实现盈亏
    pub fn update_unrealized_pnl(&mut self, position_id: &str, current_price: f64) {
        if let Some(pos) = self.positions.get_mut(position_id) {
            pos.unrealized_pnl = pos.calc_unrealized_pnl(current_price);
            pos.updated_at = now_ms();
        }
    }

    /// 根据信号和可用余额计算仓位大小
    ///
    /// 返回 (quantity, notional_value) 或错误信息
    pub fn calculate_position_size(
        &self,
        signal: &Signal,
        available_balance: f64,
    ) -> Result<(f64, f64), String> {
        if available_balance <= 0.0 {
            return Err("available_balance must be > 0".to_string());
        }
        let notional = available_balance * signal.max_stake_pct * signal.leverage;
        if notional <= 0.0 {
            return Err("notional value is zero".to_string());
        }
        let qty = notional / signal.entry_price;
        Ok((qty, notional))
    }

    /// 获取指定交易对的所有未平仓位
    pub fn get_open_positions(&self, symbol: &str) -> Vec<&Position> {
        self.positions
            .values()
            .filter(|p| p.symbol == symbol && (p.status == "open" || p.status == "partial"))
            .collect()
    }

    /// 获取所有未平仓位
    pub fn all_open_positions(&self) -> Vec<&Position> {
        self.positions
            .values()
            .filter(|p| p.status == "open" || p.status == "partial")
            .collect()
    }

    /// 计算剩余数量（减去已平部分）
    pub fn remaining_qty(&self, position_id: &str, filled_qty: f64) -> f64 {
        if let Some(pos) = self.positions.get(position_id) {
            pos.quantity - filled_qty
        } else {
            0.0
        }
    }

    /// 仓位数量
    pub fn len(&self) -> usize {
        self.positions.len()
    }

    /// 是否为空
    pub fn is_empty(&self) -> bool {
        self.positions.is_empty()
    }
}

impl Default for PositionManager {
    fn default() -> Self {
        Self::new()
    }
}

/// 获取当前毫秒时间戳
fn now_ms() -> i64 {
    std::time::SystemTime::now()
        .duration_since(std::time::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as i64
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_open_and_get() {
        let mut pm = PositionManager::new();
        let pos = Position::new(
            "p1".to_string(),
            "s1".to_string(),
            "b1".to_string(),
            "strategy".to_string(),
            "BTCUSDT".to_string(),
            "BUY".to_string(),
            70000.0,
            0.1,
            10.0,
        );
        let id = pm.open_position(pos);
        assert_eq!(id, "p1");
        assert!(pm.get_position("p1").is_some());
    }

    #[test]
    fn test_calc_position_size() {
        let pm = PositionManager::new();
        let signal = Signal {
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
        };
        let (qty, notional) = pm.calculate_position_size(&signal, 10000.0).unwrap();
        assert!((notional - 10000.0).abs() < 1e-9);
        assert!((qty - (notional / 70000.0)).abs() < 1e-12);
    }

    #[test]
    fn test_close_position() {
        let mut pm = PositionManager::new();
        let pos = Position::new(
            "p1".to_string(),
            "s1".to_string(),
            "b1".to_string(),
            "strategy".to_string(),
            "BTCUSDT".to_string(),
            "BUY".to_string(),
            70000.0,
            0.1,
            10.0,
        );
        pm.open_position(pos);
        pm.close_position("p1", "sl_hit");
        let p = pm.get_position("p1").unwrap();
        assert_eq!(p.status, "closed");
        assert_eq!(p.close_reason, "sl_hit");
    }

    #[test]
    fn test_get_open_positions() {
        let mut pm = PositionManager::new();
        let pos1 = Position::new(
            "p1".to_string(),
            "s1".to_string(),
            "b1".to_string(),
            "strategy".to_string(),
            "BTCUSDT".to_string(),
            "BUY".to_string(),
            70000.0,
            0.1,
            10.0,
        );
        let pos2 = Position::new(
            "p2".to_string(),
            "s2".to_string(),
            "b2".to_string(),
            "strategy".to_string(),
            "ETHUSDT".to_string(),
            "SELL".to_string(),
            3500.0,
            1.0,
            5.0,
        );
        pm.open_position(pos1);
        pm.open_position(pos2);
        let btc = pm.get_open_positions("BTCUSDT");
        assert_eq!(btc.len(), 1);
        assert_eq!(btc[0].position_id, "p1");
    }
}
