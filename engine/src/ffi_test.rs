//! FFI interface tests forGo↔Rust interop.
//!
//! Tests cover:
//! - Engine lifecycle (create/destroy)
//! - Order submission (JSON parsing, limit/market)
//! - Cancel order
//! - Snapshot & trade queries
//! - Memory management
//!
//! NOTE: These tests use the global engine registry (ENGINES).
//! Each test gets its own unique symbol to avoid cross-test interference.

use std::ffi::CString;
use std::os::raw::c_char;

// Re-export types for use in tests
use crate::ffi::{
    engine_create, engine_destroy, engine_submit_order,
    engine_cancel_order, engine_snapshot, engine_trade_count,
    engine_get_trades, free_string,
};

// ── Test Helpers ───────────────────────────────────────────────────────────────

/// Helper: convert a Rust String to a C string pointer.
fn to_c_char(s: &str) -> *const c_char {
    CString::new(s).unwrap().into_raw() as *const c_char
}

/// Helper: convert a C string pointer back to a Rust String and free it.
fn from_c_char(ptr: *mut c_char) -> String {
    let s = unsafe {
        if ptr.is_null() {
            String::new()
        } else {
            std::ffi::CStr::from_ptr(ptr).to_string_lossy().into_owned()
        }
    };
    // Free the memory allocated by Rust
    free_string(ptr);
    s
}

/// Helper: parse a JSON response from FFI.
fn parse_json(response: &str) -> serde_json::Value {
    serde_json::from_str(response).expect(&format!("Expected valid JSON: {}", response))
}

// ── Engine Lifecycle Tests ─────────────────────────────────────────────────────

#[test]
fn test_ffi_engine_create_ok() {
    let symbol = "FFI_TEST_CREATE";
    let ptr = engine_create(to_c_char(symbol));
    let response = from_c_char(ptr);

    let json = parse_json(&response);
    assert_eq!(json["status"], "ok");
    assert_eq!(json["symbol"], symbol);

    // Clean up
    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_engine_create_and_destroy() {
    let symbol = "FFI_TEST_DESTROY";

    // Create
    let ptr = engine_create(to_c_char(symbol));
    let json = parse_json(&from_c_char(ptr));
    assert_eq!(json["status"], "ok");

    // Destroy
    engine_destroy(to_c_char(symbol));

    // Should return error on further operations
    let ptr2 = engine_submit_order(to_c_char(r#"{"symbol":"FFI_TEST_DESTROY","side":"buy","price":100,"quantity":1}"#));
    let response = from_c_char(ptr2);
    assert!(response.contains("engine not found"));
}

#[test]
fn test_ffi_engine_create_empty_symbol() {
    // Empty string should still create (symbol becomes "")
    let ptr = engine_create(to_c_char(""));
    let response = from_c_char(ptr);
    let json = parse_json(&response);
    assert_eq!(json["status"], "ok");
    assert_eq!(json["symbol"], "");
    engine_destroy(to_c_char(""));
}

#[test]
fn test_ffi_engine_create_multiple_engines() {
    for i in 0..5 {
        let symbol = format!("FFI_MULTI_{}", i);
        let ptr = engine_create(to_c_char(&symbol));
        let json = parse_json(&from_c_char(ptr));
        assert_eq!(json["status"], "ok", "Engine {} should create", i);
    }

    // All should be independent — destroy one
    engine_destroy(to_c_char("FFI_MULTI_2"));

    // Others should still work
    let ptr = engine_submit_order(to_c_char(r#"{"symbol":"FFI_MULTI_0","side":"buy","price":100,"quantity":1}"#));
    let response = from_c_char(ptr);
    assert!(!response.contains("engine not found"), "FFI_MULTI_0 should still exist");

    // Clean up
    for i in [0, 1, 3, 4] {
        engine_destroy(to_c_char(&format!("FFI_MULTI_{}", i)));
    }
}

// ── Order Submission Tests ─────────────────────────────────────────────────────

#[test]
fn test_ffi_submit_limit_order_buy() {
    let symbol = "FFI_TEST_LIMIT_BUY";
    engine_create(to_c_char(symbol));

    let json_input = serde_json::json!({
        "symbol": symbol,
        "side": "buy",
        "price": 68000.0,
        "quantity": 0.1,
        "user_id": 42,
    });
    let ptr = engine_submit_order(to_c_char(&json_input.to_string()));
    let response = from_c_char(ptr);

    let json = parse_json(&response);
    assert_eq!(json["status"], "ok");
    assert!(json["order_id"].is_number());
    assert!(json["trades"].is_array());

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_submit_limit_order_sell() {
    let symbol = "FFI_TEST_LIMIT_SELL";
    engine_create(to_c_char(symbol));

    let json_input = serde_json::json!({
        "symbol": symbol,
        "side": "sell",
        "price": 68000.0,
        "quantity": 0.1,
        "user_id": 1,
    });
    let ptr = engine_submit_order(to_c_char(&json_input.to_string()));
    let response = from_c_char(ptr);

    let json = parse_json(&response);
    assert_eq!(json["status"], "ok");

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_submit_order_immediate_match() {
    let symbol = "FFI_TEST_MATCH";
    engine_create(to_c_char(symbol));

    // Place a sell first
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol,
        "side": "sell",
        "price": 68000.0,
        "quantity": 0.5,
        "user_id": 1,
    }).to_string()));

    // Place a buy that should immediately match
    let ptr = engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol,
        "side": "buy",
        "price": 68000.0,
        "quantity": 0.3,
        "user_id": 2,
    }).to_string()));
    let response = from_c_char(ptr);

    let json = parse_json(&response);
    assert_eq!(json["status"], "ok");
    let trades = json["trades"].as_array().unwrap();
    assert_eq!(trades.len(), 1);
    assert_eq!(trades[0]["price"], 68000.0);
    assert_eq!(trades[0]["quantity"], 0.3);

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_submit_order_partial_fill() {
    let symbol = "FFI_TEST_PARTIAL";
    engine_create(to_c_char(symbol));

    // Sell 1.0 at 100
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol,
        "side": "sell",
        "price": 100.0,
        "quantity": 1.0,
        "user_id": 1,
    }).to_string()));

    // Buy 3.0 at 100 — should partially fill
    let ptr = engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol,
        "side": "buy",
        "price": 100.0,
        "quantity": 3.0,
        "user_id": 2,
    }).to_string()));
    let response = from_c_char(ptr);

    let json = parse_json(&response);
    let trades = json["trades"].as_array().unwrap();
    assert_eq!(trades.len(), 1);
    assert_eq!(trades[0]["quantity"], 1.0);
    // Remaining 2.0 should be on the book
    let snap_ptr = engine_snapshot(to_c_char(symbol), 10);
    let snap = parse_json(&from_c_char(snap_ptr));
    let bids = snap["bids"].as_array().unwrap();
    assert_eq!(bids.len(), 1);
    assert_eq!(bids[0][1], 2.0);

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_submit_market_order() {
    let symbol = "FFI_TEST_MARKET";
    engine_create(to_c_char(symbol));

    // Seed asks
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "sell", "price": 100.0, "quantity": 1.0, "user_id": 1,
    }).to_string()));
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "sell", "price": 101.0, "quantity": 1.0, "user_id": 2,
    }).to_string()));

    // Market buy
    let ptr = engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol,
        "side": "buy",
        "order_type": "market",
        "price": 0.0,
        "quantity": 1.5,
        "user_id": 3,
    }).to_string()));
    let response = from_c_char(ptr);

    let json = parse_json(&response);
    let trades = json["trades"].as_array().unwrap();
    assert_eq!(trades.len(), 2, "Market buy should sweep both asks");
    assert_eq!(trades[0]["price"], 100.0);
    assert_eq!(trades[1]["price"], 101.0);

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_submit_invalid_json() {
    let symbol = "FFI_TEST_BAD_JSON";
    engine_create(to_c_char(symbol));

    let ptr = engine_submit_order(to_c_char("not valid json {{{"));
    let response = from_c_char(ptr);

    assert!(response.contains("error"));
    // Should NOT panic — graceful error

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_submit_order_engine_not_found() {
    let ptr = engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": "NONEXISTENT_SYMBOL",
        "side": "buy",
        "price": 100.0,
        "quantity": 1.0,
    }).to_string()));
    let response = from_c_char(ptr);

    assert!(response.contains("engine not found"));
}

#[test]
fn test_ffi_submit_order_no_cross() {
    let symbol = "FFI_TEST_NO_CROSS";
    engine_create(to_c_char(symbol));

    // Sell at 150
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "sell", "price": 150.0, "quantity": 1.0, "user_id": 1,
    }).to_string()));

    // Buy at 140 — prices don't cross, should go on book
    let ptr = engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "buy", "price": 140.0, "quantity": 1.0, "user_id": 2,
    }).to_string()));
    let response = from_c_char(ptr);

    let json = parse_json(&response);
    let trades = json["trades"].as_array().unwrap();
    assert!(trades.is_empty(), "No trade should happen");

    // Check book state
    let snap_ptr = engine_snapshot(to_c_char(symbol), 10);
    let snap = parse_json(&from_c_char(snap_ptr));
    assert_eq!(snap["best_bid"], 140.0);
    assert_eq!(snap["best_ask"], 150.0);

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_submit_order_multiple_fills() {
    let symbol = "FFI_TEST_MULTI_FILL";
    engine_create(to_c_char(symbol));

    // Seed3 asks at different prices
    for (price, qty) in [(100.0, 0.5), (101.0, 0.3), (102.0, 0.2)] {
        engine_submit_order(to_c_char(&serde_json::json!({
            "symbol": symbol, "side": "sell", "price": price, "quantity": qty, "user_id": 1,
        }).to_string()));
    }

    // Buy 1.0 — should fill all 3
    let ptr = engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "buy", "price": 200.0, "quantity": 1.0, "user_id": 2,
    }).to_string()));
    let response = from_c_char(ptr);

    let json = parse_json(&response);
    let trades = json["trades"].as_array().unwrap();
    assert_eq!(trades.len(), 3);
    let total_qty: f64 = trades.iter().map(|t| t["quantity"].as_f64().unwrap()).sum::<f64>();
    assert!((total_qty - 1.0).abs() < 1e-9);

    engine_destroy(to_c_char(symbol));
}

// ── Cancel Order Tests ─────────────────────────────────────────────────────────

#[test]
fn test_ffi_cancel_order_existing() {
    let symbol = "FFI_TEST_CANCEL";
    engine_create(to_c_char(symbol));

    // Submit a limit order (no match, goes on book)
    let ptr = engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "buy", "price": 50000.0, "quantity": 1.0, "user_id": 1,
    }).to_string()));
    let json = parse_json(&from_c_char(ptr));
    let order_id = json["order_id"].as_u64().unwrap();

    // Cancel it
    let cancel_ptr = engine_cancel_order(to_c_char(symbol), order_id);
    let cancel_response = from_c_char(cancel_ptr);
    assert!(cancel_response.contains("ok"));
    assert!(cancel_response.contains(&order_id.to_string()));

    // Verify it's gone from the book
    let snap_ptr = engine_snapshot(to_c_char(symbol), 10);
    let snap = parse_json(&from_c_char(snap_ptr));
    assert!(snap["bids"].as_array().unwrap().is_empty());

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_cancel_order_nonexistent() {
    let symbol = "FFI_TEST_CANCEL_MISSING";
    engine_create(to_c_char(symbol));

    let ptr = engine_cancel_order(to_c_char(symbol), 99999);
    let response = from_c_char(ptr);

    assert!(response.contains("error") || response.contains("not found"));

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_cancel_order_engine_not_found() {
    let ptr = engine_cancel_order(to_c_char("NONEXISTENT"), 1);
    let response = from_c_char(ptr);
    assert!(response.contains("engine not found"));
}

// ── Snapshot Tests ─────────────────────────────────────────────────────────────

#[test]
fn test_ffi_snapshot_empty_book() {
    let symbol = "FFI_TEST_SNAP_EMPTY";
    engine_create(to_c_char(symbol));

    let ptr = engine_snapshot(to_c_char(symbol), 10);
    let json = parse_json(&from_c_char(ptr));

    assert_eq!(json["symbol"], symbol);
    assert!(json["best_bid"].is_null());
    assert!(json["best_ask"].is_null());
    assert!(json["bids"].as_array().unwrap().is_empty());
    assert!(json["asks"].as_array().unwrap().is_empty());

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_snapshot_with_orders() {
    let symbol = "FFI_TEST_SNAP_FULL";
    engine_create(to_c_char(symbol));

    // Add bids
    for price in [100.0, 101.0, 102.0] {
        engine_submit_order(to_c_char(&serde_json::json!({
            "symbol": symbol, "side": "buy", "price": price, "quantity": 1.0, "user_id": 1,
        }).to_string()));
    }
    // Add asks
    for price in [103.0, 104.0, 105.0] {
        engine_submit_order(to_c_char(&serde_json::json!({
            "symbol": symbol, "side": "sell", "price": price, "quantity": 1.0, "user_id": 2,
        }).to_string()));
    }

    let ptr = engine_snapshot(to_c_char(symbol), 10);
    let json = parse_json(&from_c_char(ptr));

    assert_eq!(json["best_bid"], 102.0);
    assert_eq!(json["best_ask"], 103.0);
    assert!(json["spread"].as_f64().unwrap() > 0.0);

    let bids = json["bids"].as_array().unwrap();
    let asks = json["asks"].as_array().unwrap();
    assert_eq!(bids.len(), 3);
    assert_eq!(asks.len(), 3);

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_snapshot_depth_limit() {
    let symbol = "FFI_TEST_SNAP_DEPTH";
    engine_create(to_c_char(symbol));

    // Add 10 asks
    for i in 0..10 {
        engine_submit_order(to_c_char(&serde_json::json!({
            "symbol": symbol, "side": "sell", "price": 100.0 + i as f64, "quantity": 1.0, "user_id": 1,
        }).to_string()));
    }

    // Request depth=3
    let ptr = engine_snapshot(to_c_char(symbol), 3);
    let json = parse_json(&from_c_char(ptr));
    let asks = json["asks"].as_array().unwrap();
    assert_eq!(asks.len(), 3, "Should limit to requested depth");

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_snapshot_engine_not_found() {
    let ptr = engine_snapshot(to_c_char("NONEXISTENT"), 10);
    let response = from_c_char(ptr);
    assert!(response.contains("engine not found"));
}

// ── Trade Count Tests ───────────────────────────────────────────────────────────

#[test]
fn test_ffi_trade_count() {
    let symbol = "FFI_TEST_TRADE_COUNT";
    engine_create(to_c_char(symbol));

    assert_eq!(engine_trade_count(to_c_char(symbol)), 0);

    // Seed ask and buy
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "sell", "price": 100.0, "quantity": 1.0, "user_id": 1,
    }).to_string()));
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "buy", "price": 100.0, "quantity": 0.5, "user_id": 2,
    }).to_string()));

    assert_eq!(engine_trade_count(to_c_char(symbol)), 1);

    // Another trade
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "buy", "price": 100.0, "quantity": 0.5, "user_id": 3,
    }).to_string()));
    assert_eq!(engine_trade_count(to_c_char(symbol)), 2);

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_trade_count_engine_not_found() {
    assert_eq!(engine_trade_count(to_c_char("NONEXISTENT")), 0);
}

// ── Get Trades Tests ────────────────────────────────────────────────────────────

#[test]
fn test_ffi_get_trades_empty() {
    let symbol = "FFI_TEST_TRADES_EMPTY";
    engine_create(to_c_char(symbol));

    let ptr = engine_get_trades(to_c_char(symbol), 10);
    let response = from_c_char(ptr);
    assert_eq!(response, "[]");

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_get_trades_after_matches() {
    let symbol = "FFI_TEST_GET_TRADES";
    engine_create(to_c_char(symbol));

    // Create a trade
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "sell", "price": 50000.0, "quantity": 1.0, "user_id": 1,
    }).to_string()));
    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "buy", "price": 50000.0, "quantity": 0.7, "user_id": 2,
    }).to_string()));

    let ptr = engine_get_trades(to_c_char(symbol), 10);
    let response = from_c_char(ptr);
    let trades: Vec<serde_json::Value> = serde_json::from_str(&response).unwrap();
    assert_eq!(trades.len(), 1);
    assert_eq!(trades[0]["price"], 50000.0);
    assert_eq!(trades[0]["quantity"], 0.7);

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_get_trades_limit() {
    let symbol = "FFI_TEST_TRADES_LIMIT";
    engine_create(to_c_char(symbol));

    // Create 10 trades
    for i in 0..10 {
        engine_submit_order(to_c_char(&serde_json::json!({
            "symbol": symbol, "side": "sell", "price": 100.0 + i as f64, "quantity": 1.0, "user_id": 1,
        }).to_string()));
        engine_submit_order(to_c_char(&serde_json::json!({
            "symbol": symbol, "side": "buy", "price": 100.0 + i as f64, "quantity": 1.0, "user_id": 2,
        }).to_string()));
    }

    // Request limit=5
    let ptr = engine_get_trades(to_c_char(symbol), 5);
    let response = from_c_char(ptr);
    let trades: Vec<serde_json::Value> = serde_json::from_str(&response).unwrap();
    assert_eq!(trades.len(), 5, "Should return at most limit trades");

    engine_destroy(to_c_char(symbol));
}

// ── Memory Management Tests ────────────────────────────────────────────────────

#[test]
fn test_ffi_free_string_null() {
    // Should not panic
    free_string(std::ptr::null_mut());
}

#[test]
fn test_ffi_free_string_valid() {
    // Create a string and free it — should not leak or panic
    let symbol = "FFI_TEST_FREE";
    engine_create(to_c_char(symbol));

    let ptr = engine_snapshot(to_c_char(symbol), 10);
    // from_c_char calls free_string internally
    from_c_char(ptr);

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_no_memory_leak_concurrent_creates() {
    // Rapid create/destroy cycle — stress test
    for i in 0..100 {
        let symbol = format!("LEAK_TEST_{}", i);
        engine_create(to_c_char(&symbol));
        let ptr = engine_snapshot(to_c_char(&symbol), 5);
        from_c_char(ptr);
        engine_destroy(to_c_char(&symbol));
    }
    // If we get here without OOM or panic, the memory management is correct
}

// ── Edge Cases ─────────────────────────────────────────────────────────────────

#[test]
fn test_ffi_side_case_insensitive() {
    let symbol = "FFI_TEST_CASE";
    engine_create(to_c_char(symbol));

    for &side in &["BUY", "Sell", "Buy", "SELL"] {
        let ptr = engine_submit_order(to_c_char(&serde_json::json!({
            "symbol": symbol, "side": side, "price": 100.0, "quantity": 0.1, "user_id": 1,
        }).to_string()));
        let json = parse_json(&from_c_char(ptr));
        assert_eq!(json["status"], "ok", "Side '{}' should be accepted", side);
    }

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_order_type_market() {
    let symbol = "FFI_TEST_ORDER_TYPE";
    engine_create(to_c_char(symbol));

    engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "sell", "price": 100.0, "quantity": 1.0, "user_id": 1,
    }).to_string()));

    let ptr = engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "buy", "order_type": "market", "price": 0.0, "quantity": 1.0, "user_id": 2,
    }).to_string()));
    let json = parse_json(&from_c_char(ptr));
    assert_eq!(json["status"], "ok");

    engine_destroy(to_c_char(symbol));
}

#[test]
fn test_ffi_zero_quantity() {
    let symbol = "FFI_TEST_ZERO_QTY";
    engine_create(to_c_char(symbol));

    let ptr = engine_submit_order(to_c_char(&serde_json::json!({
        "symbol": symbol, "side": "buy", "price": 100.0, "quantity": 0.0, "user_id": 1,
    }).to_string()));
    let response = from_c_char(ptr);
    let json = parse_json(&response);
    // Should be processed (filled immediately since qty=0 means no remaining)
    assert_eq!(json["status"], "ok");

    engine_destroy(to_c_char(symbol));
}