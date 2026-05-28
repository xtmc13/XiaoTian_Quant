"""
Email & Telegram Notification Service for XiaoTianQuant.
Sends trade alerts, risk warnings, and daily summaries.

Patterns adapted from QuantDinger's email_service.py and signal_notifier.py.
"""

import os
import logging
import asyncio
from typing import Optional, Dict, Any
from email.mime.text import MIMEText
from email.mime.multipart import MIMEMultipart

logger = logging.getLogger("xtquant.notify")

# ── SMTP Email ──

HAS_SMTP = False
try:
    import aiosmtplib
    HAS_SMTP = True
except ImportError:
    logger.info("aiosmtplib not installed — email notifications disabled. pip install aiosmtplib")


async def send_email(
    subject: str,
    body: str,
    to: Optional[str] = None,
    html: bool = False,
) -> bool:
    """Send an email via SMTP."""
    if not HAS_SMTP:
        logger.warning("aiosmtplib not available — cannot send email")
        return False

    smtp_host = os.getenv("SMTP_HOST", "smtp.gmail.com")
    smtp_port = int(os.getenv("SMTP_PORT", "587"))
    smtp_user = os.getenv("SMTP_USER", "")
    smtp_pass = os.getenv("SMTP_PASSWORD", "")
    smtp_from = os.getenv("SMTP_FROM", "XiaoTianQuant <noreply@xtquant.com>")
    to_addr = to or smtp_user

    if not smtp_user or not smtp_pass or not to_addr:
        logger.warning("SMTP credentials not configured")
        return False

    try:
        msg = MIMEMultipart("alternative")
        msg["Subject"] = subject
        msg["From"] = smtp_from
        msg["To"] = to_addr

        if html:
            msg.attach(MIMEText(body, "html", "utf-8"))
        else:
            msg.attach(MIMEText(body, "plain", "utf-8"))

        await aiosmtplib.send(
            msg,
            hostname=smtp_host,
            port=smtp_port,
            username=smtp_user,
            password=smtp_pass,
            start_tls=True,
        )
        logger.info(f"Email sent: {subject}")
        return True
    except Exception as e:
        logger.error(f"Email failed: {e}")
        return False


# ── Telegram ──

async def send_telegram(message: str, parse_mode: str = "HTML") -> bool:
    """Send a Telegram message via bot API."""
    bot_token = os.getenv("TELEGRAM_BOT_TOKEN", "")
    chat_id = os.getenv("TELEGRAM_CHAT_ID", "")

    if not bot_token or not chat_id:
        return False

    try:
        import urllib.request
        import urllib.parse

        url = f"https://api.telegram.org/bot{bot_token}/sendMessage"
        data = urllib.parse.urlencode({
            "chat_id": chat_id,
            "text": message[:4096],
            "parse_mode": parse_mode,
        }).encode()

        await asyncio.to_thread(
            urllib.request.urlopen,
            urllib.request.Request(url, data=data),
        )
        return True
    except Exception as e:
        logger.error(f"Telegram failed: {e}")
        return False


# ── Notification Templates ──

def format_trade_alert(symbol: str, side: str, price: float, qty: float,
                       reason: str = "", pnl: float = None) -> str:
    """Format a trade execution alert."""
    emoji = "🟢" if side.upper() == "BUY" else "🔴"
    lines = [
        f"{emoji} <b>Trade Executed</b>",
        f"<b>Symbol:</b> {symbol}",
        f"<b>Side:</b> {side.upper()}",
        f"<b>Price:</b> {price:.4f}",
        f"<b>Quantity:</b> {qty:.6f}",
    ]
    if reason:
        lines.append(f"<b>Reason:</b> {reason}")
    if pnl is not None:
        sign = "+" if pnl >= 0 else ""
        lines.append(f"<b>PnL:</b> {sign}{pnl:.2f} USDT")
    return "\n".join(lines)


def format_risk_alert(alert_type: str, message: str) -> str:
    """Format a risk management alert."""
    return f"⚠️ <b>Risk Alert: {alert_type}</b>\n{message}"


def format_daily_summary(metrics: Dict[str, Any]) -> str:
    """Format a daily performance summary."""
    lines = [
        "📊 <b>Daily Portfolio Summary</b>",
        f"Equity: ${metrics.get('total_equity', 0):,.2f}",
        f"Daily PnL: {metrics.get('daily_pnl_pct', 0):+.2f}%",
        f"Max Drawdown: {metrics.get('max_drawdown_pct', 0):.1f}%",
        f"Sharpe: {metrics.get('sharpe_ratio', 0):.2f}",
        f"Win Rate: {metrics.get('win_rate_pct', 0):.1f}%",
    ]
    return "\n".join(lines)


# ── Unified Notifier ──

class NotificationManager:
    """Manages notification channels and dispatches messages."""

    def __init__(self):
        self.channels = []

    def use_email(self):
        self.channels.append("email")

    def use_telegram(self):
        self.channels.append("telegram")

    async def notify(self, subject: str, body: str, html: bool = True):
        """Send to all enabled channels."""
        tasks = []
        if "email" in self.channels:
            tasks.append(send_email(subject, body, html=html))
        if "telegram" in self.channels:
            tasks.append(send_telegram(f"<b>{subject}</b>\n\n{body}" if html else f"{subject}\n\n{body}"))
        if tasks:
            await asyncio.gather(*tasks, return_exceptions=True)

    async def notify_trade(self, symbol: str, side: str, price: float, qty: float,
                           reason: str = "", pnl: float = None):
        """Send a trade execution notification."""
        body = format_trade_alert(symbol, side, price, qty, reason, pnl)
        await self.notify(f"Trade: {symbol} {side.upper()}", body)

    async def notify_risk(self, alert_type: str, message: str):
        """Send a risk alert."""
        body = format_risk_alert(alert_type, message)
        await self.notify(f"Risk: {alert_type}", body)

    async def notify_daily_summary(self, metrics: Dict[str, Any]):
        """Send daily performance summary."""
        body = format_daily_summary(metrics)
        await self.notify("Daily Summary", body)


# Global instance
notifier = NotificationManager()
