use criterion::{black_box, criterion_group, criterion_main, Criterion, BenchmarkId};
use xt_matching::matching::MatchingEngine;
use xt_matching::orderbook::{Order, OrderType, Side};

/// Build a limit order with deterministic parameters.
fn make_limit_order(id: u64, price: f64, qty: f64, side: Side) -> Order {
    Order::new(id, price, qty, side, OrderType::Limit, 1)
}

/// Benchmark: submit orders without any matching (all on same side, different prices).
fn bench_submit_no_match(c: &mut Criterion) {
    let mut group = c.benchmark_group("submit_order_no_match");
    for size in [100, 1_000, 10_000].iter() {
        group.bench_with_input(BenchmarkId::from_parameter(size), size, |b, &size| {
            b.iter_with_setup(
                || MatchingEngine::new("BTCUSDT".to_string()),
                |mut engine| {
                    for i in 0..size {
                        let price = 50_000.0 + (i as f64) * 10.0;
                        let order = make_limit_order(i as u64 + 1, price, 0.1, Side::Buy);
                        let _ = engine.submit_order(black_box(order));
                    }
                },
            );
        });
    }
    group.finish();
}

/// Benchmark: submit orders with 100% matching (alternating buy/sell at same price).
fn bench_submit_full_match(c: &mut Criterion) {
    let mut group = c.benchmark_group("submit_order_full_match");
    for size in [100, 1_000, 10_000].iter() {
        group.bench_with_input(BenchmarkId::from_parameter(size), size, |b, &size| {
            b.iter_with_setup(
                || MatchingEngine::new("BTCUSDT".to_string()),
                |mut engine| {
                    for i in 0..size {
                        let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
                        let order = make_limit_order(i as u64 + 1, 50_000.0, 0.1, side);
                        let _ = engine.submit_order(black_box(order));
                    }
                },
            );
        });
    }
    group.finish();
}

/// Benchmark: cancel orders from a pre-populated book.
fn bench_cancel_order(c: &mut Criterion) {
    let mut group = c.benchmark_group("cancel_order");
    for size in [100, 1_000, 10_000].iter() {
        group.bench_with_input(BenchmarkId::from_parameter(size), size, |b, &size| {
            b.iter_with_setup(
                || {
                    let mut engine = MatchingEngine::new("BTCUSDT".to_string());
                    let mut ids = Vec::with_capacity(size);
                    for i in 0..size {
                        let price = 50_000.0 + (i as f64) * 10.0;
                        let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
                        let order = make_limit_order(i as u64 + 1, price, 0.1, side);
                        let (id, _) = engine.submit_order(order);
                        ids.push(id);
                    }
                    (engine, ids)
                },
                |(mut engine, ids)| {
                    for id in ids {
                        let _ = engine.cancel_order(black_box(id));
                    }
                },
            );
        });
    }
    group.finish();
}

/// Benchmark: order book snapshot at various depths.
fn bench_snapshot(c: &mut Criterion) {
    let mut group = c.benchmark_group("snapshot");
    
    // Pre-populate engine with 10k orders
    let mut engine = MatchingEngine::new("BTCUSDT".to_string());
    for i in 0..10_000 {
        let price = 50_000.0 + (i as f64) * 10.0;
        let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
        let order = make_limit_order(i as u64 + 1, price, 0.1, side);
        engine.submit_order(order);
    }
    
    for depth in [10, 100, 1_000].iter() {
        group.bench_with_input(BenchmarkId::from_parameter(depth), depth, |b, &depth| {
            b.iter(|| {
                let _ = engine.snapshot(black_box(depth));
            });
        });
    }
    group.finish();
}

/// Benchmark: mixed workload (50% submit, 25% cancel, 25% snapshot).
fn bench_mixed_workload(c: &mut Criterion) {
    c.bench_function("mixed_workload_10k", |b| {
        b.iter_with_setup(
            || {
                let mut engine = MatchingEngine::new("BTCUSDT".to_string());
                let mut ids = Vec::with_capacity(5_000);
                // Seed with 5k orders
                for i in 0..5_000 {
                    let price = 50_000.0 + (i as f64) * 10.0;
                    let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
                    let order = make_limit_order(i as u64 + 1, price, 0.1, side);
                    let (id, _) = engine.submit_order(order);
                    ids.push(id);
                }
                (engine, ids)
            },
            |(mut engine, mut ids)| {
                for i in 0..10_000 {
                    match i % 4 {
                        0 | 1 => {
                            // Submit new order
                            let price = 50_000.0 + (i as f64) * 0.1;
                            let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
                            let order = make_limit_order(5_000 + i as u64 + 1, price, 0.1, side);
                            let (id, _) = engine.submit_order(order);
                            ids.push(id);
                        }
                        2 => {
                            // Cancel random existing order
                            if let Some(idx) = ids.pop() {
                                let _ = engine.cancel_order(idx);
                            }
                        }
                        _ => {
                            // Snapshot
                            let _ = engine.snapshot(100);
                        }
                    }
                }
            },
        );
    });
}

/// Benchmark: best bid/ask query.
fn bench_best_bid_ask(c: &mut Criterion) {
    let mut engine = MatchingEngine::new("BTCUSDT".to_string());
    for i in 0..10_000 {
        let price = 50_000.0 + (i as f64) * 10.0;
        let side = if i % 2 == 0 { Side::Buy } else { Side::Sell };
        let order = make_limit_order(i as u64 + 1, price, 0.1, side);
        engine.submit_order(order);
    }
    
    c.bench_function("best_bid_ask", |b| {
        b.iter(|| {
            let _ = black_box(engine.book.best_bid());
            let _ = black_box(engine.book.best_ask());
        });
    });
}

criterion_group!(
    benches,
    bench_submit_no_match,
    bench_submit_full_match,
    bench_cancel_order,
    bench_snapshot,
    bench_mixed_workload,
    bench_best_bid_ask
);
criterion_main!(benches);
