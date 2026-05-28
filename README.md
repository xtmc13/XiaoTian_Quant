# 小天量化交易 (XiaoTian Quant) v2.0.0

> 融合Qbot架构的AI智能量化投研平台，支持币安/OKX，事件驱动，回测实盘一体。

## ✨ 特性

- **统一数据层** — 策略与交易所解耦
- **事件驱动** — 异步高性能EventBus
- **多交易所** — 币安/OKX统一接口
- **回测引擎** — 事件驱动仿真，滑点/手续费模拟
- **AI策略** — ML/RL策略框架
- **多因子** — 7个内置技术指标因子
- **企业风控** — 6维风险拦截
- **多渠道通知** — 邮件/飞书/微信/钉钉
- **Web监控** — 实时面板 + WebSocket推送
- **图表系统** — K线/权益/深度/因子可视化

## 🚀 快速开始

```bash
pip install -r requirements.txt
# 编辑 config.yaml 填入API密钥
python main.py              # 实盘
python main.py --mode backtest  # 回测
```

访问面板: http://localhost:8080

## ⚠️ 安全提示

1. 先用Testnet测试
2. 设置API IP白名单
3. 限制API权限
4. 小资金起步

## 📜 License

MIT
