"""
小天量化交易 - 币安交易所适配器
WebSocket实时行情 + REST交易
支持: 现货 / U本位合约
"""

import asyncio
import websockets
import json
import time
import hmac
import hashlib
from typing import Dict, List, Optional
import aiohttp
import logging

from .base import BaseExchange
from ..core.data import Tick, OrderBook, OrderData, Position

logger = logging.getLogger("xtquant.binance")


class BinanceExchange(BaseExchange):
    """
    币安交易所实现

    WebSocket端点:
      - 现货: wss://stream.binance.com:9443/ws
      - 合约: wss://fstream.binance.com/ws
    REST端点:
      - 现货: https://api.binance.com
      - 合约: https://fapi.binance.com
    """

    def __init__(self, api_key: str = "", secret: str = "", testnet: bool = False, 
                 futures: bool = False):
        super().__init__("BINANCE", api_key, secret, testnet=testnet)
        self.futures = futures

        if futures:
            self.ws_url = "wss://fstream.binance.com/ws"
            self.rest_url = "https://fapi.binance.com"
        else:
            self.ws_url = "wss://stream.binance.com:9443/ws" if not testnet else "wss://testnet.binance.vision/ws"
            self.rest_url = "https://api.binance.com" if not testnet else "https://testnet.binance.vision"

        self._session: Optional[aiohttp.ClientSession] = None
        self._listen_key: Optional[str] = None
        self._listen_key_task: Optional[asyncio.Task] = None

    async def _get_session(self) -> aiohttp.ClientSession:
        if self._session is None or self._session.closed:
            self._session = aiohttp.ClientSession()
        return self._session

    def _generate_signature(self, query_string: str) -> str:
        return hmac.new(
            self.secret.encode('utf-8'),
            query_string.encode('utf-8'),
            hashlib.sha256
        ).hexdigest()

    async def connect_market_data(self, symbols: List[str]):
        """
        连接币安WebSocket行情
        使用多路复用: /stream?streams=stream1/stream2/...
        """
        self._running = True

        streams = []
        for sym in symbols:
            sym_lower = sym.lower().replace("-", "")
            streams.extend([
                f"{sym_lower}@ticker",
                f"{sym_lower}@trade",
                f"{sym_lower}@depth5@100ms"
            ])

        stream_path = "/".join(streams)
        # Binance combined stream is at /stream, not /ws/stream
        base = self.ws_url.rstrip("/").replace("/ws", "")
        endpoint = f"{base}/stream?streams={stream_path}"

        task = asyncio.create_task(self._ws_loop(endpoint))
        self._ws_tasks.append(task)

        # 连接用户数据流(需要API Key)
        if self.api_key:
            try:
                await self._connect_user_data_stream()
            except Exception as e:
                logger.warning(f"[Binance] user data stream unavailable: {e}")

        logger.info(f"[Binance] 行情连接已建立, 订阅 {len(symbols)} 个交易对, {len(streams)} 个数据流")

    async def _ws_loop(self, endpoint: str):
        """WebSocket连接管理: 自动重连 + 心跳"""
        retry_delay = 1
        max_retry = 60

        while self._running:
            try:
                async with websockets.connect(endpoint, ping_interval=20, ping_timeout=10) as ws:
                    logger.info("[Binance] WebSocket连接成功")
                    self._connected = True
                    retry_delay = 1

                    async for message in ws:
                        if not self._running:
                            break
                        self._last_ws_msg_time = time.time()
                        await self._handle_message(message)

            except websockets.exceptions.ConnectionClosed as e:
                self._connected = False
                logger.warning(f"[Binance] WS断开: {e}, {retry_delay}s后重连...")
            except Exception as e:
                self._connected = False
                logger.error(f"[Binance] WS错误: {e}, {retry_delay}s后重连...")

            self._ws_reconnect_count += 1
            if self._running:
                await asyncio.sleep(min(retry_delay, max_retry))
                retry_delay *= 2

    _msg_count: dict = {"ticker": 0, "trade": 0, "depth": 0, "other": 0, "raw": 0}

    async def _handle_message(self, message: str):
        """处理WebSocket消息"""
        try:
            data = json.loads(message)

            # Multi-stream format
            if "stream" in data:
                stream = data["stream"]
                payload = data["data"]
                if isinstance(payload, str):
                    payload = json.loads(payload)

                if "@ticker" in stream:
                    self._msg_count["ticker"] += 1
                    await self._parse_ticker(payload)
                elif "@trade" in stream:
                    self._msg_count["trade"] += 1
                    await self._parse_trade(payload)
                elif "@depth" in stream:
                    self._msg_count["depth"] += 1
                    await self._parse_depth(payload, stream)
                elif "@account" in stream or "@executionReport" in stream:
                    await self._parse_user_data(payload)
                else:
                    self._msg_count["other"] += 1
            else:
                # Raw stream (user data, non-combined)
                self._msg_count["raw"] += 1
                if "e" in data:
                    etype = data["e"]
                    if etype == "24hrTicker":
                        self._msg_count["ticker"] += 1
                        await self._parse_ticker(data)
                    elif etype == "trade":
                        self._msg_count["trade"] += 1
                        await self._parse_trade(data)
                    elif etype == "depthUpdate":
                        self._msg_count["depth"] += 1
                        await self._parse_depth(data)
                    elif etype in ("executionReport", "outboundAccountPosition"):
                        await self._parse_user_data(data)

        except json.JSONDecodeError:
            logger.error("[Binance] JSON解析失败")
        except Exception as e:
            logger.error(f"[Binance] 消息处理错误: {e}", exc_info=True)

    async def _parse_ticker(self, data: dict):
        """解析24hr Ticker"""
        tick = Tick(
            exchange="BINANCE",
            symbol=data["s"].upper(),
            price=float(data["c"]),
            volume=float(data.get("v", 0)),
            timestamp=data["E"],
            bid=float(data.get("b", 0)),
            ask=float(data.get("a", 0))
        )
        self._tick_cache[tick.symbol] = tick
        self._emit(tick.to_event())

    async def _parse_trade(self, data: dict):
        """解析逐笔成交"""
        tick = Tick(
            exchange="BINANCE",
            symbol=data["s"].upper(),
            price=float(data["p"]),
            volume=float(data["q"]),
            timestamp=data["T"],
            trade_id=str(data["t"]),
            is_buyer_maker=data.get("m", False)
        )
        # Also cache trade price (ticker events may be sparse in combined streams)
        self._tick_cache[tick.symbol] = tick
        self._emit(tick.to_event())

    async def _parse_depth(self, data: dict, stream: str = ""):
        """解析订单簿深度"""
        # Partial book depth (@depth5, @depth10, etc.) doesn't include "s" in payload
        symbol = data.get("s", "").upper()
        if not symbol and stream:
            symbol = stream.split("@")[0].upper()
        bids_key = "bids" if "bids" in data else "b"
        asks_key = "asks" if "asks" in data else "a"
        book = OrderBook(
            symbol=symbol,
            exchange="BINANCE",
            bids=[[float(p), float(q)] for p, q in data.get(bids_key, [])],
            asks=[[float(p), float(q)] for p, q in data.get(asks_key, [])],
            timestamp=data.get("E", int(time.time() * 1000)),
            last_update_id=data.get("u", data.get("lastUpdateId", 0))
        )
        self._book_cache[symbol] = book
        self._emit(book.to_event())

    async def _parse_user_data(self, data: dict):
        """解析用户数据推送"""
        event_type = data.get("e", "")
        if event_type == "executionReport":
            order = OrderData(
                order_id=str(data["i"]),
                exchange="BINANCE",
                symbol=data["s"],
                side=data["S"],
                order_type=data["o"],
                price=float(data["p"]),
                quantity=float(data["q"]),
                status=data["X"],
                filled_qty=float(data.get("z", 0)),
                remaining_qty=float(data.get("q", 0)) - float(data.get("z", 0)),
                timestamp=data["T"]
            )
            self._emit(order.to_event())

    async def _connect_user_data_stream(self):
        """连接用户数据流(订单更新等)"""
        session = await self._get_session()

        # 获取listenKey
        async with session.post(f"{self.rest_url}/api/v3/userDataStream",
                                headers={"X-MBX-APIKEY": self.api_key}) as resp:
            result = await resp.json()
            self._listen_key = result.get("listenKey")

        if self._listen_key:
            # 启动定时续期
            self._listen_key_task = asyncio.create_task(self._keepalive_listen_key())
            # 连接用户数据WS
            user_ws_url = f"{self.ws_url}/{self._listen_key}"
            task = asyncio.create_task(self._ws_loop(user_ws_url))
            self._ws_tasks.append(task)

    async def _keepalive_listen_key(self):
        """每30分钟续期listenKey"""
        while self._running:
            await asyncio.sleep(1800)
            if self._listen_key:
                session = await self._get_session()
                await session.put(f"{self.rest_url}/api/v3/userDataStream",
                                 headers={"X-MBX-APIKEY": self.api_key},
                                 params={"listenKey": self._listen_key})

    async def place_order(self, symbol: str, side: str, order_type: str,
                         price: float, quantity: float, client_id: str = "", **kwargs) -> OrderData:
        """下单"""
        session = await self._get_session()

        params = {
            "symbol": symbol.upper().replace("-", ""),
            "side": side.upper(),
            "type": order_type.upper(),
            "quantity": quantity,
            "timestamp": int(time.time() * 1000)
        }

        if client_id:
            params["newClientOrderId"] = client_id

        if order_type.upper() == "LIMIT":
            params["price"] = price
            params["timeInForce"] = kwargs.get("time_in_force", "GTC")

        # 止盈止损参数
        if "stop_price" in kwargs:
            params["stopPrice"] = kwargs["stop_price"]

        query = "&".join(f"{k}={v}" for k, v in sorted(params.items()))
        params["signature"] = self._generate_signature(query)

        headers = {"X-MBX-APIKEY": self.api_key}

        async with session.post(f"{self.rest_url}/api/v3/order",
                               headers=headers, data=params) as resp:
            result = await resp.json()

            if resp.status == 200:
                order = OrderData(
                    order_id=str(result["orderId"]),
                    exchange="BINANCE",
                    symbol=symbol,
                    side=side,
                    order_type=order_type,
                    price=float(result.get("price", price)),
                    quantity=float(result["origQty"]),
                    status=result["status"],
                    filled_qty=float(result.get("executedQty", 0)),
                    timestamp=result["transactTime"],
                    client_order_id=result.get("clientOrderId", "")
                )
                self._emit(order.to_event())
                logger.info(f"[Binance] 下单成功: {order.order_id}")
                return order
            else:
                logger.error(f"[Binance] 下单失败: {result}")
                raise Exception(f"Binance下单失败: {result}")

    async def cancel_order(self, order_id: str, symbol: str) -> bool:
        """撤单"""
        session = await self._get_session()
        params = {
            "symbol": symbol.upper().replace("-", ""),
            "orderId": order_id,
            "timestamp": int(time.time() * 1000)
        }
        query = "&".join(f"{k}={v}" for k, v in sorted(params.items()))
        params["signature"] = self._generate_signature(query)

        async with session.delete(f"{self.rest_url}/api/v3/order",
                                 headers={"X-MBX-APIKEY": self.api_key}, data=params) as resp:
            return resp.status == 200

    async def get_balance(self) -> Dict[str, float]:
        """获取余额"""
        session = await self._get_session()
        params = {"timestamp": int(time.time() * 1000)}
        query = "&".join(f"{k}={v}" for k, v in sorted(params.items()))
        params["signature"] = self._generate_signature(query)

        async with session.get(f"{self.rest_url}/api/v3/account",
                              headers={"X-MBX-APIKEY": self.api_key}, params=params) as resp:
            result = await resp.json()
            balances = {}
            for b in result.get("balances", []):
                free = float(b["free"])
                locked = float(b["locked"])
                if free > 0 or locked > 0:
                    balances[b["asset"]] = free + locked
            return balances

    async def get_position(self, symbol: str) -> Optional[Position]:
        """币安现货无持仓概念，返回None"""
        return None

    async def get_open_orders(self, symbol: str) -> List[OrderData]:
        """获取未成交订单"""
        session = await self._get_session()
        params = {
            "symbol": symbol.upper().replace("-", ""),
            "timestamp": int(time.time() * 1000)
        }
        query = "&".join(f"{k}={v}" for k, v in sorted(params.items()))
        params["signature"] = self._generate_signature(query)

        async with session.get(f"{self.rest_url}/api/v3/openOrders",
                              headers={"X-MBX-APIKEY": self.api_key}, params=params) as resp:
            result = await resp.json()
            orders = []
            for d in result:
                orders.append(OrderData(
                    order_id=str(d["orderId"]), exchange="BINANCE", symbol=symbol,
                    side=d["side"], order_type=d["type"], price=float(d["price"]),
                    quantity=float(d["origQty"]), status=d["status"],
                    filled_qty=float(d.get("executedQty", 0))
                ))
            return orders

    # ── Kline cache ──
    _klines_cache: dict = {}

    async def get_klines(self, symbol: str, interval: str = "1h", limit: int = 100) -> list:
        """Fetch historical klines from Binance REST API. Results are cached for 5 minutes."""
        from ..core.data import Bar

        sym = symbol.upper().replace("-", "")
        cache_key = f"{sym}:{interval}:{limit}"
        now = time.time()
        cached = self._klines_cache.get(cache_key)
        if cached and (now - cached["ts"]) < 300:
            return cached["data"]

        session = await self._get_session()
        params = {"symbol": sym, "interval": interval, "limit": limit}
        async with session.get(f"{self.rest_url}/api/v3/klines", params=params) as resp:
            if resp.status != 200:
                text = await resp.text()
                logger.error(f"[Binance] get_klines failed: HTTP {resp.status}: {text[:200]}")
                return []
            raw = await resp.json()

        bars = []
        for k in raw:
            bars.append(Bar(
                exchange="BINANCE", symbol=sym, interval=interval,
                open=float(k[1]), high=float(k[2]), low=float(k[3]),
                close=float(k[4]), volume=float(k[5]),
                quote_volume=float(k[7]), trade_count=int(k[8]),
                timestamp=int(k[0]),
            ))
        self._klines_cache[cache_key] = {"ts": now, "data": bars}
        return bars

    async def stop(self):
        await super().stop()
        if self._listen_key_task:
            self._listen_key_task.cancel()
        if self._session and not self._session.closed:
            await self._session.close()
