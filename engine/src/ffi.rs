/// C ABI FFI for Go↔Rust interop.
///
/// Go calls these functions via cgo.
///
/// Memory management: Rust allocates, Go frees via `free_*` functions.
use std::ffi::{CStr, CString};
use std::os::raw::c_char;

use crate::matching::{BalanceLimits, MatchingEngine};
use crate::orderbook::{Order, OrderType, Side};

// ── Global engine registry ──
use std::collections::HashMap;
use std::sync::{Arc, Mutex, OnceLock};

type SharedEngine = Arc<Mutex<MatchingEngine>>;

static ENGINES: OnceLock<Mutex<HashMap<String, SharedEngine>>> = OnceLock::new();

fn get_engines() -> &'static Mutex<HashMap<String, SharedEngine>> {
    ENGINES.get_or_init(|| Mutex::new(HashMap::new()))
}

fn lock_engine(engine: &SharedEngine) -> std::sync::MutexGuard<'_, MatchingEngine> {
    engine
        .lock()
        .unwrap_or_else(|poisoned| poisoned.into_inner())
}

fn to_c_string(s: String) -> *mut c_char {
    CString::new(s).unwrap_or_default().into_raw()
}

fn json_error(message: impl AsRef<str>) -> String {
    serde_json::json!({ "error": message.as_ref() }).to_string()
}

fn optional_balance(value: Option<&serde_json::Value>, field: &str) -> Result<Option<f64>, String> {
    match value {
        None | Some(serde_json::Value::Null) => Ok(None),
        Some(value) => match value.as_f64() {
            Some(amount) if amount.is_finite() && amount >= 0.0 => Ok(Some(amount)),
            _ => Err(format!(
                "{field} must be a finite non-negative number or null"
            )),
        },
    }
}

fn status_name(status: crate::orderbook::OrderStatus) -> &'static str {
    match status {
        crate::orderbook::OrderStatus::New => "NEW",
        crate::orderbook::OrderStatus::PartiallyFilled => "PARTIALLY_FILLED",
        crate::orderbook::OrderStatus::Filled => "FILLED",
        crate::orderbook::OrderStatus::Cancelled => "CANCELLED",
        crate::orderbook::OrderStatus::Rejected => "REJECTED",
    }
}

unsafe fn from_c_str(ptr: *const c_char) -> String {
    if ptr.is_null() {
        return String::new();
    }
    CStr::from_ptr(ptr).to_string_lossy().into_owned()
}

// ═══════════════════════════════════════════ Engine Lifecycle ═══════════════════════════════════════

/// Helper to get a locked reference to the engines map, or return an error string.
macro_rules! lock_engines {
    () => {
        match get_engines().lock() {
            Ok(guard) => guard,
            Err(poisoned) => {
                let guard = poisoned.into_inner();
                guard
            }
        }
    };
}

fn get_engine(symbol: &str) -> Option<SharedEngine> {
    lock_engines!().get(symbol).cloned()
}

/// Create a new matching engine for a symbol. Returns the symbol as confirmation.
///
/// # Safety
/// `symbol` must be null or point to a readable, null-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn engine_create(symbol: *const c_char) -> *mut c_char {
    let sym = unsafe { from_c_str(symbol) };
    if sym.trim().is_empty() {
        return to_c_string(json_error("missing or empty symbol"));
    }
    let engine = Arc::new(Mutex::new(MatchingEngine::new(sym.clone())));
    lock_engines!().insert(sym.clone(), engine);
    to_c_string(serde_json::json!({ "status": "ok", "symbol": sym }).to_string())
}

/// Delete a matching engine.
///
/// # Safety
/// `symbol` must be null or point to a readable, null-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn engine_destroy(symbol: *const c_char) {
    let sym = unsafe { from_c_str(symbol) };
    lock_engines!().remove(&sym);
}

// ═══════════════════════════════════════════ Order Submission ═══════════════════════════════════════

/// Submit a limit order. JSON input, JSON output.
/// Input: {"symbol":"BTCUSDT","side":"buy","price":68000,"quantity":0.1,"user_id":1}
/// Output includes order status, cumulative fill, trades, and per-order fill updates.
///
/// # Safety
/// `json_ptr` must be null or point to a readable, null-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn engine_submit_order(json_ptr: *const c_char) -> *mut c_char {
    let json = unsafe { from_c_str(json_ptr) };

    let parsed: serde_json::Value = match serde_json::from_str(&json) {
        Ok(v) => v,
        Err(e) => return to_c_string(json_error(format!("JSON parse: {e}"))),
    };

    let symbol = match parsed.get("symbol").and_then(serde_json::Value::as_str) {
        Some(symbol) if !symbol.trim().is_empty() => symbol,
        _ => return to_c_string(json_error("missing or empty symbol")),
    };
    let side_str = match parsed.get("side").and_then(serde_json::Value::as_str) {
        Some(side) => side,
        None => return to_c_string(json_error("missing side")),
    };
    let price = parsed["price"].as_f64().unwrap_or(0.0);
    let quantity = parsed["quantity"].as_f64().unwrap_or(0.0);
    if !quantity.is_finite() || quantity <= 0.0 {
        return to_c_string(json_error("quantity must be finite and greater than zero"));
    }
    let user_id = match parsed.get("user_id") {
        None => 0,
        Some(value) => match value.as_u64() {
            Some(user_id) => user_id,
            None => return to_c_string(json_error("user_id must be a non-negative integer")),
        },
    };
    let order_type_str = parsed["order_type"].as_str().unwrap_or("limit");

    let side = match side_str.to_lowercase().as_str() {
        "buy" => Side::Buy,
        "sell" => Side::Sell,
        _ => return to_c_string(json_error("side must be buy or sell")),
    };
    let order_type = match order_type_str.to_lowercase().as_str() {
        "market" => OrderType::Market,
        "limit" => OrderType::Limit,
        _ => return to_c_string(json_error("order_type must be limit or market")),
    };
    if !price.is_finite()
        || (order_type == OrderType::Limit && price <= 0.0)
        || (order_type == OrderType::Market && price < 0.0)
    {
        return to_c_string(json_error(
            "price must be finite and positive for limit orders, and non-negative for market orders",
        ));
    };

    let limits = match (
        optional_balance(parsed.get("available_base"), "available_base"),
        optional_balance(parsed.get("available_quote"), "available_quote"),
    ) {
        (Ok(base), Ok(quote)) => BalanceLimits { base, quote },
        (Err(err), _) | (_, Err(err)) => return to_c_string(json_error(err)),
    };
    let order = Order::new(0, price, quantity, side, order_type, user_id);

    let result = match get_engine(symbol) {
        Some(engine) => {
            let result = lock_engine(&engine).submit_order_with_limits(order, limits);
            match result {
                Ok(result) => serde_json::json!({
                    "status": status_name(result.status),
                    "order_id": result.order_id,
                    "side": side_str.to_lowercase(),
                    "order_type": order_type_str.to_lowercase(),
                    "price": price,
                    "quantity": quantity,
                    "filled": result.filled,
                    "trades": result.trades.iter().map(|t| {
                        serde_json::json!({
                            "id": t.id,
                            "price": t.price,
                            "quantity": t.quantity,
                            "buy_order_id": t.buy_order_id,
                            "sell_order_id": t.sell_order_id,
                        })
                    }).collect::<Vec<_>>(),
                    "fills": result.fills.iter().map(|fill| {
                        serde_json::json!({
                            "order_id": fill.order_id,
                            "filled_qty": fill.filled_qty,
                            "avg_price": fill.avg_price,
                        })
                    }).collect::<Vec<_>>(),
                })
                .to_string(),
                Err(err) => json_error(err),
            }
        }
        None => json_error(format!("engine not found for {symbol}")),
    };

    to_c_string(result)
}

// ═══════════════════════════════════════════ Cancel Order ═══════════════════════════════════════

/// # Safety
/// `symbol` must be null or point to a readable, null-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn engine_cancel_order(symbol: *const c_char, order_id: u64) -> *mut c_char {
    let sym = unsafe { from_c_str(symbol) };
    let result = match get_engine(&sym) {
        Some(engine) => match lock_engine(&engine).cancel_order(order_id) {
            Some(_) => serde_json::json!({ "status": "ok", "order_id": order_id }).to_string(),
            None => json_error(format!("order {order_id} not found")),
        },
        None => json_error(format!("engine not found for {sym}")),
    };
    to_c_string(result)
}

// ═══════════════════════════════════════════ Snapshot / Query ═══════════════════════════════════════

/// # Safety
/// `symbol` must be null or point to a readable, null-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn engine_snapshot(symbol: *const c_char, depth: u32) -> *mut c_char {
    let sym = unsafe { from_c_str(symbol) };
    let result = match get_engine(&sym) {
        Some(engine) => {
            let engine = lock_engine(&engine);
            let (bids, asks) = engine.snapshot(depth as usize);
            serde_json::json!({
                "symbol": sym,
                "best_bid": engine.book.best_bid(),
                "best_ask": engine.book.best_ask(),
                "spread": engine.book.spread(),
                "bids": bids.iter().map(|(p, q)| serde_json::json!([p, q])).collect::<Vec<_>>(),
                "asks": asks.iter().map(|(p, q)| serde_json::json!([p, q])).collect::<Vec<_>>(),
            })
            .to_string()
        }
        None => json_error(format!("engine not found for {sym}")),
    };
    to_c_string(result)
}

/// # Safety
/// `symbol` must be null or point to a readable, null-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn engine_trade_count(symbol: *const c_char) -> u64 {
    let sym = unsafe { from_c_str(symbol) };
    match get_engine(&sym) {
        Some(engine) => lock_engine(&engine).book.trade_count,
        None => 0,
    }
}

/// Get trades as JSON array.
///
/// # Safety
/// `symbol` must be null or point to a readable, null-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn engine_get_trades(symbol: *const c_char, limit: u32) -> *mut c_char {
    let sym = unsafe { from_c_str(symbol) };
    let result = match get_engine(&sym) {
        Some(engine) => {
            let engine = lock_engine(&engine);
            let trades = engine.get_trades(limit as usize);
            serde_json::json!(trades
                .iter()
                .map(|t| {
                    serde_json::json!({
                        "id": t.id,
                        "price": t.price,
                        "quantity": t.quantity,
                        "buy_order_id": t.buy_order_id,
                        "sell_order_id": t.sell_order_id,
                    })
                })
                .collect::<Vec<_>>())
            .to_string()
        }
        None => "[]".to_string(),
    };
    to_c_string(result)
}

// ═══════════════════════════════════════════ Memory Management ═══════════════════════════════════════

/// Free a string allocated by Rust. Must be called from Go for every non-null return value.
///
/// # Safety
/// `ptr` must be null or a string pointer returned by this library that has not been freed.
#[no_mangle]
pub unsafe extern "C" fn free_string(ptr: *mut c_char) {
    if ptr.is_null() {
        return;
    }
    unsafe {
        let _ = CString::from_raw(ptr);
    }
}

// ═══════════════════════════════════════════ Signal Executor FFI ═══════════════════════════════════════

use crate::executor::signal::Signal;
use crate::executor::{actions_to_json, SignalExecutor};

static SIGNAL_EXECUTOR: OnceLock<Mutex<SignalExecutor>> = OnceLock::new();

fn get_signal_executor() -> &'static Mutex<SignalExecutor> {
    SIGNAL_EXECUTOR.get_or_init(|| Mutex::new(SignalExecutor::new()))
}

/// Execute a trading signal.
///
/// # Input JSON format
/// ```json
/// {
///   "signal": {
///     "id": "sig-001",
///     "bot_id": "bot-001",
///     "bot_type": "strategy",
///     "symbol": "BTCUSDT",
///     "side": "BUY",
///     "direction": "LONG",
///     "entry_price": 70000.0,
///     "market_order": false,
///     "slippage_pct": 0.001,
///     "tp1": 75000.0, "tp2": 80000.0, "tp3": 85000.0,
///     "tp1_pct": 0.4, "tp2_pct": 0.3, "tp3_pct": 0.3,
///     "stop_loss": 65000.0,
///     "move_sl_after": 75000.0,
///     "move_sl_to": 70000.0,
///     "trailing_tp": 0.0,
///     "trailing_sl": 0.0,
///     "leverage": 10.0,
///     "max_stake_pct": 0.1,
///     "confidence": 0.85,
///     "ai_reason": "breakout detected",
///     "timestamp": 1700000000000,
///     "expire_at": 1700003600000
///   },
///   "available_balance": 10000.0
/// }
/// ```
///
/// # Returns
/// JSON string of `ExecutionRecord` or error message.
///
/// # Safety
/// `json_ptr` must be null or point to a readable, null-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn engine_execute_signal(json_ptr: *const c_char) -> *mut c_char {
    let json = unsafe { from_c_str(json_ptr) };

    let parsed: serde_json::Value = match serde_json::from_str(&json) {
        Ok(v) => v,
        Err(e) => return to_c_string(format!("{{\"error\":\"JSON parse: {}\"}}", e)),
    };

    // Extract signal
    let signal: Signal = match serde_json::from_value(parsed["signal"].clone()) {
        Ok(s) => s,
        Err(e) => return to_c_string(format!("{{\"error\":\"Signal parse: {}\"}}", e)),
    };

    // Extract available_balance
    let available_balance = parsed["available_balance"].as_f64().unwrap_or(0.0);

    let mut executor = match get_signal_executor().lock() {
        Ok(g) => g,
        Err(poisoned) => poisoned.into_inner(),
    };

    match executor.execute_signal(signal, available_balance) {
        Ok(record) => match serde_json::to_string(&record) {
            Ok(json) => to_c_string(json),
            Err(e) => to_c_string(format!("{{\"error\":\"serialize: {}\"}}", e)),
        },
        Err(e) => to_c_string(format!("{{\"error\":\"{}\"}}", e)),
    }
}

/// Update price and check TP/SL for a symbol.
///
/// # Input JSON format
/// ```json
/// {
///   "symbol": "BTCUSDT",
///   "price": 76000.0
/// }
/// ```
///
/// # Returns
/// JSON array of `TPAction` objects.
///
/// # Safety
/// `json_ptr` must be null or point to a readable, null-terminated C string.
#[no_mangle]
pub unsafe extern "C" fn engine_update_price(json_ptr: *const c_char) -> *mut c_char {
    let json = unsafe { from_c_str(json_ptr) };

    let parsed: serde_json::Value = match serde_json::from_str(&json) {
        Ok(v) => v,
        Err(e) => return to_c_string(format!("{{\"error\":\"JSON parse: {}\"}}", e)),
    };

    let symbol = parsed["symbol"].as_str().unwrap_or("");
    let price = parsed["price"].as_f64().unwrap_or(0.0);

    if symbol.is_empty() {
        return to_c_string("{\"error\":\"missing symbol\"}".to_string());
    }

    let mut executor = match get_signal_executor().lock() {
        Ok(g) => g,
        Err(poisoned) => poisoned.into_inner(),
    };

    let actions = executor.on_price_update(symbol, price);

    match actions_to_json(&actions) {
        Ok(json) => to_c_string(json),
        Err(e) => to_c_string(format!("{{\"error\":\"serialize: {}\"}}", e)),
    }
}
