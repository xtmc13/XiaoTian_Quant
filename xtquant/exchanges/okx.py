"""
小天量化交易 - OKX交易所适配器
V5 API: WebSocket实时行情 + REST交易
"""

import asyncio
import websockets
import json
import time
import hmac
import hashlib
import base64
from typing import Dict, List, Optional
import aiohttp
import logging

from .base import BaseExchange
from ..core.data import Tick, OrderBook, OrderData, Balance, Position

logger = logging.getLogger("xtquant.okx")


class OKXExchange(BaseExchange):
    """
    OKX交易所实现 (V5 API)

    WebSocket端点:
      - 公共: wss://ws.okx.com:8443/ws/v5/public
      - 私有: wss://ws.okx.com:8443/ws/v5/private
      - 模拟盘: wss://wspap.okx.com:8443/ws/v5/public?brokerId=9999
    REST端点:
      - https://www.okx.com
    """

    def __init__(self, api_key: str = "", secret: str = "", passphrase: str = "", 
                 testnet: bool = False):
        super().__init__("OKX", api_key, secret, passphrase, testnet)

        if testnet:
            self.ws_public = "wss://wspap.okx.com:8443/ws/v5/public?brokerId=9999"
            self.ws_private = "wss://wspap.okx.com:8443/ws/v5/private?brokerId=9999"
            self.rest_url = "https://www.okx.com"
        else:
            self.ws_public = "wss://ws.okx.com:8443/ws/v5/public"
            self.ws_private = "wss://ws.okx.com:8443/ws/v5/private"
            self.rest_url = "https://www.okx.com"

        self._session: Optional[aiohttp.ClientSession] = None
        self._private_ws: Optional[websockets.WebSocketClientProtocol] = None

    async def _get_session(self) -> aiohttp.ClientSession:
        if self._session is None or self._session.closed:
            self._session = aiohttp.ClientSession()
        return self._session

    def _generate_signature(self, timestamp: str, method: str, request_path: str, body: str = "") -> str:
        """OKX签名: Base64(HMAC-SHA256(timestamp + method + requestPath + body))"""
        message = timestamp + method.upper() + request_path + body
        mac = hmac.new(self.secret.encode(), message.encode(), hashlib.sha256)
        return base64.b64encode(mac.digest()).decode()

    async def connect_market_data(self, symbols: List[str]):
        """连接OKX WebSocket"""
        self._running = True

        # 公共频道
        channels = []
        for sym in symbols:
            channels.extend([
                {"channel": "tickers", "instId": sym},
                {"channel": "trades", "instId": sym},
                {"channel": "books5", "instId": sym}
            ])

        task = asyncio.create_task(self._ws_public_loop(channels))
        self._ws_tasks.append(task)

        # 私有频道(订单/账户更新)
        if self.api_key:
            task = asyncio.create_task(self._ws_private_loop())
            self._ws_tasks.append(task)

        logger.info(f"[OKX] 行情连接已建立, 订阅 {len(symbols)} 个交易对")

    async def _ws_public_loop(self, channels: List[dict]):
        """公共WebSocket循环"""
        retry = 1
        while self._running:
            try:
                async with websockets.connect(self.ws_public, ping_interval=30, ping_timeout=10) as ws:
                    logger.info("[OKX] 公共WS连接成功")
                    self._connected = True
                    retry = 1

                    await ws.send(json.dumps({"op": "subscribe", "args": channels}))

                    async for msg in ws:
                        if not self._running: break
                        self._last_ws_msg_time = time.time()
                        await self._handle_public_msg(msg)

            except Exception as e:
                self._connected = False
                logger.warning(f"[OKX] 公共WS断开: {e}, {retry}s后重连")
                await asyncio.sleep(min(retry, 60))
                retry *= 2

    async def _ws_private_loop(self):
        """私有WebSocket循环(账户/订单)"""
        retry = 1
        while self._running:
            try:
                async with websockets.connect(self.ws_private, ping_interval=30, ping_timeout=10) as ws:
                    logger.info("[OKX] 私有WS连接成功")

                    # 登录
                    ts = str(int(time.time()))
                    login_msg = {
                        "op": "login",
                        "args": [{
                            "apiKey": self.api_key,
                            "passphrase": self.passphrase,
                            "timestamp": ts,
                            "sign": self._generate_signature(ts, "GET", "/users/self/verify")
                        }]
                    }
                    await ws.send(json.dumps(login_msg))

                    # 订阅私有频道
                    await ws.send(json.dumps({
                        "op": "subscribe",
                        "args": [
                            {"channel": "orders", "instType": "SPOT"},
                            {"channel": "account"}
                        ]
                    }))

                    async for msg in ws:
                        if not self._running: break
                        await self._handle_private_msg(msg)

            except Exception as e:
                logger.warning(f"[OKX] 私有WS断开: {e}, {retry}s后重连")
                await asyncio.sleep(min(retry, 60))
                retry *= 2

    async def _handle_public_msg(self, message: str):
        """处理公共频道消息"""
        try:
            data = json.loads(message)
            if "event" in data:
                if data["event"] == "subscribe":
                    logger.info(f"[OKX] 订阅确认: {data.get('arg', {})}")
                return

            if "data" not in data or "arg" not in data:
                return

            arg = data["arg"]
            channel, inst_id = arg.get("channel", ""), arg.get("instId", "")

            for item in data["data"]:
                if channel == "tickers":
                    tick = Tick(
                        exchange="OKX", symbol=inst_id,
                        price=float(item.get("last", 0)),
                        volume=float(item.get("vol24h", 0)),
                        timestamp=int(item.get("ts", time.time()*1000)),
                        bid=float(item.get("bidPx", 0)),
                        ask=float(item.get("askPx", 0))
                    )
                    self._tick_cache[tick.symbol] = tick
                    self._emit(tick.to_event())

                elif channel == "trades":
                    tick = Tick(
                        exchange="OKX", symbol=inst_id,
                        price=float(item.get("px", 0)),
                        volume=float(item.get("sz", 0)),
                        timestamp=int(item.get("ts", time.time()*1000)),
                        side=item.get("side", ""),
                        trade_id=item.get("tradeId", "")
                    )
                    self._emit(tick.to_event())

                elif channel == "books5":
                    book = OrderBook(
                        symbol=inst_id, exchange="OKX",
                        bids=[[float(p), float(q)] for p, q in item.get("bids", [])],
                        asks=[[float(p), float(q)] for p, q in item.get("asks", [])],
                        timestamp=int(item.get("ts", time.time()*1000)),
                        last_update_id=int(item.get("seqId", 0))
                    )
                    self._book_cache[book.symbol] = book
                    self._emit(book.to_event())

        except Exception as e:
            logger.error(f"[OKX] 公共消息处理错误: {e}")

    async def _handle_private_msg(self, message: str):
        """处理私有频道消息"""
        try:
            data = json.loads(message)
            if "data" not in data:
                return

            arg = data.get("arg", {})
            channel = arg.get("channel", "")

            for item in data["data"]:
                if channel == "orders":
                    order = OrderData(
                        order_id=item["ordId"],
                        exchange="OKX",
                        symbol=item["instId"],
                        side=item["side"].upper(),
                        order_type=item["ordType"].upper(),
                        price=float(item.get("px", 0)),
                        quantity=float(item["sz"]),
                        status=item["state"],
                        filled_qty=float(item.get("accFillSz", 0)),
                        remaining_qty=float(item["sz"]) - float(item.get("accFillSz", 0)),
                        avg_fill_price=float(item.get("avgPx", 0)),
                        fee=float(item.get("fee", 0)),
                        fee_asset=item.get("feeCcy", ""),
                        timestamp=int(item.get("uTime", time.time()*1000)),
                        client_order_id=item.get("clOrdId", "")
                    )
                    self._emit(order.to_event())

                elif channel == "account":
                    for detail in item.get("details", []):
                        balance = Balance(
                            exchange="OKX",
                            asset=detail["ccy"],
                            free=float(detail.get("availBal", 0)),
                            locked=float(detail.get("frozenBal", 0)),
                            timestamp=int(item.get("uTime", time.time()*1000))
                        )
                        self._balance_cache[balance.asset] = balance
                        self._emit(balance.to_event())

        except Exception as e:
            logger.error(f"[OKX] 私有消息处理错误: {e}")

    async def place_order(self, symbol: str, side: str, order_type: str,
                         price: float, quantity: float, client_id: str = "", **kwargs) -> OrderData:
        """下单"""
        session = await self._get_session()

        okx_type = "limit" if order_type.upper() == "LIMIT" else "market"
        okx_side = "buy" if side.upper() == "BUY" else "sell"

        body = {
            "instId": symbol,
            "tdMode": "cash",  # 现货
            "side": okx_side,
            "ordType": okx_type,
            "sz": str(quantity)
        }

        if client_id:
            body["clOrdId"] = client_id

        if order_type.upper() == "LIMIT":
            body["px"] = str(price)

        body_json = json.dumps(body)
        ts = str(int(time.time()))

        headers = {
            "OK-ACCESS-KEY": self.api_key,
            "OK-ACCESS-SIGN": self._generate_signature(ts, "POST", "/api/v5/trade/order", body_json),
            "OK-ACCESS-TIMESTAMP": ts,
            "OK-ACCESS-PASSPHRASE": self.passphrase,
            "Content-Type": "application/json"
        }

        async with session.post(f"{self.rest_url}/api/v5/trade/order", 
                               headers=headers, data=body_json) as resp:
            result = await resp.json()

            if result.get("code") == "0":
                d = result["data"][0]
                order = OrderData(
                    order_id=d["ordId"],
                    exchange="OKX",
                    symbol=symbol,
                    side=side,
                    order_type=order_type,
                    price=price,
                    quantity=quantity,
                    status=d["sCode"],
                    timestamp=int(time.time()*1000),
                    client_order_id=client_id
                )
                self._emit(order.to_event())
                logger.info(f"[OKX] 下单成功: {order.order_id}")
                return order
            else:
                logger.error(f"[OKX] 下单失败: {result}")
                raise Exception(f"OKX下单失败: {result}")

    async def cancel_order(self, order_id: str, symbol: str) -> bool:
        """撤单"""
        session = await self._get_session()
        body = json.dumps({"instId": symbol, "ordId": order_id})
        ts = str(int(time.time()))
        headers = {
            "OK-ACCESS-KEY": self.api_key,
            "OK-ACCESS-SIGN": self._generate_signature(ts, "POST", "/api/v5/trade/cancel-order", body),
            "OK-ACCESS-TIMESTAMP": ts,
            "OK-ACCESS-PASSPHRASE": self.passphrase,
            "Content-Type": "application/json"
        }
        async with session.post(f"{self.rest_url}/api/v5/trade/cancel-order",
                               headers=headers, data=body) as resp:
            result = await resp.json()
            return result.get("code") == "0"

    async def get_balance(self) -> Dict[str, float]:
        """获取余额"""
        session = await self._get_session()
        ts = str(int(time.time()))
        headers = {
            "OK-ACCESS-KEY": self.api_key,
            "OK-ACCESS-SIGN": self._generate_signature(ts, "GET", "/api/v5/account/balance"),
            "OK-ACCESS-TIMESTAMP": ts,
            "OK-ACCESS-PASSPHRASE": self.passphrase
        }
        async with session.get(f"{self.rest_url}/api/v5/account/balance", headers=headers) as resp:
            result = await resp.json()
            balances = {}
            if result.get("code") == "0":
                for detail in result["data"]:
                    for b in detail.get("details", []):
                        ccy = b["ccy"]
                        total = float(b.get("availBal", 0)) + float(b.get("frozenBal", 0))
                        if total > 0:
                            balances[ccy] = total
            return balances

    async def get_position(self, symbol: str) -> Optional[Position]:
        """OKX现货无持仓概念"""
        return None

    async def get_open_orders(self, symbol: str) -> List[OrderData]:
        """获取未成交订单"""
        session = await self._get_session()
        ts = str(int(time.time()))
        headers = {
            "OK-ACCESS-KEY": self.api_key,
            "OK-ACCESS-SIGN": self._generate_signature(ts, "GET", "/api/v5/trade/orders-pending"),
            "OK-ACCESS-TIMESTAMP": ts,
            "OK-ACCESS-PASSPHRASE": self.passphrase
        }
        async with session.get(f"{self.rest_url}/api/v5/trade/orders-pending?instId={symbol}",
                              headers=headers) as resp:
            result = await resp.json()
            orders = []
            if result.get("code") == "0":
                for d in result["data"]:
                    orders.append(OrderData(
                        order_id=d["ordId"], exchange="OKX", symbol=symbol,
                        side=d["side"].upper(), order_type=d["ordType"].upper(),
                        price=float(d.get("px", 0)), quantity=float(d["sz"]),
                        status=d["state"], client_order_id=d.get("clOrdId", "")
                    ))
            return orders

    async def stop(self):
        await super().stop()
        if self._session and not self._session.closed:
            await self._session.close()
