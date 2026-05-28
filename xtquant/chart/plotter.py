"""
小天量化交易 - 图表绘制模块
融合Qbot的图表能力:
  • K线图 + 成交量
  • 订单簿深度图
  • 权益曲线 + 回撤曲线
  • 多因子可视化
  • 交易标记 (买卖点)
  • 实时WebSocket推送图表数据

支持输出: matplotlib静态图 / HTML交互图 / JSON数据供前端渲染
"""

import json
from typing import Dict, List, Optional
from collections import deque
import logging

from ..core.data import Bar, Tick, OrderBook, OrderData

logger = logging.getLogger("xtquant.chart")


class ChartData:
    """图表数据容器 - 供前端或matplotlib使用"""

    def __init__(self, max_points: int = 500):
        self.max_points = max_points
        self.klines: deque = deque(maxlen=max_points)      # K线数据
        self.ticks: deque = deque(maxlen=max_points)       # Tick数据
        self.depth: Optional[dict] = None                   # 最新深度
        self.trades: deque = deque(maxlen=100)             # 成交标记
        self.equity: deque = deque(maxlen=max_points)      # 权益曲线
        self.drawdown: deque = deque(maxlen=max_points)    # 回撤曲线
        self.factors: Dict[str, deque] = {}                # 因子曲线
        self.indicators: Dict[str, deque] = {}             # 技术指标

    def add_kline(self, bar: Bar):
        self.klines.append({
            "time": bar.timestamp,
            "open": bar.open,
            "high": bar.high,
            "low": bar.low,
            "close": bar.close,
            "volume": bar.volume
        })

    def add_tick(self, tick: Tick):
        self.ticks.append({
            "time": tick.timestamp,
            "price": tick.price,
            "volume": tick.volume
        })

    def add_depth(self, book: OrderBook):
        self.depth = {
            "symbol": book.symbol,
            "timestamp": book.timestamp,
            "bids": book.bids[:10],   # 只取前10档
            "asks": book.asks[:10],
            "mid": book.mid_price(),
            "spread": book.spread(),
            "imbalance": book.imbalance()
        }

    def add_trade_marker(self, order: OrderData, marker_type: str = "buy"):
        """添加交易标记"""
        self.trades.append({
            "time": order.timestamp,
            "price": order.price,
            "qty": order.quantity,
            "side": order.side,
            "type": marker_type,
            "order_id": order.order_id
        })

    def add_equity(self, timestamp: int, value: float):
        self.equity.append({"time": timestamp, "value": value})

    def add_drawdown(self, timestamp: int, value: float):
        self.drawdown.append({"time": timestamp, "value": value})

    def add_factor(self, name: str, timestamp: int, value: float):
        if name not in self.factors:
            self.factors[name] = deque(maxlen=self.max_points)
        self.factors[name].append({"time": timestamp, "value": value})

    def to_dict(self) -> dict:
        return {
            "klines": list(self.klines),
            "ticks": list(self.ticks),
            "depth": self.depth,
            "trades": list(self.trades),
            "equity": list(self.equity),
            "drawdown": list(self.drawdown),
            "factors": {k: list(v) for k, v in self.factors.items()}
        }

    def to_json(self) -> str:
        return json.dumps(self.to_dict())


class ChartPlotter:
    """
    图表绘制器
    生成matplotlib图表或HTML图表
    """

    def __init__(self, style: str = "dark"):
        self.style = style
        self._chart_data = ChartData()

    def update_from_event(self, event_type: str, data: dict):
        """从事件更新图表数据"""
        if event_type == "bar":
            bar = Bar("", data["symbol"], data["interval"], data["open"], data["high"],
                     data["low"], data["close"], data["volume"], timestamp=data.get("timestamp", 0))
            self._chart_data.add_kline(bar)
        elif event_type == "tick":
            tick = Tick("", data["symbol"], data["price"], data["volume"], timestamp=data.get("timestamp", 0))
            self._chart_data.add_tick(tick)
        elif event_type == "orderbook":
            book = OrderBook(data["symbol"], "", data["bids"], data["asks"], timestamp=data.get("timestamp", 0))
            self._chart_data.add_depth(book)
        elif event_type == "equity":
            self._chart_data.add_equity(data["timestamp"], data["value"])
        elif event_type == "factor":
            self._chart_data.add_factor(data["name"], data["timestamp"], data["value"])

    def plot_kline(self, title: str = "K线图", save_path: str = None):
        """绘制K线图 (matplotlib)"""
        try:
            import matplotlib.pyplot as plt
            import matplotlib.dates as mdates
            from datetime import datetime

            klines = list(self._chart_data.klines)
            if not klines:
                logger.warning("[Chart] 无K线数据")
                return

            fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(14, 8), 
                                           gridspec_kw={'height_ratios': [3, 1]},
                                           facecolor='#0d1117' if self.style == "dark" else 'white')
            fig.suptitle(title, color='white' if self.style == "dark" else 'black', fontsize=14)

            times = [datetime.fromtimestamp(k["time"]/1000) for k in klines]
            opens = [k["open"] for k in klines]
            highs = [k["high"] for k in klines]
            lows = [k["low"] for k in klines]
            closes = [k["close"] for k in klines]
            volumes = [k["volume"] for k in klines]

            # 绘制K线 (简化版)
            for i, (t, o, h, l, c) in enumerate(zip(times, opens, highs, lows, closes)):
                color = '#3fb950' if c >= o else '#f85149'
                ax1.plot([t, t], [l, h], color=color, linewidth=1)
                ax1.plot([t, t], [o, c], color=color, linewidth=3)

            # 成交量
            colors = ['#3fb950' if closes[i] >= opens[i] else '#f85149' for i in range(len(closes))]
            ax2.bar(times, volumes, color=colors, alpha=0.7)

            # 样式
            for ax in [ax1, ax2]:
                ax.set_facecolor('#0d1117' if self.style == "dark" else 'white')
                ax.tick_params(colors='white' if self.style == "dark" else 'black')
                ax.xaxis.label.set_color('white' if self.style == "dark" else 'black')
                ax.yaxis.label.set_color('white' if self.style == "dark" else 'black')

            ax1.set_ylabel("Price")
            ax2.set_ylabel("Volume")
            ax1.grid(True, alpha=0.2, color='gray')

            # 交易标记
            for trade in self._chart_data.trades:
                t = datetime.fromtimestamp(trade["time"]/1000)
                color = '#58a6ff' if trade["side"] == "BUY" else '#d29922'
                ax1.axvline(t, color=color, alpha=0.3, linestyle='--')

            plt.tight_layout()
            if save_path:
                plt.savefig(save_path, dpi=150, bbox_inches='tight', 
                           facecolor='#0d1117' if self.style == "dark" else 'white')
                logger.info(f"[Chart] K线图已保存: {save_path}")
            else:
                plt.show()
            plt.close()

        except ImportError:
            logger.error("[Chart] 请安装matplotlib: pip install matplotlib")

    def plot_equity(self, title: str = "权益曲线", save_path: str = None):
        """绘制权益曲线"""
        try:
            import matplotlib.pyplot as plt
            from datetime import datetime

            equity = list(self._chart_data.equity)
            if not equity:
                logger.warning("[Chart] 无权益数据")
                return

            fig, ax = plt.subplots(figsize=(12, 6), facecolor='#0d1117' if self.style == "dark" else 'white')
            fig.suptitle(title, color='white' if self.style == "dark" else 'black')

            times = [datetime.fromtimestamp(e["time"]/1000) for e in equity]
            values = [e["value"] for e in equity]

            ax.plot(times, values, color='#58a6ff', linewidth=1.5)
            ax.fill_between(times, values, alpha=0.2, color='#58a6ff')

            ax.set_facecolor('#0d1117' if self.style == "dark" else 'white')
            ax.tick_params(colors='white' if self.style == "dark" else 'black')
            ax.grid(True, alpha=0.2, color='gray')
            ax.set_ylabel("Equity (USDT)")

            plt.tight_layout()
            if save_path:
                plt.savefig(save_path, dpi=150, bbox_inches='tight',
                           facecolor='#0d1117' if self.style == "dark" else 'white')
            else:
                plt.show()
            plt.close()

        except ImportError:
            logger.error("[Chart] 请安装matplotlib")

    def plot_multi_factor(self, factor_names: List[str], save_path: str = None):
        """绘制多因子对比图"""
        try:
            import matplotlib.pyplot as plt
            from datetime import datetime

            fig, axes = plt.subplots(len(factor_names), 1, figsize=(12, 3*len(factor_names)),
                                    facecolor='#0d1117' if self.style == "dark" else 'white')
            if len(factor_names) == 1:
                axes = [axes]

            for idx, name in enumerate(factor_names):
                data = list(self._chart_data.factors.get(name, []))
                if not data:
                    continue
                times = [datetime.fromtimestamp(d["time"]/1000) for d in data]
                values = [d["value"] for d in data]
                axes[idx].plot(times, values, color='#58a6ff', linewidth=1)
                axes[idx].set_title(name, color='white' if self.style == "dark" else 'black')
                axes[idx].set_facecolor('#0d1117' if self.style == "dark" else 'white')
                axes[idx].tick_params(colors='white' if self.style == "dark" else 'black')
                axes[idx].grid(True, alpha=0.2, color='gray')

            plt.tight_layout()
            if save_path:
                plt.savefig(save_path, dpi=150, bbox_inches='tight')
            else:
                plt.show()
            plt.close()

        except ImportError:
            logger.error("[Chart] 请安装matplotlib")

    def generate_html(self, title: str = "小天量化 - 交易监控") -> str:
        """生成完整的HTML交互图表 (使用TradingView风格)"""
        data = self._chart_data.to_dict()

        html = f"""
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <title>{title}</title>
    <script src="https://unpkg.com/lightweight-charts@4.1.0/dist/lightweight-charts.standalone.production.js"></script>
    <style>
        body {{ margin: 0; padding: 0; background: #0d1117; color: #c9d1d9; font-family: -apple-system, sans-serif; }}
        .header {{ padding: 15px 20px; background: #161b22; border-bottom: 1px solid #30363d; }}
        .header h1 {{ margin: 0; font-size: 20px; color: #58a6ff; }}
        .grid {{ display: grid; grid-template-columns: 2fr 1fr; gap: 10px; padding: 10px; }}
        .panel {{ background: #161b22; border-radius: 8px; border: 1px solid #30363d; padding: 10px; }}
        .panel-title {{ font-size: 12px; color: #8b949e; margin-bottom: 8px; text-transform: uppercase; }}
        #chart {{ height: 400px; }}
        #equity-chart {{ height: 200px; }}
        #depth-chart {{ height: 200px; }}
        .stats {{ display: grid; grid-template-columns: repeat(3, 1fr); gap: 8px; }}
        .stat {{ background: #0d1117; padding: 10px; border-radius: 6px; }}
        .stat-label {{ font-size: 11px; color: #8b949e; }}
        .stat-value {{ font-size: 16px; font-weight: 600; margin-top: 4px; }}
        .positive {{ color: #3fb950; }}
        .negative {{ color: #f85149; }}
    </style>
</head>
<body>
    <div class="header">
        <h1>🚀 小天量化交易 - 实时监控</h1>
    </div>
    <div class="grid">
        <div>
            <div class="panel">
                <div class="panel-title">K线图</div>
                <div id="chart"></div>
            </div>
            <div class="panel" style="margin-top:10px">
                <div class="panel-title">权益曲线</div>
                <div id="equity-chart"></div>
            </div>
        </div>
        <div>
            <div class="panel">
                <div class="panel-title">实时统计</div>
                <div class="stats">
                    <div class="stat">
                        <div class="stat-label">最新价</div>
                        <div class="stat-value" id="last-price">--</div>
                    </div>
                    <div class="stat">
                        <div class="stat-label">24h涨跌</div>
                        <div class="stat-value positive" id="change">+0.00%</div>
                    </div>
                    <div class="stat">
                        <div class="stat-label">权益</div>
                        <div class="stat-value" id="equity">--</div>
                    </div>
                </div>
            </div>
            <div class="panel" style="margin-top:10px">
                <div class="panel-title">订单簿深度</div>
                <div id="depth-chart"></div>
            </div>
        </div>
    </div>
    <script>
        // K线图
        const chart = LightweightCharts.createChart(document.getElementById('chart'), {{
            layout: {{ background: {{ color: '#161b22' }}, textColor: '#c9d1d9' }},
            grid: {{ vertLines: {{ color: '#30363d' }}, horzLines: {{ color: '#30363d' }} }},
            crosshair: {{ mode: LightweightCharts.CrosshairMode.Normal }},
            rightPriceScale: {{ borderColor: '#30363d' }},
            timeScale: {{ borderColor: '#30363d', timeVisible: true }},
        }});
        const candleSeries = chart.addCandlestickSeries({{
            upColor: '#3fb950', downColor: '#f85149',
            borderUpColor: '#3fb950', borderDownColor: '#f85149',
            wickUpColor: '#3fb950', wickDownColor: '#f85149'
        }});

        const klines = {json.dumps(data.get('klines', []))};
        candleSeries.setData(klines.map(k => ({{
            time: k.time / 1000,
            open: k.open, high: k.high, low: k.low, close: k.close
        }})));

        // 权益曲线
        const equityChart = LightweightCharts.createChart(document.getElementById('equity-chart'), {{
            layout: {{ background: {{ color: '#161b22' }}, textColor: '#c9d1d9' }},
            grid: {{ vertLines: {{ color: '#30363d' }}, horzLines: {{ color: '#30363d' }} }},
            rightPriceScale: {{ borderColor: '#30363d' }},
            timeScale: {{ borderColor: '#30363d', timeVisible: true }},
        }});
        const equitySeries = equityChart.addAreaSeries({{
            topColor: 'rgba(88, 166, 255, 0.4)',
            bottomColor: 'rgba(88, 166, 255, 0.01)',
            lineColor: '#58a6ff',
            lineWidth: 2
        }});

        const equity = {json.dumps(data.get('equity', []))};
        equitySeries.setData(equity.map(e => ({{
            time: e.time / 1000,
            value: e.value
        }})));

        // 深度图
        const depth = {json.dumps(data.get('depth'))};
        if (depth) {{
            const depthContainer = document.getElementById('depth-chart');
            let html = '<div style="font-size:11px">';
            html += '<div style="color:#f85149;margin-bottom:4px">卖盘</div>';
            depth.asks.slice(0, 5).reverse().forEach(a => {{
                html += `<div style="display:flex;justify-content:space-between"><span>${{a[0]}}</span><span>${{a[1]}}</span></div>`;
            }});
            html += `<div style="color:#58a6ff;margin:4px 0;font-weight:bold">中间价: ${{depth.mid.toFixed(2)}}</div>`;
            html += '<div style="color:#3fb950;margin-bottom:4px">买盘</div>';
            depth.bids.slice(0, 5).forEach(b => {{
                html += `<div style="display:flex;justify-content:space-between"><span>${{b[0]}}</span><span>${{b[1]}}</span></div>`;
            }});
            html += '</div>';
            depthContainer.innerHTML = html;
        }}

        // 更新最新价
        if (klines.length > 0) {{
            const last = klines[klines.length - 1];
            document.getElementById('last-price').textContent = last.close.toFixed(2);
            if (equity.length > 0) {{
                document.getElementById('equity').textContent = equity[equity.length-1].value.toFixed(2);
            }}
        }}
    </script>
</body>
</html>
"""
        return html

    def get_chart_data(self) -> ChartData:
        return self._chart_data
