use crate::orderbook::{Order, OrderBook, OrderStatus, OrderType, Side, Trade};

/// Matching engine — price-time priority
pub struct MatchingEngine {
    pub book: OrderBook,
    pub trades: Vec<Trade>,
}

impl MatchingEngine {
    pub fn new(symbol: String) -> Self {
        MatchingEngine {
            book: OrderBook::new(symbol),
            trades: Vec::new(),
        }
    }

    /// Submit a limit order and match immediately
    pub fn submit_order(&mut self, mut order: Order) -> (u64, Vec<Trade>) {
        // Always allocate an order ID first, even if the order is fully matched.
        // This ensures filled orders have correct IDs in trade records.
        self.book.order_count += 1;
        order.id = self.book.order_count;
        let new_trades = self.match_order(&mut order);
        let order_id = order.id;
        if !order.is_done() {
            self.book.add_order(order);
        }
        (order_id, new_trades)
    }

    /// Match an incoming order against the book
    fn match_order(&mut self, taker: &mut Order) -> Vec<Trade> {
        let mut trades = Vec::new();

        loop {
            if taker.remaining() <= 0.0 {
                taker.status = OrderStatus::Filled;
                break;
            }

            let maker_price = match taker.side {
                Side::Buy => {
                    // For a buy, look at the lowest ask
                    self.book.best_ask()
                }
                Side::Sell => {
                    // For a sell, look at the highest bid
                    self.book.best_bid()
                }
            };

            let maker_price = match maker_price {
                Some(p) => p,
                None => {
                    // No matching orders, order goes on book
                    break;
                }
            };

            // Check if prices cross
            let can_match = match taker.side {
                Side::Buy => {
                    taker.order_type == OrderType::Market || taker.price >= maker_price
                }
                Side::Sell => {
                    taker.order_type == OrderType::Market || taker.price <= maker_price
                }
            };

            if !can_match {
                break;
            }

            // Execute trade against maker orders at this price level
            let maker_orders = match taker.side {
                Side::Buy => {
                    let key = crate::orderbook::OrderedFloat::from_price(maker_price, Side::Sell);
                    self.book.asks.get_mut(&key)
                }
                Side::Sell => {
                    let key = crate::orderbook::OrderedFloat::from_price(maker_price, Side::Buy);
                    self.book.bids.get_mut(&key)
                }
            };

            if let Some(maker_orders) = maker_orders {
                let mut to_remove = Vec::new();
                for (idx, maker) in maker_orders.iter_mut().enumerate() {
                    if taker.remaining() <= 0.0 {
                        break;
                    }

                    let trade_qty = f64::min(taker.remaining(), maker.remaining());
                    maker.filled += trade_qty;
                    taker.filled += trade_qty;

                    if maker.remaining() <= 0.0 {
                        maker.status = OrderStatus::Filled;
                        to_remove.push(idx);
                    }

                    self.book.trade_count += 1;
                    let trade = Trade {
                        id: self.book.trade_count,
                        buy_order_id: if taker.side == Side::Buy { taker.id } else { maker.id },
                        sell_order_id: if taker.side == Side::Sell { taker.id } else { maker.id },
                        price: maker_price,
                        quantity: trade_qty,
                        timestamp: std::time::SystemTime::now()
                            .duration_since(std::time::UNIX_EPOCH)
                            .unwrap()
                            .as_nanos() as u64,
                    };
                    trades.push(trade);
                }

                // Remove filled maker orders
                for &idx in to_remove.iter().rev() {
                    maker_orders.remove(idx);
                }
            }

            // Clean up empty levels
            self.book.cleanup();
        }

        // Handle unfilled market orders — they just cancel
        if taker.order_type == OrderType::Market && taker.remaining() > 0.0 {
            taker.status = OrderStatus::Cancelled;
        }

        self.trades.extend(trades.clone());
        trades
    }

    /// Cancel an order
    pub fn cancel_order(&mut self, order_id: u64) -> Option<Order> {
        self.book.cancel_order(order_id)
    }

    /// Get recent trades
    pub fn get_trades(&self, limit: usize) -> &[Trade] {
        let len = self.trades.len();
        let start = if len > limit { len - limit } else { 0 };
        &self.trades[start..]
    }

    /// Get the order book snapshot
    pub fn snapshot(&self, depth: usize) -> (Vec<(f64, f64)>, Vec<(f64, f64)>) {
        self.book.depth(depth)
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::orderbook::{Order, OrderType, Side};

    #[test]
    fn test_limit_buy_match() {
        let mut engine = MatchingEngine::new("BTCUSDT".into());

        // Add a sell order to the book
        let sell = Order::new(0, 68000.0, 0.1, Side::Sell, OrderType::Limit, 1);
        engine.book.add_order(sell);

        // Submit a matching buy order
        let buy = Order::new(0, 68000.0, 0.1, Side::Buy, OrderType::Limit, 2);
        let (_, trades) = engine.submit_order(buy);

        assert_eq!(trades.len(), 1);
        assert_eq!(trades[0].price, 68000.0);
        assert_eq!(trades[0].quantity, 0.1);
    }

    #[test]
    fn test_limit_sell_match() {
        let mut engine = MatchingEngine::new("ETHUSDT".into());

        // Add a buy order to the book
        let buy = Order::new(0, 3500.0, 1.0, Side::Buy, OrderType::Limit, 1);
        engine.book.add_order(buy);

        // Submit a matching sell order
        let sell = Order::new(0, 3500.0, 1.0, Side::Sell, OrderType::Limit, 2);
        let (_, trades) = engine.submit_order(sell);

        assert_eq!(trades.len(), 1);
        assert_eq!(trades[0].price, 3500.0);
    }

    #[test]
    fn test_no_cross_no_match() {
        let mut engine = MatchingEngine::new("SOLUSDT".into());

        // Add sell at 150
        engine.book.add_order(Order::new(0, 150.0, 10.0, Side::Sell, OrderType::Limit, 1));
        // Try to buy at 140 — should not match
        let buy = Order::new(0, 140.0, 5.0, Side::Buy, OrderType::Limit, 2);
        let (_, trades) = engine.submit_order(buy);

        assert!(trades.is_empty());
        assert_eq!(engine.book.best_bid(), Some(140.0));
        assert_eq!(engine.book.best_ask(), Some(150.0));
    }

    #[test]
    fn test_partial_fill() {
        let mut engine = MatchingEngine::new("BNBUSDT".into());

        engine.book.add_order(Order::new(0, 600.0, 1.0, Side::Sell, OrderType::Limit, 1));
        let buy = Order::new(0, 600.0, 3.0, Side::Buy, OrderType::Limit, 2);
        let (_, trades) = engine.submit_order(buy);

        assert_eq!(trades.len(), 1);
        assert_eq!(trades[0].quantity, 1.0);
        assert_eq!(engine.book.best_bid(), Some(600.0));
    }

    // ── Stress Tests ─────────────────────────────────────────────

    #[test]
    fn stress_test_1000_random_orders() {
        let mut engine = MatchingEngine::new("STRESS".into());
        let mut rng = fast_rng();
        let mut order_id = 0u64;

        for _ in 0..1000 {
            order_id += 1;
            let price = 100.0 + (rng % 1000) as f64 / 10.0;
            let qty = 0.01 + ((rng >> 16) % 100) as f64 / 100.0;
            let side = if (rng >> 8) % 2 == 0 { Side::Buy } else { Side::Sell };
            rng = rng.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);

            let order = Order::new(0, price, qty, side, OrderType::Limit, order_id);
            engine.submit_order(order);
        }

        // Order book should still be consistent
        if let (Some(bid), Some(ask)) = (engine.book.best_bid(), engine.book.best_ask()) {
            assert!(bid <= ask,
                "best bid {} should be <= best ask {}", bid, ask);
        }
    }

    #[test]
    fn stress_test_10000_orders() {
        let mut engine = MatchingEngine::new("STRESS10K".into());
        let mut rng = fast_rng();
        let mut order_id = 0u64;

        let start = std::time::Instant::now();
        for _ in 0..10000 {
            order_id += 1;
            let price = 100.0 + (rng % 1000) as f64 / 10.0;
            let qty = 0.01 + ((rng >> 16) % 100) as f64 / 100.0;
            let side = if (rng >> 8) % 2 == 0 { Side::Buy } else { Side::Sell };
            rng = rng.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);

            let order = Order::new(0, price, qty, side, OrderType::Limit, order_id);
            engine.submit_order(order);
        }
        let elapsed = start.elapsed();

        println!("10,000 orders in {:?} ({:.0} orders/sec)",
            elapsed, 10000.0 / elapsed.as_secs_f64());

        assert!(elapsed.as_secs_f64() < 5.0, "Should process 10k orders in < 5s");
    }

    #[test]
    fn stress_test_crossing_orders() {
        let mut engine = MatchingEngine::new("CROSS".into());
        let mut rng = fast_rng();

        // Fill both sides at same prices — should match aggressively
        for i in 0..5000u64 {
            let price = 50000.0; // All at same price = all cross
            let qty = 0.1;
            let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
            rng = rng.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);

            let order = Order::new(0, price, qty, side, OrderType::Limit, i + 1);
            engine.submit_order(order);
        }

        // Book should be nearly empty (all orders crossed)
        let (bids, asks) = engine.book.depth(10);
        let total_qty: f64 = bids.iter().map(|(_, q)| q).sum::<f64>()
            + asks.iter().map(|(_, q)| q).sum::<f64>();
        assert!(total_qty < 500.0, "Most crossing orders should match, remaining: {}", total_qty);
    }

    #[test]
    fn benchmark_submit_100k() {
        let mut engine = MatchingEngine::new("BENCH".into());
        let mut rng = fast_rng();
        let mut order_id = 0u64;

        let start = std::time::Instant::now();
        for _ in 0..100_000 {
            order_id += 1;
            let price = 50000.0 + (rng % 2000) as f64 / 10.0;
            let qty = 0.001 + ((rng >> 16) % 10) as f64 / 100.0;
            let side = if (rng >> 8) % 2 == 0 { Side::Buy } else { Side::Sell };
            rng = rng.wrapping_mul(6364136223846793005).wrapping_add(1442695040888963407);

            let order = Order::new(0, price, qty, side, OrderType::Limit, order_id);
            engine.submit_order(order);
        }
        let elapsed = start.elapsed();
        let tps = 100_000.0 / elapsed.as_secs_f64();

        println!("\n═══ RUST MATCHING ENGINE BENCHMARK ═══");
        println!("  100,000 orders in {:?}", elapsed);
        println!("  Throughput: {:.0} orders/sec", tps);
        println!("  Avg latency: {:.2} μs/order", elapsed.as_micros() as f64 / 100_000.0);
        println!("  Trades executed: {}", engine.trades.len());
        println!("══════════════════════════════════════════\n");

        assert!(tps > 10_000.0, "Should exceed 10k TPS, got {:.0}", tps);
    }

    // ── Cancel Order Tests ─────────────────────────────────────────

    #[test]
    fn test_cancel_existing_order() {
        let mut engine = MatchingEngine::new("CANCEL".into());
        let (id, _) = engine.submit_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
        
        let cancelled = engine.cancel_order(id);
        assert!(cancelled.is_some(), "Should cancel existing order");
        assert_eq!(cancelled.unwrap().id, id);
        assert!(engine.book.best_bid().is_none(), "Book should be empty after cancel");
    }

    #[test]
    fn test_cancel_nonexistent_order() {
        let mut engine = MatchingEngine::new("CANCEL".into());
        let cancelled = engine.cancel_order(99999);
        assert!(cancelled.is_none(), "Should return None for non-existent order");
    }

    #[test]
    fn test_cancel_from_middle_of_price_level() {
        let mut engine = MatchingEngine::new("CANCEL".into());
        // Add 3 orders at same price
        let (_id1, _) = engine.submit_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
        let (id2, _) = engine.submit_order(Order::new(0, 100.0, 2.0, Side::Buy, OrderType::Limit, 2));
        let (_id3, _) = engine.submit_order(Order::new(0, 100.0, 3.0, Side::Buy, OrderType::Limit, 3));
        
        // Cancel middle order
        let cancelled = engine.cancel_order(id2);
        assert!(cancelled.is_some());
        assert_eq!(cancelled.unwrap().quantity, 2.0);
        
        // Remaining orders should still be there
        let (bids, _) = engine.book.depth(10);
        assert_eq!(bids.len(), 1);
        assert_eq!(bids[0].1, 4.0, "Remaining qty should be 1+3=4"); // 1.0 + 3.0
    }

    #[test]
    fn test_cancel_after_partial_fill() {
        let mut engine = MatchingEngine::new("CANCEL".into());
        // Sell 1.0 at 100
        engine.submit_order(Order::new(0, 100.0, 1.0, Side::Sell, OrderType::Limit, 1));
        // Buy 3.0 at 100 — partial fill
        let (buy_id, trades) = engine.submit_order(Order::new(0, 100.0, 3.0, Side::Buy, OrderType::Limit, 2));
        
        assert_eq!(trades.len(), 1);
        assert_eq!(trades[0].quantity, 1.0);
        
        // Cancel remaining 2.0
        let cancelled = engine.cancel_order(buy_id);
        assert!(cancelled.is_some());
        assert_eq!(cancelled.unwrap().remaining(), 2.0);
        assert!(engine.book.best_bid().is_none());
    }

    #[test]
    fn test_cancel_all_orders_in_book() {
        let mut engine = MatchingEngine::new("CANCEL".into());
        let mut ids = Vec::new();
        
        for i in 0..100 {
            let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
            let price = if i % 2 == 0 { 100.0 + i as f64 } else { 200.0 + i as f64 };
            let (id, _) = engine.submit_order(Order::new(0, price, 1.0, side, OrderType::Limit, i as u64 + 1));
            ids.push(id);
        }
        
        for id in ids {
            assert!(engine.cancel_order(id).is_some(), "Should cancel order {}", id);
        }
        
        let (bids, asks) = engine.book.depth(100);
        assert!(bids.is_empty() && asks.is_empty(), "Book should be empty");
    }

    // ── Market Order Tests ───────────────────────────────────────

    #[test]
    fn test_market_buy_order() {
        let mut engine = MatchingEngine::new("MARKET".into());
        engine.submit_order(Order::new(0, 100.0, 1.0, Side::Sell, OrderType::Limit, 1));
        engine.submit_order(Order::new(0, 101.0, 1.0, Side::Sell, OrderType::Limit, 2));
        
        let (_, trades) = engine.submit_order(Order::new(0, 0.0, 1.5, Side::Buy, OrderType::Market, 3));
        
        assert_eq!(trades.len(), 2, "Market buy should match both asks");
        assert_eq!(trades[0].price, 100.0);
        assert_eq!(trades[1].price, 101.0);
    }

    #[test]
    fn test_market_sell_order() {
        let mut engine = MatchingEngine::new("MARKET".into());
        engine.submit_order(Order::new(0, 100.0, 1.0, Side::Buy, OrderType::Limit, 1));
        engine.submit_order(Order::new(0, 99.0, 1.0, Side::Buy, OrderType::Limit, 2));
        
        let (_, trades) = engine.submit_order(Order::new(0, 0.0, 1.5, Side::Sell, OrderType::Market, 3));
        
        assert_eq!(trades.len(), 2, "Market sell should match both bids");
        assert_eq!(trades[0].price, 100.0);
        assert_eq!(trades[1].price, 99.0);
    }

    #[test]
    fn test_market_order_no_liquidity() {
        let mut engine = MatchingEngine::new("MARKET".into());
        let (_, trades) = engine.submit_order(Order::new(0, 0.0, 1.0, Side::Buy, OrderType::Market, 1));
        assert!(trades.is_empty(), "Market order with no liquidity should not trade");
    }

    // ── Additional Benchmarks ────────────────────────────────────

    #[test]
    fn benchmark_cancel_10k() {
        let mut engine = MatchingEngine::new("BENCH_CANCEL".into());
        let mut ids = Vec::with_capacity(10_000);
        
        for i in 0..10_000 {
            let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
            let price = 50000.0 + (i % 1000) as f64;
            let (id, _) = engine.submit_order(Order::new(0, price, 0.1, side, OrderType::Limit, i as u64 + 1));
            ids.push(id);
        }
        
        let start = std::time::Instant::now();
        for id in ids {
            engine.cancel_order(id);
        }
        let elapsed = start.elapsed();
        let ops_per_sec = 10_000.0 / elapsed.as_secs_f64();
        
        println!("\n═══ CANCEL BENCHMARK ═══");
        println!("  10,000 cancels in {:?}", elapsed);
        println!("  Throughput: {:.0} cancels/sec", ops_per_sec);
        println!("  Avg latency: {:.2} μs/cancel", elapsed.as_micros() as f64 / 10_000.0);
        println!("═════════════════════════\n");
        
        assert!(ops_per_sec > 5_000.0, "Should exceed 5k cancels/sec");
    }

    #[test]
    fn benchmark_snapshot_1k_depth() {
        let mut engine = MatchingEngine::new("BENCH_SNAP".into());
        
        for i in 0..10_000 {
            let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
            let price = 50000.0 + (i % 1000) as f64;
            engine.submit_order(Order::new(0, price, 0.1, side, OrderType::Limit, i as u64 + 1));
        }
        
        let start = std::time::Instant::now();
        for _ in 0..1_000 {
            let _ = engine.snapshot(100);
        }
        let elapsed = start.elapsed();
        let ops_per_sec = 1_000.0 / elapsed.as_secs_f64();
        
        println!("\n═══ SNAPSHOT BENCHMARK ═══");
        println!("  1,000 snapshots (depth=100) in {:?}", elapsed);
        println!("  Throughput: {:.0} snapshots/sec", ops_per_sec);
        println!("  Avg latency: {:.2} μs/snapshot", elapsed.as_micros() as f64 / 1_000.0);
        println!("═══════════════════════════\n");
        
        assert!(ops_per_sec > 1_000.0, "Should exceed 1k snapshots/sec");
    }

    #[test]
    fn benchmark_mixed_workload() {
        let mut engine = MatchingEngine::new("BENCH_MIXED".into());
        let mut ids = Vec::with_capacity(5_000);
        
        // Seed with 5k orders
        for i in 0..5_000 {
            let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
            let price = 50000.0 + (i % 500) as f64;
            let (id, _) = engine.submit_order(Order::new(0, price, 0.1, side, OrderType::Limit, i as u64 + 1));
            ids.push(id);
        }
        
        let start = std::time::Instant::now();
        for i in 0..10_000 {
            match i % 4 {
                0 | 1 => {
                    let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
                    let price = 50000.0 + (i % 500) as f64;
                    let (id, _) = engine.submit_order(Order::new(0, price, 0.1, side, OrderType::Limit, 5000 + i as u64 + 1));
                    ids.push(id);
                }
                2 => {
                    if let Some(id) = ids.pop() {
                        engine.cancel_order(id);
                    }
                }
                _ => {
                    let _ = engine.snapshot(50);
                }
            }
        }
        let elapsed = start.elapsed();
        let ops_per_sec = 10_000.0 / elapsed.as_secs_f64();
        
        println!("\n═══ MIXED WORKLOAD BENCHMARK ═══");
        println!("  10,000 mixed ops in {:?}", elapsed);
        println!("  Throughput: {:.0} ops/sec", ops_per_sec);
        println!("  Avg latency: {:.2} μs/op", elapsed.as_micros() as f64 / 10_000.0);
        println!("  Final book orders: {}", ids.len());
        println!("═════════════════════════════════\n");
        
        assert!(ops_per_sec > 5_000.0, "Should exceed 5k mixed ops/sec");
    }

    fn fast_rng() -> u64 {
        std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos() as u64
    }
}
