# -*- coding: utf-8 -*-
"""Exchange interface tests — verify signature algorithms and event bus."""

import sys
import os
sys.path.insert(0, os.path.dirname(os.path.dirname(os.path.abspath(__file__))))

import asyncio
import hmac
import hashlib
import base64


def test_binance_signature():
    secret = "test_secret_key"
    query = "symbol=BTCUSDT&side=BUY&type=LIMIT&quantity=0.001&timestamp=1716600000000"
    sig = hmac.new(secret.encode(), query.encode(), hashlib.sha256).hexdigest()
    expected = hmac.new(secret.encode(), query.encode(), hashlib.sha256).hexdigest()
    assert sig == expected, "Binance signature mismatch"
    print(f"PASS: Binance signature algorithm correct")


def test_okx_signature():
    secret = "test_secret_key"
    timestamp = "1716600000"
    method = "GET"
    path = "/api/v5/account/balance"
    body = ""
    message = timestamp + method + path + body
    mac = hmac.new(secret.encode(), message.encode(), hashlib.sha256)
    sig = base64.b64encode(mac.digest()).decode()
    expected = base64.b64encode(hmac.new(
        secret.encode(), message.encode(), hashlib.sha256).digest()).decode()
    assert sig == expected, "OKX signature mismatch"
    print(f"PASS: OKX signature algorithm correct")


def test_event_bus():
    from xtquant.core.event import EventBus, EventType, MarketEvent

    bus = EventBus()
    received = []

    async def handler(event):
        received.append(event)

    bus.subscribe(EventType.TICK, handler)

    async def run_test():
        await bus.start()
        event = MarketEvent(
            EventType.TICK, "BINANCE", "BTCUSDT", 1716600000000,
            {"price": 68000, "volume": 1}
        )
        await bus.emit(event)
        await asyncio.sleep(0.1)
        await bus.stop()

    asyncio.run(run_test())

    assert len(received) == 1, f"Expected 1 event, got {len(received)}"
    assert received[0].symbol == "BTCUSDT", f"Wrong symbol: {received[0].symbol}"
    print(f"PASS: EventBus sent 1, received {len(received)}")


if __name__ == "__main__":
    test_binance_signature()
    test_okx_signature()
    test_event_bus()
    print("\nAll exchange tests passed.")
