"""
小天量化交易 - 多渠道通知系统
支持: 邮件 / 飞书(Lark) / 企业微信 / 钉钉 / 日志
Qbot风格的多渠道告警
"""

import asyncio
import json
import time
import hmac
import hashlib
import base64
import urllib.parse
import smtplib
import urllib.request
from email.mime.text import MIMEText
from email.mime.multipart import MIMEMultipart
from abc import ABC, abstractmethod
from typing import List, Optional
from dataclasses import dataclass
from datetime import datetime
import logging

logger = logging.getLogger("xtquant.notify")


@dataclass
class NotifyMessage:
    """通知消息"""
    title: str
    content: str
    level: str = "INFO"          # INFO / WARNING / ERROR / CRITICAL
    timestamp: int = 0
    tags: List[str] = None

    def __post_init__(self):
        if self.timestamp == 0:
            self.timestamp = int(time.time() * 1000)
        if self.tags is None:
            self.tags = []

    def to_markdown(self) -> str:
        """转换为Markdown格式"""
        emoji = {"INFO": "ℹ️", "WARNING": "⚠️", "ERROR": "❌", "CRITICAL": "🚨"}.get(self.level, "ℹ️")
        return f"""{emoji} **{self.title}**

{self.content}

---
时间: {datetime.fromtimestamp(self.timestamp/1000).strftime('%Y-%m-%d %H:%M:%S')}
级别: {self.level}
"""


class BaseNotifier(ABC):
    """通知器基类"""

    def __init__(self, name: str):
        self.name = name
        self._enabled = True
        self._rate_limit = 0        # 每秒最大发送数，0表示不限
        self._last_send = 0

    @abstractmethod
    async def send(self, msg: NotifyMessage) -> bool:
        pass

    async def _check_rate_limit(self):
        if self._rate_limit > 0:
            elapsed = time.time() - self._last_send
            if elapsed < 1.0 / self._rate_limit:
                await asyncio.sleep(1.0 / self._rate_limit - elapsed)
        self._last_send = time.time()


class EmailNotifier(BaseNotifier):
    """邮件通知"""

    def __init__(self, smtp_host: str, smtp_port: int, sender: str, 
                 password: str, receivers: List[str], use_ssl: bool = True):
        super().__init__("email")
        self.smtp_host = smtp_host
        self.smtp_port = smtp_port
        self.sender = sender
        self.password = password
        self.receivers = receivers
        self.use_ssl = use_ssl

    async def send(self, msg: NotifyMessage) -> bool:
        try:
            await self._check_rate_limit()

            mime_msg = MIMEMultipart()
            mime_msg["From"] = self.sender
            mime_msg["To"] = ", ".join(self.receivers)
            mime_msg["Subject"] = f"[小天量化-{msg.level}] {msg.title}"

            color = {"INFO": "#58a6ff", "WARNING": "#d29922", 
                    "ERROR": "#f85149", "CRITICAL": "#ff0000"}.get(msg.level, "#58a6ff")

            html = f"""
            <html><body style="font-family:Arial,sans-serif">
            <div style="border-left:4px solid {color};padding-left:12px">
                <h2 style="color:{color}">{msg.title}</h2>
                <pre style="background:#f6f8fa;padding:12px;border-radius:6px">{msg.content}</pre>
                <p style="color:#666;font-size:12px">
                    时间: {datetime.fromtimestamp(msg.timestamp/1000)}<br>
                    级别: <strong>{msg.level}</strong>
                </p>
            </div></body></html>
            """
            mime_msg.attach(MIMEText(html, "html", "utf-8"))

            loop = asyncio.get_event_loop()
            await loop.run_in_executor(None, self._send_sync, mime_msg)
            logger.info(f"[Notify] 邮件已发送: {msg.title}")
            return True

        except Exception as e:
            logger.error(f"[Notify] 邮件发送失败: {e}")
            return False

    def _send_sync(self, mime_msg):
        if self.use_ssl:
            with smtplib.SMTP_SSL(self.smtp_host, self.smtp_port) as server:
                server.login(self.sender, self.password)
                server.sendmail(self.sender, self.receivers, mime_msg.as_string())
        else:
            with smtplib.SMTP(self.smtp_host, self.smtp_port) as server:
                server.starttls()
                server.login(self.sender, self.password)
                server.sendmail(self.sender, self.receivers, mime_msg.as_string())


class LarkNotifier(BaseNotifier):
    """飞书(Lark)机器人通知"""

    def __init__(self, webhook_url: str, secret: str = None):
        super().__init__("lark")
        self.webhook_url = webhook_url
        self.secret = secret

    async def send(self, msg: NotifyMessage) -> bool:
        try:
            await self._check_rate_limit()

            color_map = {"INFO": "blue", "WARNING": "orange", "ERROR": "red", "CRITICAL": "red"}

            payload = {
                "msg_type": "interactive",
                "card": {
                    "header": {
                        "title": {"tag": "plain_text", "content": f"[小天量化] {msg.title}"},
                        "template": color_map.get(msg.level, "blue")
                    },
                    "elements": [
                        {"tag": "div", "text": {"tag": "lark_md", "content": msg.content}},
                        {"tag": "div", "text": {"tag": "lark_md", "content": f"**级别**: {msg.level} | **时间**: {datetime.fromtimestamp(msg.timestamp/1000).strftime('%H:%M:%S')}"}}
                    ]
                }
            }

            if self.secret:
                ts = str(int(time.time()))
                sign_str = f"{ts}\n{self.secret}"
                sign = base64.b64encode(
                    hmac.new(self.secret.encode(), sign_str.encode(), hashlib.sha256).digest()
                ).decode()
                payload["timestamp"] = ts
                payload["sign"] = sign

            data = json.dumps(payload).encode()
            req = urllib.request.Request(
                self.webhook_url, data=data,
                headers={"Content-Type": "application/json"}
            )

            loop = asyncio.get_event_loop()
            await loop.run_in_executor(None, urllib.request.urlopen, req)
            logger.info(f"[Notify] 飞书已发送: {msg.title}")
            return True

        except Exception as e:
            logger.error(f"[Notify] 飞书发送失败: {e}")
            return False


class WechatNotifier(BaseNotifier):
    """企业微信机器人通知"""

    def __init__(self, webhook_url: str):
        super().__init__("wechat")
        self.webhook_url = webhook_url

    async def send(self, msg: NotifyMessage) -> bool:
        try:
            await self._check_rate_limit()

            color_map = {"INFO": "info", "WARNING": "warning", "ERROR": "comment", "CRITICAL": "warning"}

            payload = {
                "msgtype": "markdown",
                "markdown": {
                    "content": f"""<font color="{color_map.get(msg.level, 'info')}">**[小天量化] {msg.title}**</font>
{msg.content}
> 级别: {msg.level} | 时间: {datetime.fromtimestamp(msg.timestamp/1000).strftime('%H:%M:%S')}
"""
                }
            }

            data = json.dumps(payload).encode()
            req = urllib.request.Request(
                self.webhook_url, data=data,
                headers={"Content-Type": "application/json"}
            )

            loop = asyncio.get_event_loop()
            await loop.run_in_executor(None, urllib.request.urlopen, req)
            logger.info(f"[Notify] 微信已发送: {msg.title}")
            return True

        except Exception as e:
            logger.error(f"[Notify] 微信发送失败: {e}")
            return False


class DingTalkNotifier(BaseNotifier):
    """钉钉机器人通知"""

    def __init__(self, webhook_url: str, secret: str = None):
        super().__init__("dingtalk")
        self.webhook_url = webhook_url
        self.secret = secret

    async def send(self, msg: NotifyMessage) -> bool:
        try:
            await self._check_rate_limit()

            payload = {
                "msgtype": "markdown",
                "markdown": {
                    "title": f"[小天量化] {msg.title}",
                    "text": f"#### [小天量化] {msg.title}\n{msg.content}\n\n> 级别: {msg.level} | 时间: {datetime.fromtimestamp(msg.timestamp/1000).strftime('%H:%M:%S')}"
                }
            }

            url = self.webhook_url
            if self.secret:
                ts = str(int(time.time() * 1000))
                sign_str = f"{ts}\n{self.secret}"
                sign = urllib.parse.quote_plus(
                    base64.b64encode(
                        hmac.new(self.secret.encode(), sign_str.encode(), hashlib.sha256).digest()
                    ).decode()
                )
                url = f"{self.webhook_url}&timestamp={ts}&sign={sign}"

            data = json.dumps(payload).encode()
            req = urllib.request.Request(
                url, data=data,
                headers={"Content-Type": "application/json"}
            )

            loop = asyncio.get_event_loop()
            await loop.run_in_executor(None, urllib.request.urlopen, req)
            logger.info(f"[Notify] 钉钉已发送: {msg.title}")
            return True

        except Exception as e:
            logger.error(f"[Notify] 钉钉发送失败: {e}")
            return False


class LogNotifier(BaseNotifier):
    """日志通知 (默认)"""

    def __init__(self):
        super().__init__("log")

    async def send(self, msg: NotifyMessage) -> bool:
        log_func = {
            "INFO": logger.info,
            "WARNING": logger.warning,
            "ERROR": logger.error,
            "CRITICAL": logger.critical
        }.get(msg.level, logger.info)

        log_func(f"[Notify] {msg.title} | {msg.content}")
        return True


class NotificationManager:
    """
    通知管理器
    聚合多个通知渠道，支持消息队列和批量发送
    """

    def __init__(self, default_level: str = "INFO"):
        self.notifiers: List[BaseNotifier] = []
        self._queue: asyncio.Queue = asyncio.Queue()
        self._task: Optional[asyncio.Task] = None
        self._running = False
        self.default_level = default_level
        self._history: List[NotifyMessage] = []
        self._max_history = 1000

    def add_notifier(self, notifier: BaseNotifier):
        self.notifiers.append(notifier)
        logger.info(f"[Notify] 添加通知渠道: {notifier.name}")

    async def notify(self, title: str, content: str, level: str = None, tags: List[str] = None):
        """发送通知"""
        msg = NotifyMessage(title, content, level or self.default_level, tags=tags or [])
        await self._queue.put(msg)

    async def notify_nowait(self, title: str, content: str, level: str = None):
        """非阻塞通知"""
        try:
            msg = NotifyMessage(title, content, level or self.default_level)
            self._queue.put_nowait(msg)
        except asyncio.QueueFull:
            logger.warning("[Notify] 通知队列已满")

    async def _worker(self):
        """通知处理工作线程"""
        while self._running:
            try:
                msg = await asyncio.wait_for(self._queue.get(), timeout=1.0)
                self._history.append(msg)
                if len(self._history) > self._max_history:
                    self._history.pop(0)

                for notifier in self.notifiers:
                    if not notifier._enabled:
                        continue
                    try:
                        await notifier.send(msg)
                    except Exception as e:
                        logger.error(f"[Notify] {notifier.name} 发送失败: {e}")

            except asyncio.TimeoutError:
                continue

    def get_history(self, level: str = None, n: int = 100) -> List[NotifyMessage]:
        """获取通知历史"""
        msgs = self._history
        if level:
            msgs = [m for m in msgs if m.level == level]
        return msgs[-n:]

    async def start(self):
        self._running = True
        self._task = asyncio.create_task(self._worker())
        # 默认添加日志通知
        if not self.notifiers:
            self.add_notifier(LogNotifier())
        logger.info("[Notify] 通知系统已启动")

    async def stop(self):
        self._running = False
        if self._task:
            self._task.cancel()
            try:
                await self._task
            except asyncio.CancelledError:
                pass
        logger.info("[Notify] 通知系统已停止")
