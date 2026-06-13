use std::collections::{BTreeMap, HashMap};

/// Order side
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(C)]
pub enum Side {
    Buy = 0,
    Sell = 1,
}

/// Order type
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(C)]
pub enum OrderType {
    Limit = 0,
    Market = 1,
}

/// Order status
#[derive(Debug, Clone, Copy, PartialEq, Eq)]
#[repr(C)]
pub enum OrderStatus {
    New = 0,
    PartiallyFilled = 1,
    Filled = 2,
    Cancelled = 3,
    Rejected = 4,
}

/// A single order in the book
#[derive(Debug, Clone)]
pub struct Order {
    pub id: u64,
    pub price: f64,
    pub quantity: f64,
    pub filled: f64,
    pub side: Side,
    pub order_type: OrderType,
    pub status: OrderStatus,
    pub timestamp: u64,
    pub user_id: u64,
}

impl Order {
    pub fn new(id: u64, price: f64, quantity: f64, side: Side, order_type: OrderType, user_id: u64) -> Self {
        Order {
            id,
            price,
            quantity,
            filled: 0.0,
            side,
            order_type,
            status: OrderStatus::New,
            timestamp: 0,
            user_id,
        }
    }

    pub fn remaining(&self) -> f64 {
        self.quantity - self.filled
    }

    pub fn is_done(&self) -> bool {
        matches!(self.status, OrderStatus::Filled | OrderStatus::Cancelled | OrderStatus::Rejected)
    }
}

/// Price level in the order book (bids: highest first, asks: lowest first)
#[derive(Debug, Clone)]
pub struct PriceLevel {
    pub price: f64,
    pub orders: Vec<Order>,
}

/// Trade result from matching
#[derive(Debug, Clone)]
pub struct Trade {
    pub id: u64,
    pub buy_order_id: u64,
    pub sell_order_id: u64,
    pub price: f64,
    pub quantity: f64,
    pub timestamp: u64,
}

/// The order book for a single symbol
pub struct OrderBook {
    pub symbol: String,
    /// Bids: price -> orders at that price. BTreeMap reversed (highest first via custom iteration).
    pub bids: BTreeMap<OrderedFloat, Vec<Order>>,
    /// Asks: price -> orders at that price. Natural order (lowest first).
    pub asks: BTreeMap<OrderedFloat, Vec<Order>>,
    /// Index for O(1) order lookup by ID: order_id -> (side, price_key, position_hint)
    order_index: HashMap<u64, (Side, OrderedFloat)>,
    pub trade_count: u64,
    pub order_count: u64,
}

/// Fixed-point price precision: 8 decimal places.
/// e.g. $68000.12345678 → 6800012345678
const PRICE_PRECISION: u64 = 100_000_000;

/// Wrapper for ordered fixed-point price keys in BTreeMap.
/// Replaces the previous f64::to_bits() approach which is unsafe for
/// ordering because NaN / -0 / +0 have non-unique bit patterns.
#[derive(Debug, Clone, Copy, PartialEq, Eq, PartialOrd, Ord)]
pub struct OrderedFloat(u64);

impl OrderedFloat {
    /// Convert an f64 price to a fixed-point OrderedFloat key.
    /// Panics if the price is NaN, infinite, or negative (for Sell side checks
    /// are done by the caller via can_match in the matching engine).
    pub fn from_price(price: f64, side: Side) -> Self {
        assert!(
            price.is_finite(),
            "price must be finite, got {}",
            price
        );
        let fixed = (price.abs() * PRICE_PRECISION as f64).round() as i64;
        // For buys: negate so higher price = smaller key → iterates first (descending).
        // For sells: keep as-is so higher price = larger key → iterates first (ascending).
        match side {
            Side::Buy => OrderedFloat((-fixed) as u64),
            Side::Sell => OrderedFloat(fixed as u64),
        }
    }

    /// Recover the original f64 price from the fixed-point key.
    pub fn to_price(&self, side: Side) -> f64 {
        let fixed = match side {
            Side::Buy => -(self.0 as i64),
            Side::Sell => self.0 as i64,
        };
        fixed as f64 / PRICE_PRECISION as f64
    }
}

impl OrderBook {
    pub fn new(symbol: String) -> Self {
        OrderBook {
            symbol,
            bids: BTreeMap::new(),
            asks: BTreeMap::new(),
            order_index: HashMap::new(),
            trade_count: 0,
            order_count: 0,
        }
    }

    /// Add an order to the book.
    /// If the order already has a non-zero ID (set by submit_order), that ID is used.
    /// Otherwise, a new ID is allocated.
    pub fn add_order(&mut self, mut order: Order) -> u64 {
        // If order.id is 0, it was never processed by submit_order → allocate a fresh ID.
        // If order.id > 0, submit_order already assigned it — preserve it.
        if order.id == 0 {
            self.order_count += 1;
            order.id = self.order_count;
        }
        let assigned_id = order.id;
        order.timestamp = std::time::SystemTime::now()
            .duration_since(std::time::UNIX_EPOCH)
            .unwrap()
            .as_nanos() as u64;

        let key = OrderedFloat::from_price(order.price, order.side);
        self.order_index.insert(assigned_id, (order.side, key));

        match order.side {
            Side::Buy => {
                self.bids.entry(key).or_default().push(order);
            }
            Side::Sell => {
                self.asks.entry(key).or_default().push(order);
            }
        }
        assigned_id
    }

    /// Remove an order by ID (O(log n) via index).
    pub fn cancel_order(&mut self, order_id: u64) -> Option<Order> {
        let &(side, key) = self.order_index.get(&order_id)?;
        let orders = match side {
            Side::Buy => self.bids.get_mut(&key)?,
            Side::Sell => self.asks.get_mut(&key)?,
        };
        let pos = orders.iter().position(|o| o.id == order_id)?;
        let mut order = orders.remove(pos);
        order.status = OrderStatus::Cancelled;
        self.order_index.remove(&order_id);
        // Clean up empty price level
        if orders.is_empty() {
            match side {
                Side::Buy => { self.bids.remove(&key); }
                Side::Sell => { self.asks.remove(&key); }
            }
        }
        Some(order)
    }

    /// Get best bid price (skips empty levels)
    pub fn best_bid(&self) -> Option<f64> {
        self.bids
            .iter()
            .find(|(_, orders)| !orders.is_empty())
            .map(|(k, _)| k.to_price(Side::Buy))
    }

    /// Get best ask price (skips empty levels)
    pub fn best_ask(&self) -> Option<f64> {
        self.asks
            .iter()
            .find(|(_, orders)| !orders.is_empty())
            .map(|(k, _)| k.to_price(Side::Sell))
    }

    /// Get spread
    pub fn spread(&self) -> Option<f64> {
        match (self.best_bid(), self.best_ask()) {
            (Some(bid), Some(ask)) => Some(ask - bid),
            _ => None,
        }
    }

    /// Get total quantity at bid/ask levels
    pub fn depth(&self, levels: usize) -> (Vec<(f64, f64)>, Vec<(f64, f64)>) {
        let bids: Vec<(f64, f64)> = self.bids
            .iter()
            .take(levels)
            .map(|(k, orders)| {
                let price = k.to_price(Side::Buy);
                let qty: f64 = orders.iter().map(|o| o.remaining()).sum();
                (price, qty)
            })
            .collect();

        let asks: Vec<(f64, f64)> = self.asks
            .iter()
            .take(levels)
            .map(|(k, orders)| {
                let price = k.to_price(Side::Sell);
                let qty: f64 = orders.iter().map(|o| o.remaining()).sum();
                (price, qty)
            })
            .collect();

        (bids, asks)
    }

    /// Clean up empty price levels
    pub fn cleanup(&mut self) {
        self.bids.retain(|_, orders| !orders.is_empty());
        self.asks.retain(|_, orders| !orders.is_empty());
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_order_creation() {
        let order = Order::new(1, 68000.0, 0.1, Side::Buy, OrderType::Limit, 100);
        assert_eq!(order.price, 68000.0);
        assert_eq!(order.remaining(), 0.1);
        assert!(!order.is_done());
    }

    #[test]
    fn test_order_book_add() {
        let mut ob = OrderBook::new("BTCUSDT".into());
        let buy = Order::new(0, 68000.0, 0.1, Side::Buy, OrderType::Limit, 1);
        let sell = Order::new(0, 68100.0, 0.1, Side::Sell, OrderType::Limit, 2);
        ob.add_order(buy);
        ob.add_order(sell);
        assert_eq!(ob.best_bid(), Some(68000.0));
        assert_eq!(ob.best_ask(), Some(68100.0));
        assert_eq!(ob.spread(), Some(100.0));
    }

    #[test]
    fn test_order_book_cancel() {
        let mut ob = OrderBook::new("ETHUSDT".into());
        let buy = Order::new(0, 3500.0, 1.0, Side::Buy, OrderType::Limit, 1);
        let id = ob.add_order(buy);
        let cancelled = ob.cancel_order(id);
        assert!(cancelled.is_some());
        assert_eq!(cancelled.unwrap().status, OrderStatus::Cancelled);
    }
}
