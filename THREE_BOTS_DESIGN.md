# 小天量化 三机器体系 完整实现方案

> **设计原则**: 信号生成层与信号执行层完全解耦  
> **核心模块**: Rust SignalExecutor 统一执行  
> **三类机器人**: Freqtrade(自主) / Cryptoleks(信号) / AI Alpha(AI决策)

---

## 一总体架构

```
┌────────────────────────────────────────────────────────┐
│                    信号生成层 (Signal Generation)                        │
│                                                                             │
│  ┌───────────┐  ┌───────────┐  ┌────────────────┐                        │
│  │ Freqtrade     │  │ Cryptoleks    │  │ AI Alpha            │                        │
│  │ Strategy Bot  │  │ Signal Bot    │  │ Decision Bot        │                        │
│  │ (自主扫描)    │  │ (跟单执行)    │  │ (智能过滤)          │                        │
│  └───────────┘  └───────────┘  └────────────────┘                        │
│        │                │                  │                              │
│        │ OnTick/OnBar   │ Webhook/API      │ gRPC/HTTP                      │
│        │                │                  │                              │
│        └──────────────┬───────────────┬────────────────┘                              │
│                     │                │                                          │
│                     ▼                ▼                                          │
└──────────────────┬─────────────┬──────────────────────────────────────────────┘
                      │                │
                      ▼                ▼
┌──────────────────┼─────────────┼──────────────────────────────────────────────┐
│            Go: Bot Manager (Signal Router + 生命周期)                         │
│                                                                             │
│  ┌───────────────────────────────────────────────────────┐     │
│  │ SignalQueue (有序信号队列) → SignalValidator → SignalRouter      │     │
│  │   ├─ priority排序          ├─ 参数校验       ├─ 按bot路由       │     │
│  │   ├─ 去重                ├─ 风控检查       ├─ 批量/单条       │     │
│  │   └─ 滥流控制            └─ 权限验证       └─ 异步处理       │     │
│  └───────────────────────────────────────────────────────┘     │
│                              │                                            │
│                              ▼                                            │
└───────────────────────┬────────────────────────────────────────────────┘
                               │
                               ▼ CGO / FFI
┌───────────────────────┼────────────────────────────────────────────────┐
│            Rust: SignalExecutor (统一执行层)                                │
│                                                                             │
│  ┌───────────────────────────────────────────────────────┐     │
│  │ ┌───────────┐  ┌──────────────┐  ┌─────────────┐  ┌──────────┐  │     │
│  │ │Position     │  │ TP/SL Manager  │  │Execution      │  │OrderBook   │  │     │
│  │ │Manager      │  │ (阶梯止盈)   │  │Engine         │  │Snapshot    │  │     │
│  │ ├─ 仓位跟踪  │  ├─ T1/T2/T3    │  ├─ 速度优化  │  ├─ 深度查询 │  │     │
│  │ ├─ 均价计算  │  ├─ 移动止损  │  ├─ 部分成交  │  └─ 最佳价  │  │     │
│  │ └─ 盈亏计算  │  └─ 追踪止盈  │  └─ 错误重试  │              │  │     │
│  │ └───────────┘  └──────────────┘  └─────────────┘  └──────────┘  │     │
│  └─────────────────────────────────────────────────────────────────────┘     │
│                                                                             │
└────────────────────────────────────────────────────────────────────────┘
```

---

## 二数据模型

### 2.1 Signal (统一信号格式)

```go
// gateway/internal/model/bot.go  (新建)

package model

// SignalType 信号来源类型
type SignalType string

const (
	SignalTypeStrategy  SignalType = "strategy"   // Freqtrade 策略生成
	SignalTypeExternal  SignalType = "external"   // Cryptoleks 外部信号
	SignalTypeAI        SignalType = "ai"         // AI Alpha 决策
)

// SignalRiskLevel 风险等级
type SignalRiskLevel string

const (
	RiskConservative SignalRiskLevel = "T1"  // 保守: 小仓位，远止损
	RiskModerate     SignalRiskLevel = "T2"  // 平衡: 中等仓位
	RiskAggressive   SignalRiskLevel = "T3"  // 激进: 大仓位，近止损
)

// Signal 统一信号格式 — 三种机器人都产生此格式
type Signal struct {
	// 基础字段
	ID          string      `json:"id"`           // uuid
	Type        SignalType  `json:"type"`         // strategy / external / ai
	BotID       string      `json:"bot_id"`       // 来源机器人ID
	Symbol      string      `json:"symbol"`       // BTCUSDT
	Side        string      `json:"side"`         // BUY / SELL
	Direction   string      `json:"direction"`    // LONG / SHORT

	// 价格
	EntryPrice   float64 `json:"entry_price"`    // 入场价 (限价单)
	MarketOrder  bool    `json:"market_order"`   // 是否市价单
	SlippagePct  float64 `json:"slippage_pct"`   // 允许滑点%

	// 止盈止损 (关键—信号执行器核心参数)
	TP1         float64         `json:"tp1"`        // 第一档止盈
	TP2         float64         `json:"tp2"`        // 第二档止盈
	TP3         float64         `json:"tp3"`        // 第三档止盈
	TP1Pct      float64         `json:"tp1_pct"`    // TP1 分仓比例 (0.4 = 40%)
	TP2Pct      float64         `json:"tp2_pct"`    // TP2 分仓比例
	TP3Pct      float64         `json:"tp3_pct"`    // TP3 分仓比例
	StopLoss    float64         `json:"stop_loss"`   // 固定止损
	MoveSLAfter float64         `json:"move_sl_after"` // 激活移动止损的价格
	MoveSLTo    float64         `json:"move_sl_to"`    // 移动止损目标价 (如: 入场价)
	TrailingTP  float64         `json:"trailing_tp"`   // 追踪止盈比例
	TrailingSL  float64         `json:"trailing_sl"`   // 追踪止损比例

	// 仓位
	RiskLevel   SignalRiskLevel `json:"risk_level"`   // T1/T2/T3
	MaxStakePct float64         `json:"max_stake_pct"` // 占资金池比例
	Leverage    float64         `json:"leverage"`      // 杠杆倍数

	// AI 特定
	Confidence  float64 `json:"confidence"`       // AI 置信度 (0-100)
	AIReason    string  `json:"ai_reason"`        // AI 决策原因
	AIFilters   []string `json:"ai_filters"`      // 过滤条件

	// 时效
	Timestamp   int64  `json:"timestamp"`         // 生成时间
	ExpireAt    int64  `json:"expire_at"`         // 过期时间 (信号执行器不执行过期信号)
	ValidSec    int64  `json:"valid_sec"`         // 有效期秒数

	// 元数据
	Source      string  `json:"source"`           // 来源标识
	RawData     string  `json:"raw_data"`         // 原始JSON
}

// IsExpired 检查信号是否过期
func (s *Signal) IsExpired() bool {
	if s.ExpireAt > 0 {
		return time.Now().Unix() > s.ExpireAt
	}
	if s.ValidSec > 0 {
		return time.Now().Unix() > s.Timestamp+s.ValidSec
	}
	return false
}

// TotalTPPct TP分仓总比
func (s *Signal) TotalTPPct() float64 {
	return s.TP1Pct + s.TP2Pct + s.TP3Pct
}

// IsValid 检查信号参数是否有效
func (s *Signal) IsValid() error {
	if s.Symbol == "" || s.Side == "" {
		return fmt.Errorf("signal missing symbol or side")
	}
	if s.EntryPrice <= 0 && !s.MarketOrder {
		return fmt.Errorf("entry_price must be > 0 for limit orders")
	}
	if s.StopLoss <= 0 && s.TrailingSL <= 0 {
		return fmt.Errorf("must specify stop_loss or trailing_sl")
	}
	if s.TotalTPPct() <= 0 {
		return fmt.Errorf("must specify at least one TP")
	}
	if s.TotalTPPct() > 1.0 {
		return fmt.Errorf("TP percentages sum to > 100%%")
	}
	return nil
}
```

### 2.2 BotConfig (机器人配置)

```go
// BotType 机器人类型
type BotType string

const (
	BotTypeStrategy BotType = "strategy"  // Freqtrade 自主策略
	BotTypeSignal   BotType = "signal"    // Cryptoleks 信号跟单
	BotTypeAI       BotType = "ai"        // AI Alpha 决策
)

// BotStatus 机器人状态
type BotStatus string

const (
	BotStatusRunning  BotStatus = "running"
	BotStatusStopped  BotStatus = "stopped"
	BotStatusPaused   BotStatus = "paused"
	BotStatusError    BotStatus = "error"
)

// BotConfig 机器人配置
type BotConfig struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Type        BotType   `json:"type" db:"bot_type"`       // strategy/signal/ai
	Status      BotStatus `json:"status" db:"status"`
	Symbol      string    `json:"symbol" db:"symbol"`
	Exchange    string    `json:"exchange" db:"exchange"`
	CreatedAt   int64     `json:"created_at" db:"created_at"`
	UpdatedAt   int64     `json:"updated_at" db:"updated_at"`

	// Freqtrade 策略机器人特定
	StrategyName    string          `json:"strategy_name,omitempty" db:"strategy_name"`     // 策略名称
	StrategyParams  json.RawMessage `json:"strategy_params,omitempty" db:"strategy_params"` // 策略参数
	Timeframes      []string        `json:"timeframes,omitempty" db:"timeframes"`           // 监听周期 ["5m","15m","1h"]
	Indicators      []string        `json:"indicators,omitempty" db:"indicators"`           // 指标列表

	// Cryptoleks 信号机器人特定
	SignalSource    string          `json:"signal_source,omitempty" db:"signal_source"`     // 信号来源: internal/indicator_ide/api/webhook
	WebhookURL      string          `json:"webhook_url,omitempty" db:"webhook_url"`         // Webhook 接收地址
	SignalTimeout   int             `json:"signal_timeout,omitempty" db:"signal_timeout"`   // 信号超时秒
	AutoConfirm     bool            `json:"auto_confirm,omitempty" db:"auto_confirm"`       // 是否自动确认
	RiskProfile     json.RawMessage `json:"risk_profile,omitempty" db:"risk_profile"`       // 风险配置

	// AI Alpha 机器人特定
	AIModel         string          `json:"ai_model,omitempty" db:"ai_model"`               // 使用的模型
	ConfidenceThreshold float64     `json:"confidence_threshold,omitempty" db:"confidence_threshold"` // 最低置信度
	ScanInterval    int             `json:"scan_interval,omitempty" db:"scan_interval"`     // 扫描间隔秒
	MarketConditionFilter bool      `json:"market_condition_filter,omitempty" db:"market_condition_filter"` // 是否启用市场条件过滤
	AIPrompt        string          `json:"ai_prompt,omitempty" db:"ai_prompt"`             // 自定义AI提示

	// 通用执行参数 (传递给 SignalExecutor)
	DefaultSignal   *Signal         `json:"default_signal,omitempty" db:"default_signal"`   // 默认信号模板
	MaxDailyTrades  int             `json:"max_daily_trades,omitempty" db:"max_daily_trades"` // 每日最大交易
	MaxOpenPos      int             `json:"max_open_pos,omitempty" db:"max_open_pos"`       // 最大持仓
	PnlSummary      json.RawMessage `json:"pnl_summary,omitempty" db:"pnl_summary"`         // 盈亏汇总
}
```

### 2.3 ExecutionRecord (执行记录)

```go
// ExecutionStatus 执行状态
type ExecutionStatus string

const (
	ExecPending    ExecutionStatus = "pending"     // 等待执行
	ExecPartial    ExecutionStatus = "partial"     // 部分执行 (某个TP已触发)
	ExecFilled     ExecutionStatus = "filled"      // 全部执行
	ExecCancelled  ExecutionStatus = "cancelled"   // 已取消
	ExecExpired    ExecutionStatus = "expired"     // 信号过期
)

// ExecutionRecord 单条信号的执行全过程记录
type ExecutionRecord struct {
	ID          string          `json:"id" db:"id"`
	SignalID    string          `json:"signal_id" db:"signal_id"`
	BotID       string          `json:"bot_id" db:"bot_id"`
	BotType     BotType         `json:"bot_type" db:"bot_type"`
	Symbol      string          `json:"symbol" db:"symbol"`
	Status      ExecutionStatus `json:"status" db:"status"`

	// 入场
	EntryOrderID string  `json:"entry_order_id,omitempty" db:"entry_order_id"`
	EntryPrice   float64 `json:"entry_price" db:"entry_price"`
	EntryQty     float64 `json:"entry_qty" db:"entry_qty"`
	EntryTime    int64   `json:"entry_time" db:"entry_time"`

	// TP 执行历史
	TP1OrderID   string  `json:"tp1_order_id,omitempty" db:"tp1_order_id"`
	TP1Price     float64 `json:"tp1_price" db:"tp1_price"`
	TP1Qty       float64 `json:"tp1_qty" db:"tp1_qty"`
	TP1Time      int64   `json:"tp1_time" db:"tp1_time"`

	TP2OrderID   string  `json:"tp2_order_id,omitempty" db:"tp2_order_id"`
	TP2Price     float64 `json:"tp2_price" db:"tp2_price"`
	TP2Qty       float64 `json:"tp2_qty" db:"tp2_qty"`
	TP2Time      int64   `json:"tp2_time" db:"tp2_time"`

	TP3OrderID   string  `json:"tp3_order_id,omitempty" db:"tp3_order_id"`
	TP3Price     float64 `json:"tp3_price" db:"tp3_price"`
	TP3Qty       float64 `json:"tp3_qty" db:"tp3_qty"`
	TP3Time      int64   `json:"tp3_time" db:"tp3_time"`

	// 止损
	SLTriggered  bool    `json:"sl_triggered" db:"sl_triggered"`
	SLPrice      float64 `json:"sl_price" db:"sl_price"`
	SLTime       int64   `json:"sl_time" db:"sl_time"`

	// 追踪止盈/止损
	TrailingActive bool    `json:"trailing_active" db:"trailing_active"`
	CurrentTP      float64 `json:"current_tp" db:"current_tp"`  // 当前有效TP价
	CurrentSL      float64 `json:"current_sl" db:"current_sl"`  // 当前有效SL价

	// 结果
	RealizedPnL  float64 `json:"realized_pnl" db:"realized_pnl"`
	ClosedAt     int64   `json:"closed_at" db:"closed_at"`
	CloseReason  string  `json:"close_reason" db:"close_reason"` // tp1/tp2/tp3/sl/expire/cancel

	CreatedAt    int64   `json:"created_at" db:"created_at"`
}
```

---

## 三Rust 层: SignalExecutor

### 3.1 模块位置

```
engine/src/
  ├─ executor/         (新建目录)
  │   ├─ mod.rs        — 模块入口 + SignalExecutor 结构体
  │   ├─ position.rs   — PositionManager 仓位管理
  │   ├─ tpsl.rs       — TPSLManager 止盈止损管理
  │   ├─ execution.rs  — ExecutionEngine 执行引擎
  │   └─ signal.rs     — Signal 解析/校验
  └─ ffi.rs            — 新增 FFI 函数
```

### 3.2 SignalExecutor 核心结构

```rust
// engine/src/executor/mod.rs

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

pub mod position;
pub mod tpsl;
pub mod execution;
pub mod signal;

use position::PositionManager;
use tpsl::{TPSLManager, TPLevel};
use execution::ExecutionEngine;
use signal::Signal;

/// SignalExecutor 统一信号执行引擎
/// 三种机器人的信号都进入此处理，统一处理仓位/止盈/止损/执行
pub struct SignalExecutor {
    /// 按 symbol 组织的仓位管理器
    positions: Arc<Mutex<HashMap<String, PositionManager>>>,

    /// 止盈止损管理器
    tpsl: Arc<Mutex<TPSLManager>>,

    /// 执行引擎 (与交易所对接)
    execution: ExecutionEngine,

    /// 执行记录回调 (Go 端通过 FFI 注册)
    on_execution: Option<Box<dyn Fn(ExecutionRecord) + Send>>,
}

/// 执行记录 — 通过 FFI 传回 Go
#[derive(Debug, Clone, serde::Serialize)]
pub struct ExecutionRecord {
    pub signal_id: String,
    pub bot_id: String,
    pub bot_type: String,      // "strategy" | "signal" | "ai"
    pub symbol: String,
    pub status: String,        // "pending" | "partial" | "filled" | "cancelled" | "expired"

    // 入场
    pub entry_price: f64,
    pub entry_qty: f64,
    pub entry_time: i64,

    // TP1/TP2/TP3 执行状态
    pub tp1_filled: bool,
    pub tp1_price: f64,
    pub tp1_qty: f64,
    pub tp2_filled: bool,
    pub tp2_price: f64,
    pub tp2_qty: f64,
    pub tp3_filled: bool,
    pub tp3_price: f64,
    pub tp3_qty: f64,

    // SL
    pub sl_triggered: bool,
    pub sl_price: f64,

    // 追踪
    pub trailing_active: bool,
    pub current_tp: f64,
    pub current_sl: f64,

    // 盈亏
    pub realized_pnl: f64,
    pub close_reason: String,
}
```

### 3.3 核心流程: execute_signal

```rust
/// 主入口: 执行一条信号
/// 被 Go 通过 FFI 调用: engine_execute_signal(json_ptr)
pub fn execute_signal(&mut self, signal: Signal) -> Result<ExecutionRecord, String> {
    // 1. 校验
    signal.validate()?;

    // 2. 检查过期
    if signal.is_expired() {
        return Err("Signal expired".into());
    }

    // 3. 获取/创建 PositionManager
    let mut positions = self.positions.lock().unwrap();
    let pos_mgr = positions
        .entry(signal.symbol.clone())
        .or_insert_with(|| PositionManager::new(&signal.symbol));

    // 4. 检查最大持仓
    if pos_mgr.open_positions_count() >= signal.max_open_positions {
        return Err("Max open positions reached".into());
    }

    // 5. 计算仓位大小 (基于 risk_level + max_stake_pct)
    let (qty, notional) = pos_mgr.calculate_position_size(&signal)?;

    // 6. 下入场单
    let entry_result = self.execution.place_entry_order(&signal, qty)?;

    // 7. 记录仓位
    let position_id = pos_mgr.open_position(OpenPositionReq {
        signal_id: signal.id.clone(),
        bot_id: signal.bot_id.clone(),
        bot_type: signal.bot_type.clone(),
        entry_price: entry_result.fill_price,
        quantity: qty,
        side: signal.side.clone(),
    });

    // 8. 挂 TP/SL 单 (关键步骤)
    self.tpsl.lock().unwrap().attach_tpsl(TPConfig {
        position_id: position_id.clone(),
        symbol: signal.symbol.clone(),
        entry_price: entry_result.fill_price,
        quantity: qty,

        // 阶梯止盈
        tp_levels: vec![
            TPLevel { price: signal.tp1, pct: signal.tp1_pct, label: "TP1".into() },
            TPLevel { price: signal.tp2, pct: signal.tp2_pct, label: "TP2".into() },
            TPLevel { price: signal.tp3, pct: signal.tp3_pct, label: "TP3".into() },
        ],

        // 止损
        stop_loss: signal.stop_loss,

        // 移动止损
        move_sl_after: signal.move_sl_after,
        move_sl_to: signal.move_sl_to,

        // 追踪
        trailing_tp_pct: signal.trailing_tp,
        trailing_sl_pct: signal.trailing_sl,
    });

    // 9. 返回执行记录
    let record = ExecutionRecord {
        signal_id: signal.id,
        bot_id: signal.bot_id,
        bot_type: signal.bot_type,
        symbol: signal.symbol,
        status: "pending".into(),
        entry_price: entry_result.fill_price,
        entry_qty: qty,
        entry_time: now_ms(),
        ..Default::default()
    };

    // 10. 通知 Go 端
    if let Some(cb) = &self.on_execution {
        cb(record.clone());
    }

    Ok(record)
}
```

### 3.4 TPSLManager — 阶梯止盈核心

```rust
// engine/src/executor/tpsl.rs

/// TPSLManager 监控价格变化，触发止盈止损
pub struct TPSLManager {
    /// 活跃的 TP/SL 配置
    active_configs: HashMap<String, TPConfig>,

    /// 已触发的 TP记录 (position_id -> 已触发TP列表)
    triggered_tps: HashMap<String, Vec<String>>, // TP1, TP2...

    /// 移动止损激活状态
    move_sl_active: HashMap<String, bool>,
}

impl TPSLManager {
    /// 价格更新时被调用 (from Go 通过 FFI 每秒调用)
    pub fn on_price_update(&mut self, symbol: &str, price: f64) -> Vec<TPAction> {
        let mut actions = vec![];

        for (pos_id, config) in &self.active_configs {
            if config.symbol != symbol {
                continue;
            }

            // 检查止盈
            for tp in &config.tp_levels {
                if self.is_tp_triggered(pos_id, &tp.label) {
                    continue; // 已触发
                }

                let triggered = match config.side.as_str() {
                    "BUY" | "LONG" => price >= tp.price,
                    "SELL" | "SHORT" => price <= tp.price,
                    _ => false,
                };

                if triggered {
                    let qty = config.quantity * tp.pct;
                    actions.push(TPAction::TakeProfit {
                        position_id: pos_id.clone(),
                        level: tp.label.clone(),
                        price: tp.price,
                        quantity: qty,
                    });
                    self.mark_tp_triggered(pos_id, &tp.label);

                    // 移动止损: TP1 触发后把SL移到入场价
                    if tp.label == "TP1" && config.move_sl_after > 0.0 {
                        if price >= config.move_sl_after {
                            self.move_sl_active.insert(pos_id.clone(), true);
                            actions.push(TPAction::MoveStopLoss {
                                position_id: pos_id.clone(),
                                new_sl_price: config.move_sl_to,
                            });
                        }
                    }
                }
            }

            // 检查止损
            let current_sl = self.get_current_sl(pos_id, config);
            let sl_triggered = match config.side.as_str() {
                "BUY" | "LONG" => price <= current_sl,
                "SELL" | "SHORT" => price >= current_sl,
                _ => false,
            };

            if sl_triggered {
                actions.push(TPAction::StopLoss {
                    position_id: pos_id.clone(),
                    price: current_sl,
                    quantity: self.remaining_qty(pos_id, config),
                });
            }

            // 追踪止盈
            if config.trailing_tp_pct > 0.0 {
                if let Some(action) = self.check_trailing_tp(pos_id, config, price) {
                    actions.push(action);
                }
            }

            // 追踪止损
            if config.trailing_sl_pct > 0.0 {
                if let Some(action) = self.check_trailing_sl(pos_id, config, price) {
                    actions.push(action);
                }
            }
        }

        actions
    }
}
```

### 3.5 新增 FFI 函数

```rust
// engine/src/ffi.rs 增加

/// 执行信号
/// Input: JSON Signal
/// Output: JSON ExecutionRecord
#[no_mangle]
pub extern "C" fn engine_execute_signal(json_ptr: *const c_char) -> *mut c_char {
    let json = unsafe { from_c_str(json_ptr) };
    let signal: Signal = match serde_json::from_str(&json) {
        Ok(s) => s,
        Err(e) => return to_c_string(format!("{{\"error\":\"{}\"}}", e)),
    };

    let result = lock_engines!().get_mut("executor")
        .and_then(|eng| eng.execute_signal(signal))
        .map(|record| serde_json::to_string(&record).unwrap())
        .unwrap_or_else(|| "{{\"error\":\"executor not found\"}}".into());

    to_c_string(result)
}

/// 价格更新 — 触发 TP/SL 检查
/// Input: {"symbol":"BTCUSDT","price":68000}
/// Output: [{"action":"take_profit","position_id":"...","level":"TP1","price":...,"quantity":...}, ...]
#[no_mangle]
pub extern "C" fn engine_update_price(json_ptr: *const c_char) -> *mut c_char {
    let json = unsafe { from_c_str(json_ptr) };
    let parsed: serde_json::Value = serde_json::from_str(&json).unwrap_or_default();
    let symbol = parsed["symbol"].as_str().unwrap_or("");
    let price = parsed["price"].as_f64().unwrap_or(0.0);

    // TODO: 实现 TPSLManager 全局访问
    let actions: Vec<TPAction> = vec![];

    to_c_string(serde_json::to_string(&actions).unwrap())
}

/// 设置执行回调 (从 Go 注册回调函数指针)
#[no_mangle]
pub extern "C" fn engine_set_execution_callback(cb: extern "C" fn(*const c_char)) {
    // 存储回调指针，执行时通过此回调通知 Go
}
```

---

## 四Go 层: Bot Manager

### 4.1 模块位置

```
gateway/internal/
  ├─ bot/                    (新建目录)
  │   ├─ manager.go         — BotManager: 生命周期管理
  │   ├─ strategy_bot.go    — Freqtrade 策略机器人
  │   ├─ signal_bot.go      — Cryptoleks 信号机器人
  │   ├─ ai_bot.go          — AI Alpha 决策机器人
  │   ├─ router.go          — 信号路由器
  │   └─ store.go           — Bot 配置存储
  └─ handler/bot.go         — HTTP Handler (REST API)
```

### 4.2 BotManager 结构

```go
// gateway/internal/bot/manager.go

package bot

import (
    "sync"
    "github.com/xiaotian-quant/gateway/internal/model"
    "github.com/xiaotian-quant/gateway/internal/event"
)

// BotManager 管理三种机器人的生命周期
type BotManager struct {
    // 信号路由器
    router *SignalRouter

    // 信号队列 (缓冲和排序)
    queue *SignalQueue

    // 三种机器人实例
    strategyBots map[string]*StrategyBot  // Freqtrade 策略
    signalBots   map[string]*SignalBot    // Cryptoleks 信号
    aiBots       map[string]*AIBot        // AI Alpha

    // 事件总线
    bus *event.EventBus

    // 执行回调 (传递给 Rust SignalExecutor)
    onSignalExecute func(signal model.Signal) error

    mu sync.RWMutex
}

// SignalRouter 将信号分发到对应的 Bot
type SignalRouter struct {
    // 按 symbol 路由
    symbolRoutes map[string][]string // symbol -> []bot_id

    // 按 bot_type 过滤
    typeFilters map[model.BotType]bool
}
```

### 4.3 Freqtrade 策略机器人

```go
// gateway/internal/bot/strategy_bot.go

// StrategyBot 实现 Freqtrade 式自主策略扫描
// 基于现有 strategy.Engine 扩展
type StrategyBot struct {
    config    model.BotConfig
    engine    *strategy.Engine  // 复用现有策略引擎
    barGen    *BarGenerator      // K线构建器
    indicators []IndicatorFunc   // 指标计算函数

    // 输出: 生成的信号传递给 SignalExecutor
    onSignal func(model.Signal)
}

// Start 启动策略监听
func (b *StrategyBot) Start() error {
    // 1. 注册策略到 engine
    s := strategies.Get(b.config.StrategyName)
    s.SetSymbol(b.config.Symbol)

    // 2. 设置信号回调 — 当策略产生信号时，转换为统一 Signal 格式
    b.engine.OnSignal = func(sig model.Signal) {
        // 增强: 将策略信号转换为带 TP/SL 的完整信号
        unifiedSignal := b.enhanceSignal(sig)
        b.onSignal(unifiedSignal)
    }

    b.engine.Register(s)
    return b.engine.Start(s.Name(), nil)
}

// enhanceSignal 将简单策略信号转换为带完整 TP/SL 的信号
func (b *StrategyBot) enhanceSignal(sig model.Signal) model.Signal {
    // 从策略参数中读取 TP/SL 配置
    params := b.config.StrategyParams

    sig.Type = model.SignalTypeStrategy
    sig.BotID = b.config.ID

    // 设置阶梯止盈
    if sig.TP1 <= 0 {
        sig.TP1 = sig.EntryPrice * 1.02  // 默认 2%
        sig.TP1Pct = 0.4
    }
    if sig.TP2 <= 0 {
        sig.TP2 = sig.EntryPrice * 1.04  // 默认 4%
        sig.TP2Pct = 0.3
    }
    if sig.TP3 <= 0 {
        sig.TP3 = sig.EntryPrice * 1.08  // 默认 8%
        sig.TP3Pct = 0.3
    }

    // 设置止损
    if sig.StopLoss <= 0 {
        sig.StopLoss = sig.EntryPrice * 0.97  // 默认 -3%
    }

    // 移动止损: TP1 触发后移至入场价
    sig.MoveSLAfter = sig.TP1
    sig.MoveSLTo = sig.EntryPrice

    return sig
}
```

### 4.4 Cryptoleks 信号机器人

```go
// gateway/internal/bot/signal_bot.go

// SignalBot 实现 Cryptoleks 式信号跟单
// 不主动生成信号，只监听外部信号并执行
type SignalBot struct {
    config      model.BotConfig
    webhookSrv  *WebhookServer  // Webhook 接收服务
    signalQueue chan model.Signal

    // 信号来源
    sources []SignalSource

    // 输出
    onSignal func(model.Signal)
}

// SignalSource 信号来源接口
type SignalSource interface {
    Name() string
    Start() error
    Stop() error
    Signals() <-chan model.Signal
}

// Start 启动信号监听
func (b *SignalBot) Start() error {
    // 1. 启动 Webhook 服务
    if b.config.SignalSource == "webhook" {
        b.webhookSrv = NewWebhookServer(b.config.WebhookURL)
        b.webhookSrv.OnSignal = func(raw []byte) {
            signal := b.parseWebhookSignal(raw)
            b.onSignal(signal)
        }
        go b.webhookSrv.Start()
    }

    // 2. 启动 API 轮询 (如果配置了第三方 API)
    if b.config.SignalSource == "api" {
        go b.pollExternalAPI()
    }

    // 3. 监听内部信号 (来自 IndicatorIDE)
    if b.config.SignalSource == "internal" {
        b.bus.Subscribe("indicator_signal", event.PrioHigh, func(evt event.Event) {
            if sig, ok := evt.Data.(model.Signal); ok {
                b.onSignal(sig)
            }
        }, event.TypeSignal)
    }

    return nil
}

// parseWebhookSignal 解析外部信号 (Cryptoleks 格式)
func (b *SignalBot) parseWebhookSignal(raw []byte) model.Signal {
    // Cryptoleks 信号格式:
    // {
    //   "symbol": "BTCUSDT",
    //   "side": "BUY",
    //   "entry": 65000,
    //   "tp1": 68000, "tp2": 70000, "tp3": 72000,
    //   "sl": 58000,
    //   "risk_level": "T2"
    // }
    var incoming struct {
        Symbol     string  `json:"symbol"`
        Side       string  `json:"side"`
        Entry      float64 `json:"entry"`
        TP1        float64 `json:"tp1"`
        TP2        float64 `json:"tp2"`
        TP3        float64 `json:"tp3"`
        SL         float64 `json:"sl"`
        RiskLevel  string  `json:"risk_level"`
    }
    json.Unmarshal(raw, &incoming)

    return model.Signal{
        Type:      model.SignalTypeExternal,
        BotID:     b.config.ID,
        Symbol:    incoming.Symbol,
        Side:      incoming.Side,
        EntryPrice: incoming.Entry,
        TP1:       incoming.TP1,
        TP2:       incoming.TP2,
        TP3:       incoming.TP3,
        StopLoss:  incoming.SL,
        RiskLevel: model.SignalRiskLevel(incoming.RiskLevel),
        // 根据风险等级设置分仓比例
        TP1Pct:    b.getTP1PctForRisk(incoming.RiskLevel),
        TP2Pct:    b.getTP2PctForRisk(incoming.RiskLevel),
        TP3Pct:    b.getTP3PctForRisk(incoming.RiskLevel),
        Timestamp: time.Now().Unix(),
    }
}
```

### 4.5 AI Alpha 决策机器人

```go
// gateway/internal/bot/ai_bot.go

// AIBot 实现 AI Alpha 式智能决策
// 纯 AI 驱动，带市场条件过滤
type AIBot struct {
    config      model.BotConfig
    aiClient    *ai.Provider    // 复用现有 AI Provider
    marketFeed  *MarketFeed     // 市场数据接口
    scanner     *AIScanner      // 市场扫描器

    onSignal func(model.Signal)
}

// AIScanner 定期扫描市场，调用 AI 分析
type AIScanner struct {
    interval   time.Duration
    symbols    []string
    timeframes []string
    aiAnalyze  func(prompt string) (*AIAnalysisResult, error)
}

// AIAnalysisResult AI 分析结果
type AIAnalysisResult struct {
    Signal      string   `json:"signal"`       // "buy" / "sell" / "neutral"
    Confidence  float64  `json:"confidence"`   // 0-100
    TP1         float64  `json:"tp1"`
    TP2         float64  `json:"tp2"`
    TP3         float64  `json:"tp3"`
    StopLoss    float64  `json:"stop_loss"`
    Reason      string   `json:"reason"`
    Filters     []string `json:"filters"`      // 过滤条件
    MarketCondition string `json:"market_condition"` // "stable" / "volatile" / "trending"
}

// Start 启动 AI 定期扫描
func (b *AIBot) Start() error {
    b.scanner = &AIScanner{
        interval:   time.Duration(b.config.ScanInterval) * time.Second,
        symbols:    []string{b.config.Symbol},
        timeframes: b.config.Timeframes,
        aiAnalyze:  b.aiAnalyze,
    }

    go b.scanner.Run(func(result *AIAnalysisResult) {
        // 市场条件过滤 — 这是 AI Alpha 的核心卖点
        if b.config.MarketConditionFilter {
            if result.MarketCondition == "volatile" {
                // 市场波动大 → 跳过信号
                log.Printf("[AI Alpha] Signal skipped: market too volatile (%.1f%% confidence)",
                    result.Confidence)
                return
            }
        }

        // 置信度门限
        if result.Confidence < b.config.ConfidenceThreshold {
            return
        }

        // 生成统一信号
        signal := model.Signal{
            Type:        model.SignalTypeAI,
            BotID:       b.config.ID,
            Symbol:      b.config.Symbol,
            Side:        strings.ToUpper(result.Signal),
            EntryPrice:  0, // 市价单
            MarketOrder: true,
            TP1:         result.TP1,
            TP2:         result.TP2,
            TP3:         result.TP3,
            TP1Pct:      0.4,
            TP2Pct:      0.3,
            TP3Pct:      0.3,
            StopLoss:    result.StopLoss,
            Confidence:  result.Confidence,
            AIReason:    result.Reason,
            AIFilters:   result.Filters,
            Timestamp:   time.Now().Unix(),
        }

        b.onSignal(signal)
    })

    return nil
}

// aiAnalyze 调用 LLM 分析市场
type aiAnalyzePrompt struct {
    Symbol     string   `json:"symbol"`
    Timeframes []string `json:"timeframes"`
    Klines     []any    `json:"klines"`
    OrderBook  any      `json:"orderbook"`
}

func (b *AIBot) aiAnalyze(promptStr string) (*AIAnalysisResult, error) {
    provider := ai.GetProvider(b.config.AIModel)
    if provider == nil {
        return nil, fmt.Errorf("AI provider %s not found", b.config.AIModel)
    }

    // 构建分析 prompt
    prompt := fmt.Sprintf(`You are a professional crypto trading analyst.
Analyze %s based on the following market data and provide a trading signal.

Rules:
- Only generate signals when market condition is "stable" or "trending"
- Skip when market is "volatile" (this is your core value)
- Provide 3 TP levels and 1 SL level
- Confidence must be 60+ to generate a signal

Output JSON format:
{
  "signal": "buy|sell|neutral",
  "confidence": 0-100,
  "tp1": price,
  "tp2": price,
  "tp3": price,
  "stop_loss": price,
  "reason": "detailed analysis",
  "filters": ["filter1", "filter2"],
  "market_condition": "stable|volatile|trending"
}

Market Data: %s`, b.config.Symbol, promptStr)

    resp, err := provider.ChatCompletion(ai.CompletionRequest{
        Messages: []ai.Message{{Role: "user", Content: prompt}},
        Temperature: 0.3,
    })
    if err != nil {
        return nil, err
    }

    // 解析 AI 返回的 JSON
    var result AIAnalysisResult
    if err := json.Unmarshal([]byte(resp.Content), &result); err != nil {
        return nil, err
    }

    return &result, nil
}
```

---

## 五API 设计

### 5.1 Bot 管理 API

```
# 机器人 CRUD
POST   /api/bots                    # 创建机器人
GET    /api/bots?type=strategy|signal|ai  # 列表
GET    /api/bots/:id                # 详情
PUT    /api/bots/:id                # 更新
DELETE /api/bots/:id                # 删除

# 生命周期
POST   /api/bots/:id/start          # 启动
POST   /api/bots/:id/stop           # 停止
POST   /api/bots/:id/pause          # 暂停

# 信号
POST   /api/bots/:id/signal         # 手动发送信号 (信号机器人)
GET    /api/bots/:id/signals        # 历史信号
GET    /api/bots/:id/executions     # 执行记录

# Cryptoleks Webhook
POST   /webhook/signal/:bot_id      # 外部信号接收 (无需认证或简单验签)

# AI 状态
GET    /api/bots/:id/ai-status      # AI 当前状态，置信度，过滤统计
```

### 5.2 WebSocket 推送

```json
// bot.status 机器人状态变化
{
  "type": "bot.status",
  "bot_id": "bot_123",
  "status": "running",
  "type": "signal",
  "uptime": 3600,
  "stats": {
    "signals_received": 12,
    "signals_executed": 10,
    "win_count": 7,
    "lose_count": 3,
    "total_pnl": 1250.50
  }
}

// bot.signal 机器人产生的信号
{
  "type": "bot.signal",
  "bot_id": "bot_123",
  "bot_type": "ai",
  "signal": {
    "id": "sig_456",
    "symbol": "BTCUSDT",
    "side": "BUY",
    "entry_price": 65000,
    "tp1": 68000,
    "tp2": 70000,
    "tp3": 72000,
    "stop_loss": 58000,
    "confidence": 85,
    "ai_reason": "Strong bullish divergence..."
  }
}

// execution.update 执行状态更新
{
  "type": "execution.update",
  "execution_id": "exec_789",
  "signal_id": "sig_456",
  "status": "partial",
  "tp1_filled": true,
  "tp1_price": 68000,
  "tp1_qty": 0.04,
  "current_sl": 65000,
  "realized_pnl": 120.0,
  "timestamp": 1718870400
}
```

---

## 六前端: Bots.tsx 三标签页

```tsx
// web/src/pages/Bots.tsx 改造

import { Tabs, TabsList, TabsTrigger, TabsContent } from '@/components/ui/Tabs'

export function Bots() {
  return (
    <div className="h-full overflow-y-auto p-5">
      {/* KPI 行 — 不变 */}
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">...KPI...</div>

      {/* 三标签页 — 核心改造 */}
      <Tabs defaultValue="strategy" className="mt-6">
        <TabsList className="grid w-full grid-cols-3">
          <TabsTrigger value="strategy">
            <Bot className="h-4 w-4 mr-1" />
            策略机器人 (Freqtrade)
            <Badge variant="outline" className="ml-1">{strategyCount}</Badge>
          </TabsTrigger>
          <TabsTrigger value="signal">
            <Radio className="h-4 w-4 mr-1" />
            信号机器人 (Cryptoleks)
            <Badge variant="outline" className="ml-1">{signalCount}</Badge>
          </TabsTrigger>
          <TabsTrigger value="ai">
            <Brain className="h-4 w-4 mr-1" />
            AI 机器人 (Alpha)
            <Badge variant="outline" className="ml-1">{aiCount}</Badge>
          </TabsTrigger>
        </TabsList>

        {/* Freqtrade 策略机器人 */}
        <TabsContent value="strategy">
          <StrategyBotTab />
        </TabsContent>

        {/* Cryptoleks 信号机器人 */}
        <TabsContent value="signal">
          <SignalBotTab />
        </TabsContent>

        {/* AI Alpha 决策机器人 */}
        <TabsContent value="ai">
          <AIBotTab />
        </TabsContent>
      </Tabs>
    </div>
  )
}
```

### SignalBotTab 组件

```tsx
// web/src/components/bots/SignalBotTab.tsx

function SignalBotTab() {
  const { bots, createBot, startBot, stopBot } = useBotData('signal')
  const [showCreate, setShowCreate] = useState(false)

  return (
    <div className="space-y-4">
      {/* 创建按钮 */}
      <Button onClick={() => setShowCreate(true)}>
        <Plus className="h-4 w-4 mr-1" />
        新建信号机器人
      </Button>

      {/* 机器人卡片 */}
      {bots.map(bot => (
        <SignalBotCard key={bot.id} bot={bot} />
      ))}

      {/* 创建对话框 */}
      {showCreate && <SignalBotCreateModal onClose={() => setShowCreate(false)} />}
    </div>
  )
}

// 信号机器人卡片 — 显示 Webhook 地址、信号统计、执行状态
function SignalBotCard({ bot }: { bot: BotItem }) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle>{bot.name}</CardTitle>
            <CardDescription>
              来源: {bot.signal_source} | {bot.symbol}
            </CardDescription>
          </div>
          <StatusBadge status={bot.status} />
        </div>
      </CardHeader>
      <CardContent>
        {/* Webhook 地址 */}
        <div className="flex items-center gap-2 text-sm">
          <span className="text-muted-foreground">Webhook:</span>
          <code className="bg-muted px-2 py-1 rounded">{bot.webhook_url}</code>
          <CopyButton text={bot.webhook_url} />
        </div>

        {/* 执行统计 */}
        <div className="grid grid-cols-4 gap-2 mt-4">
          <Stat label="信号接收" value={bot.stats.signals_received} />
          <Stat label="成功执行" value={bot.stats.signals_executed} />
          <Stat label="过期忽略" value={bot.stats.signals_expired} />
          <Stat label="累计盈亏" value={`${bot.stats.total_pnl >= 0 ? '+' : ''}${bot.stats.total_pnl}`} 
               positive={bot.stats.total_pnl >= 0} />
        </div>

        {/* TP/SL 配置概览 */}
        <div className="mt-4 flex gap-1">
          <TPLabel price={bot.default_signal?.tp1} label="TP1" pct={bot.default_signal?.tp1_pct} />
          <TPLabel price={bot.default_signal?.tp2} label="TP2" pct={bot.default_signal?.tp2_pct} />
          <TPLabel price={bot.default_signal?.tp3} label="TP3" pct={bot.default_signal?.tp3_pct} />
          <SLLabel price={bot.default_signal?.stop_loss} />
        </div>

        {/* 操作按钮 */}
        <div className="flex gap-2 mt-4">
          {bot.status === 'stopped' ? (
            <Button size="sm" onClick={() => startBot(bot.id)}>启动</Button>
          ) : (
            <Button size="sm" variant="destructive" onClick={() => stopBot(bot.id)}>停止</Button>
          )}
          <Button size="sm" variant="outline">设置</Button>
        </div>
      </CardContent>
    </Card>
  )
}
```

### AIBotTab 组件

```tsx
// web/src/components/bots/AIBotTab.tsx

function AIBotTab() {
  const { bots } = useBotData('ai')

  return (
    <div className="space-y-4">
      {bots.map(bot => (
        <AIBotCard key={bot.id} bot={bot} />
      ))}
    </div>
  )
}

function AIBotCard({ bot }: { bot: BotItem }) {
  return (
    <Card>
      <CardHeader>
        <div className="flex items-center justify-between">
          <div>
            <CardTitle className="flex items-center gap-2">
              <Brain className="h-5 w-5 text-purple-500" />
              {bot.name}
            </CardTitle>
            <CardDescription>
              模型: {bot.ai_model} | 置信度门限: {bot.confidence_threshold}%
            </CardDescription>
          </div>
          <StatusBadge status={bot.status} />
        </div>
      </CardHeader>
      <CardContent>
        {/* AI 状态仪表盘 */}
        <div className="grid grid-cols-3 gap-4">
          {/* 置信度仪表 */}
          <Gauge
            value={bot.ai_status?.avg_confidence || 0}
            max={100}
            label="平均置信度"
            color={bot.ai_status?.avg_confidence > 70 ? 'green' : 'yellow'}
          />
          {/* 过滤率 */}
          <Gauge
            value={bot.ai_status?.filter_rate || 0}
            max={100}
            label="信号过滤率"
            description="波动市场下跳过的信号占比"
          />
          {/* 胜率 */}
          <Gauge
            value={bot.ai_status?.win_rate || 0}
            max={100}
            label="历史胜率"
            color={bot.ai_status?.win_rate > 50 ? 'green' : 'red'}
          />
        </div>

        {/* 最近信号 */}
        {bot.last_signal && (
          <div className="mt-4 p-3 rounded-lg border bg-muted/50">
            <div className="flex items-center justify-between">
              <span className="font-medium">最近信号</span>
              <ConfidenceBadge value={bot.last_signal.confidence} />
            </div>
            <p className="text-sm text-muted-foreground mt-1">
              {bot.last_signal.ai_reason}
            </p>
            <div className="flex gap-2 mt-2">
              {bot.last_signal.ai_filters?.map(f => (
                <Badge key={f} variant="secondary" size="sm">{f}</Badge>
              ))}
            </div>
          </div>
        )}

        {/* 市场条件 */}
        <div className="mt-4">
          <span className="text-sm text-muted-foreground">当前市场条件: </span>
          <MarketConditionBadge condition={bot.ai_status?.market_condition} />
        </div>
      </CardContent>
    </Card>
  )
}
```

---

## 七实现路线图

```
Week 1: 核心基座
  Day 1-2:  实现 Rust SignalExecutor (入场下单 + TP/SL 挂单)
  Day 3-4:  实现 Rust TPSLManager (阶梯止盈 + 移动止损 + 追踪)
  Day 5:    FFI 接口 + Go 桥接层

Week 2: Go Bot Manager + 策略机器人
  Day 1-2:  实现 Go BotManager (生命周期 + 路由)
  Day 3-4:  实现 Freqtrade StrategyBot (复用现有 engine)
  Day 5:    REST API + WebSocket 推送

Week 3: 信号机器人 + AI 机器人
  Day 1-2:  实现 SignalBot (Webhook + API + 内部信号)
  Day 3-4:  实现 AIBot (扫描 + LLM + 市场过滤)
  Day 5:    前端三标签页 + 创建对话框

Week 4: 调试 + 优化
  Day 1-2:  整合测试 (三种机器人并行运行)
  Day 3-4:  Paper trading 验证
  Day 5:    文档 + 部署
```

---

## 八关键设计决策

| 决策 | 选择 | 理由 |
|------|------|------|
| TP/SL 实现层 | Rust SignalExecutor | 延迟低、无 GC 暂停，适合比价格检查 |
| 机器人生命周期 | Go BotManager | 利用 Go 的并发原语，与现有架构一致 |
| AI 推理 | Go 调用 LLM API | 不引入 Python 服务，保持三栈架构 |
| 信号格式 | 统一 Signal struct | 三种机器人甬同一格式，SignalExecutor 不区分来源 |
| 过滤机制 | AI Alpha 市场条件检查 | 核心卖点: 波动市场不交易 |

---

*这是完整的技术方案，接下来可以逐模块实现。需要先实现哪个部分？*
