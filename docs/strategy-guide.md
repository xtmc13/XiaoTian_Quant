# 策略开发指南

## 策略接口

所有策略必须实现 `Strategy` 接口：

```go
type Strategy interface {
    Name() string
    Symbol() string
    Params() map[string]any
    
    Start(params map[string]any) error
    Stop() error
    IsRunning() bool
    
    OnTick(tick model.Tick, bus *event.EventBus) (*model.Signal, error)
    OnOrderBook(ob model.OrderBookData, bus *event.EventBus) (*model.Signal, error)
    OnBar(bar model.Bar, bus *event.EventBus) (*model.Signal, error)
    OnOrderUpdate(order model.OrderData, bus *event.EventBus) (*model.Signal, error)
}
```

## 内置策略

### 1. 突破策略 (Breakout)

```go
// 参数
lookback:        20      // 回溯周期
buffer_pct:      0.002   // 突破缓冲 (0.2%)
stop_loss_pct:   0.02    // 止损 (2%)
take_profit_pct: 0.04    // 止盈 (4%)
position_size:   500     // 仓位 (USDT)

// 逻辑: 价格突破 N 期最高价 → 做多, 跌破最低价 → 做空
```

### 2. 网格策略 (Grid)

```go
// 参数
grid_levels: 10     // 网格层数
grid_spacing: 0.01  // 层间距 (1%)
upper_price: 70000  // 上限
lower_price: 60000  // 下限
```

## 策略参数系统

使用类型安全的参数声明：

```go
type MyStrategy struct {
    BaseStrategy
    params *ParamRegistry
}

func NewMyStrategy() *MyStrategy {
    s := &MyStrategy{
        params: NewParamRegistry(),
    }
    s.params.Register(IntParameter("fast_period", 12, 5, 50, "buy"))
    s.params.Register(FloatParameter("stoploss", 0.02, 0.01, 0.10, 0.01, "stoploss"))
    s.params.Register(BoolParameter("use_trailing", true, "trailing"))
    s.params.Register(CategoricalParameter("mode", "normal",
        []string{"aggressive", "normal", "conservative"}, "buy"))
    return s
}

func (s *MyStrategy) Start(params map[string]any) error {
    return s.params.SetAll(params)
}
```

## 增强回调

`BaseStrategy` 提供可覆盖的回调：

| 回调 | 说明 | 返回值 |
|------|------|--------|
| `CustomStoploss(position, price)` | 自定义止损价 | 0 = 使用默认 |
| `CustomStakeAmount(balance, signal)` | 自定义仓位大小 | 0 = 使用默认 |
| `ConfirmTradeEntry(signal)` | 入场确认 | false = 跳过 |
| `ConfirmTradeExit(position)` | 出场确认 | false = 跳过 |
| `AdjustEntryPrice(signal, ob)` | 入场价微调 | 0 = 不调整 |

### 示例：自定义止损

```go
func (s *MyStrategy) CustomStoploss(pos *Position, price float64) float64 {
    // ATR 动态止损
    atr := s.calcATR(14)
    if pos.Side == "LONG" {
        return price - atr * 2.0
    }
    return price + atr * 2.0
}
```

## 多时间框架

声明额外需要的数据：

```go
func (s *MyStrategy) InformativePairs() []InformativePair {
    return []InformativePair{
        {Symbol: "BTCUSDT", Timeframe: "1d"},    // 日线辅助
        {Symbol: "ETHUSDT", Timeframe: "1h", Asset: "ETH"}, // 关联币种
    }
}
```

## 回测与优化

### 运行回测

```bash
curl -X POST /api/backtest/run \
  -H "Content-Type: application/json" \
  -d '{"symbol":"BTCUSDT","interval":"1h","strategy_type":"sma_cross",
       "from":"2024-01-01","to":"2024-12-31","initial_balance":{"USDT":10000}}'
```

### 超参优化

```go
// 定义搜索空间
spaces := []ParamSpace{
    {Name: "fast_period", Type: ParamInt, Min: 5, Max: 50, Step: 5},
    {Name: "stoploss", Type: ParamFloat, Min: 0.01, Max: 0.10, Step: 0.01},
}

// 选择损失函数
optimizer := NewGridOptimizer(OptimizerConfig{
    LossFunc: LossSharpe,
}, spaces, evaluator)

results, _ := optimizer.Run()
best := optimizer.Best()
```

## 信号发布

```go
signal := &model.Signal{
    Symbol:    "BTCUSDT",
    Direction: "LONG",     // LONG / SHORT / CLOSE
    Strength:  0.85,       // 0.0 - 1.0
    Strategy:  s.Name(),
    Reason:    "sma golden cross",
}
PublishSignal(bus, *signal)
```
