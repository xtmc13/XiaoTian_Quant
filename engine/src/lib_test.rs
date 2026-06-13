//! Library-level integration tests.
//!
//! Tests the public API of the xt_matching crate as a whole:
//! - Module re-exports are correct
//! - Public API surface is usable without internal details
//! - Order book + matching engine integration
//! - FFI + core integration end-to-end

use crate::{
    // Re-exported from orderbook
    Order, OrderStatus, OrderType, Side, Trade,
    // Re-exported from matching
    MatchingEngine,
};

// ── Module Re-export Tests ────────────────────────────────────────────────────

/// Verify that the library re-exports all public types correctly.
/// A user should be able to `use xt_matching::{Order, MatchingEngine, ...}`
/// without knowing internal module paths.
#[test]
fn test_public_api_exports_order() {
    let order = Order::new(1, 100.0, 0.5, Side::Buy, OrderType::Limit, 42);
    assert_eq!(order.id, 1);
    assert_eq!(order.price, 100.0);
    assert_eq!(order.quantity, 0.5);
    assert_eq!(order.side, Side::Buy);
    assert_eq!(order.order_type, OrderType::Limit);
    assert_eq!(order.user_id, 42);
    assert_eq!(order.remaining(), 0.5);
    assert!(!order.is_done());
}

#[test]
fn test_public_api_exports_order_status() {
    let order = Order::new(1, 100.0, 1.0, Side::Buy, OrderType::Limit, 1);
    assert_eq!(order.status, OrderStatus::New);
    assert!(!order.is_done());
}

#[test]
fn test_public_api_exports_order_type() {
    let limit = Order::new(1, 100.0, 1.0, Side::Buy, OrderType::Limit, 1);
    let market = Order::new(2, 0.0, 1.0, Side::Buy, OrderType::Market, 2);
    assert_eq!(limit.order_type, OrderType::Limit);
    assert_eq!(market.order_type, OrderType::Market);
}

#[test]
fn test_public_api_exports_side() {
    let buy = Order::new(1, 100.0, 1.0, Side::Buy, OrderType::Limit, 1);
    let sell = Order::new(2, 100.0, 1.0, Side::Sell, OrderType::Limit, 2);
    assert_eq!(buy.side, Side::Buy);
    assert_eq!(sell.side, Side::Sell);
}

#[test]
fn test_public_api_exports_trade() {
    let trade = Trade {
        id: 1,
        buy_order_id: 10,
        sell_order_id: 20,
        price: 68000.0,
        quantity: 0.5,
        timestamp: 1234567890,
    };
    assert_eq!(trade.id, 1);
    assert_eq!(trade.buy_order_id, 10);
    assert_eq!(trade.sell_order_id, 20);
    assert_eq!(trade.price, 68000.0);
    assert_eq!(trade.quantity, 0.5);
}

// ── OrderBook + MatchingEngine Integration ─────────────────────────────────────

#[test]
fn test_integration_full_trade_lifecycle() {
    let mut engine = MatchingEngine::new("INTEG_TEST".into());

    // Step 1: Place a sell order on the book
    let (sell_id, no_trades) = engine.submit_order(
        Order::new(0, 50000.0, 2.0, Side::Sell, OrderType::Limit, 1)
    );
    assert!(no_trades.is_empty());
    assert_eq!(engine.book.best_ask(), Some(50000.0));
    assert!(engine.book.best_bid().is_none());

    // Step 2: Place a buy that crosses — full fill
    let (buy_id, trades) = engine.submit_order(
        Order::new(0, 50000.0, 2.0, Side::Buy, OrderType::Limit, 2)
    );
    assert_eq!(trades.len(), 1);
    assert_eq!(trades[0].price, 50000.0);
    assert_eq!(trades[0].quantity, 2.0);
    assert_eq!(trades[0].buy_order_id, buy_id);
    assert_eq!(trades[0].sell_order_id, sell_id);

    // Step 3: Book should be empty after full fill
    assert!(engine.book.best_ask().is_none());
    assert!(engine.book.best_bid().is_none());

    // Step 4: Trade count should be 1
    assert_eq!(engine.book.trade_count, 1);
}

#[test]
fn test_integration_multi_order_cross() {
    let mut engine = MatchingEngine::new("INTEG_CROSS".into());

    // 5 asks at ascending prices
    for (i, price) in [100.0, 101.0, 102.0, 103.0, 104.0].iter().enumerate() {
        engine.submit_order(
            Order::new(0, *price, 1.0, Side::Sell, OrderType::Limit, i as u64 + 1)
        );
    }

    // One aggressive buy sweeping all asks
    let (_, trades) = engine.submit_order(
        Order::new(0, 200.0, 5.0, Side::Buy, OrderType::Limit, 10)
    );

    assert_eq!(trades.len(), 5);
    let total_qty: f64 = trades.iter().map(|t| t.quantity).sum();
    assert!((total_qty - 5.0).abs() < 1e-9);
    assert_eq!(engine.book.trade_count, 5);
}

#[test]
fn test_integration_snapshot_consistency() {
    let mut engine = MatchingEngine::new("INTEG_SNAP".into());

    // Add mixed orders
    engine.submit_order(Order::new(0, 98.0, 1.0, Side::Buy, OrderType::Limit, 1));
    engine.submit_order(Order::new(0, 99.0, 2.0, Side::Buy, OrderType::Limit, 2));
    engine.submit_order(Order::new(0, 101.0, 1.5, Side::Sell, OrderType::Limit, 3));

    let (bids, asks) = engine.snapshot(10);

    // Bids in descending order: 99.0 (user2 with qty 2.0), then 98.0 (user1 with qty 1.0)
    assert_eq!(bids[0].0, 99.0);
    assert_eq!(bids[0].1, 2.0);
    assert_eq!(bids[1].0, 98.0);
    // Asks in ascending order: 101.0 (lowest ask)
    assert_eq!(asks[0].0, 101.0);
    assert_eq!(asks[0].1, 1.5);

    // Spread = 101 - 99 = 2
    assert_eq!(engine.book.spread(), Some(2.0));
}

#[test]
fn test_integration_get_trades_returns_recent() {
    let mut engine = MatchingEngine::new("INTEG_TRADES".into());

    for i in 0..20 {
        let price = 100.0 + i as f64;
        engine.submit_order(Order::new(0, price, 1.0, Side::Sell, OrderType::Limit, i * 2));
        engine.submit_order(Order::new(0, price, 1.0, Side::Buy, OrderType::Limit, i * 2 + 1));
    }

    // Get last 5 trades
    let recent = engine.get_trades(5);
    assert_eq!(recent.len(), 5);
    // Trades are in chronological order
    for window in recent.windows(2) {
        assert!(window[0].id <= window[1].id);
    }
}

#[test]
fn test_integration_cancel_and_verify_book() {
    let mut engine = MatchingEngine::new("INTEG_CANCEL".into());

    let (id1, _) = engine.submit_order(
        Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1)
    );
    let (id2, _) = engine.submit_order(
        Order::new(0, 101.0, 2.0, Side::Buy, OrderType::Limit, 2)
    );
    let (id3, _) = engine.submit_order(
        Order::new(0, 99.0, 3.0, Side::Buy, OrderType::Limit, 3)
    );

    // Cancel middle order (101.0)
    let cancelled = engine.cancel_order(id2);
    assert!(cancelled.is_some());
    assert_eq!(cancelled.unwrap().remaining(), 2.0);

    // Remaining:100.0 (id1) and 99.0 (id3) after canceling101.0 (id2)
    // Bids are returned in descending price order: 100.0 first, then 99.0
    let (bids, _) = engine.snapshot(10);
    assert_eq!(bids[0].0, 100.0);
    assert_eq!(bids[0].1, 1.0);
    assert_eq!(bids[1].0, 99.0);
    assert_eq!(bids[1].1, 3.0);

    // Cancel remaining
    engine.cancel_order(id1);
    engine.cancel_order(id3);
    let (bids, asks) = engine.snapshot(10);
    assert!(bids.is_empty() && asks.is_empty());
}

#[test]
fn test_integration_mixed_limit_market_orders() {
    let mut engine = MatchingEngine::new("INTEG_MIXED".into());

    // Seed asks
    engine.submit_order(Order::new(0, 50.0, 1.0, Side::Sell, OrderType::Limit, 1));
    engine.submit_order(Order::new(0, 51.0, 1.0, Side::Sell, OrderType::Limit, 2));
    engine.submit_order(Order::new(0, 52.0, 1.0, Side::Sell, OrderType::Limit, 3));

    // Market buy: sweeps all 3 asks
    let (_, market_trades) = engine.submit_order(
        Order::new(0, 0.0, 3.0, Side::Buy, OrderType::Market, 4)
    );
    assert_eq!(market_trades.len(), 3);

    // Limit buy at lower price: no match (goes on book)
    let (_, limit_trades) = engine.submit_order(
        Order::new(0, 49.0, 1.0, Side::Buy, OrderType::Limit, 5)
    );
    assert!(limit_trades.is_empty());
    assert_eq!(engine.book.best_bid(), Some(49.0));

    // Limit sell that crosses: sell at 48.0 crosses against the 49.0 bid
    let (_, sell_trades) = engine.submit_order(
        Order::new(0, 48.0, 1.0, Side::Sell, OrderType::Limit, 6)
    );
    assert_eq!(sell_trades.len(), 1);

    assert_eq!(engine.book.trade_count, 4);
}

#[test]
fn test_integration_order_remaining_after_partial() {
    let mut engine = MatchingEngine::new("INTEG_REMAIN".into());

    // Sell 0.5
    engine.submit_order(Order::new(0, 100.0, 0.5, Side::Sell, OrderType::Limit, 1));

    // Buy 1.0 — partial fill
    let (_buy_id, trades) = engine.submit_order(
        Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 2)
    );

    assert_eq!(trades.len(), 1);
    assert_eq!(trades[0].quantity, 0.5);

    // Check the remaining on the book (the unfilled buy order)
    let (bids, _) = engine.snapshot(10);
    assert_eq!(bids[0].1, 0.5, "Remaining qty should be 0.5");
}

#[test]
fn test_integration_book_depth_accumulation() {
    let mut engine = MatchingEngine::new("INTEG_DEPTH".into());

    // Multiple orders at same price level
    for _ in 0..5 {
        engine.submit_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
    }

    let (bids, _) = engine.snapshot(10);
    assert_eq!(bids.len(), 1); // Same price level
    assert_eq!(bids[0].0, 100.0);
    assert_eq!(bids[0].1, 5.0, "Depth should accumulate to 5.0");
}

#[test]
fn test_integration_spread_calculation() {
    let mut engine = MatchingEngine::new("INTEG_SPREAD".into());

    assert!(engine.book.spread().is_none(), "Empty book has no spread");

    engine.submit_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
    assert!(engine.book.spread().is_none(), "No ask yet");

    engine.submit_order(Order::new(0, 101.0, 1.0, Side::Sell, OrderType::Limit, 2));
    assert_eq!(engine.book.spread(), Some(1.0));

    // Place tighter ask
    engine.submit_order(Order::new(0, 100.5, 1.0, Side::Sell, OrderType::Limit, 3));
    assert_eq!(engine.book.spread(), Some(0.5));

    // Place tighter bid
    engine.submit_order(Order::new(0, 100.3, 1.0, Side::Buy, OrderType::Limit, 4));
    assert!((engine.book.spread().unwrap() - 0.2).abs() < 1e-9);
}

#[test]
fn test_integration_multiple_symbols_independent() {
    let mut btc = MatchingEngine::new("BTCUSDT".into());
    let mut eth = MatchingEngine::new("ETHUSDT".into());

    btc.submit_order(Order::new(0, 50000.0, 1.0, Side::Buy, OrderType::Limit, 1));
    eth.submit_order(Order::new(0, 3000.0, 1.0, Side::Buy, OrderType::Limit, 2));

    assert_eq!(btc.book.best_bid(), Some(50000.0));
    assert_eq!(eth.book.best_bid(), Some(3000.0));
    assert!(btc.book.best_ask().is_none());
    assert!(eth.book.best_ask().is_none());

    btc.submit_order(Order::new(0, 50001.0, 1.0, Side::Sell, OrderType::Limit, 3));
    eth.submit_order(Order::new(0, 3001.0, 1.0, Side::Sell, OrderType::Limit, 4));

    assert_eq!(btc.book.spread(), Some(1.0));
    assert_eq!(eth.book.spread(), Some(1.0));
}

#[test]
fn test_integration_stress_repeated_cancel_requeue() {
    let mut engine = MatchingEngine::new("INTEG_STRESS_CANCEL".into());

    // Repeatedly cancel and re-add orders at different prices
    for i in 0..100 {
        let id = if i % 2 == 0 {
            engine.submit_order(Order::new(0, 100.0 + i as f64, 0.1, Side::Buy, OrderType::Limit, i as u64)).0
        } else {
            engine.submit_order(Order::new(0, 100.0 + i as f64, 0.1, Side::Sell, OrderType::Limit, i as u64)).0
        };
        // Cancel every3rd order
        if i % 3 == 0 {
            engine.cancel_order(id);
        }
    }

    // Book should still be consistent (no crossed prices)
    if let (Some(bid), Some(ask)) = (engine.book.best_bid(), engine.book.best_ask()) {
        assert!(bid <= ask, "Best bid {} should not exceed best ask {}", bid, ask);
    }
}