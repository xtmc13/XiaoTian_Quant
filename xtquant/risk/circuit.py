"""
熔断器 — 异常情况自动停止交易
"""

import time
import threading


class CircuitBreaker:
    """
    熔断器

    触发条件:
      - 风控连续拦截
      - 账户权益异常下降
      - 手动触发

    恢复:
      - 手动重置
      - 定时自动恢复 (配置)
    """

    def __init__(self, auto_reset_sec: float = 0):
        self.auto_reset_sec = auto_reset_sec  # 0 = 不自动恢复
        self._is_open: bool = False
        self._reason: str = ""
        self._tripped_at: float = 0
        self._trip_count: int = 0
        self._lock = threading.Lock()

    @property
    def is_open(self) -> bool:
        with self._lock:
            if not self._is_open:
                return False
            if self.auto_reset_sec > 0:
                elapsed = time.time() - self._tripped_at
                if elapsed > self.auto_reset_sec:
                    self._is_open = False
                    self._reason = ""
                    self._tripped_at = 0
                    return False
            return True

    @property
    def reason(self) -> str:
        return self._reason

    @property
    def tripped_at(self) -> float:
        return self._tripped_at

    @property
    def trip_count(self) -> int:
        return self._trip_count

    def trip(self, reason: str):
        with self._lock:
            self._is_open = True
            self._reason = reason
            self._tripped_at = time.time()
            self._trip_count += 1

    def reset(self):
        with self._lock:
            self._is_open = False
            self._reason = ""
            self._tripped_at = 0

    def __repr__(self):
        state = "OPEN" if self._is_open else "CLOSED"
        return f"CircuitBreaker({state} | trips={self._trip_count} | reason='{self._reason}')"
