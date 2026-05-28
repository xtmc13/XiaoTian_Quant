"""
配置校验 — Pydantic模型，启动时校验config.yaml的完整性和正确性
"""

from __future__ import annotations
from typing import List, Dict, Literal
from pydantic import BaseModel, Field, field_validator


class SystemConfig(BaseModel):
    name: str = "XiaoTianQuant"
    version: str = "2.0.0"
    log_level: Literal["DEBUG", "INFO", "WARNING", "ERROR"] = "INFO"
    log_format: Literal["text", "json"] = "json"
    data_dir: str = "./data"
    watchdog: WatchdogConfig = Field(default_factory=lambda: WatchdogConfig())


class WatchdogConfig(BaseModel):
    enabled: bool = False
    max_restarts_per_minute: int = Field(default=3, ge=1, le=10)
    heartbeat_interval_sec: float = Field(default=5.0, ge=1.0)


class ExchangeConfig(BaseModel):
    enabled: bool = True
    testnet: bool = True
    api_key: str = ""
    secret: str = ""
    passphrase: str = ""
    futures: bool = False
    max_retries: int = Field(default=3, ge=0, le=10)
    request_timeout_sec: float = Field(default=10.0, ge=1.0)
    rate_limit_per_sec: float = Field(default=10.0, ge=0.1)

    @field_validator("api_key")
    @classmethod
    def warn_empty_key(cls, v):
        if not v:
            import warnings
            warnings.warn("API key is empty — exchange will not connect private channels")
        return v


class RiskConfig(BaseModel):
    max_order_usdt: float = Field(default=10000.0, gt=0)
    max_daily_orders: int = Field(default=100, gt=0)
    max_position_pct: float = Field(default=0.5, gt=0, le=1.0)
    max_net_exposure_pct: float = Field(default=0.8, gt=0, le=2.0)
    max_drawdown_pct: float = Field(default=0.1, gt=0, le=0.5)
    price_deviation_pct: float = Field(default=0.05, gt=0)
    price_spike_pct: float = Field(default=0.03, gt=0)
    volatility_threshold: float = Field(default=0.02, gt=0)
    max_consecutive_losses: int = Field(default=5, gt=0)
    max_concurrent_orders: int = Field(default=5, gt=0)
    order_rate_limit_sec: float = Field(default=1.0, ge=0.1)
    min_margin_ratio: float = Field(default=1.5, ge=1.0)
    funding_rate_warn: float = Field(default=0.00375, gt=0)
    profit_protection_enabled: bool = False
    blacklist: List[str] = Field(default_factory=list)
    circuit_breaker_enabled: bool = True


class NotifyChannel(BaseModel):
    type: Literal["log", "email", "lark", "wechat", "dingtalk"]
    webhook_url: str = ""
    smtp_host: str = ""
    smtp_port: int = 587
    sender: str = ""
    password: str = ""
    receivers: List[str] = Field(default_factory=list)


class NotifyConfig(BaseModel):
    enabled: bool = True
    channels: List[NotifyChannel] = Field(default_factory=lambda: [
        NotifyChannel(type="log")
    ])
    alert_escalation_sec: float = Field(default=300.0, ge=60.0)


class WebConfig(BaseModel):
    enabled: bool = True
    host: str = "0.0.0.0"
    port: int = Field(default=8080, ge=1, le=65535)
    jwt_secret: str = ""
    jwt_expire_hours: int = Field(default=24, gt=0)
    cors_origins: List[str] = Field(default_factory=lambda: ["*"])


class StrategyConfig(BaseModel):
    enabled: bool = True
    symbols: List[str] = Field(default_factory=lambda: ["BTCUSDT"])
    qty: float = Field(default=0.001, gt=0)
    params: Dict = Field(default_factory=dict)


class StrategiesConfig(BaseModel):
    market_making: StrategyConfig = Field(default_factory=StrategyConfig)
    breakout: StrategyConfig = Field(default_factory=StrategyConfig)
    grid_trading: StrategyConfig = Field(default_factory=StrategyConfig)
    arbitrage: StrategyConfig = Field(default_factory=StrategyConfig)
    torch: StrategyConfig = Field(default_factory=StrategyConfig)


class AgentAIConfig(BaseModel):
    provider: str = ""
    api_key: str = ""
    base_url: str = ""
    model: str = ""
    proxy_enabled: bool = False
    http_proxy: str = ""
    https_proxy: str = ""


class AgentConfig(BaseModel):
    live_trading_enabled: bool = False
    ai: AgentAIConfig = Field(default_factory=AgentAIConfig)


class BacktestConfig(BaseModel):
    initial_balance: Dict[str, float] = Field(default_factory=lambda: {"USDT": 100000.0})
    fee_rate: float = Field(default=0.001, ge=0)
    slippage: float = Field(default=0.0005, ge=0)
    slippage_model: Literal["fixed", "proportional", "random"] = "fixed"
    delay_ms: int = Field(default=100, ge=0)


class AIProviderConfig(BaseModel):
    api_key: str = ""
    base_url: str = ""
    model: str = ""


class AIConfig(BaseModel):
    deepseek: AIProviderConfig = Field(default_factory=AIProviderConfig)
    openai: AIProviderConfig = Field(default_factory=AIProviderConfig)
    qwen: AIProviderConfig = Field(default_factory=AIProviderConfig)


class MultiAgentConfig(BaseModel):
    enabled: bool = True
    max_iterations: int = Field(default=3, ge=1, le=10)


class Config(BaseModel):
    system: SystemConfig = Field(default_factory=SystemConfig)
    exchanges: Dict[str, ExchangeConfig] = Field(default_factory=lambda: {
        "binance": ExchangeConfig(),
        "okx": ExchangeConfig(),
    })
    risk: RiskConfig = Field(default_factory=RiskConfig)
    notify: NotifyConfig = Field(default_factory=NotifyConfig)
    web: WebConfig = Field(default_factory=WebConfig)
    strategies: StrategiesConfig = Field(default_factory=StrategiesConfig)
    backtest: BacktestConfig = Field(default_factory=BacktestConfig)
    agent: AgentConfig = Field(default_factory=AgentConfig)
    ai: AIConfig = Field(default_factory=AIConfig)
    multi_agent: MultiAgentConfig = Field(default_factory=MultiAgentConfig)
    default_ai_provider: str = "deepseek"
    default_exchange: str = "binance"


def _resolve_env(value):
    """递归解析 ${ENV_VAR} 占位符"""
    import os
    if isinstance(value, str) and value.startswith("${") and value.endswith("}"):
        return os.environ.get(value[2:-1], "")
    if isinstance(value, str):
        return value
    if isinstance(value, dict):
        return {k: _resolve_env(v) for k, v in value.items()}
    if isinstance(value, list):
        return [_resolve_env(v) for v in value]
    return value


def load_config(path: str = "config.yaml") -> Config:
    """加载并校验配置，自动解析 ${ENV_VAR} 环境变量占位符"""
    import yaml
    from pathlib import Path

    p = Path(path)
    raw: dict = {}
    if p.exists():
        with open(p, encoding="utf-8") as f:
            raw = yaml.safe_load(f) or {}
    raw = _resolve_env(raw)

    # 展平嵌套的 strategies 配置
    strategies_raw = raw.get("strategies", {})
    normalized = {
        "system": raw.get("system", {}),
        "exchanges": raw.get("exchanges", {}),
        "risk": raw.get("risk", {}),
        "notify": raw.get("notify", {}),
        "web": raw.get("web", {}),
        "strategies": {
            "market_making": strategies_raw.get("market_making", {}),
            "breakout": strategies_raw.get("breakout", {}),
            "grid_trading": strategies_raw.get("grid_trading", {}),
            "arbitrage": strategies_raw.get("arbitrage", {}),
            "torch": strategies_raw.get("torch", {}),
        },
        "backtest": raw.get("backtest", {}),
        "agent": raw.get("agent", {}),
        "ai": raw.get("ai", {}),
        "multi_agent": raw.get("multi_agent", {}),
        "default_ai_provider": raw.get("default_ai_provider", "deepseek"),
        "default_exchange": raw.get("default_exchange", "binance"),
    }
    return Config(**normalized)
