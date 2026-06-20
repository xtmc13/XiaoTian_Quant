//! 统一信号结构体定义
//!
//! Signal 是策略机器人、信号机器人和 AI 机器人三种 bot 发出的统一交易信号，
//! 包含完整的入场、止盈止损和仓位管理参数。

use serde::{Deserialize, Serialize};

/// 交易信号的统一表示
///
/// 支持三种机器人类型：strategy / signal / ai
/// side 为 "BUY" 或 "SELL"，direction 为 "LONG" 或 "SHORT"
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Signal {
    /// 信号唯一标识（UUID）
    pub id: String,
    /// 发出信号的机器人 ID
    pub bot_id: String,
    /// 机器人类型：strategy | signal | ai
    pub bot_type: String,
    /// 交易对，如 BTCUSDT
    pub symbol: String,
    /// 买卖方向：BUY | SELL
    pub side: String,
    /// 仓位方向：LONG | SHORT
    pub direction: String,
    /// 入场价格（market_order=true 时仅作参考）
    pub entry_price: f64,
    /// 是否市价单入场
    pub market_order: bool,
    /// 允许滑点百分比（如 0.001 表示 0.1%）
    pub slippage_pct: f64,
    // ── 三档止盈 ──
    /// 第一档止盈价格
    pub tp1: f64,
    /// 第二档止盈价格
    pub tp2: f64,
    /// 第三档止盈价格
    pub tp3: f64,
    /// 第一档止盈分仓比例（0.0~1.0）
    pub tp1_pct: f64,
    /// 第二档止盈分仓比例（0.0~1.0）
    pub tp2_pct: f64,
    /// 第三档止盈分仓比例（0.0~1.0）
    pub tp3_pct: f64,
    // ── 止损与移动止损 ──
    /// 固定止损价格
    pub stop_loss: f64,
    /// 激活移动止损的价格阈值
    pub move_sl_after: f64,
    /// 移动止损目标价格
    pub move_sl_to: f64,
    /// 追踪止盈比例（如 0.02 表示 2% 回撤触发）
    pub trailing_tp: f64,
    /// 追踪止损比例（如 0.015 表示 1.5% 反向移动触发）
    pub trailing_sl: f64,
    // ── 仓位参数 ──
    /// 杠杆倍数
    pub leverage: f64,
    /// 最大占用资金池比例（0.0~1.0）
    pub max_stake_pct: f64,
    // ── AI 专用 ──
    /// AI 置信度（0.0~1.0）
    pub confidence: f64,
    /// AI 推理原因
    pub ai_reason: String,
    // ── 时效 ──
    /// 信号生成时间戳（毫秒）
    pub timestamp: i64,
    /// 信号过期时间戳（毫秒）
    pub expire_at: i64,
}

impl Signal {
    /// 校验信号的必填字段和数值范围
    pub fn validate(&self) -> Result<(), String> {
        if self.id.is_empty() {
            return Err("signal.id is empty".to_string());
        }
        if self.bot_id.is_empty() {
            return Err("signal.bot_id is empty".to_string());
        }
        if !matches!(self.bot_type.as_str(), "strategy" | "signal" | "ai") {
            return Err(format!("invalid bot_type: {}", self.bot_type));
        }
        if self.symbol.is_empty() {
            return Err("signal.symbol is empty".to_string());
        }
        if !matches!(self.side.as_str(), "BUY" | "SELL") {
            return Err(format!("invalid side: {}", self.side));
        }
        if !matches!(self.direction.as_str(), "LONG" | "SHORT") {
            return Err(format!("invalid direction: {}", self.direction));
        }
        if self.entry_price <= 0.0 {
            return Err(format!("entry_price must be > 0, got {}", self.entry_price));
        }
        let total_tp = self.total_tp_pct();
        if (total_tp - 1.0).abs() > 1e-9 && total_tp != 0.0 {
            return Err(format!("tp percentages must sum to 1.0 or 0.0, got {}", total_tp));
        }
        if self.leverage <= 0.0 {
            return Err(format!("leverage must be > 0, got {}", self.leverage));
        }
        if !(0.0..=1.0).contains(&self.max_stake_pct) {
            return Err(format!(
                "max_stake_pct must be in [0,1], got {}",
                self.max_stake_pct
            ));
        }
        if self.expire_at <= self.timestamp {
            return Err("expire_at must be greater than timestamp".to_string());
        }
        Ok(())
    }

    /// 检查信号是否已过期（基于毫秒时间戳）
    pub fn is_expired(&self) -> bool {
        let now = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap_or_default()
            .as_millis() as i64;
        now > self.expire_at
    }

    /// 返回三档止盈分仓比例之和
    pub fn total_tp_pct(&self) -> f64 {
        self.tp1_pct + self.tp2_pct + self.tp3_pct
    }

    /// 根据方向返回止盈价格列表（从低到高或从高到低）
    pub fn tp_levels(&self) -> [(f64, f64, String); 3] {
        [
            (self.tp1, self.tp1_pct, "TP1".to_string()),
            (self.tp2, self.tp2_pct, "TP2".to_string()),
            (self.tp3, self.tp3_pct, "TP3".to_string()),
        ]
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn valid_signal() -> Signal {
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
            ai_reason: "breakout detected".to_string(),
            timestamp: 1700000000000,
            expire_at: 1700003600000,
        }
    }

    #[test]
    fn test_validate_ok() {
        let s = valid_signal();
        assert!(s.validate().is_ok());
    }

    #[test]
    fn test_validate_empty_id() {
        let mut s = valid_signal();
        s.id = "".to_string();
        assert!(s.validate().is_err());
    }

    #[test]
    fn test_validate_invalid_bot_type() {
        let mut s = valid_signal();
        s.bot_type = "invalid".to_string();
        assert!(s.validate().is_err());
    }

    #[test]
    fn test_validate_tp_pct_sum() {
        let mut s = valid_signal();
        s.tp1_pct = 0.5;
        s.tp2_pct = 0.5;
        s.tp3_pct = 0.1;
        assert!(s.validate().is_err());
    }

    #[test]
    fn test_total_tp_pct() {
        let s = valid_signal();
        assert!((s.total_tp_pct() - 1.0).abs() < 1e-9);
    }

    #[test]
    fn test_is_expired() {
        let mut s = valid_signal();
        s.expire_at = 1; // far in the past
        s.timestamp = 0;
        assert!(s.is_expired());
    }
}
