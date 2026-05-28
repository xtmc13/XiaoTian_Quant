"""
结构化日志 — JSON格式输出，兼容ELK/Loki等日志平台
"""

import logging
import json
import sys
from datetime import datetime, timezone
from typing import Optional


class JsonFormatter(logging.Formatter):
    """JSON格式化器"""

    def __init__(self, service_name: str = "xtquant"):
        super().__init__()
        self.service_name = service_name

    def format(self, record: logging.LogRecord) -> str:
        log_entry = {
            "timestamp": datetime.now(timezone.utc).isoformat(),
            "service": self.service_name,
            "level": record.levelname,
            "module": record.name,
            "message": record.getMessage(),
            "line": record.lineno,
        }
        if record.exc_info and record.exc_info[0]:
            log_entry["exception"] = self.formatException(record.exc_info)
        for key in ("order_id", "symbol", "strategy"):
            val = getattr(record, key, None)
            if val:
                log_entry[key] = val
        return json.dumps(log_entry, ensure_ascii=False, default=str)


class TextFormatter(logging.Formatter):
    """可读文本格式"""

    GREY = "\033[90m"
    BLUE = "\033[94m"
    YELLOW = "\033[93m"
    RED = "\033[91m"
    BOLD_RED = "\033[1;91m"
    RESET = "\033[0m"

    COLORS = {
        "DEBUG": GREY,
        "INFO": BLUE,
        "WARNING": YELLOW,
        "ERROR": RED,
        "CRITICAL": BOLD_RED,
    }

    def format(self, record: logging.LogRecord) -> str:
        color = self.COLORS.get(record.levelname, "")
        ts = datetime.now(timezone.utc).strftime("%H:%M:%S")
        msg = f"{ts} {color}{record.levelname:<8}{self.RESET} | {record.name:<24} | {record.getMessage()}"
        if record.exc_info and record.exc_info[0]:
            msg += "\n" + self.formatException(record.exc_info)
        return msg


def setup_logging(
    level: str = "INFO",
    fmt: str = "json",
    service_name: str = "xtquant",
    log_file: Optional[str] = None,
):
    """
    初始化全局日志配置

    Args:
        level: DEBUG | INFO | WARNING | ERROR
        fmt: json | text
        service_name: 服务名（JSON日志中显示）
        log_file: 可选的文件路径
    """
    root = logging.getLogger()
    root.setLevel(getattr(logging, level.upper(), logging.INFO))
    root.handlers.clear()

    if fmt == "json":
        formatter = JsonFormatter(service_name)
    else:
        formatter = TextFormatter()

    console = logging.StreamHandler(sys.stdout)
    console.setFormatter(formatter)
    root.addHandler(console)

    if log_file:
        from pathlib import Path
        Path(log_file).parent.mkdir(parents=True, exist_ok=True)
        fh = logging.FileHandler(log_file, encoding="utf-8")
        fh.setFormatter(JsonFormatter(service_name) if fmt == "json" else TextFormatter())
        root.addHandler(fh)

    # 降低第三方库的日志噪音
    for lib in ("aiohttp", "websockets", "urllib3", "matplotlib"):
        logging.getLogger(lib).setLevel(logging.WARNING)

    root.info(f"日志系统已初始化 | level={level} | format={fmt}")
    return root


def get_logger(name: str) -> logging.Logger:
    """获取模块日志器"""
    return logging.getLogger(name)
