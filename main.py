#!/usr/bin/env python3
"""
小天量化 v2.0 — 主入口

启动方式:
  python main.py                     # 默认实盘模式
  python main.py --mode backtest     # 回测模式
  python main.py --config config.yaml

v2.0 新特性:
  - SQLite 状态持久化（崩溃恢复）
  - OMS 订单管理中心
  - 14 级风控检查链
  - 仓位管理 + 对账
  - 结构化日志
  - FastAPI Web 监控面板
"""

import asyncio
import argparse
from pathlib import Path

from xtquant.core.engine import TradingEngine
from xtquant.core.clock import Clock
from xtquant.db.database import Database
from xtquant.order.oms import OrderManager
from xtquant.risk.manager import RiskManager
from xtquant.portfolio.manager import PortfolioManager
from xtquant.exchanges.binance import BinanceExchange
from xtquant.exchanges.okx import OKXExchange
from xtquant.exchanges.backtest import BacktestExchange, BacktestConfig
from xtquant.factors.technical import (
    RSIFactor, MACDFactor, OrderBookImbalanceFactor,
    SpreadFactor, VWAPFactor, MomentumFactor, VolatilityFactor
)
from xtquant.factors.base import FactorPipeline
from xtquant.notify.notifier import NotificationManager, LogNotifier
from xtquant.utils.logging import setup_logging
from xtquant.utils.watchdog import Watchdog
from xtquant.core.data import Bar

from strategies.market_making import MarketMakingStrategy, SimpleBreakoutStrategy

try:
    from strategies.ai_strategy import RLStrategy
except ImportError:
    RLStrategy = None

try:
    from strategies.torch_strategy import TorchStrategy
except ImportError:
    TorchStrategy = None

from config.schema import load_config


class XiaoTianQuant:
    """小天量化交易 v2.0"""

    def __init__(self):
        self.config = None
        self.clock = None
        self.db = None
        self.engine = None
        self.web_server = None
        self.watchdog = Watchdog(max_restarts_per_minute=3)
        self._shutdown_event = asyncio.Event()

    def init(self, config_path: str = "config.yaml", mode: str = "live"):
        """初始化系统"""
        # 1. 加载配置
        self.config = load_config(config_path)
        sys_cfg = self.config.system.__dict__ if hasattr(self.config.system, '__dict__') else {}

        # 2. 日志
        setup_logging(
            level=sys_cfg.get("log_level", "INFO"),
            fmt=sys_cfg.get("log_format", "json"),
            service_name="xtquant",
        )

        import logging
        logger = logging.getLogger("xtquant.main")

        # 3. 时钟
        self.clock = Clock(mode=mode)

        # 4. 数据库
        data_dir = Path(sys_cfg.get("data_dir", "./data"))
        data_dir.mkdir(parents=True, exist_ok=True)
        self.db = Database(str(data_dir / "xtquant.db"))

        # 5. 引擎
        self.engine = TradingEngine(config=self.config.__dict__, clock=self.clock)

        # 6. OMS
        oms = OrderManager(self.engine.event_bus, self.db)
        self.engine.register_oms(oms)
        self.engine.register_db(self.db)

        # 6.5 策略管理仓库
        from xtquant.db.strategy_repo import (
            StrategyConfigRepository, StrategyLogRepository,
            StrategyTradeLogRepository, StrategyGlobalRepository
        )
        self.engine.register_strategy_repos(
            StrategyConfigRepository(self.db),
            StrategyLogRepository(self.db),
            StrategyTradeLogRepository(self.db),
            StrategyGlobalRepository(self.db),
        )

        # 7. 风控
        risk_config = self.config.risk.__dict__ if self.config.risk else {}
        risk = RiskManager(config=risk_config, db=self.db)
        self.engine.register_risk(risk)

        # 8. 仓位管理
        portfolio = PortfolioManager(db=self.db)
        self.engine.register_portfolio(portfolio)

        # 9. 通知
        notify_cfg = self.config.notify
        if notify_cfg and getattr(notify_cfg, "enabled", False):
            notifier = NotificationManager()
            notifier.add_notifier(LogNotifier())
            self.engine.register_notifier(notifier)

        # 10. 因子管道
        pipeline = FactorPipeline()
        pipeline.add_factor(RSIFactor(period=14))
        pipeline.add_factor(MACDFactor(fast=12, slow=26))
        pipeline.add_factor(OrderBookImbalanceFactor(depth=5))
        pipeline.add_factor(SpreadFactor())
        pipeline.add_factor(VWAPFactor(window=20))
        pipeline.add_factor(MomentumFactor(period=20))
        pipeline.add_factor(VolatilityFactor(period=20))
        self.engine.register_factor_pipeline(pipeline)

        # 11. 交易所
        self._init_exchanges(mode)

        # 12. 策略
        self._init_strategies()

        # 13. Web 服务器
        if mode == "live":
            self._init_web()

        logger.info("v2.0 初始化完成")

    def _init_exchanges(self, mode: str):
        import logging
        logger = logging.getLogger("xtquant.main")

        if mode == "backtest":
            bt_cfg = self.config.backtest
            conf = BacktestConfig(
                initial_balance=bt_cfg.initial_balance if hasattr(bt_cfg, 'initial_balance') else {"USDT": 100000},
                fee_rate=bt_cfg.fee_rate if hasattr(bt_cfg, 'fee_rate') else 0.001,
                slippage=bt_cfg.slippage if hasattr(bt_cfg, 'slippage') else 0.0005,
            )
            self.engine.register_exchange(BacktestExchange(conf))
            return

        exchanges_cfg = getattr(self.config, 'exchanges', {})
        if not exchanges_cfg:
            return

        for name, cfg in exchanges_cfg.items():
            if not hasattr(cfg, 'enabled') or not cfg.enabled:
                continue

            if name == "binance":
                ex = BinanceExchange(
                    api_key=cfg.api_key,
                    secret=cfg.secret,
                    testnet=cfg.testnet,
                    futures=getattr(cfg, 'futures', False),
                )
                self.engine.register_exchange(ex)
                logger.info(f"[Main] 币安交易所已注册 (testnet={cfg.testnet})")

            elif name == "okx":
                ex = OKXExchange(
                    api_key=cfg.api_key,
                    secret=cfg.secret,
                    passphrase=getattr(cfg, 'passphrase', ''),
                    testnet=cfg.testnet,
                )
                self.engine.register_exchange(ex)
                logger.info(f"[Main] OKX交易所已注册 (testnet={cfg.testnet})")

    def _init_strategies(self):
        import logging
        logger = logging.getLogger("xtquant.main")

        strat_cfg = getattr(self.config, 'strategies', {})

        if strat_cfg:
            mm_cfg = getattr(strat_cfg, 'market_making', None)
            if mm_cfg and getattr(mm_cfg, 'enabled', False):
                self.engine.register_strategy(MarketMakingStrategy(
                    symbols=mm_cfg.symbols,
                    base_spread=mm_cfg.params.get("base_spread", 0.002),
                    qty=mm_cfg.qty,
                ))
                logger.info("[Main] 做市策略已注册")

            bo_cfg = getattr(strat_cfg, 'breakout', None)
            if bo_cfg and getattr(bo_cfg, 'enabled', False):
                self.engine.register_strategy(SimpleBreakoutStrategy(
                    symbols=bo_cfg.symbols,
                    period=bo_cfg.params.get("period", 20),
                    qty=bo_cfg.qty,
                ))
                logger.info("[Main] 突破策略已注册")

    def _init_web(self):
        import logging
        logger = logging.getLogger("xtquant.main")

        web_cfg = getattr(self.config, 'web', None)
        if not web_cfg or not getattr(web_cfg, 'enabled', False):
            return
        try:
            from xtquant.web.app import WebServer
            self.web_server = WebServer(
                self.engine,
                host=web_cfg.host,
                port=web_cfg.port,
            )
            logger.info(f"[Main] Web服务已配置: {web_cfg.host}:{web_cfg.port}")
        except ImportError:
            logger.warning("[Main] FastAPI未安装，Web服务不可用")

    async def run_backtest(self):
        """运行回测模式"""
        import logging
        logger = logging.getLogger("xtquant.main")
        import numpy as np
        import time as _time

        logger.info("=" * 60)
        logger.info("回测模式 v2.0")
        logger.info("=" * 60)

        # 生成模拟K线
        bars = []
        base_price = 68000
        for i in range(500):
            noise = np.random.normal(0, 200)
            trend = i * 5
            price = base_price + noise + trend
            bars.append(Bar(
                "BACKTEST", "BTCUSDT", "1m",
                price - 50, price + 100, price - 100, price,
                100 + i, quote_volume=price * (100 + i),
                timestamp=int(_time.time() * 1000) + i * 60000
            ))

        backtest = list(self.engine.exchanges.values())[0]
        backtest.load_bars("BTCUSDT", bars)

        strategy = SimpleBreakoutStrategy(["BTCUSDT"], period=20, qty=0.001)
        self.engine.register_strategy(strategy)

        await self.engine.start()

        while backtest._running:
            await asyncio.sleep(0.5)

        # 绩效报告
        from xtquant.backtest.stats import compute_performance_report
        equity = [e[1] for e in backtest.equity_curve]
        report = compute_performance_report(equity, backtest.trades)

        logger.info("=" * 60)
        logger.info("回测绩效报告")
        logger.info("=" * 60)
        for k, v in report.items():
            logger.info(f"  {k}: {v}")
        logger.info("=" * 60)

    async def run_live(self):
        """运行实盘模式"""
        import logging
        logger = logging.getLogger("xtquant.main")

        logger.info("=" * 60)
        logger.info("  小天量化 v2.0 — 实盘模式")
        logger.info("=" * 60)

        await self.engine.start()

        if self.web_server:
            await self.web_server.start()

        await self.watchdog.start()

        logger.info("系统运行中... 按 Ctrl+C 停止")
        await self.watchdog.run_until_shutdown()

    async def shutdown(self):
        """优雅关闭"""
        import logging
        logger = logging.getLogger("xtquant.main")
        logger.info("正在关闭系统...")

        if self.web_server:
            await self.web_server.stop()
        if self.engine:
            await self.engine.stop()
        if self.watchdog:
            await self.watchdog.stop()

        logger.info("系统已安全关闭")


async def main():
    parser = argparse.ArgumentParser(description="小天量化交易 v2.0")
    parser.add_argument("--mode", choices=["backtest", "live"], default="live")
    parser.add_argument("--config", default="config.yaml")
    args = parser.parse_args()

    app = XiaoTianQuant()
    app.init(args.config, args.mode)

    try:
        if args.mode == "backtest":
            await app.run_backtest()
        else:
            await app.run_live()
    except KeyboardInterrupt:
        pass
    finally:
        await app.shutdown()


if __name__ == "__main__":
    asyncio.run(main())
