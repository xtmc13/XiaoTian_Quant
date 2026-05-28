"""
小天量化交易 (XiaoTian Quant)
================================================================================
一个融合Qbot架构的AI智能量化投研平台

核心特性:
  • 统一数据中间表达层 - 策略与交易所解耦
  • 事件驱动架构 - 异步高性能
  • 多交易所支持 - 币安/OKX/回测
  • AI策略框架 - 监督学习/强化学习/多因子
  • 企业级风控 - 6维风险拦截
  • 多渠道通知 - 邮件/飞书/微信/钉钉
  • 实时Web监控 - 轻量HTTP面板
  • 完整图表系统 - K线/深度/权益/因子可视化

作者: 小天量化团队
版本: 1.0.0
================================================================================
"""

__version__ = "1.0.0"
__author__ = "XiaoTian Quant Team"

from .core.event import EventBus, EventType, MarketEvent
from .core.data import Tick, OrderBook, Bar, OrderData, Balance, Position
from .core.engine import TradingEngine
from .exchanges.base import BaseExchange
from .exchanges.binance import BinanceExchange
from .exchanges.okx import OKXExchange
from .exchanges.backtest import BacktestExchange, BacktestConfig
from .strategy.base import BaseStrategy
from .factors.base import BaseFactor, FactorPipeline
from .factors.technical import *
from .chart.plotter import ChartPlotter, ChartData
from .notify.notifier import (
    NotificationManager, EmailNotifier, LarkNotifier,
    WechatNotifier, DingTalkNotifier, LogNotifier
)

__all__ = [
    # 核心
    "EventBus", "EventType", "MarketEvent",
    "Tick", "OrderBook", "Bar", "OrderData", "Balance", "Position",
    "TradingEngine",
    # 交易所
    "BaseExchange", "BinanceExchange", "OKXExchange",
    "BacktestExchange", "BacktestConfig",
    # 策略
    "BaseStrategy",
    # 因子
    "BaseFactor", "FactorPipeline",
    "RSIFactor", "MACDFactor", "OrderBookImbalanceFactor",
    "SpreadFactor", "VWAPFactor", "MomentumFactor", "VolatilityFactor",
    # 图表
    "ChartPlotter", "ChartData",
    # 通知
    "NotificationManager", "EmailNotifier", "LarkNotifier",
    "WechatNotifier", "DingTalkNotifier", "LogNotifier",
]
