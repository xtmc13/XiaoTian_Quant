# XiaoTianQuant Matching Engine

高性能 Rust 撮合引擎——价格-时间优先的订单簿匹配核心。

## 技术栈

- **语言**: Rust edition 2021
- **序列化**: serde + serde_json
- **构建**: cdylib + rlib，通过 C FFI 暴露给 Go (CGo)
- **优化**: LTO + strip，release profile 优化

## 架构

```
┌─────────────────────────────────────────────┐
│               Go Gateway (CGo)               │
│  ┌──────────────────────────────────────┐   │
│  │     MatchingEngine (cgo_bridge.go)   │   │
│  │  engine_create / engine_submit_order │   │
│  │  engine_cancel_order / engine_snapshot│  │
│  │  engine_get_trades / engine_destroy  │   │
│  └──────────────┬───────────────────────┘   │
│                 │  C FFI                     │
└─────────────────┼───────────────────────────┘
                  │
┌─────────────────▼───────────────────────────┐
│           Rust Engine (libxt_matching)       │
│                                              │
│  ┌──────────┐  ┌───────────┐  ┌──────────┐ │
│  │ OrderBook │→│ Matching  │→│   FFI    │ │
│  │ (BTreeMap)│  │  Engine   │  │ (ffi.rs) │ │
│  └──────────┘  └───────────┘  └──────────┘ │
│                                              │
│  - 价格-时间优先撮合                          │
│  - O(1) 订单 ID 查找 (HashMap 索引)          │
│  - 完整状态机 (New → Filled / Cancelled)     │
│  - 精度: 8 位小数 (OrderedFloat)             │
└─────────────────────────────────────────────┘
```

## 核心类型

| 类型 | 文件 | 说明 |
|------|------|------|
| `OrderBook` | `orderbook.rs` | 双向订单簿，BTreeMap 按价格排序，HashMap 按 ID 索引 |
| `MatchingEngine` | `matching.rs` | 撮合引擎，Limit/Market 订单，状态机 |
| FFI Exports | `ffi.rs` | C ABI 导出 (engine_create/submit/cancel/snapshot/trades) |

## 构建

```bash
# Debug
cargo build

# Release (LTO + strip, 用于生产)
cargo build --release

# 跨平台编译
cargo build --release --target x86_64-unknown-linux-gnu
cargo build --release --target x86_64-apple-darwin
cargo build --release --target aarch64-apple-darwin
cargo build --release --target x86_64-pc-windows-msvc

# 基准测试
cargo bench
```

输出产物: `target/release/libxt_matching.so` (Linux) / `.dylib` (macOS) / `.dll` (Windows)

## 接口 (Go CGo 调用)

```go
// 创建引擎
char* engine_create(const char* symbol);

// 提交订单 (JSON)
char* engine_submit_order(const char* json);
// JSON: {"symbol":"BTC/USDT","side":"buy","order_type":"limit","price":50000,"quantity":0.1,"user_id":1}

// 撤销订单
char* engine_cancel_order(const char* symbol, unsigned long long order_id);

// 快照
char* engine_snapshot(const char* symbol, unsigned int depth);

// 交易记录
unsigned long long engine_trade_count(const char* symbol);
char* engine_get_trades(const char* symbol, unsigned int limit);

// 清理
void engine_destroy(const char* symbol);
```

## 性能

基准测试目标: > 10,000 TPS (单 symbol, 单线程)

## 已知限制

- 仅支持单 symbol 引擎实例 (全局 registry 但并发锁粒度粗)
- 不支持 IOC/FOK/GTC 等 Time-In-Force 修饰符
- 不支持 Post-Only / Reduce-Only 订单修饰
- 内存引擎，重启后数据丢失
