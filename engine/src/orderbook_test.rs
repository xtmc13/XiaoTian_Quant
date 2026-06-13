// Unit tests for the OrderBook data structure
// Tests cover: price levels, bid/ask ordering, cancel, depth, cleanup, edge cases

use crate::orderbook::{Order, OrderBook, OrderStatus, OrderType, Side, OrderedFloat};

// ── OrderBook basics ────────────────────────────────────────────────

#[test]
fn test_orderbook_empty() {
    let ob = OrderBook::new("BTCUSDT".into());
    assert_eq!(ob.best_bid(), None);
    assert_eq!(ob.best_ask(), None);
    assert_eq!(ob.spread(), None);
    assert_eq!(ob.trade_count, 0);
}

#[test]
fn test_orderbook_add_single_order() {
    let mut ob = OrderBook::new("BTCUSDT".into());
    ob.add_order(Order::new(0, 68000.0, 1.0, Side::Buy, OrderType::Limit, 1));
    assert_eq!(ob.best_bid(), Some(68000.0));
    assert_eq!(ob.best_ask(), None);
}

#[test]
fn test_orderbook_bid_ordering() {
    let mut ob = OrderBook::new("TEST".into());
    // Add bids at different prices
    ob.add_order(Order::new(0, 67000.0, 1.0, Side::Buy, OrderType::Limit, 1));
    ob.add_order(Order::new(0, 68500.0, 1.0, Side::Buy, OrderType::Limit, 2));
    ob.add_order(Order::new(0, 67500.0, 1.0, Side::Buy, OrderType::Limit, 3));

    // Best bid should be the highest price
    assert_eq!(ob.best_bid(), Some(68500.0));
}

#[test]
fn test_orderbook_ask_ordering() {
    let mut ob = OrderBook::new("TEST".into());
    ob.add_order(Order::new(0, 69000.0, 1.0, Side::Sell, OrderType::Limit, 1));
    ob.add_order(Order::new(0, 68500.0, 1.0, Side::Sell, OrderType::Limit, 2));
    ob.add_order(Order::new(0, 69500.0, 1.0, Side::Sell, OrderType::Limit, 3));

    // Best ask should be the lowest price
    assert_eq!(ob.best_ask(), Some(68500.0));
}

#[test]
fn test_orderbook_spread() {
    let mut ob = OrderBook::new("TEST".into());
    ob.add_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
    ob.add_order(Order::new(0, 110.0, 1.0, Side::Sell, OrderType::Limit, 2));

    assert_eq!(ob.spread(), Some(10.0));
}

#[test]
fn test_orderbook_no_spread_single_side() {
    let mut ob = OrderBook::new("TEST".into());
    ob.add_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));

    assert_eq!(ob.spread(), None);
}

// ── Cancel Order ───────────────────────────────────────────────────

#[test]
fn test_orderbook_cancel_by_id() {
    let mut ob = OrderBook::new("TEST".into());
    let buy = Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1);
    let id = ob.add_order(buy);

    let cancelled = ob.cancel_order(id);
    assert!(cancelled.is_some());
    assert_eq!(cancelled.unwrap().status, OrderStatus::Cancelled);
    assert_eq!(ob.best_bid(), None);
}

#[test]
fn test_orderbook_cancel_nonexistent() {
    let mut ob = OrderBook::new("TEST".into());
    assert!(ob.cancel_order(999).is_none());
}

#[test]
fn test_orderbook_cancel_leaves_other_orders() {
    let mut ob = OrderBook::new("TEST".into());
    let id1 = ob.add_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
    let _id2 = ob.add_order(Order::new(0, 101.0, 1.0, Side::Buy, OrderType::Limit, 2));

    ob.cancel_order(id1);
    assert_eq!(ob.best_bid(), Some(101.0));
}

#[test]
fn test_orderbook_cleanup_empty_levels() {
    let mut ob = OrderBook::new("TEST".into());
    let id = ob.add_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
    assert!(!ob.bids.is_empty());

    ob.cancel_order(id);
    assert!(ob.bids.is_empty());
}

// ── Depth ──────────────────────────────────────────────────────────

#[test]
fn test_orderbook_depth() {
    let mut ob = OrderBook::new("TEST".into());
    ob.add_order(Order::new(0, 100.0, 2.0, Side::Buy, OrderType::Limit, 1));
    ob.add_order(Order::new(0, 101.0, 3.0, Side::Buy, OrderType::Limit, 2));
    ob.add_order(Order::new(0, 102.0, 4.0, Side::Buy, OrderType::Limit, 3));
    ob.add_order(Order::new(0, 105.0, 5.0, Side::Sell, OrderType::Limit, 4));
    ob.add_order(Order::new(0, 106.0, 6.0, Side::Sell, OrderType::Limit, 5));

    let (bids, asks) = ob.depth(2);
    assert_eq!(bids.len(), 2);
    assert_eq!(asks.len(), 2);
    // Best bid = 102.0, then 101.0
    assert_eq!(bids[0].0, 102.0);
    assert_eq!(bids[0].1, 4.0);
}

// ── Price Precision ────────────────────────────────────────────────

#[test]
fn test_ordered_float_precision() {
    let price = 68000.12345678_f64;
    let key_buy = OrderedFloat::from_price(price, Side::Buy);
    let recovered = key_buy.to_price(Side::Buy);
    assert!((price - recovered).abs() < 1e-6);
}

#[test]
fn test_ordered_float_ordering_bids() {
    // Higher prices should have smaller keys for bids
    // (so they sort first in BTreeMap iteration which is ascending)
    let lo = OrderedFloat::from_price(100.0, Side::Buy);
    let hi = OrderedFloat::from_price(110.0, Side::Buy);
    assert!(lo > hi, "Higher buy price (110) should have smaller key than lower (100)");
}

#[test]
fn test_ordered_float_ordering_asks() {
    // Lower prices should sort first for asks
    let lo = OrderedFloat::from_price(100.0, Side::Sell);
    let hi = OrderedFloat::from_price(110.0, Side::Sell);
    assert!(lo < hi, "Lower sell price should be smaller");
}

#[test]
#[should_panic(expected = "price must be finite")]
fn test_ordered_float_nan_panic() {
    OrderedFloat::from_price(f64::NAN, Side::Buy);
}

// ── Multiple orders at same price level ────────────────────────────

#[test]
fn test_orderbook_same_price_level() {
    let mut ob = OrderBook::new("TEST".into());
    ob.add_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
    ob.add_order(Order::new(0, 100.0, 2.0, Side::Buy, OrderType::Limit, 2));
    ob.add_order(Order::new(0, 100.0, 3.0, Side::Buy, OrderType::Limit, 3));

    assert_eq!(ob.best_bid(), Some(100.0));
    let (bids, _) = ob.depth(1);
    assert_eq!(bids[0].1, 6.0); // 1+2+3
}

// ── Order ID Allocation ───────────────────────────────────────────

#[test]
fn test_order_id_auto_allocation() {
    let mut ob = OrderBook::new("TEST".into());
    let id1 = ob.add_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
    let id2 = ob.add_order(Order::new(0, 101.0, 1.0, Side::Sell, OrderType::Limit, 2));
    assert_eq!(id1, 1);
    assert_eq!(id2, 2);
}

#[test]
fn test_orderbook_order_count_tracking() {
    let mut ob = OrderBook::new("TEST".into());
    ob.add_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
    ob.add_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 2));
    assert_eq!(ob.order_count, 2);
}

// ── Edge Cases ─────────────────────────────────────────────────────

#[test]
fn test_orderbook_zero_quantity() {
    let mut ob = OrderBook::new("TEST".into());
    ob.add_order(Order::new(0, 100.0, 0.0, Side::Buy, OrderType::Limit, 1));
    assert_eq!(ob.best_bid(), Some(100.0));
}

#[test]
fn test_orderbook_very_small_prices() {
    let mut ob = OrderBook::new("TEST".into());
    ob.add_order(Order::new(0, 0.00000001, 1.0, Side::Buy, OrderType::Limit, 1));
    // Should still maintain precision
    assert!(ob.best_bid().is_some());
}

#[test]
fn test_orderbook_very_large_prices() {
    let mut ob = OrderBook::new("TEST".into());
    ob.add_order(Order::new(0, 1_000_000.0, 0.001, Side::Sell, OrderType::Limit, 1));
    assert_eq!(ob.best_ask(), Some(1_000_000.0));
}
