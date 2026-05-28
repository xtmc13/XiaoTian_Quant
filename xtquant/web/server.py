"""
DEPRECATED: Use xtquant.web.app (FastAPI) instead.
This aiohttp-based server is no longer maintained.
Kept for reference only — will be removed in a future version.
"""

import warnings
warnings.warn(
    "xtquant.web.server is deprecated. Use xtquant.web.app (FastAPI) instead.",
    DeprecationWarning,
    stacklevel=2,
)

import asyncio
import json
import time
from typing import List, Optional
import logging

try:
    import aiohttp
    from aiohttp import web
    HAS_AIOHTTP = True
except ImportError:
    HAS_AIOHTTP = False
    logging.warning("[Web] aiohttp未安装，Web服务不可用。运行: pip install aiohttp")

logger = logging.getLogger("xtquant.web")


class WebServer:
    """
    小天量化Web服务

    端点:
      GET  /              -> 监控面板HTML
      GET  /api/status    -> 系统状态JSON
      GET  /api/stats     -> 统计信息
      GET  /api/engine    -> 引擎详情
      GET  /api/exchanges -> 交易所状态
      GET  /api/strategies-> 策略状态
      GET  /api/factors   -> 因子状态
      GET  /api/chart     -> 图表数据
      GET  /api/trades    -> 交易记录
      POST /api/strategy/{name}/start -> 启动策略
      POST /api/strategy/{name}/stop  -> 停止策略
      POST /api/order     -> 手动下单
      WS   /ws            -> WebSocket实时推送
    """

    def __init__(self, engine, host: str = "0.0.0.0", port: int = 8080):
        if not HAS_AIOHTTP:
            raise ImportError("需要安装aiohttp: pip install aiohttp")

        self.engine = engine
        self.host = host
        self.port = port
        self._app = web.Application()
        self._runner: Optional[web.AppRunner] = None
        self._ws_clients: List[web.WebSocketResponse] = []
        self._ws_task: Optional[asyncio.Task] = None
        self._running = False

        self._setup_routes()

    def _setup_routes(self):
        """设置路由"""
        self._app.router.add_get("/", self._index)
        self._app.router.add_get("/api/status", self._api_status)
        self._app.router.add_get("/api/stats", self._api_stats)
        self._app.router.add_get("/api/engine", self._api_engine)
        self._app.router.add_get("/api/exchanges", self._api_exchanges)
        self._app.router.add_get("/api/strategies", self._api_strategies)
        self._app.router.add_get("/api/factors", self._api_factors)
        self._app.router.add_get("/api/chart", self._api_chart)
        self._app.router.add_get("/api/trades", self._api_trades)
        self._app.router.add_post("/api/strategy/{name}/start", self._api_strategy_start)
        self._app.router.add_post("/api/strategy/{name}/stop", self._api_strategy_stop)
        self._app.router.add_post("/api/order", self._api_place_order)
        self._app.router.add_get("/ws", self._ws_handler)

        # 静态文件
        self._app.router.add_static("/static", "xtquant/web/static", show_index=False)

    async def _index(self, request):
        """首页 - 监控面板"""
        html = self._generate_dashboard_html()
        return web.Response(text=html, content_type="text/html")

    def _generate_dashboard_html(self) -> str:
        """生成监控面板HTML"""
        return """
<!DOCTYPE html>
<html>
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>小天量化交易 - 监控面板</title>
    <script src="https://unpkg.com/lightweight-charts@4.1.0/dist/lightweight-charts.standalone.production.js"></script>
    <style>
        * { margin: 0; padding: 0; box-sizing: border-box; }
        body { font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif; background: #0d1117; color: #c9d1d9; }
        .header { background: #161b22; padding: 16px 24px; border-bottom: 1px solid #30363d; display: flex; justify-content: space-between; align-items: center; }
        .header h1 { font-size: 20px; color: #58a6ff; display: flex; align-items: center; gap: 8px; }
        .status-badge { padding: 4px 12px; border-radius: 12px; font-size: 12px; font-weight: 600; }
        .status-running { background: rgba(63, 185, 80, 0.15); color: #3fb950; }
        .status-stopped { background: rgba(248, 81, 73, 0.15); color: #f85149; }
        .container { padding: 16px 24px; display: grid; grid-template-columns: repeat(auto-fit, minmax(300px, 1fr)); gap: 16px; }
        .card { background: #161b22; border: 1px solid #30363d; border-radius: 12px; padding: 16px; }
        .card-header { display: flex; justify-content: space-between; align-items: center; margin-bottom: 12px; }
        .card-title { font-size: 14px; font-weight: 600; color: #8b949e; text-transform: uppercase; letter-spacing: 0.5px; }
        .metric-grid { display: grid; grid-template-columns: repeat(2, 1fr); gap: 12px; }
        .metric { background: #0d1117; padding: 12px; border-radius: 8px; }
        .metric-label { font-size: 11px; color: #8b949e; margin-bottom: 4px; }
        .metric-value { font-size: 18px; font-weight: 700; color: #f0f6fc; }
        .metric-value.positive { color: #3fb950; }
        .metric-value.negative { color: #f85149; }
        .chart-container { height: 300px; margin-top: 8px; }
        .log-container { max-height: 300px; overflow-y: auto; font-family: 'Courier New', monospace; font-size: 12px; }
        .log-entry { padding: 4px 0; border-bottom: 1px solid #21262d; }
        .log-time { color: #8b949e; margin-right: 8px; }
        .log-info { color: #58a6ff; }
        .log-warning { color: #d29922; }
        .log-error { color: #f85149; }
        .btn { padding: 6px 14px; border: none; border-radius: 6px; font-size: 12px; font-weight: 600; cursor: pointer; transition: opacity 0.2s; }
        .btn:hover { opacity: 0.8; }
        .btn-primary { background: #238636; color: white; }
        .btn-danger { background: #da3633; color: white; }
        .btn-secondary { background: #1f6feb; color: white; }
        .strategy-row { display: flex; justify-content: space-between; align-items: center; padding: 8px 0; border-bottom: 1px solid #21262d; }
        .strategy-name { font-weight: 600; }
        .strategy-status { font-size: 11px; padding: 2px 8px; border-radius: 4px; }
        .strategy-running { background: rgba(63, 185, 80, 0.15); color: #3fb950; }
        .strategy-stopped { background: rgba(139, 148, 158, 0.15); color: #8b949e; }
        .tabs { display: flex; gap: 4px; margin-bottom: 12px; }
        .tab { padding: 6px 16px; border-radius: 6px; cursor: pointer; font-size: 13px; transition: all 0.2s; }
        .tab:hover { background: #21262d; }
        .tab.active { background: #1f6feb; color: white; }
        .tab-content { display: none; }
        .tab-content.active { display: block; }
        #ws-status { font-size: 11px; padding: 2px 8px; border-radius: 4px; background: rgba(139, 148, 158, 0.15); color: #8b949e; }
        #ws-status.connected { background: rgba(63, 185, 80, 0.15); color: #3fb950; }
    </style>
</head>
<body>
    <div class="header">
        <h1>🚀 小天量化交易</h1>
        <div style="display:flex;gap:8px;align-items:center">
            <span id="ws-status">WS: 未连接</span>
            <span class="status-badge status-running" id="system-status">运行中</span>
        </div>
    </div>

    <div class="container">
        <!-- 概览卡片 -->
        <div class="card">
            <div class="card-header">
                <span class="card-title">📊 系统概览</span>
                <span style="font-size:11px;color:#8b949e" id="uptime">运行时间: --</span>
            </div>
            <div class="metric-grid">
                <div class="metric">
                    <div class="metric-label">交易所</div>
                    <div class="metric-value" id="exchange-count">--</div>
                </div>
                <div class="metric">
                    <div class="metric-label">策略</div>
                    <div class="metric-value" id="strategy-count">--</div>
                </div>
                <div class="metric">
                    <div class="metric-label">总订单</div>
                    <div class="metric-value" id="total-orders">--</div>
                </div>
                <div class="metric">
                    <div class="metric-label">成交率</div>
                    <div class="metric-value" id="fill-rate">--</div>
                </div>
            </div>
        </div>

        <!-- 价格卡片 -->
        <div class="card">
            <div class="card-header">
                <span class="card-title">💰 实时行情</span>
            </div>
            <div class="metric-grid" id="price-grid">
                <div class="metric">
                    <div class="metric-label">BTC/USDT</div>
                    <div class="metric-value" id="price-btc">--</div>
                </div>
                <div class="metric">
                    <div class="metric-label">ETH/USDT</div>
                    <div class="metric-value" id="price-eth">--</div>
                </div>
            </div>
        </div>

        <!-- 策略管理 -->
        <div class="card" style="grid-column: span 2;">
            <div class="card-header">
                <span class="card-title">🎯 策略管理</span>
                <button class="btn btn-secondary" onclick="refreshStrategies()">刷新</button>
            </div>
            <div id="strategy-list"></div>
        </div>

        <!-- K线图 -->
        <div class="card" style="grid-column: span 2;">
            <div class="card-header">
                <span class="card-title">📈 K线图表</span>
                <div class="tabs">
                    <div class="tab active" onclick="switchTab('kline')">K线</div>
                    <div class="tab" onclick="switchTab('equity')">权益</div>
                    <div class="tab" onclick="switchTab('depth')">深度</div>
                </div>
            </div>
            <div id="tab-kline" class="tab-content active">
                <div class="chart-container" id="kline-chart"></div>
            </div>
            <div id="tab-equity" class="tab-content">
                <div class="chart-container" id="equity-chart"></div>
            </div>
            <div id="tab-depth" class="tab-content">
                <div class="chart-container" id="depth-chart" style="display:flex;gap:20px">
                    <div style="flex:1" id="bids-list"></div>
                    <div style="flex:1" id="asks-list"></div>
                </div>
            </div>
        </div>

        <!-- 日志 -->
        <div class="card" style="grid-column: span 2;">
            <div class="card-header">
                <span class="card-title">📝 实时日志</span>
                <button class="btn btn-secondary" onclick="clearLogs()">清空</button>
            </div>
            <div class="log-container" id="log-container"></div>
        </div>
    </div>

    <script>
        // WebSocket连接
        let ws = null;
        let reconnectTimer = null;

        function connectWS() {
            const protocol = window.location.protocol === 'https:' ? 'wss:' : 'ws:';
            ws = new WebSocket(`${protocol}//${window.location.host}/ws`);

            ws.onopen = () => {
                document.getElementById('ws-status').textContent = 'WS: 已连接';
                document.getElementById('ws-status').classList.add('connected');
                addLog('WebSocket已连接', 'info');
            };

            ws.onmessage = (event) => {
                const data = JSON.parse(event.data);
                handleWSMessage(data);
            };

            ws.onclose = () => {
                document.getElementById('ws-status').textContent = 'WS: 未连接';
                document.getElementById('ws-status').classList.remove('connected');
                addLog('WebSocket断开，3秒后重连...', 'warning');
                reconnectTimer = setTimeout(connectWS, 3000);
            };

            ws.onerror = (err) => {
                addLog('WebSocket错误: ' + err, 'error');
            };
        }

        function handleWSMessage(data) {
            if (data.type === 'tick') {
                updatePrice(data.symbol, data.price);
            } else if (data.type === 'orderbook') {
                updateDepth(data);
            } else if (data.type === 'log') {
                addLog(data.message, data.level);
            } else if (data.type === 'status') {
                updateStatus(data);
            }
        }

        function updatePrice(symbol, price) {
            const id = 'price-' + symbol.toLowerCase().replace('usdt', '').replace('/', '');
            const el = document.getElementById(id);
            if (el) {
                const old = parseFloat(el.textContent) || 0;
                el.textContent = price.toFixed(2);
                el.className = 'metric-value ' + (price >= old ? 'positive' : 'negative');
            }
        }

        function updateDepth(data) {
            const bidsEl = document.getElementById('bids-list');
            const asksEl = document.getElementById('asks-list');
            if (bidsEl && asksEl) {
                bidsEl.innerHTML = '<h4 style="color:#3fb950;margin-bottom:8px">买盘</h4>' + 
                    data.bids.slice(0, 8).map(b => `<div style="display:flex;justify-content:space-between;font-size:12px;padding:2px 0"><span>${parseFloat(b[0]).toFixed(2)}</span><span>${parseFloat(b[1]).toFixed(4)}</span></div>`).join('');
                asksEl.innerHTML = '<h4 style="color:#f85149;margin-bottom:8px">卖盘</h4>' + 
                    data.asks.slice(0, 8).map(a => `<div style="display:flex;justify-content:space-between;font-size:12px;padding:2px 0"><span>${parseFloat(a[0]).toFixed(2)}</span><span>${parseFloat(a[1]).toFixed(4)}</span></div>`).join('');
            }
        }

        function addLog(message, level) {
            const container = document.getElementById('log-container');
            const entry = document.createElement('div');
            entry.className = 'log-entry';
            const time = new Date().toLocaleTimeString();
            entry.innerHTML = `<span class="log-time">${time}</span><span class="log-${level}">${message}</span>`;
            container.insertBefore(entry, container.firstChild);
            if (container.children.length > 100) container.lastChild.remove();
        }

        function clearLogs() {
            document.getElementById('log-container').innerHTML = '';
        }

        function switchTab(name) {
            document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(t => t.classList.remove('active'));
            event.target.classList.add('active');
            document.getElementById('tab-' + name).classList.add('active');
        }

        async function refreshStrategies() {
            try {
                const resp = await fetch('/api/strategies');
                const data = await resp.json();
                renderStrategies(data);
            } catch (e) {
                addLog('获取策略失败: ' + e, 'error');
            }
        }

        function renderStrategies(data) {
            const container = document.getElementById('strategy-list');
            container.innerHTML = Object.entries(data).map(([name, info]) => `
                <div class="strategy-row">
                    <div>
                        <span class="strategy-name">${name}</span>
                        <span class="strategy-status ${info.running ? 'strategy-running' : 'strategy-stopped'}">${info.running ? '运行中' : '已停止'}</span>
                    </div>
                    <div style="display:flex;gap:8px">
                        <button class="btn btn-primary" onclick="controlStrategy('${name}', 'start')" ${info.running ? 'disabled' : ''}>启动</button>
                        <button class="btn btn-danger" onclick="controlStrategy('${name}', 'stop')" ${!info.running ? 'disabled' : ''}>停止</button>
                    </div>
                </div>
            `).join('');
        }

        async function controlStrategy(name, action) {
            try {
                await fetch(`/api/strategy/${name}/${action}`, {method: 'POST'});
                addLog(`策略 ${name} ${action === 'start' ? '启动' : '停止'}`, 'info');
                refreshStrategies();
            } catch (e) {
                addLog('操作失败: ' + e, 'error');
            }
        }

        async function updateStatus(data) {
            document.getElementById('exchange-count').textContent = Object.keys(data.exchanges || {}).length;
            document.getElementById('strategy-count').textContent = Object.keys(data.strategies || {}).length;
            document.getElementById('total-orders').textContent = data.total_orders || 0;
            document.getElementById('fill-rate').textContent = data.fill_rate || '--';
            document.getElementById('uptime').textContent = '运行: ' + formatTime(data.runtime_seconds || 0);
        }

        function formatTime(seconds) {
            const h = Math.floor(seconds / 3600);
            const m = Math.floor((seconds % 3600) / 60);
            const s = seconds % 60;
            return `${h}h ${m}m ${s}s`;
        }

        // 初始化K线图
        function initCharts() {
            const chart = LightweightCharts.createChart(document.getElementById('kline-chart'), {
                layout: { background: { color: '#161b22' }, textColor: '#c9d1d9' },
                grid: { vertLines: { color: '#30363d' }, horzLines: { color: '#30363d' } },
                crosshair: { mode: LightweightCharts.CrosshairMode.Normal },
                rightPriceScale: { borderColor: '#30363d' },
                timeScale: { borderColor: '#30363d', timeVisible: true },
            });
            const candleSeries = chart.addCandlestickSeries({
                upColor: '#3fb950', downColor: '#f85149',
                borderUpColor: '#3fb950', borderDownColor: '#f85149',
                wickUpColor: '#3fb950', wickDownColor: '#f85149'
            });
            window.klineSeries = candleSeries;

            const equityChart = LightweightCharts.createChart(document.getElementById('equity-chart'), {
                layout: { background: { color: '#161b22' }, textColor: '#c9d1d9' },
                grid: { vertLines: { color: '#30363d' }, horzLines: { color: '#30363d' } },
                rightPriceScale: { borderColor: '#30363d' },
                timeScale: { borderColor: '#30363d' },
            });
            const equitySeries = equityChart.addAreaSeries({
                topColor: 'rgba(88, 166, 255, 0.4)',
                bottomColor: 'rgba(88, 166, 255, 0.01)',
                lineColor: '#58a6ff',
                lineWidth: 2
            });
            window.equitySeries = equitySeries;
        }

        // 初始化
        connectWS();
        initCharts();
        refreshStrategies();

        // 定时刷新状态
        setInterval(async () => {
            try {
                const resp = await fetch('/api/status');
                const data = await resp.json();
                updateStatus(data);
            } catch (e) {}
        }, 5000);
    </script>
</body>
</html>
"""

    async def _api_status(self, request):
        """系统状态API"""
        stats = self.engine.get_stats()
        return web.json_response(stats)

    async def _api_stats(self, request):
        """统计信息API"""
        return web.json_response({
            "timestamp": int(time.time() * 1000),
            "engine": self.engine.get_stats(),
            "exchanges": {name: ex.get_stats() for name, ex in self.engine.exchanges.items()},
            "strategies": {name: st.get_stats() for name, st in self.engine.strategies.items()},
        })

    async def _api_engine(self, request):
        """引擎详情"""
        return web.json_response(self.engine.get_stats())

    async def _api_exchanges(self, request):
        """交易所状态"""
        return web.json_response({
            name: ex.get_stats() for name, ex in self.engine.exchanges.items()
        })

    async def _api_strategies(self, request):
        """策略状态"""
        return web.json_response({
            name: st.get_stats() for name, st in self.engine.strategies.items()
        })

    async def _api_factors(self, request):
        """因子状态"""
        if self.engine.factor_pipeline:
            return web.json_response(self.engine.factor_pipeline.get_all_stats())
        return web.json_response({})

    async def _api_chart(self, request):
        """图表数据"""
        symbol = request.query.get("symbol", "BTCUSDT")
        # 这里可以从chart模块获取数据
        return web.json_response({"symbol": symbol, "data": []})

    async def _api_trades(self, request):
        """交易记录"""
        # 从回测引擎或交易历史获取
        return web.json_response([])

    async def _api_strategy_start(self, request):
        """启动策略"""
        name = request.match_info.get("name")
        strategy = self.engine.strategies.get(name)
        if strategy:
            await strategy.start()
            return web.json_response({"success": True, "message": f"策略 {name} 已启动"})
        return web.json_response({"success": False, "message": "策略不存在"}, status=404)

    async def _api_strategy_stop(self, request):
        """停止策略"""
        name = request.match_info.get("name")
        strategy = self.engine.strategies.get(name)
        if strategy:
            await strategy.stop()
            return web.json_response({"success": True, "message": f"策略 {name} 已停止"})
        return web.json_response({"success": False, "message": "策略不存在"}, status=404)

    async def _api_place_order(self, request):
        """手动下单"""
        try:
            data = await request.json()
            order = await self.engine.place_order(
                data.get("exchange", "BINANCE"),
                data["symbol"],
                data["side"],
                data.get("type", "LIMIT"),
                float(data.get("price", 0)),
                float(data["quantity"])
            )
            if order:
                return web.json_response({"success": True, "order": {
                    "order_id": order.order_id,
                    "symbol": order.symbol,
                    "side": order.side,
                    "price": order.price,
                    "quantity": order.quantity,
                    "status": order.status,
                }})
            return web.json_response({"success": False, "message": "下单被风控拦截"})
        except Exception as e:
            return web.json_response({"success": False, "message": str(e)}, status=400)

    async def _ws_handler(self, request):
        """WebSocket处理器"""
        ws = web.WebSocketResponse()
        await ws.prepare(request)
        self._ws_clients.append(ws)
        logger.info(f"[Web] WebSocket客户端连接，当前 {len(self._ws_clients)} 个")

        try:
            async for msg in ws:
                if msg.type == aiohttp.WSMsgType.TEXT:
                    # 处理客户端消息
                    pass
                elif msg.type == aiohttp.WSMsgType.ERROR:
                    logger.error(f"[Web] WS错误: {ws.exception()}")
        finally:
            self._ws_clients.remove(ws)
            logger.info(f"[Web] WebSocket客户端断开，剩余 {len(self._ws_clients)} 个")

        return ws

    async def broadcast(self, data: dict):
        """广播消息到所有WebSocket客户端"""
        if not self._ws_clients:
            return

        msg = json.dumps(data)
        dead_clients = []
        for ws in self._ws_clients:
            try:
                await ws.send_str(msg)
            except Exception:
                dead_clients.append(ws)

        for ws in dead_clients:
            self._ws_clients.remove(ws)

    async def _broadcast_loop(self):
        """定时广播状态"""
        while self._running:
            try:
                if self._ws_clients:
                    await self.broadcast({
                        "type": "status",
                        **self.engine.get_stats()
                    })
                await asyncio.sleep(5)
            except Exception as e:
                logger.error(f"[Web] 广播异常: {e}")

    async def start(self):
        """启动Web服务"""
        self._running = True
        self._runner = web.AppRunner(self._app)
        await self._runner.setup()
        site = web.TCPSite(self._runner, self.host, self.port)
        await site.start()

        # 启动广播任务
        self._ws_task = asyncio.create_task(self._broadcast_loop())

        logger.info(f"[Web] HTTP服务已启动: http://{self.host}:{self.port}")

    async def stop(self):
        """停止Web服务"""
        self._running = False
        if self._ws_task:
            self._ws_task.cancel()

        # 关闭所有WebSocket连接
        for ws in self._ws_clients:
            await ws.close()
        self._ws_clients.clear()

        if self._runner:
            await self._runner.cleanup()
        logger.info("[Web] HTTP服务已停止")
