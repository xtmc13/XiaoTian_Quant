"""
小天量化交易 - 回测引擎
事件驱动仿真，支持滑点、手续费、延迟模拟
同一套策略代码回测和实盘无缝切换
"""

import asyncio
import time
import random
from typing import Dict, List, Optional
from collections import defaultdict, deque
from dataclasses import dataclass, field
import logging
import numpy as np

from .base import BaseExchange
from ..core.event import MarketEvent, EventType
from ..core.data import Tick, OrderBook, OrderData, Position, Bar
from ..backtest.stats import (
    sharpe_ratio, max_drawdown as stats_max_dd,
    win_rate, profit_factor, compute_performance_report, compute_returns,
)

logger = logging.getLogger("xtquant.backtest")


@dataclass
class BacktestConfig:
    """回测配置"""
    initial_balance: Dict[str, float] = field(default_factory=lambda: {"USDT": 100000.0})
    fee_rate: float = 0.001              # 手续费率 (0.1%)
    slippage: float = 0.0005             # 滑点 (0.05%)
    slippage_model: str = "fixed"        # fixed / proportional / random
    delay_ms: int = 100                  # 下单延迟模拟
    price_impact: float = 0.0            # 大额订单价格冲击
    allow_short: bool = False            # 是否允许做空
    margin_rate: float = 1.0             # 保证金率


class BacktestExchange(BaseExchange):
    """
    事件驱动回测交易所

    核心能力:
      • 按时间戳顺序回放历史数据
      • 模拟真实交易的滑点和手续费
      • 追踪权益曲线和绩效指标
      • 生成详细的交易记录
    """

    def __init__(self, config: BacktestConfig = None):
        super().__init__("BACKTEST")
        self.config = config or BacktestConfig()

        # 账户状态
        self.balance = dict(self.config.initial_balance)
        self.positions: Dict[str, float] = defaultdict(float)
        self.position_cost: Dict[str, float] = defaultdict(float)

        # 订单管理
        self.orders: Dict[str, OrderData] = {}
        self.open_orders: Dict[str, OrderData] = {}
        self.order_counter = 0

        # 历史记录
        self.trades: List[dict] = []
        self.equity_curve: List[tuple] = []  # (timestamp, equity)
        self.drawdown_curve: List[tuple] = []
        self.returns: List[float] = []

        # 回放控制
        self._events: List[MarketEvent] = []
        self._event_idx = 0
        self._playback_task: Optional[asyncio.Task] = None
        self._playback_speed = 0.0           # 0 = 最快速度, 1.0 = 真实时间
        self._current_time = 0

        # 市场数据缓存
        self._last_prices: Dict[str, float] = {}
        self._last_books: Dict[str, OrderBook] = {}
        self._bar_history: Dict[str, deque] = defaultdict(lambda: deque(maxlen=1000))

        # 统计
        self._peak_equity = 0.0
        self._total_fees = 0.0
        self._total_slippage = 0.0

    def load_bars(self, symbol: str, bars: List[Bar]):
        """从Bar列表加载历史数据"""
        for bar in bars:
            self._events.append(bar.to_event())
            self._bar_history[symbol].append(bar)
        self._events.sort(key=lambda e: e.timestamp)
        logger.info(f"[Backtest] 加载 {symbol} Bar数据: {len(bars)} 条")

    def load_ticks(self, symbol: str, ticks: List[Tick]):
        """从Tick列表加载历史数据"""
        for tick in ticks:
            self._events.append(tick.to_event())
        self._events.sort(key=lambda e: e.timestamp)
        logger.info(f"[Backtest] 加载 {symbol} Tick数据: {len(ticks)} 条")

    def load_csv(self, symbol: str, filepath: str, event_type: str = "bar"):
        """从CSV加载历史数据"""
        import csv
        from pathlib import Path

        path = Path(filepath)
        if not path.exists():
            logger.warning(f"[Backtest] 文件不存在: {filepath}")
            return

        count = 0
        with open(path, 'r') as f:
            reader = csv.DictReader(f)
            for row in reader:
                ts = int(row.get("timestamp", row.get("ts", time.time()*1000)))
                if event_type == "bar":
                    bar = Bar(
                        "BACKTEST", symbol, row.get("interval", "1m"),
                        float(row["open"]), float(row["high"]), float(row["low"]),
                        float(row["close"]), float(row["volume"]),
                        float(row.get("quote_volume", 0)),
                        int(row.get("trade_count", 0)), ts
                    )
                    self._events.append(bar.to_event())
                    self._bar_history[symbol].append(bar)
                elif event_type == "tick":
                    tick = Tick("BACKTEST", symbol, float(row["price"]),
                               float(row["volume"]), ts)
                    self._events.append(tick.to_event())
                count += 1

        self._events.sort(key=lambda e: e.timestamp)
        logger.info(f"[Backtest] 从CSV加载 {symbol}: {count} 条记录")

    async def connect_market_data(self, symbols: List[str]):
        """启动回放"""
        self._running = True
        self._playback_task = asyncio.create_task(self._playback_loop())
        logger.info(f"[Backtest] 回放启动: {len(self._events)} 个事件")

    async def _playback_loop(self):
        """事件回放主循环"""
        last_ts = 0

        for event in self._events:
            if not self._running:
                break

            # 时间流逝模拟
            if last_ts > 0 and self._playback_speed > 0:
                delay = (event.timestamp - last_ts) / 1000.0 / self._playback_speed
                if delay > 0:
                    await asyncio.sleep(min(delay, 0.05))  # 上限50ms
            last_ts = event.timestamp
            self._current_time = event.timestamp

            # 更新市场数据缓存
            if event.event_type == EventType.TICK:
                self._last_prices[event.symbol] = event.data["price"]
            elif event.event_type == EventType.ORDERBOOK:
                self._last_books[event.symbol] = OrderBook(
                    event.symbol, event.exchange,
                    event.data["bids"], event.data["asks"], event.timestamp
                )
            elif event.event_type == EventType.BAR:
                self._last_prices[event.symbol] = event.data["close"]
                self._bar_history[event.symbol].append(
                    Bar(event.exchange, event.symbol, event.data["interval"],
                        event.data["open"], event.data["high"], event.data["low"],
                        event.data["close"], event.data["volume"],
                        event.data.get("quote_volume", 0),
                        event.data.get("trade_count", 0), event.timestamp)
                )

            # 检查条件单和止盈止损
            await self._check_conditional_orders(event.symbol, event.data.get("price", event.data.get("close", 0)))

            # 发布事件
            self._emit(event)

            # 更新权益
            self._update_equity()

        self._running = False
        logger.info("[Backtest] 回放结束")

        # 最终权益计算
        self._update_equity()

        # 打印绩效报告
        report = self.get_performance_report()
        logger.info("=" * 60)
        logger.info("回测绩效报告")
        logger.info("=" * 60)
        for k, v in report.items():
            logger.info(f"  {k}: {v}")

    async def _check_conditional_orders(self, symbol: str, price: float):
        """检查条件单触发"""
        to_remove = []
        for order_id, order in self.open_orders.items():
            if order.symbol != symbol:
                continue

            triggered = False
            if order.order_type == "STOP_LOSS" and order.side == "SELL" and price <= order.price:
                triggered = True
            elif order.order_type == "TAKE_PROFIT" and order.side == "SELL" and price >= order.price:
                triggered = True
            elif order.order_type == "STOP_LOSS" and order.side == "BUY" and price >= order.price:
                triggered = True
            elif order.order_type == "TAKE_PROFIT" and order.side == "BUY" and price <= order.price:
                triggered = True

            if triggered:
                to_remove.append(order_id)
                # 模拟市价成交
                await self._simulate_fill(order, price)

        for oid in to_remove:
            del self.open_orders[oid]

    async def place_order(self, symbol: str, side: str, order_type: str,
                         price: float, quantity: float, client_id: str = "", **kwargs) -> OrderData:
        """模拟下单"""
        # 延迟模拟
        if self.config.delay_ms > 0:
            await asyncio.sleep(self.config.delay_ms / 1000.0)

        self.order_counter += 1
        order_id = f"BT_{self.order_counter}_{int(time.time()*1000)}"

        # Conditional orders: register and wait for price trigger
        if order_type in ("STOP_LOSS", "TAKE_PROFIT", "STOP_LOSS_LIMIT", "TAKE_PROFIT_LIMIT"):
            order = OrderData(
                order_id=order_id,
                exchange="BACKTEST",
                symbol=symbol,
                side=side,
                order_type=order_type,
                price=price,
                quantity=quantity,
                status="NEW",
                filled_qty=0.0,
                remaining_qty=quantity,
                avg_fill_price=0.0,
                fee=0.0,
                timestamp=self._current_time or int(time.time()*1000),
                client_order_id=client_id
            )
            self.orders[order_id] = order
            self.open_orders[order_id] = order
            self._emit(order.to_event())
            return order

        # Market/Limit orders: immediate execution
        exec_price = self._calculate_exec_price(symbol, side, price, quantity)
        notional = exec_price * quantity
        fee = notional * self.config.fee_rate
        slip = exec_price - price if side == "BUY" else price - exec_price
        self._total_fees += fee
        self._total_slippage += abs(slip) * quantity

        base = self._get_base_asset(symbol)
        quote = "USDT"

        if side == "BUY":
            cost = notional + fee
            if self.balance.get(quote, 0) < cost:
                raise Exception(f"回测余额不足: 需要 {cost:.4f} {quote}, 拥有 {self.balance.get(quote, 0):.4f}")
            self.balance[quote] = self.balance.get(quote, 0) - cost
            self.positions[base] += quantity
            old_qty = self.positions[base] - quantity
            old_cost = self.position_cost[base]
            self.position_cost[base] = (old_cost * old_qty + exec_price * quantity) / self.positions[base] if self.positions[base] > 0 else 0
        else:
            if not self.config.allow_short and self.positions.get(base, 0) < quantity:
                raise Exception(f"回测持仓不足: 需要 {quantity}, 拥有 {self.positions.get(base, 0)}")
            self.positions[base] -= quantity
            self.balance[quote] = self.balance.get(quote, 0) + notional - fee

        order = OrderData(
            order_id=order_id,
            exchange="BACKTEST",
            symbol=symbol,
            side=side,
            order_type=order_type,
            price=exec_price,
            quantity=quantity,
            status="FILLED",
            filled_qty=quantity,
            remaining_qty=0.0,
            avg_fill_price=exec_price,
            fee=fee,
            timestamp=self._current_time or int(time.time()*1000),
            client_order_id=client_id
        )

        self.orders[order_id] = order

        self.trades.append({
            "timestamp": order.timestamp,
            "symbol": symbol,
            "side": side,
            "price": exec_price,
            "qty": quantity,
            "fee": fee,
            "slippage": slip,
            "notional": notional
        })

        self._emit(order.to_event())
        self._update_equity()
        return order

    def _calculate_exec_price(self, symbol: str, side: str, price: float, quantity: float) -> float:
        """计算考虑滑点的执行价格"""
        if self.config.slippage_model == "fixed":
            slip = price * self.config.slippage
        elif self.config.slippage_model == "proportional":
            # 根据订单量占市场深度的比例调整滑点
            slip = price * self.config.slippage * (1 + quantity * 0.01)
        elif self.config.slippage_model == "random":
            slip = price * self.config.slippage * random.uniform(0.5, 1.5)
        else:
            slip = 0

        # 价格冲击
        if self.config.price_impact > 0:
            impact = price * self.config.price_impact * quantity * 0.001
            slip += impact

        return price + slip if side == "BUY" else price - slip

    async def cancel_order(self, order_id: str, symbol: str) -> bool:
        """撤单"""
        if order_id in self.open_orders:
            order = self.open_orders[order_id]
            order.status = "CANCELLED"
            self._emit(order.to_event())
            del self.open_orders[order_id]
            return True
        return False

    async def get_balance(self) -> Dict[str, float]:
        return dict(self.balance)

    async def get_position(self, symbol: str) -> Optional[Position]:
        base = self._get_base_asset(symbol)
        size = self.positions.get(base, 0)
        if size == 0:
            return None

        last_price = self._last_prices.get(symbol, 0)
        entry = self.position_cost.get(base, last_price)
        pnl = (last_price - entry) * size if size > 0 else (entry - last_price) * abs(size)

        return Position(
            exchange="BACKTEST",
            symbol=symbol,
            side="LONG" if size > 0 else "SHORT",
            size=abs(size),
            entry_price=entry,
            mark_price=last_price,
            unrealized_pnl=pnl,
            timestamp=self._current_time
        )

    async def get_open_orders(self, symbol: str) -> List[OrderData]:
        return [o for o in self.open_orders.values() if o.symbol == symbol]

    def _get_base_asset(self, symbol: str) -> str:
        """从交易对提取基础资产"""
        return symbol.replace("USDT", "").replace("-USDT", "").replace("_USDT", "")

    def _update_equity(self):
        """更新权益曲线"""
        total = sum(self.balance.values())
        for base, qty in self.positions.items():
            sym = f"{base}USDT"
            if sym in self._last_prices:
                total += qty * self._last_prices[sym]

        self.equity_curve.append((self._current_time or int(time.time()*1000), total))

        if total > self._peak_equity:
            self._peak_equity = total

        dd = (self._peak_equity - total) / self._peak_equity if self._peak_equity > 0 else 0
        self.drawdown_curve.append((self._current_time or int(time.time()*1000), dd))

    def get_performance_report(self) -> dict:
        """生成回测绩效报告 (使用 stats.py 统一计算)"""
        if not self.equity_curve:
            return {"error": "无权益数据"}

        equity = [e[1] for e in self.equity_curve]
        initial = equity[0]
        final = equity[-1]

        # 用 stats.py 计算所有指标
        returns = compute_returns(equity)
        sharpe = sharpe_ratio(returns) if len(returns) > 1 else 0.0
        dd, _, _ = stats_max_dd(equity)

        # 配对交易计算盈亏
        paired_trades = self._pair_trades()
        wr = win_rate(paired_trades)
        pf = profit_factor(paired_trades)

        total_return = (final - initial) / initial if initial > 0 else 0

        buy_trades = [t for t in self.trades if t["side"] == "BUY"]
        sell_trades = [t for t in self.trades if t["side"] == "SELL"]

        gross_pnl = sum(t["notional"] * (-1 if t["side"] == "BUY" else 1) for t in self.trades)
        net_pnl = gross_pnl - self._total_fees

        return {
            "initial_equity": f"{initial:.2f}",
            "final_equity": f"{final:.2f}",
            "total_return": f"{total_return*100:.2f}%",
            "net_pnl": f"{net_pnl:.2f}",
            "sharpe_ratio": f"{sharpe:.2f}",
            "max_drawdown": f"{dd*100:.2f}%",
            "total_trades": len(self.trades),
            "buy_trades": len(buy_trades),
            "sell_trades": len(sell_trades),
            "total_fees": f"{self._total_fees:.4f}",
            "total_slippage": f"{self._total_slippage:.4f}",
            "avg_trade_size": f"{np.mean([t['notional'] for t in self.trades]):.2f}" if self.trades else "0",
            "win_rate": f"{wr*100:.2f}%",
            "profit_factor": f"{pf:.3f}",
        }

    def _pair_trades(self) -> list:
        """将买卖订单配对为完整的交易记录"""
        paired = []
        buys = [t for t in self.trades if t["side"] == "BUY"]
        sells = [t for t in self.trades if t["side"] == "SELL"]

        for i in range(min(len(buys), len(sells))):
            buy_notional = buys[i]["notional"]
            sell_notional = sells[i]["notional"]
            pnl = sell_notional - buy_notional
            paired.append({
                "pnl": pnl,
                "entry_time": buys[i].get("timestamp", 0),
                "exit_time": sells[i].get("timestamp", 0),
            })
        return paired

    def get_equity_curve(self) -> List[tuple]:
        return self.equity_curve

    def get_drawdown_curve(self) -> List[tuple]:
        return self.drawdown_curve

    def get_trade_log(self) -> List[dict]:
        return self.trades

    def get_bar_history(self, symbol: str) -> deque:
        return self._bar_history[symbol]
