"""
小天量化 v2.0 — 交易引擎主控
编排器：连接所有组件，管理生命周期

组件拓扑:
  EventBus ← 数据总线
  ├── Database (SQLite)
  ├── OrderManager (OMS)
  ├── RiskManager
  ├── PortfolioManager
  ├── Exchange adapters
  ├── Strategy runner
  ├── Factor pipeline
  └── Web server
"""

from __future__ import annotations
import asyncio
import json
import time
from typing import Dict, Optional, Any

from .event import EventBus, EventType, MarketEvent
from .clock import Clock
from .data import Tick, OrderBook
from ..utils.logging import get_logger

logger = get_logger("xtquant.engine")


class TradingEngine:
    """
    v2.0 交易引擎 — 精简编排器

    职责:
      - 组件注册与生命周期管理
      - 统一下单入口（委托给OMS）
      - 数据缓存与查询
      - 统计聚合
    """

    def __init__(self, config: dict = None, clock: Clock = None):
        self.config = config or {}
        self.clock = clock or Clock(mode="live")

        # 核心组件
        self.event_bus = EventBus()
        self.db: Optional[Any] = None
        self.oms: Optional[Any] = None
        self.risk_manager: Optional[Any] = None
        self.portfolio: Optional[Any] = None
        self.factor_pipeline = None
        self.notifier = None

        # 交易所 & 策略
        self.exchanges: Dict[str, Any] = {}
        self.strategies: Dict[str, Any] = {}

        # 策略管理 repos
        self.strategy_config_repo: Optional[Any] = None
        self.strategy_log_repo: Optional[Any] = None
        self.strategy_trade_repo: Optional[Any] = None
        self.strategy_global_repo: Optional[Any] = None

        # 缓存
        self._tick_cache: Dict[str, Tick] = {}
        self._book_cache: Dict[str, OrderBook] = {}
        self._last_prices: Dict[str, float] = {}

        # 状态
        self._running = False
        self._start_time: float = 0
        self._health_check_task: Optional[asyncio.Task] = None

        # 注册内部事件
        self.event_bus.subscribe(EventType.TICK, self._on_tick, priority=0)
        self.event_bus.subscribe(EventType.ORDERBOOK, self._on_book, priority=0)
        self.event_bus.subscribe(EventType.RISK_ALERT, self._on_risk_alert, priority=-50)

    # ============================================================
    #  组件注册
    # ============================================================

    def register_db(self, db):
        self.db = db
        logger.info("[Engine] 数据库已注册")

    def register_strategy_repos(self, config_repo, log_repo, trade_repo, global_repo):
        self.strategy_config_repo = config_repo
        self.strategy_log_repo = log_repo
        self.strategy_trade_repo = trade_repo
        self.strategy_global_repo = global_repo
        logger.info("[Engine] 策略管理仓库已注册")

    def register_oms(self, oms):
        self.oms = oms
        logger.info("[Engine] OMS 已注册")

    def register_risk(self, risk_manager):
        self.risk_manager = risk_manager
        self.risk_manager.set_event_bus(self.event_bus)
        self.event_bus.subscribe(EventType.SIGNAL, risk_manager.check, priority=-100)
        logger.info("[Engine] 风控管理器已注册")

    def register_portfolio(self, portfolio):
        self.portfolio = portfolio
        portfolio._exchanges = self.exchanges
        logger.info("[Engine] 仓位管理器已注册")

    def register_exchange(self, exchange):
        from ..exchanges.base import BaseExchange
        if not isinstance(exchange, BaseExchange):
            raise TypeError("exchange must be BaseExchange")
        self.exchanges[exchange.name] = exchange
        exchange.set_event_bus(self.event_bus)
        if self.portfolio:
            self.portfolio._exchanges = self.exchanges
        logger.info(f"[Engine] 交易所已注册: {exchange.name}")

    async def reload_exchanges(self, exchanges_config: dict):
        """Hot-reload exchanges with new config without full restart"""
        all_symbols = list(set(
            s for st in self.strategies.values() for s in st.symbols
        ))
        if not all_symbols:
            all_symbols = ["BTCUSDT", "ETHUSDT"]

        # Stop and remove old exchanges
        for name, exch in list(self.exchanges.items()):
            try:
                await exch.stop()
            except Exception as e:
                logger.warning(f"[Engine] error stopping exchange {name}: {e}")
            del self.exchanges[name]
            logger.info(f"[Engine] exchange removed: {name}")

        # Create new exchanges
        for name, cfg in exchanges_config.items():
            if not cfg.get("enabled", True):
                continue

            if name == "binance":
                from ..exchanges.binance import BinanceExchange
                ex = BinanceExchange(
                    api_key=cfg.get("api_key", ""),
                    secret=cfg.get("secret", ""),
                    testnet=cfg.get("testnet", True),
                    futures=cfg.get("futures", False),
                )
                self.register_exchange(ex)
            elif name == "okx":
                from ..exchanges.okx import OKXExchange
                ex = OKXExchange(
                    api_key=cfg.get("api_key", ""),
                    secret=cfg.get("secret", ""),
                    passphrase=cfg.get("passphrase", ""),
                    testnet=cfg.get("testnet", True),
                )
                self.register_exchange(ex)

        # Reconnect market data (non-fatal if it fails)
        connected = []
        failed = []
        if all_symbols:
            for name, exch in self.exchanges.items():
                if hasattr(exch, 'connect_market_data'):
                    try:
                        await exch.connect_market_data(all_symbols)
                        connected.append(name)
                    except Exception as e:
                        logger.warning(f"[Engine] exchange {name} market data failed: {e}")
                        failed.append(name)

        logger.info(f"[Engine] exchanges reloaded: connected={connected}, failed={failed}")
        return {"exchanges": list(self.exchanges.keys()), "connected": connected, "failed": failed, "symbols": all_symbols}

    def register_strategy(self, strategy):
        from ..strategy.base import BaseStrategy
        if not isinstance(strategy, BaseStrategy):
            raise TypeError("strategy must be BaseStrategy")
        self.strategies[strategy.name] = strategy
        strategy.set_event_bus(self.event_bus)
        strategy.set_engine(self)
        logger.info(f"[Engine] 策略已注册: {strategy.name}")

    async def start_strategy(self, name: str) -> dict:
        if name not in self.strategies:
            return {"error": f"strategy '{name}' not found"}
        st = self.strategies[name]
        if st._running:
            return {"status": "already_running", "name": name}
        await st.start()
        return {"status": "started", "name": name}

    async def stop_strategy(self, name: str) -> dict:
        if name not in self.strategies:
            return {"error": f"strategy '{name}' not found"}
        st = self.strategies[name]
        if not st._running:
            return {"status": "already_stopped", "name": name}
        await st.stop()
        return {"status": "stopped", "name": name}

    def get_strategy_info(self, name: str = None) -> dict:
        if name:
            st = self.strategies.get(name)
            if not st:
                return {"error": f"strategy '{name}' not found"}
            return {
                "name": st.name, "running": st._running,
                "symbols": st.symbols, "trade_count": st.trade_count,
                "pnl": st.pnl,
            }
        return {
            name: {"running": st._running, "symbols": st.symbols,
                   "trade_count": st.trade_count, "pnl": st.pnl}
            for name, st in self.strategies.items()
        }

    def register_factor_pipeline(self, pipeline):
        self.factor_pipeline = pipeline
        # 订阅因子计算
        self.event_bus.subscribe(EventType.TICK, self._on_factor_tick, priority=100)
        self.event_bus.subscribe(EventType.ORDERBOOK, self._on_factor_book, priority=100)
        self.event_bus.subscribe(EventType.BAR, self._on_factor_bar, priority=100)
        logger.info("[Engine] 因子管道已注册")

    def register_notifier(self, notifier):
        self.notifier = notifier
        logger.info("[Engine] 通知管理器已注册")

    # ============================================================
    #  统一下单入口
    # ============================================================

    async def place_order(self, exchange: str, symbol: str, side: str,
                          order_type: str, price: float, quantity: float,
                          strategy: str = "", **kwargs) -> Optional[Any]:
        """
        统一下单入口 — 委托给OMS

        策略调用此方法，实际由OMS完成风控→执行→交易所
        """
        if not self.oms:
            logger.error("[Engine] OMS未初始化，无法下单")
            return None

        from ..order.types import OrderRequest, OrderSide, OrderType

        try:
            req = OrderRequest(
                symbol=symbol,
                side=OrderSide(side.upper()),
                order_type=OrderType(order_type.upper()),
                price=price,
                quantity=quantity,
                exchange=exchange or list(self.exchanges.keys())[0],
                strategy=strategy,
                **kwargs,
            )
            return await self.oms.submit(req)
        except ValueError as e:
            logger.error(f"[Engine] 下单参数错误: {e}")
            return None

    async def cancel_order(self, exchange: str, order_id: str, symbol: str) -> bool:
        """撤销订单"""
        exch = self.exchanges.get(exchange)
        if not exch:
            return False
        return await exch.cancel_order(order_id, symbol)

    # ============================================================
    #  数据查询
    # ============================================================

    def get_price(self, symbol: str) -> float:
        """获取最新价格"""
        if symbol in self._last_prices:
            return self._last_prices[symbol]
        if symbol in self._tick_cache:
            return self._tick_cache[symbol].price
        if symbol in self._book_cache:
            return self._book_cache[symbol].mid_price()
        return 0.0

    def get_orderbook(self, symbol: str):
        return self._book_cache.get(symbol)

    def get_tick(self, symbol: str):
        return self._tick_cache.get(symbol)

    # ============================================================
    #  内部事件处理
    # ============================================================

    async def _on_tick(self, event: MarketEvent):
        """Tick 缓存 + 风控更新"""
        d = event.data
        tick = Tick(
            event.exchange, event.symbol, d["price"], d["volume"],
            event.timestamp, d.get("bid", 0), d.get("ask", 0)
        )
        self._tick_cache[event.symbol] = tick
        self._last_prices[event.symbol] = d["price"]

        # 更新风控价格
        if self.risk_manager:
            self.risk_manager.update_price(event.symbol, d["price"])

    async def _on_book(self, event: MarketEvent):
        """OrderBook 缓存"""
        d = event.data
        self._book_cache[event.symbol] = OrderBook(
            event.symbol, event.exchange,
            d["bids"], d["asks"], event.timestamp
        )

    async def _on_risk_alert(self, event: MarketEvent):
        """风控告警处理"""
        data = event.data
        logger.warning(f"[Engine] 风控告警: {data.get('check')} - {data.get('detail')}")
        if self.notifier:
            await self.notifier.notify(
                f"风控告警: {data.get('check')}",
                data.get('detail', ''),
                "WARNING"
            )

    async def _on_factor_tick(self, event: MarketEvent):
        if self.factor_pipeline:
            self.factor_pipeline.on_event(event)

    async def _on_factor_book(self, event: MarketEvent):
        if self.factor_pipeline:
            self.factor_pipeline.on_event(event)

    async def _on_factor_bar(self, event: MarketEvent):
        if self.factor_pipeline:
            self.factor_pipeline.on_event(event)

    # ============================================================
    #  生命周期
    # ============================================================

    async def start(self):
        """启动引擎"""
        self._running = True
        self._start_time = time.time()

        # 1. EventBus
        await self.event_bus.start()

        # 2. 数据库
        if self.db:
            await self.db.connect()

        # 3. OMS
        if self.oms:
            await self.oms.initialize()
            await self.oms.start()

        # 4. 风控
        if self.risk_manager:
            await self.risk_manager.initialize()
            await self.risk_manager.start()

        # 5. 仓位管理
        if self.portfolio:
            await self.portfolio.initialize()
            await self.portfolio.start()

        # 6. 通知
        if self.notifier:
            await self.notifier.start()

        # 7. 连接交易所行情
        all_symbols = list(set(
            s for st in self.strategies.values() for s in st.symbols
        ))
        if not all_symbols:
            all_symbols = ["BTCUSDT", "ETHUSDT"]
        for name, exch in self.exchanges.items():
            if hasattr(exch, 'connect_market_data'):
                await exch.connect_market_data(all_symbols)

        # 8. 启动策略
        for name, st in self.strategies.items():
            await st.start()

        # 9. 健康检查
        self._health_check_task = asyncio.create_task(self._health_check_loop())

        # 10. 启动通知
        if self.notifier:
            await self.notifier.notify(
                "小天量化 v2.0 启动",
                f"交易所: {list(self.exchanges.keys())}\n"
                f"策略: {list(self.strategies.keys())}\n"
                f"风控: {'启用' if self.risk_manager else '未启用'}",
                "INFO"
            )

        logger.info("=" * 60)
        logger.info("  小天量化 v2.0 已启动")
        logger.info(f"  交易所: {list(self.exchanges.keys())}")
        logger.info(f"  策略: {list(self.strategies.keys())}")
        logger.info(f"  OMS: {'已连接' if self.oms else '未连接'}")
        logger.info(f"  风控: {'已连接' if self.risk_manager else '未连接'}")
        logger.info(f"  仓位管理: {'已连接' if self.portfolio else '未连接'}")
        logger.info("=" * 60)

    async def stop(self):
        """停止引擎"""
        self._running = False

        if self._health_check_task:
            self._health_check_task.cancel()

        # 逆序停止
        for st in self.strategies.values():
            await st.stop()
        for exch in self.exchanges.values():
            await exch.stop()

        if self.portfolio:
            await self.portfolio.stop()
        if self.oms:
            await self.oms.stop()
        if self.risk_manager:
            await self.risk_manager.stop()
        if self.notifier:
            await self.notifier.notify("小天量化 v2.0 停止", "系统已安全关闭", "INFO")
            await self.notifier.stop()

        await self.event_bus.stop()

        if self.db:
            await self.db.close()

        logger.info("[Engine] 系统已安全关闭")

    async def _health_check_loop(self):
        """定期健康检查"""
        while self._running:
            try:
                await self._run_health_checks()
                await asyncio.sleep(15)
            except asyncio.CancelledError:
                break

    async def _run_health_checks(self):
        """执行健康检查"""
        # 检查交易所连接
        for name, exch in self.exchanges.items():
            if not exch.is_connected:
                logger.warning(f"[Health] 交易所 {name} 未连接")

        # 检查OMS
        if self.oms and not self.oms.is_running:
            logger.error("[Health] OMS 未运行!")

        # 更新风控上下文
        if self.portfolio and self.risk_manager:
            self.risk_manager.update_positions(self.portfolio.get_positions_summary())
            self.risk_manager.update_equity(self.portfolio.total_equity)
            self.risk_manager.update_balance(self.portfolio.available_balance)
            self.risk_manager.update_margin(
                self.portfolio.margin_used,
                self.portfolio.margin_ratio,
            )

    # ============================================================
    #  统计
    # ============================================================

    def get_stats(self) -> dict:
        runtime = time.time() - self._start_time if self._start_time else 0
        portfolio_stats = self.portfolio.get_stats() if self.portfolio else {}
        equity_curve = []
        if self.portfolio:
            snapshots = getattr(self.portfolio, '_equity_snapshots', [])
            if snapshots:
                equity_curve = [{"time": ts, "equity": eq} for ts, eq in snapshots[-200:]]
            elif portfolio_stats.get("total_equity"):
                try:
                    total_eq = float(portfolio_stats["total_equity"])
                    equity_curve = [{"time": int(time.time()), "equity": total_eq}]
                except (ValueError, TypeError):
                    pass
        return {
            "running": self._running,
            "runtime_seconds": round(runtime, 2),
            "exchanges": list(self.exchanges.keys()),
            "exchange_count": len(self.exchanges),
            "strategies": list(self.strategies.keys()),
            "strategy_count": len(self.strategies),
            "event_bus": self.event_bus.get_stats(),
            "oms": self.oms.get_stats() if self.oms else {},
            "risk": self.risk_manager.get_stats() if self.risk_manager else {},
            "portfolio": portfolio_stats,
            "equity_curve": equity_curve,
        }

    # ============================================================
    #  回测 API
    # ============================================================

    _last_backtest_result: dict = {}

    async def run_backtest_web(self, config: dict) -> dict:
        """Run backtest from web UI and return results"""
        import numpy as np
        from ..exchanges.backtest import BacktestExchange, BacktestConfig
        from ..core.data import Bar
        from strategies.market_making import SimpleBreakoutStrategy

        symbol = config.get("symbol", "BTCUSDT")
        initial_balance = config.get("initial_balance", 100000)
        if not isinstance(initial_balance, dict):
            initial_balance = {"USDT": float(initial_balance)}
        fee_rate = config.get("fee_rate", 0.001)
        slippage = config.get("slippage", 0.0005)
        strategy_name = config.get("strategy", "breakout")
        strategy_params = config.get("params", {})
        num_bars = config.get("num_bars", 500)

        bt_config = BacktestConfig(
            initial_balance=initial_balance,
            fee_rate=fee_rate,
            slippage=slippage,
        )
        backtest = BacktestExchange(bt_config)
        backtest.set_event_bus(self.event_bus)

        # Fetch real kline data from Binance
        interval_map = {"1m": "1m", "5m": "5m", "15m": "15m", "30m": "30m",
                        "1h": "1h", "4h": "4h", "1d": "1d", "1w": "1w"}
        interval = interval_map.get(config.get("interval", "1h"), "1h")
        bars = await self._fetch_binance_klines(symbol, interval, num_bars)

        if not bars:
            logger.warning(f"[Backtest] No real data for {symbol}, using fallback simulation")
            bars = self._generate_fallback_bars(symbol, num_bars, config.get("base_price", 68000))

        backtest.load_bars(symbol, bars)

        # Create strategy
        if strategy_name == "breakout":
            strategy = SimpleBreakoutStrategy(
                [symbol],
                period=strategy_params.get("period", 20),
                qty=strategy_params.get("qty", 0.001),
            )
        elif strategy_name == "market_making":
            from strategies.market_making import MarketMakingStrategy
            strategy = MarketMakingStrategy(
                [symbol],
                base_spread=strategy_params.get("base_spread", 0.002),
                qty=strategy_params.get("qty", 0.001),
            )
        elif strategy_name == "grid":
            from strategies.grid_trading import GridTradingStrategy
            strategy = GridTradingStrategy(
                symbols=[symbol],
                upper_price=strategy_params.get("upper_price", 72000),
                lower_price=strategy_params.get("lower_price", 64000),
                grid_num=strategy_params.get("grid_num", 10),
                qty_per_grid=strategy_params.get("qty", 0.001),
            )
        else:
            return {"error": f"unknown strategy: {strategy_name}"}

        strategy.set_event_bus(self.event_bus)
        strategy.set_engine(self)

        # Run backtest
        await backtest.connect_market_data([symbol])
        await strategy.start()

        # Feed bars one by one with a small delay
        bar_index = 0
        while backtest._running and bar_index < len(bars):
            # Simulate tick from bar
            bar = bars[bar_index]
            tick_data = {
                "exchange": "BACKTEST",
                "symbol": symbol,
                "price": bar.close,
                "bid": bar.close * 0.9999,
                "ask": bar.close * 1.0001,
                "bid_size": 1.0,
                "ask_size": 1.0,
                "volume": bar.volume,
                "timestamp": bar.timestamp,
            }
            # Directly call strategy's on_tick
            from ..core.data import Tick
            tick = Tick(
                exchange="BACKTEST", symbol=symbol,
                price=bar.close, bid=bar.close * 0.9999, ask=bar.close * 1.0001,
                volume=bar.volume,
                timestamp=bar.timestamp,
            )
            await strategy.on_tick(tick)
            bar_index += 1
            if bar_index % 50 == 0:
                await asyncio.sleep(0.01)

        await asyncio.sleep(0.1)
        await strategy.stop()
        await backtest.stop()

        # Compute performance report
        from ..backtest.stats import compute_performance_report
        equity = [e[1] for e in backtest.equity_curve] if backtest.equity_curve else [initial_balance.get("USDT", 100000)]
        if not equity:
            equity = [initial_balance.get("USDT", 100000)]
        report = compute_performance_report(equity, backtest.trades)

        # Sanitize inf/nan for JSON
        import math as _math
        def _sanitize(obj):
            if isinstance(obj, dict):
                return {k: _sanitize(v) for k, v in obj.items()}
            if isinstance(obj, list):
                return [_sanitize(v) for v in obj]
            if isinstance(obj, float):
                if _math.isinf(obj) or _math.isnan(obj):
                    return None
            return obj

        result = {
            "report": _sanitize(report),
            "equity_curve": [{"index": i, "equity": e[1]} for i, e in enumerate(backtest.equity_curve)],
            "trades": _sanitize(backtest.trades),
            "config": config,
        }
        self._last_backtest_result = result
        return result

    async def run_backtest_ai(self, config: dict, strategy_class: type) -> dict:
        """Run backtest for an AI-generated strategy class. Returns same format as run_backtest_web."""
        from ..exchanges.backtest import BacktestExchange, BacktestConfig
        from ..core.data import Bar, Tick
        from ..backtest.stats import compute_performance_report

        symbol = config.get("symbol", "BTCUSDT")
        initial_balance = config.get("initial_balance", 100000)
        if not isinstance(initial_balance, dict):
            initial_balance = {"USDT": float(initial_balance)}
        fee_rate = config.get("fee_rate", 0.001)
        slippage = config.get("slippage", 0.0005)
        strategy_params = config.get("params", {})
        num_bars = config.get("num_bars", 500)

        bt_config = BacktestConfig(
            initial_balance=initial_balance,
            fee_rate=fee_rate,
            slippage=slippage,
        )
        backtest = BacktestExchange(bt_config)
        backtest.set_event_bus(self.event_bus)

        # Fetch real kline data from Binance
        interval_map = {"1m": "1m", "5m": "5m", "15m": "15m", "30m": "30m",
                        "1h": "1h", "4h": "4h", "1d": "1d", "1w": "1w"}
        interval = interval_map.get(config.get("interval", "1h"), "1h")
        bars = await self._fetch_binance_klines(symbol, interval, num_bars)

        if not bars:
            logger.warning(f"[Backtest] No real data for {symbol}, using fallback simulation")
            bars = self._generate_fallback_bars(symbol, num_bars, config.get("base_price", 68000))

        num_bars = len(bars)
        logger.info(f"[Backtest] AI backtest using real Binance data: {num_bars} bars for {symbol}")

        backtest.load_bars(symbol, bars)

        # Instantiate AI-generated strategy
        strategy = strategy_class([symbol], **strategy_params)
        strategy.set_event_bus(self.event_bus)
        strategy.set_engine(self)

        # Run backtest
        await backtest.connect_market_data([symbol])
        await strategy.start()

        # Feed bars one by one
        for bar in bars:
            tick = Tick(
                exchange="BACKTEST", symbol=symbol,
                price=bar.close, bid=bar.close * 0.9999, ask=bar.close * 1.0001,
                volume=bar.volume, timestamp=bar.timestamp,
            )
            await strategy.on_tick(tick)

        await asyncio.sleep(0.1)
        await strategy.stop()
        await backtest.stop()

        # Compute performance report
        equity = [e[1] for e in backtest.equity_curve] if backtest.equity_curve else [initial_balance.get("USDT", 100000)]
        if not equity:
            equity = [initial_balance.get("USDT", 100000)]
        report = compute_performance_report(equity, backtest.trades)

        # Sanitize inf/nan for JSON
        import math as _math
        def _sanitize(obj):
            if isinstance(obj, dict):
                return {k: _sanitize(v) for k, v in obj.items()}
            if isinstance(obj, list):
                return [_sanitize(v) for v in obj]
            if isinstance(obj, float):
                if _math.isinf(obj) or _math.isnan(obj):
                    return None
            return obj

        result = {
            "report": _sanitize(report),
            "equity_curve": [{"index": i, "equity": e[1]} for i, e in enumerate(backtest.equity_curve)],
            "trades": _sanitize(backtest.trades),
            "config": config,
        }
        self._last_backtest_result = result
        return result

    def get_backtest_result(self) -> dict:
        return self._last_backtest_result

    async def _fetch_binance_klines(self, symbol: str, interval: str, limit: int) -> list:
        """Fetch real kline bars from Binance REST API."""
        from ..core.data import Bar
        try:
            import urllib.request
            url = f"https://api.binance.com/api/v3/klines?symbol={symbol}&interval={interval}&limit={limit}"
            req = urllib.request.Request(url)
            req.add_header("Accept", "application/json")
            resp = await asyncio.to_thread(urllib.request.urlopen, req, None, 10)
            raw = json.loads(resp.read().decode("utf-8"))
            bars = []
            for k in raw:
                bars.append(Bar(
                    "BINANCE", symbol, interval,
                    open=float(k[1]), high=float(k[2]), low=float(k[3]),
                    close=float(k[4]), volume=float(k[5]),
                    quote_volume=float(k[7]) if len(k) > 7 else 0,
                    timestamp=k[0],
                ))
            logger.info(f"[Backtest] Fetched {len(bars)} real klines for {symbol} {interval} from Binance")
            return bars
        except Exception as e:
            logger.error(f"[Backtest] Binance fetch failed for {symbol}: {e}")
            return []

    def _generate_fallback_bars(self, symbol: str, count: int, base_price: float = 68000) -> list:
        """Generate simulated bars as fallback when real data is unavailable."""
        import numpy as np
        from ..core.data import Bar
        bars = []
        for i in range(count):
            noise = float(np.random.normal(0, base_price * 0.003))
            drift = float(i * base_price * 0.0001)
            price = max(base_price + noise + drift, base_price * 0.5)
            bars.append(Bar(
                "BACKTEST", symbol, "1h",
                price - 50, price + 100, price - 100, price,
                100 + i, quote_volume=price * (100 + i),
                timestamp=int(time.time() * 1000) - (count - i) * 3600000,
            ))
        logger.warning(f"[Backtest] Generated {len(bars)} simulated bars for {symbol}")
        return bars

    # ============================================================
    #  Bridge: 兼容 v1.0 接口
    # ============================================================

    def add_exchange(self, exchange):
        """兼容旧接口"""
        self.register_exchange(exchange)

    def add_strategy(self, strategy):
        """兼容旧接口"""
        self.register_strategy(strategy)

    def set_risk_manager(self, risk_manager):
        """兼容旧接口"""
        self.register_risk(risk_manager)

    def set_notifier(self, notifier):
        """兼容旧接口"""
        self.register_notifier(notifier)

    def set_factor_pipeline(self, pipeline):
        """兼容旧接口"""
        self.register_factor_pipeline(pipeline)

    def get_current_price(self, symbol: str) -> float:
        """兼容旧接口"""
        return self.get_price(symbol)
